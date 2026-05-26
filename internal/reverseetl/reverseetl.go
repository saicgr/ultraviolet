// Package reverseetl pushes computed result sets back into operational tools
// (Salesforce, HubSpot, Zendesk, Marketo, Customer.io, Braze, Iterable),
// destination webhooks, and Kafka / Pub-Sub / Kinesis sinks.
package reverseetl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

type Destination interface {
	Kind() string
	Push(ctx context.Context, rows []map[string]any) (int, error) // returns rows-pushed
}

// Webhook — POSTs each row as JSON.
type Webhook struct {
	URL   string
	Token string
}

func (d *Webhook) Kind() string { return "webhook" }

func (d *Webhook) Push(ctx context.Context, rows []map[string]any) (int, error) {
	c := &http.Client{Timeout: 30 * time.Second}
	pushed := 0
	for _, row := range rows {
		buf, err := json.Marshal(row)
		if err != nil {
			return pushed, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL, bytes.NewReader(buf))
		if err != nil {
			return pushed, err
		}
		if d.Token != "" {
			req.Header.Set("Authorization", "Bearer "+d.Token)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			return pushed, err
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			return pushed, fmt.Errorf("webhook http %d", resp.StatusCode)
		}
		pushed++
	}
	return pushed, nil
}

// Remaining stub destinations — real Salesforce/HubSpot/Zendesk/Marketo/
// CustomerIO/Braze/Iterable impls live in their own files.
type Kafka struct{}
type PubSub struct{}
type Kinesis struct{}

func (Kafka) Kind() string  { return "kafka" }
func (PubSub) Kind() string { return "pubsub" }
func (Kinesis) Kind() string { return "kinesis" }

var ErrNotImplemented = errors.New("destination not yet implemented")

func (Kafka) Push(context.Context, []map[string]any) (int, error)   { return 0, ErrNotImplemented }
func (PubSub) Push(context.Context, []map[string]any) (int, error)  { return 0, ErrNotImplemented }
func (Kinesis) Push(context.Context, []map[string]any) (int, error) { return 0, ErrNotImplemented }

// Registry returns all known destination kinds. Drives the onboarding UI.
func Registry() []string {
	return []string{
		"webhook", "salesforce", "hubspot", "zendesk", "marketo",
		"customer_io", "braze", "iterable", "kafka", "pubsub", "kinesis",
	}
}
