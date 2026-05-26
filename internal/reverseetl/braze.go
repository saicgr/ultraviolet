package reverseetl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Braze pushes rows as user attributes via /users/track.
// Config (decrypted JSONB):
//
//	{
//	  "api_key":  "...",
//	  "instance": "iad-01"   // -> https://rest.iad-01.braze.com
//	}
type Braze struct {
	APIKey   string `json:"api_key"`
	Instance string `json:"instance"`
}

func (Braze) Kind() string { return "braze" }

func (b *Braze) Push(ctx context.Context, rows []map[string]any) (int, error) {
	if b.APIKey == "" || b.Instance == "" {
		return 0, fmt.Errorf("braze: api_key and instance required")
	}
	endpoint := "https://rest." + b.Instance + ".braze.com/users/track"
	c := &http.Client{Timeout: 15 * time.Second}

	const batchSize = 75
	pushed := 0
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		chunk := rows[i:end]
		body, err := json.Marshal(map[string]any{"attributes": chunk})
		if err != nil {
			return pushed, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return pushed, err
		}
		req.Header.Set("Authorization", "Bearer "+b.APIKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			return pushed, err
		}
		buf, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			return pushed, fmt.Errorf("braze http %d: %s", resp.StatusCode, truncate(buf, 256))
		}
		pushed += len(chunk)
	}
	return pushed, nil
}
