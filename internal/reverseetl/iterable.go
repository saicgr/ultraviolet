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

// Iterable pushes rows via /api/users/bulkUpdate. Each row is mapped to
// {email, userId, dataFields} — email/userId pulled off the row, the rest
// goes into dataFields.
// Config (decrypted JSONB):
//
//	{
//	  "api_key": "..."
//	}
type Iterable struct {
	APIKey string `json:"api_key"`
}

func (Iterable) Kind() string { return "iterable" }

func (it *Iterable) Push(ctx context.Context, rows []map[string]any) (int, error) {
	if it.APIKey == "" {
		return 0, fmt.Errorf("iterable: api_key required")
	}
	endpoint := "https://api.iterable.com/api/users/bulkUpdate"
	c := &http.Client{Timeout: 15 * time.Second}

	users := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		u := map[string]any{}
		df := map[string]any{}
		for k, v := range row {
			switch k {
			case "email":
				u["email"] = v
			case "userId", "user_id":
				u["userId"] = v
			default:
				df[k] = v
			}
		}
		if _, hasEmail := u["email"]; !hasEmail {
			if _, hasUID := u["userId"]; !hasUID {
				return 0, fmt.Errorf("iterable: row missing email and userId")
			}
		}
		u["dataFields"] = df
		users = append(users, u)
	}

	body, err := json.Marshal(map[string]any{"users": users})
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Api-Key", it.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("iterable http %d: %s", resp.StatusCode, truncate(b, 256))
	}
	return len(users), nil
}
