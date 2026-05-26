package reverseetl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Marketo pushes rows as leads via the REST API. OAuth2 client_credentials
// flow fetches a bearer token from /identity/oauth/token.
// Config (decrypted JSONB):
//
//	{
//	  "client_id":     "...",
//	  "client_secret": "...",
//	  "munchkin_id":   "123-ABC-456"   // -> https://123-ABC-456.mktorest.com
//	}
type Marketo struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	MunchkinID   string `json:"munchkin_id"`

	accessToken string
}

func (Marketo) Kind() string { return "marketo" }

func (m *Marketo) Push(ctx context.Context, rows []map[string]any) (int, error) {
	if m.ClientID == "" || m.ClientSecret == "" || m.MunchkinID == "" {
		return 0, fmt.Errorf("marketo: client_id, client_secret, munchkin_id required")
	}
	base := "https://" + m.MunchkinID + ".mktorest.com"
	c := &http.Client{Timeout: 15 * time.Second}
	if m.accessToken == "" {
		if err := m.authenticate(ctx, c, base); err != nil {
			return 0, err
		}
	}
	endpoint := base + "/rest/v1/leads.json"

	const batchSize = 300
	pushed := 0
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		chunk := rows[i:end]
		body, err := json.Marshal(map[string]any{"action": "createOrUpdate", "input": chunk})
		if err != nil {
			return pushed, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return pushed, err
		}
		req.Header.Set("Authorization", "Bearer "+m.accessToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.Do(req)
		if err != nil {
			return pushed, err
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 {
			return pushed, fmt.Errorf("marketo http %d: %s", resp.StatusCode, truncate(b, 256))
		}
		pushed += len(chunk)
	}
	return pushed, nil
}

func (m *Marketo) authenticate(ctx context.Context, c *http.Client, base string) error {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", m.ClientID)
	form.Set("client_secret", m.ClientSecret)
	tokenURL := base + "/identity/oauth/token?" + form.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("marketo oauth http %d: %s", resp.StatusCode, truncate(b, 256))
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return err
	}
	if out.AccessToken == "" {
		return fmt.Errorf("marketo oauth: empty access_token")
	}
	m.accessToken = out.AccessToken
	return nil
}
