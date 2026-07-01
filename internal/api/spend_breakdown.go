// Package api — fo-3: Spend by team / user / dashboard heatmap.
//
// GET /api/v1/customers/{id}/spend/breakdown?by=user|api_key|dashboard&window=30d
//
// Aggregates query_log.actual_cost_usd over the requested window grouped by the
// requested dimension. Returns {rows: [{key, cost_usd, query_count, share_pct}], total}.
//
// Schema reality: migration 0019 added query_log.user_id and dashboard_id, so
// all three dimensions now GROUP BY a real column:
//   - by=api_key   → GROUP BY api_key_id
//   - by=user      → GROUP BY user_id
//   - by=dashboard → GROUP BY dashboard_id
// Rows whose dimension is NULL (e.g. raw BI-tool traffic over the proxy, which
// carries no app-user/dashboard context) aggregate under the 'unattributed'
// key — a real, honest bucket, never a fabricated split.
package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type spendRow struct {
	Key        string  `json:"key"`
	CostUSD    float64 `json:"cost_usd"`
	QueryCount int64   `json:"query_count"`
	SharePct   float64 `json:"share_pct"`
}

type spendBreakdownResp struct {
	Rows  []spendRow `json:"rows"`
	Total float64    `json:"total"`
}

// parseWindow accepts "30d", "7d", "24h". Defaults to 30d. Caps at 365d.
func parseWindow(s string) time.Duration {
	if s == "" {
		return 30 * 24 * time.Hour
	}
	unit := s[len(s)-1]
	nStr := s[:len(s)-1]
	n, err := strconv.Atoi(nStr)
	if err != nil || n <= 0 {
		return 30 * 24 * time.Hour
	}
	var d time.Duration
	switch unit {
	case 'd':
		d = time.Duration(n) * 24 * time.Hour
	case 'h':
		d = time.Duration(n) * time.Hour
	default:
		return 30 * 24 * time.Hour
	}
	if d > 365*24*time.Hour {
		d = 365 * 24 * time.Hour
	}
	return d
}

func (s *Server) spendBreakdown(w http.ResponseWriter, r *http.Request) {
	cid, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	by := strings.ToLower(r.URL.Query().Get("by"))
	if by == "" {
		by = "api_key"
	}
	switch by {
	case "user", "api_key", "dashboard":
	default:
		writeErr(w, http.StatusBadRequest, fmt.Errorf("by must be user|api_key|dashboard"))
		return
	}
	win := parseWindow(r.URL.Query().Get("window"))
	since := time.Now().Add(-win)

	rows, total, err := s.queryBreakdown(r, cid, by, since)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if rows == nil {
		rows = []spendRow{}
	}
	writeJSON(w, http.StatusOK, spendBreakdownResp{Rows: rows, Total: total})
}

// breakdownColumn maps the validated `by` value to its query_log column. The
// returned value is from a fixed whitelist — never interpolated user input —
// so embedding it in the SQL string below is injection-safe.
func breakdownColumn(by string) string {
	switch by {
	case "user":
		return "user_id"
	case "dashboard":
		return "dashboard_id"
	default:
		return "api_key_id"
	}
}

// queryBreakdown runs the aggregate. Returns ([], 0, nil) when window is empty.
func (s *Server) queryBreakdown(r *http.Request, cid uuid.UUID, by string, since time.Time) ([]spendRow, float64, error) {
	col := breakdownColumn(by)
	q := fmt.Sprintf(`
		SELECT COALESCE(%[1]s::text, 'unattributed') AS key,
		       COALESCE(SUM(actual_cost_usd), 0)::float8 AS cost_usd,
		       COUNT(*)::bigint                          AS query_count
		  FROM query_log
		 WHERE customer_id = $1
		   AND started_at >= $2
		   AND actual_cost_usd IS NOT NULL
		 GROUP BY %[1]s
		 ORDER BY cost_usd DESC
		 LIMIT 500`, col)
	pgRows, err := s.db.Pool().Query(r.Context(), q, cid, since)
	if err != nil {
		return nil, 0, err
	}
	defer pgRows.Close()

	out := []spendRow{}
	var total float64
	for pgRows.Next() {
		var rr spendRow
		if err := pgRows.Scan(&rr.Key, &rr.CostUSD, &rr.QueryCount); err != nil {
			return nil, 0, err
		}
		total += rr.CostUSD
		out = append(out, rr)
	}
	if err := pgRows.Err(); err != nil {
		return nil, 0, err
	}
	if total > 0 {
		for i := range out {
			out[i].SharePct = (out[i].CostUSD / total) * 100.0
		}
	}
	return out, total, nil
}
