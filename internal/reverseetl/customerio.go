package reverseetl

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CustomerIO pushes rows as customers via the track API. Identifies by the
// `id` field on each row. Uses basic-auth (site_id:api_key).
// Config (decrypted JSONB):
//
//	{
//	  "site_id": "...",
//	  "api_key": "..."
//	}
type CustomerIO struct {
	SiteID string `json:"site_id"`
	APIKey string `json:"api_key"`
}

func (CustomerIO) Kind() string { return "customer_io" }

func (ci *CustomerIO) Push(ctx context.Context, rows []map[string]any) (int, error) {
	if ci.SiteID == "" || ci.APIKey == "" {
		return 0, fmt.Errorf("customer_io: site_id and api_key required")
	}
	authRaw := ci.SiteID + ":" + ci.APIKey
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(authRaw))
	c := &http.Client{Timeout: 15 * time.Second}
	pushed := 0
	for _, row := range rows {
		idVal, ok := row["id"]
		if !ok {
			return pushed, fmt.Errorf("customer_io: row missing id field")
		}
		id := fmt.Sprintf("%v", idVal)
		if id == "" {
			return pushed, fmt.Errorf("customer_io: row has empty id")
		}
		body, err := json.Marshal(row)
		if err != nil {
			return pushed, err
		}
		endpoint := "https://track.customer.io/api/v1/customers/" + id
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
		if err != nil {
			return pushed, err
		}
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			return pushed, err
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			return pushed, fmt.Errorf("customer_io http %d: %s", resp.StatusCode, truncate(b, 256))
		}
		pushed++
	}
	return pushed, nil
}
