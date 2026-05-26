package reverseetl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HubSpot pushes rows as CRM objects via the v3 batch API.
// Config (decrypted JSONB):
//
//	{
//	  "access_token": "pat-na1-...",
//	  "object_type":  "contacts",   // contacts|companies|deals|...
//	  "base_url":     "https://api.hubapi.com" // optional
//	}
type HubSpot struct {
	AccessToken string `json:"access_token"`
	ObjectType  string `json:"object_type"`
	BaseURL     string `json:"base_url"`
}

func (HubSpot) Kind() string { return "hubspot" }

func (h *HubSpot) Push(ctx context.Context, rows []map[string]any) (int, error) {
	if h.AccessToken == "" {
		return 0, fmt.Errorf("hubspot: access_token required")
	}
	if h.ObjectType == "" {
		return 0, fmt.Errorf("hubspot: object_type required")
	}
	base := h.BaseURL
	if base == "" {
		base = "https://api.hubapi.com"
	}
	endpoint := strings.TrimRight(base, "/") + "/crm/v3/objects/" + h.ObjectType + "/batch/create"
	c := &http.Client{Timeout: 15 * time.Second}

	// HubSpot accepts up to 100 inputs per batch.
	const batchSize = 100
	pushed := 0
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		chunk := rows[i:end]
		inputs := make([]map[string]any, len(chunk))
		for j, row := range chunk {
			inputs[j] = map[string]any{"properties": row}
		}
		body, err := json.Marshal(map[string]any{"inputs": inputs})
		if err != nil {
			return pushed, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return pushed, err
		}
		req.Header.Set("Authorization", "Bearer "+h.AccessToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			return pushed, err
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			return pushed, fmt.Errorf("hubspot http %d: %s", resp.StatusCode, truncate(b, 256))
		}
		pushed += len(chunk)
	}
	return pushed, nil
}
