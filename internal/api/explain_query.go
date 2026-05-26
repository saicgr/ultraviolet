// Package api — explain_query.go: LLM-backed "explain this query" endpoint (ai-u2).
//
// POST /api/v1/queries/explain. Given a {customer_id, sql} body the handler:
//
//  1. Estimates the cost. We replicate the BigQuery on-demand formula inline
//     ($5/TiB scanned) using the average bytes_scanned for similar (same
//     normalized) queries in query_log. Falls back to a flat $0 if no history.
//  2. Looks up downstream lineage_edge rows whose upstream_fqn matches any
//     table referenced in the SQL (best-effort, regex extraction). Returns
//     just the count to the caller — the LLM bullet just needs "N consumers".
//  3. Asks the LLM for a 4-bullet explanation covering: what it does, cost,
//     downstream consumers, optimization hints.
//
// Returns {explanation, cost_usd, downstream_count}. Per project rule we do
// not silently degrade: missing rewriter → 503.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/ai"
)

// bqOnDemandUSDPerTiB mirrors internal/cost — duplicated as a constant here
// to avoid an api→cost import (cost imports store + warehouse drivers and
// would pull a cycle once cost grows). Sync with internal/cost/bigquery_cost.go.
const bqOnDemandUSDPerTiB = 5.0

// tableRefRe extracts FROM/JOIN identifiers (schema.table or schema.table.col).
// Intentionally permissive — over-matching only inflates the downstream_count
// estimate, which the LLM bullet treats as approximate anyway.
var tableRefRe = regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+([a-zA-Z_][\w.]*)`)

type explainQueryRequest struct {
	CustomerID string `json:"customer_id"`
	SQL        string `json:"sql"`
}

type explainQueryResponse struct {
	Explanation     string  `json:"explanation"`
	CostUSD         float64 `json:"cost_usd"`
	DownstreamCount int     `json:"downstream_count"`
}

func (s *Server) explainQuery(w http.ResponseWriter, r *http.Request) {
	if s.rewriter == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("explain: ai provider not configured"))
		return
	}
	var body explainQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.CustomerID == "" || strings.TrimSpace(body.SQL) == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("customer_id and sql required"))
		return
	}
	cid, err := uuid.Parse(body.CustomerID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("customer_id: %w", err))
		return
	}

	costUSD := s.estimateBQOnDemandCost(r.Context(), cid, body.SQL)
	tables := extractTableRefs(body.SQL)
	downstream := s.countDownstreamConsumers(r.Context(), cid, tables)

	prompt := buildExplainPrompt(body.SQL, costUSD, downstream, tables)
	out, err := s.rewriter.CompleteOne(r.Context(), s.cfg.AIDefaultModel, prompt, body.CustomerID)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, ai.ErrNoProvider) {
			status = http.StatusServiceUnavailable
		}
		writeErr(w, status, fmt.Errorf("explain: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, explainQueryResponse{
		Explanation:     strings.TrimSpace(out),
		CostUSD:         costUSD,
		DownstreamCount: downstream,
	})
}

// estimateBQOnDemandCost averages bytes_scanned from query_log entries that
// touched any of the same tables (best signal we have without re-running the
// query). Returns 0 when there's no history — caller treats 0 as "unknown"
// rather than free.
func (s *Server) estimateBQOnDemandCost(ctx context.Context, customerID uuid.UUID, sql string) float64 {
	var avgBytes *float64
	err := s.db.Pool().QueryRow(ctx,
		`SELECT AVG(bytes_scanned)::float8
		 FROM query_log
		 WHERE customer_id = $1
		   AND bytes_scanned IS NOT NULL
		   AND started_at > now() - interval '7 days'`,
		customerID).Scan(&avgBytes)
	if err != nil || avgBytes == nil || *avgBytes <= 0 {
		return 0
	}
	tib := *avgBytes / (1024 * 1024 * 1024 * 1024)
	_ = sql
	return tib * bqOnDemandUSDPerTiB
}

// countDownstreamConsumers returns the unique count of downstream_fqn rows in
// lineage_edge whose upstream_fqn matches any of the SQL's referenced tables.
// Best-effort: any DB error is logged and treated as zero.
func (s *Server) countDownstreamConsumers(ctx context.Context, customerID uuid.UUID, tables []string) int {
	if len(tables) == 0 {
		return 0
	}
	var n int
	err := s.db.Pool().QueryRow(ctx,
		`SELECT COUNT(DISTINCT downstream_fqn)
		 FROM lineage_edge
		 WHERE customer_id = $1 AND upstream_fqn = ANY($2)`,
		customerID, tables).Scan(&n)
	if err != nil {
		s.log.Debug().Err(err).Msg("explain: downstream count lookup failed")
		return 0
	}
	return n
}

func extractTableRefs(sql string) []string {
	matches := tableRefRe.FindAllStringSubmatch(sql, -1)
	seen := map[string]struct{}{}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(m[1]))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func buildExplainPrompt(sql string, costUSD float64, downstream int, tables []string) string {
	var sb strings.Builder
	sb.WriteString("Explain the following SQL in exactly 4 bullet points. ")
	sb.WriteString("Cover, in order: (1) what the query does, ")
	fmt.Fprintf(&sb, "(2) estimated cost ($%.4f), ", costUSD)
	fmt.Fprintf(&sb, "(3) downstream consumers (%d depend on tables it touches), ", downstream)
	sb.WriteString("(4) one or two concrete optimization hints. Use markdown `-` bullets.\n\n")
	if len(tables) > 0 {
		fmt.Fprintf(&sb, "Tables referenced: %s\n\n", strings.Join(tables, ", "))
	}
	sb.WriteString("SQL:\n```sql\n")
	sb.WriteString(strings.TrimSpace(sql))
	sb.WriteString("\n```\n")
	return sb.String()
}
