// Package chatops implements the Slack + Microsoft Teams + Discord webhook
// targets for alerts and `/uv ask` commands. Each transport implements the
// same Sender interface so the dashboard schedule + alert engine fan out
// without per-platform wiring upstream.
package chatops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

type Sender interface {
	Kind() string
	Send(ctx context.Context, msg Message) error
}

type Message struct {
	Title   string
	Body    string
	Link    string
	Severity string // info|warn|fatal
}

type Slack struct{ WebhookURL string }

func (s *Slack) Kind() string { return "slack" }
func (s *Slack) Send(ctx context.Context, m Message) error {
	if s.WebhookURL == "" {
		return errors.New("slack: WebhookURL empty")
	}
	body, _ := json.Marshal(map[string]any{
		"text": fmt.Sprintf("*%s* — %s\n%s", m.Title, m.Body, m.Link),
	})
	return postJSON(ctx, s.WebhookURL, body)
}

type Teams struct{ WebhookURL string }

func (t *Teams) Kind() string { return "teams" }
func (t *Teams) Send(ctx context.Context, m Message) error {
	if t.WebhookURL == "" {
		return errors.New("teams: WebhookURL empty")
	}
	body, _ := json.Marshal(map[string]any{
		"@type":      "MessageCard",
		"@context":   "https://schema.org/extensions",
		"summary":    m.Title,
		"title":      m.Title,
		"text":       m.Body,
		"themeColor": severityColor(m.Severity),
	})
	return postJSON(ctx, t.WebhookURL, body)
}

type Discord struct{ WebhookURL string }

func (d *Discord) Kind() string { return "discord" }
func (d *Discord) Send(ctx context.Context, m Message) error {
	if d.WebhookURL == "" {
		return errors.New("discord: WebhookURL empty")
	}
	body, _ := json.Marshal(map[string]any{"content": fmt.Sprintf("**%s**\n%s\n%s", m.Title, m.Body, m.Link)})
	return postJSON(ctx, d.WebhookURL, body)
}

func severityColor(s string) string {
	switch s {
	case "fatal":
		return "C81D25"
	case "warn":
		return "F2B807"
	default:
		return "2B7EFF"
	}
}

func postJSON(ctx context.Context, url string, body []byte) error {
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
		return fmt.Errorf("chatops http %d", resp.StatusCode)
	}
	return nil
}
