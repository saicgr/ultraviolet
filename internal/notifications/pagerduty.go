// Package notifications — PagerDuty Events API v2 transport.
//
// POST https://events.pagerduty.com/v2/enqueue with a trigger event. routing_key
// comes from each delivery_subscription row's address column (or its config).
// No silent degrade: returns the HTTP error if PagerDuty rejects the event.
package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const pagerDutyEventsV2URL = "https://events.pagerduty.com/v2/enqueue"

// PagerDutySender posts trigger events to PagerDuty's Events API v2.
// RoutingKey is the integration key from the PagerDuty service.
type PagerDutySender struct {
	RoutingKey string
	HTTPClient *http.Client
}

// NewPagerDutySender returns a sender bound to the given routing_key.
func NewPagerDutySender(routingKey string) *PagerDutySender {
	return &PagerDutySender{
		RoutingKey: routingKey,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// Kind reports the channel name.
func (p *PagerDutySender) Kind() string { return "pagerduty" }

// Send fires a "trigger" event. severity defaults to "warning" if empty.
func (p *PagerDutySender) Send(ctx context.Context, summary, severity, source string, payload map[string]any) error {
	if p == nil || p.RoutingKey == "" {
		return errors.New("pagerduty: routing_key empty")
	}
	if severity == "" {
		severity = "warning"
	}
	if source == "" {
		source = "ultraviolet"
	}
	body, _ := json.Marshal(map[string]any{
		"routing_key":  p.RoutingKey,
		"event_action": "trigger",
		"payload": map[string]any{
			"summary":        summary,
			"severity":       severity,
			"source":         source,
			"custom_details": payload,
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pagerDutyEventsV2URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("pagerduty post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("pagerduty http %d", resp.StatusCode)
	}
	return nil
}
