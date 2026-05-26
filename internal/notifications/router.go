// Package notifications — multi-channel routing fanout (co-4).
//
// Router.Send is the single dispatch entry point used by anomaly notifier,
// dashboard subscription test-send, budget alerts, and sync-fail callbacks.
// It looks up enabled delivery_subscription rows for the customer and fans
// out to each channel transport. Always writes one in-app inbox row first so
// the badge updates even if every external transport fails (in-app is the
// source of truth; external channels are best-effort).
//
// Channel mapping (delivery_subscription.channel column):
//
//	slack    -> chatops.Slack    (webhook URL in address)
//	teams    -> chatops.Teams    (webhook URL in address)
//	webhook  -> generic HTTP POST (address = URL)  — handled inline
//	email    -> SMTPSender       (address = recipient email)
//	pagerduty-> PagerDutySender  (address = routing_key)
//
// Failures are collected per-channel and returned as a joined error so callers
// see every transport result. The notification still lands in the inbox.
package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ultraviolet-dev/ultraviolet/internal/chatops"
)

// Payload carries the message body. Title is required; Link + Body optional.
// Severity drives PagerDuty severity + chatops color.
type Payload struct {
	Title    string         `json:"title"`
	Body     string         `json:"body,omitempty"`
	Link     string         `json:"link,omitempty"`
	Severity string         `json:"severity,omitempty"` // info|warn|fatal
	Extra    map[string]any `json:"extra,omitempty"`
}

// Router fans out a Payload to every enabled delivery_subscription channel for
// a customer, plus the in-app inbox. Senders is keyed by chatops.Sender.Kind()
// ("slack"|"teams"|"discord"). Email + PagerDuty live on dedicated fields
// because they take per-row config (recipient / routing_key) rather than a
// fixed webhook URL.
type Router struct {
	pool    *pgxpool.Pool
	inbox   *Inbox
	chatops map[string]chatops.Sender // optional default webhooks (env-derived)
	smtp    *SMTPSender               // nil when SMTP_HOST unset
}

// NewRouter wires the router. smtp may be nil; if so, email subscriptions
// return a real error (no silent skip). chatopsSenders is an optional map of
// pre-configured default webhooks; per-subscription addresses always override.
func NewRouter(pool *pgxpool.Pool, smtp *SMTPSender, chatopsSenders map[string]chatops.Sender) *Router {
	if chatopsSenders == nil {
		chatopsSenders = map[string]chatops.Sender{}
	}
	return &Router{
		pool:    pool,
		inbox:   New(pool),
		chatops: chatopsSenders,
		smtp:    smtp,
	}
}

// Send writes an inbox row, then fans out to every enabled delivery_subscription
// for customerID. kind is one of the Kind* constants and is stored on the inbox
// row + used as the chatops severity hint when payload.Severity is empty.
func (r *Router) Send(ctx context.Context, customerID uuid.UUID, kind Kind, p Payload) error {
	if r == nil || r.pool == nil {
		return errors.New("router: not initialized")
	}
	if p.Title == "" {
		return errors.New("router: payload.Title required")
	}
	// 1. Persist in-app first — source of truth for the badge.
	if err := r.inbox.Push(ctx, Notification{
		CustomerID: customerID,
		Kind:       kind,
		Title:      p.Title,
		Body:       p.Body,
		Link:       p.Link,
	}); err != nil {
		return fmt.Errorf("inbox push: %w", err)
	}

	// 2. Lookup enabled subscriptions.
	rows, err := r.pool.Query(ctx,
		`SELECT id, channel, address FROM delivery_subscription
		 WHERE customer_id = $1 AND enabled = true`, customerID)
	if err != nil {
		return fmt.Errorf("subs lookup: %w", err)
	}
	type sub struct {
		ID      uuid.UUID
		Channel string
		Address string
	}
	var subs []sub
	for rows.Next() {
		var s sub
		if err := rows.Scan(&s.ID, &s.Channel, &s.Address); err != nil {
			rows.Close()
			return fmt.Errorf("subs scan: %w", err)
		}
		subs = append(subs, s)
	}
	rows.Close()
	if rerr := rows.Err(); rerr != nil {
		return rerr
	}

	// 3. Fan out, collecting per-channel errors.
	msg := chatops.Message{Title: p.Title, Body: p.Body, Link: p.Link, Severity: p.Severity}
	var errs []error
	for _, s := range subs {
		var serr error
		switch s.Channel {
		case "slack":
			serr = (&chatops.Slack{WebhookURL: s.Address}).Send(ctx, msg)
		case "teams":
			serr = (&chatops.Teams{WebhookURL: s.Address}).Send(ctx, msg)
		case "webhook":
			serr = postGenericWebhook(ctx, s.Address, p, kind)
		case "email":
			if r.smtp == nil {
				serr = errors.New("smtp not configured (SMTP_HOST unset)")
			} else {
				serr = r.smtp.Send(ctx, s.Address, p.Title, p.Body+"\n\n"+p.Link)
			}
		case "pagerduty":
			serr = NewPagerDutySender(s.Address).Send(ctx, p.Title, p.Severity, "ultraviolet", p.Extra)
		default:
			serr = fmt.Errorf("unknown channel %q", s.Channel)
		}
		if serr != nil {
			errs = append(errs, fmt.Errorf("sub %s (%s): %w", s.ID, s.Channel, serr))
			continue
		}
		_, _ = r.pool.Exec(ctx,
			`UPDATE delivery_subscription SET last_sent_at = now() WHERE id = $1`, s.ID)
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// postGenericWebhook sends a JSON envelope to a generic HTTP webhook.
func postGenericWebhook(ctx context.Context, url string, p Payload, kind Kind) error {
	if url == "" {
		return errors.New("webhook: url empty")
	}
	body, _ := json.Marshal(map[string]any{
		"kind":     kind,
		"title":    p.Title,
		"body":     p.Body,
		"link":     p.Link,
		"severity": p.Severity,
		"extra":    p.Extra,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c := &http.Client{Timeout: 15 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook http %d", resp.StatusCode)
	}
	return nil
}
