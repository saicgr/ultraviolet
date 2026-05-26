// Package ai — autodash.go: LLM-generated dashboard specs from a table FQN.
//
// Flow:
//  1. Pull column list from table_metadata (payload.columns) for the FQN.
//  2. Pull up to 5 recent normalized SQL samples that touched this FQN.
//  3. Ask the LLM to emit a 4-tile dashboard spec as strict JSON.
//  4. Parse + validate; on malformed JSON return ErrMalformedLLMOutput.
//
// Project rule: no fallback to fake data. Caller surfaces the error.
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrMalformedLLMOutput indicates the LLM did not return parseable JSON
// matching the DashboardSpec shape. Callers must NOT fall back to fake data.
var ErrMalformedLLMOutput = errors.New("ai: malformed LLM output")

// Tile is one panel of the auto-generated dashboard.
type Tile struct {
	Title     string `json:"title"`
	SQL       string `json:"sql"`
	ChartType string `json:"chart_type"` // line|bar|table|big_number
}

// DashboardSpec is the LLM-produced dashboard skeleton. The caller is
// responsible for persisting it via internal/dashboards if desired.
type DashboardSpec struct {
	Title string `json:"title"`
	Tiles []Tile `json:"tiles"`
}

type AutoDash struct {
	rw   *Rewriter
	pool *pgxpool.Pool
}

func NewAutoDash(rw *Rewriter, pool *pgxpool.Pool) *AutoDash {
	return &AutoDash{rw: rw, pool: pool}
}

// Generate pulls columns + sample SQL and asks the LLM for a 4-tile spec.
func (a *AutoDash) Generate(ctx context.Context, customerID uuid.UUID, fqn string) (DashboardSpec, error) {
	var zero DashboardSpec
	if a.rw == nil || a.rw.provider == nil {
		return zero, ErrNoProvider
	}
	if a.pool == nil {
		return zero, errors.New("autodash: nil pool")
	}
	if strings.TrimSpace(fqn) == "" {
		return zero, errors.New("autodash: empty fqn")
	}

	cols, err := a.fetchColumns(ctx, customerID, fqn)
	if err != nil {
		return zero, fmt.Errorf("autodash: fetch columns: %w", err)
	}
	samples, err := a.fetchSampleQueries(ctx, customerID, fqn, 5)
	if err != nil {
		return zero, fmt.Errorf("autodash: fetch samples: %w", err)
	}

	prompt := buildAutoDashPrompt(fqn, cols, samples)
	out, err := a.rw.CompleteOne(ctx, a.rw.cfg.AIDefaultModel, prompt, customerID.String())
	if err != nil {
		return zero, err
	}

	spec, perr := parseDashboardSpec(out)
	if perr != nil {
		return zero, ErrMalformedLLMOutput
	}
	return spec, nil
}

// fetchColumns reads column names from table_metadata.payload.columns (if present).
// Empty list is allowed — the LLM can still generate generic tiles.
func (a *AutoDash) fetchColumns(ctx context.Context, customerID uuid.UUID, fqn string) ([]string, error) {
	row := a.pool.QueryRow(ctx, `
		SELECT payload FROM table_metadata
		WHERE customer_id = $1 AND fqn = $2
		ORDER BY updated_at DESC LIMIT 1`, customerID, fqn)
	var payload []byte
	if err := row.Scan(&payload); err != nil {
		// Missing metadata is not a hard error — return empty list.
		return nil, nil
	}
	if len(payload) == 0 {
		return nil, nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return nil, nil
	}
	raw, ok := parsed["columns"]
	if !ok {
		return nil, nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out, nil
}

// fetchSampleQueries pulls up to n recent normalized SQL strings that mention fqn.
func (a *AutoDash) fetchSampleQueries(ctx context.Context, customerID uuid.UUID, fqn string, n int) ([]string, error) {
	rows, err := a.pool.Query(ctx, `
		SELECT normalized_sql FROM query_log
		WHERE customer_id = $1
		  AND normalized_sql ILIKE '%' || $2 || '%'
		ORDER BY started_at DESC
		LIMIT $3`, customerID, fqn, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func buildAutoDashPrompt(fqn string, cols []string, samples []string) string {
	var b strings.Builder
	b.WriteString("You generate a 4-tile dashboard spec for a data warehouse table.\n")
	b.WriteString("Table: ")
	b.WriteString(fqn)
	b.WriteString("\n")
	if len(cols) > 0 {
		b.WriteString("Columns: ")
		b.WriteString(strings.Join(cols, ", "))
		b.WriteString("\n")
	}
	if len(samples) > 0 {
		b.WriteString("Recent queries:\n")
		for i, s := range samples {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, s)
		}
	}
	b.WriteString(`
Respond with ONLY a JSON object of the form:
{"title":"...","tiles":[
  {"title":"...","sql":"SELECT ...","chart_type":"line|bar|table|big_number"},
  ... exactly 4 tiles ...
]}
No commentary, no markdown fences.`)
	return b.String()
}

// parseDashboardSpec tolerates a leading code fence but otherwise demands strict JSON
// matching {title, tiles[4]} with valid chart_type values.
func parseDashboardSpec(out string) (DashboardSpec, error) {
	var zero DashboardSpec
	s := strings.TrimSpace(out)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") {
		return zero, ErrMalformedLLMOutput
	}
	var spec DashboardSpec
	if err := json.Unmarshal([]byte(s), &spec); err != nil {
		return zero, ErrMalformedLLMOutput
	}
	if strings.TrimSpace(spec.Title) == "" || len(spec.Tiles) != 4 {
		return zero, ErrMalformedLLMOutput
	}
	for _, t := range spec.Tiles {
		if strings.TrimSpace(t.Title) == "" || strings.TrimSpace(t.SQL) == "" {
			return zero, ErrMalformedLLMOutput
		}
		switch t.ChartType {
		case "line", "bar", "table", "big_number":
		default:
			return zero, ErrMalformedLLMOutput
		}
	}
	return spec, nil
}
