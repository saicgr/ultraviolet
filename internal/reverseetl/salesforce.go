package reverseetl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Salesforce pushes rows as sObjects via the REST Data API.
// Config (decrypted JSONB from reverse_etl_destination.config_ct):
//
//	{
//	  "instance_url": "https://acme.my.salesforce.com",
//	  "api_version":  "v59.0",
//	  "object_type":  "Account",
//	  "access_token": "00D...",        // optional, refreshed if expired
//	  "refresh_token":"5Aep861...",    // for OAuth refresh
//	  "client_id":    "3MVG9...",
//	  "client_secret":"..."
//	}
type Salesforce struct {
	InstanceURL  string `json:"instance_url"`
	APIVersion   string `json:"api_version"`
	ObjectType   string `json:"object_type"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func (Salesforce) Kind() string { return "salesforce" }

func (s *Salesforce) Push(ctx context.Context, rows []map[string]any) (int, error) {
	if s.InstanceURL == "" || s.ObjectType == "" {
		return 0, fmt.Errorf("salesforce: instance_url and object_type required")
	}
	if s.APIVersion == "" {
		s.APIVersion = "v59.0"
	}
	c := &http.Client{Timeout: 15 * time.Second}
	endpoint := strings.TrimRight(s.InstanceURL, "/") + "/services/data/" + s.APIVersion + "/sobjects/" + s.ObjectType
	pushed := 0
	for _, row := range rows {
		buf, err := json.Marshal(row)
		if err != nil {
			return pushed, err
		}
		status, body, err := s.do(ctx, c, http.MethodPost, endpoint, buf)
		if err != nil {
			return pushed, err
		}
		if status == http.StatusUnauthorized && s.RefreshToken != "" {
			if rerr := s.refresh(ctx, c); rerr != nil {
				return pushed, fmt.Errorf("salesforce refresh: %w", rerr)
			}
			status, body, err = s.do(ctx, c, http.MethodPost, endpoint, buf)
			if err != nil {
				return pushed, err
			}
		}
		if status >= 400 {
			return pushed, fmt.Errorf("salesforce http %d: %s", status, truncate(body, 256))
		}
		pushed++
	}
	return pushed, nil
}

func (s *Salesforce) do(ctx context.Context, c *http.Client, method, url string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, nil
}

func (s *Salesforce) refresh(ctx context.Context, c *http.Client) error {
	if s.ClientID == "" || s.ClientSecret == "" || s.RefreshToken == "" {
		return fmt.Errorf("salesforce: missing oauth credentials for refresh")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", s.RefreshToken)
	form.Set("client_id", s.ClientID)
	form.Set("client_secret", s.ClientSecret)
	tokenURL := strings.TrimRight(s.InstanceURL, "/") + "/services/oauth2/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("salesforce oauth http %d: %s", resp.StatusCode, truncate(b, 256))
	}
	var out struct {
		AccessToken string `json:"access_token"`
		InstanceURL string `json:"instance_url"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return err
	}
	if out.AccessToken == "" {
		return fmt.Errorf("salesforce oauth: empty access_token")
	}
	s.AccessToken = out.AccessToken
	if out.InstanceURL != "" {
		s.InstanceURL = out.InstanceURL
	}
	return nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
