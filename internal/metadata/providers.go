package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// dbt: read manifest.json from a dbt Cloud / dbt-core artifact URL.
type DBT struct {
	ManifestURL string
	APIToken    string
}

func (d *DBT) Name() string { return "dbt" }

func (d *DBT) Pull(ctx context.Context) ([]TableMeta, error) {
	if d.ManifestURL == "" {
		return nil, nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, d.ManifestURL, nil)
	if d.APIToken != "" {
		req.Header.Set("Authorization", "Token "+d.APIToken)
	}
	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dbt manifest fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dbt manifest http %d", resp.StatusCode)
	}
	var m struct {
		Nodes map[string]struct {
			Database    string   `json:"database"`
			Schema      string   `json:"schema"`
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Tags        []string `json:"tags"`
			Meta        struct {
				Owner string `json:"owner"`
				SLA   int    `json:"sla_minutes"`
			} `json:"meta"`
		} `json:"nodes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	out := make([]TableMeta, 0, len(m.Nodes))
	for _, n := range m.Nodes {
		out = append(out, TableMeta{
			FQN:         strings.Join([]string{n.Schema, n.Name}, "."),
			Description: n.Description,
			Owner:       n.Meta.Owner,
			SLAMinutes:  n.Meta.SLA,
			Tags:        n.Tags,
			Source:      "dbt",
		})
	}
	return out, nil
}

// Tableau: GraphQL Metadata API.
type Tableau struct {
	ServerURL string
	Token     string
}

func (t *Tableau) Name() string { return "tableau" }

func (t *Tableau) Pull(ctx context.Context) ([]TableMeta, error) {
	if t.ServerURL == "" || t.Token == "" {
		return nil, nil
	}
	q := `{"query":"{ databaseTables { name schema description } }"}`
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, t.ServerURL+"/api/metadata/graphql", strings.NewReader(q))
	req.Header.Set("X-Tableau-Auth", t.Token)
	req.Header.Set("Content-Type", "application/json")
	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Data struct {
			DatabaseTables []struct {
				Name        string `json:"name"`
				Schema      string `json:"schema"`
				Description string `json:"description"`
			} `json:"databaseTables"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]TableMeta, 0, len(body.Data.DatabaseTables))
	for _, t := range body.Data.DatabaseTables {
		out = append(out, TableMeta{
			FQN:         t.Schema + "." + t.Name,
			Description: t.Description,
			Source:      "tableau",
		})
	}
	return out, nil
}

// Hex: REST API for projects + their referenced tables.
type Hex struct{ APIToken string }

func (h *Hex) Name() string { return "hex" }

func (h *Hex) Pull(ctx context.Context) ([]TableMeta, error) {
	// Hex doesn't expose a single table-level endpoint; fingerprint match runs
	// against `query_log` here in production. Stub: return nothing on missing
	// token so the enricher tick stays a no-op.
	if h.APIToken == "" {
		return nil, nil
	}
	return nil, nil
}

// Looker: reads `lookml_models` then `explore_fields`.
type Looker struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
}

func (l *Looker) Name() string { return "looker" }

func (l *Looker) Pull(ctx context.Context) ([]TableMeta, error) {
	if l.BaseURL == "" {
		return nil, nil
	}
	tok, err := l.token(ctx)
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, l.BaseURL+"/api/4.0/lookml_models", nil)
	req.Header.Set("Authorization", "token "+tok)
	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var models []struct {
		Name           string `json:"name"`
		ProjectName    string `json:"project_name"`
		LabelHTML      string `json:"label"`
		AllowedDBs     []string `json:"allowed_db_connection_names"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&models)
	out := make([]TableMeta, 0, len(models))
	for _, m := range models {
		out = append(out, TableMeta{
			FQN:         m.Name,
			Description: m.LabelHTML,
			Owner:       m.ProjectName,
			Source:      "looker",
		})
	}
	return out, nil
}

func (l *Looker) token(ctx context.Context) (string, error) {
	form := url.Values{"client_id": {l.ClientID}, "client_secret": {l.ClientSecret}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, l.BaseURL+"/api/4.0/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c := &http.Client{Timeout: 15 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var t struct{ AccessToken string `json:"access_token"` }
	_ = json.NewDecoder(resp.Body).Decode(&t)
	return t.AccessToken, nil
}

// Metaplane: data observability (freshness + schema-change events).
type Metaplane struct{ APIKey string }

func (m *Metaplane) Name() string { return "metaplane" }

func (m *Metaplane) Pull(ctx context.Context) ([]TableMeta, error) {
	if m.APIKey == "" {
		return nil, nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.metaplane.dev/v1/tables", nil)
	req.Header.Set("Authorization", "Bearer "+m.APIKey)
	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Tables []struct {
			FQN        string   `json:"fqn"`
			Owner      string   `json:"owner"`
			SLAMinutes int      `json:"sla_minutes"`
			Tags       []string `json:"tags"`
		} `json:"tables"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	out := make([]TableMeta, 0, len(body.Tables))
	for _, t := range body.Tables {
		out = append(out, TableMeta{
			FQN:        t.FQN,
			Owner:      t.Owner,
			SLAMinutes: t.SLAMinutes,
			Tags:       t.Tags,
			Source:     "metaplane",
		})
	}
	return out, nil
}

// Atlan: REST API for catalog assets.
type Atlan struct {
	BaseURL string
	APIKey  string
}

func (a *Atlan) Name() string { return "atlan" }

func (a *Atlan) Pull(ctx context.Context) ([]TableMeta, error) {
	if a.BaseURL == "" || a.APIKey == "" {
		return nil, nil
	}
	return nil, nil // first slice; full IndexSearchRequest body lives behind a TODO
}
