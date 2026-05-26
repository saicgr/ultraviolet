// Package api — fo-3: Spend by team / user / dashboard heatmap.
//
// GET /api/v1/customers/{id}/spend/breakdown?by=user|api_key|dashboard&window=30d
//
// Aggregates query_log.actual_cost_usd over the requested window grouped by the
// requested dimension. Returns {rows: [{key, cost_usd, query_count, share_pct}], total}.
//
// Schema reality (migrations/0001_initial.up.sql L83-99): query_log carries
// customer_id, connection_id, api_key_id, actual_cost_usd, started_at — but no
// user_id and no dashboard_id columns. We therefore implement:
//   - by=api_key  → direct GROUP BY api_key_id (real)
//   - by=user     → TODO: query_log has no user_id; api_keys table has no
//                   user_id either. Emit empty rows + total=0 until either
//                   a future migration adds query_log.user_id or api_keys.user_id.
//   - by=dashboard → TODO: query_log has no dashboard_id. Heuristic could
//                   FQN-match dashboard.tiles::text against normalized_sql,
//                   but that is best done in a background materialization job,
//                   not a per-request scan. Emit empty rows + total=0.
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

// queryBreakdown runs the aggregate. Returns ([], 0, nil) when window is empty.
func (s *Server) queryBreakdown(r *http.Request, cid uuid.UUID, by string, since time.Time) ([]spendRow, float64, error) {
	// by=user and by=dashboard have no backing column in query_log today —
	// see file header. Return empty (real zero, not stubbed values).
	if by == "user" || by == "dashboard" {
		// TODO(fo-3): once query_log gains user_id / dashboard_id (or once
		// dashboard.tiles → normalized_sql FQN materialization is in place),
		// branch here. Until then we return real-zero, never fake numbers.
		return []spendRow{}, 0, nil
	}

	// by=api_key
	const q = `
		SELECT COALESCE(api_key_id::text, 'unattributed') AS key,
		       COALESCE(SUM(actual_cost_usd), 0)::float8 AS cost_usd,
		       COUNT(*)::bigint                          AS query_count
		  FROM query_log
		 WHERE customer_id = $1
		   AND started_at >= $2
		   AND actual_cost_usd IS NOT NULL
		 GROUP BY api_key_id
		 ORDER BY cost_usd DESC
		 LIMIT 500`
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
