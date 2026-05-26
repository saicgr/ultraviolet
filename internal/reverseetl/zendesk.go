package reverseetl

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Zendesk pushes rows as resources via the v2 REST API with basic-auth
// (email/token:apiKey).
// Config (decrypted JSONB):
//
//	{
//	  "subdomain": "acme",            // -> https://acme.zendesk.com
//	  "email":     "ops@acme.com",
//	  "api_token": "xy...",
//	  "resource":  "tickets"          // tickets|users|organizations|...
//	}
//
// Each row is wrapped as {"<singular>": {...}} per Zendesk convention.
type Zendesk struct {
	Subdomain string `json:"subdomain"`
	Email     string `json:"email"`
	APIToken  string `json:"api_token"`
	Resource  string `json:"resource"`
}

func (Zendesk) Kind() string { return "zendesk" }

func (z *Zendesk) Push(ctx context.Context, rows []map[string]any) (int, error) {
	if z.Subdomain == "" || z.Email == "" || z.APIToken == "" || z.Resource == "" {
		return 0, fmt.Errorf("zendesk: subdomain, email, api_token, resource all required")
	}
	endpoint := "https://" + z.Subdomain + ".zendesk.com/api/v2/" + z.Resource + ".json"
	authRaw := z.Email + "/token:" + z.APIToken
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(authRaw))
	singular := strings.TrimSuffix(z.Resource, "s")
	c := &http.Client{Timeout: 15 * time.Second}
	pushed := 0
	for _, row := range rows {
		body, err := json.Marshal(map[string]any{singular: row})
		if err != nil {
			return pushed, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
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
			return pushed, fmt.Errorf("zendesk http %d: %s", resp.StatusCode, truncate(b, 256))
		}
		pushed++
	}
	return pushed, nil
}
