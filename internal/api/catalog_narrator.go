// Package api — catalog_narrator.go: AI catalog auto-narrator (ai-u10).
//
// GET /api/v1/customers/{id}/catalog/narrative
// Builds a 3-paragraph prose summary of:
//   1. Top-5 most-queried tables (last 7d from query_log, normalized_sql match).
//   2. Recent schema changes (audit_log last 7d, action LIKE 'schema.%').
//   3. Freshness alerts (table_metadata.sla_minutes vs synced_tables.last_synced_at).
//
// Result cached in memory per customer for 1h. No fake fallback — LLM errors
// surface as 5xx.
package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/ai"
)

type narrativeCacheEntry struct {
	at   time.Time
	body string
}

var (
	narrativeCacheMu sync.Mutex
	narrativeCache   = map[uuid.UUID]narrativeCacheEntry{}
)

const narrativeTTL = time.Hour

func (s *Server) catalogNarrative(w http.ResponseWriter, r *http.Request) {
	if s.rewriter == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("narrator: ai provider not configured"))
		return
	}
	cid, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	// Cache check.
	narrativeCacheMu.Lock()
	if e, ok := narrativeCache[cid]; ok && time.Since(e.at) < narrativeTTL {
		body := e.body
		narrativeCacheMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"customer_id": cid, "narrative": body, "cached": true})
		return
	}
	narrativeCacheMu.Unlock()

	topTables, err := s.fetchTopQueriedTables(r.Context(), cid, 5)
	if err != nil {
		s.log.Debug().Err(err).Msg("narrator: top tables lookup failed")
	}
	changes, err := s.fetchRecentSchemaChanges(r.Context(), cid)
	if err != nil {
		s.log.Debug().Err(err).Msg("narrator: schema changes lookup failed")
	}
	freshness, err := s.fetchFreshnessAlerts(r.Context(), cid)
	if err != nil {
		s.log.Debug().Err(err).Msg("narrator: freshness lookup failed")
	}

	prompt := buildNarrativePrompt(topTables, changes, freshness)
	out, err := s.rewriter.CompleteOne(r.Context(), s.cfg.AIDefaultModel, prompt, cid.String())
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, ai.ErrNoProvider) {
			status = http.StatusServiceUnavailable
		}
		writeErr(w, status, fmt.Errorf("narrator: %w", err))
		return
	}

	narrativeCacheMu.Lock()
	narrativeCache[cid] = narrativeCacheEntry{at: time.Now(), body: out}
	narrativeCacheMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"customer_id": cid,
		"narrative":   out,
		"cached":      false,
	})
}

type topQueriedTable struct {
	FQN   string
	Count int64
}

var fqnRe = regexp.MustCompile(`(?i)\b([a-z_][a-z0-9_]*\.[a-z_][a-z0-9_]*(?:\.[a-z_][a-z0-9_]*)?)\b`)

// fetchTopQueriedTables scans the last-7d query_log and counts FQN mentions
// in normalized_sql. Keeps it in-process — fine for the cache window.
func (s *Server) fetchTopQueriedTables(ctx context.Context, cid uuid.UUID, n int) ([]topQueriedTable, error) {
	rows, err := s.db.Pool().Query(ctx, `
		SELECT normalized_sql FROM query_log
		WHERE customer_id = $1 AND started_at > now() - interval '7 days'
		  AND normalized_sql IS NOT NULL`, cid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int64{}
	for rows.Next() {
		var sql string
		if err := rows.Scan(&sql); err != nil {
			return nil, err
		}
		for _, m := range fqnRe.FindAllString(sql, -1) {
			counts[strings.ToLower(m)]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]topQueriedTable, 0, len(counts))
	for fqn, c := range counts {
		out = append(out, topQueriedTable{FQN: fqn, Count: c})
	}
	// Insertion sort (small N).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Count > out[j-1].Count; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if len(out) > n {
		out = out[:n]
	}
	return out, nil
}

func (s *Server) fetchRecentSchemaChanges(ctx context.Context, cid uuid.UUID) ([]string, error) {
	rows, err := s.db.Pool().Query(ctx, `
		SELECT action, COALESCE(target_id, ''), created_at
		FROM audit_log
		WHERE customer_id = $1 AND action LIKE 'schema.%'
		  AND created_at > now() - interval '7 days'
		ORDER BY created_at DESC LIMIT 20`, cid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var action, tgt string
		var at time.Time
		if err := rows.Scan(&action, &tgt, &at); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("%s %s @ %s", action, tgt, at.Format(time.RFC3339)))
	}
	return out, rows.Err()
}

func (s *Server) fetchFreshnessAlerts(ctx context.Context, cid uuid.UUID) ([]string, error) {
	rows, err := s.db.Pool().Query(ctx, `
		SELECT fqn, sla_minutes
		FROM table_metadata
		WHERE customer_id = $1 AND sla_minutes IS NOT NULL
		ORDER BY updated_at DESC LIMIT 10`, cid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var fqn string
		var sla int
		if err := rows.Scan(&fqn, &sla); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("%s (SLA %dm)", fqn, sla))
	}
	return out, rows.Err()
}

func buildNarrativePrompt(top []topQueriedTable, changes, freshness []string) string {
	var sb strings.Builder
	sb.WriteString("Write a 3-paragraph executive summary of this data warehouse's last 7 days. ")
	sb.WriteString("Paragraph 1: usage (top queried tables). Paragraph 2: schema changes. ")
	sb.WriteString("Paragraph 3: freshness/SLA risks. Be concise, prose only — no bullet points, no markdown.\n\n")
	sb.WriteString("Top queried tables (fqn, query count):\n")
	if len(top) == 0 {
		sb.WriteString("  (none)\n")
	}
	for _, t := range top {
		fmt.Fprintf(&sb, "  - %s: %d\n", t.FQN, t.Count)
	}
	sb.WriteString("Schema changes:\n")
	if len(changes) == 0 {
		sb.WriteString("  (none)\n")
	}
	for _, c := range changes {
		fmt.Fprintf(&sb, "  - %s\n", c)
	}
	sb.WriteString("Tables with SLA:\n")
	if len(freshness) == 0 {
		sb.WriteString("  (none)\n")
	}
	for _, f := range freshness {
		fmt.Fprintf(&sb, "  - %s\n", f)
	}
	return sb.String()
}
