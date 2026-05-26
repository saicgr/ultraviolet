package api

// fo-6 Cost-attribution CSV / NDJSON export.
//
// GET /api/v1/customers/{id}/cost-attribution.csv?since=...&until=...
//
// Streams rows from `cost_attribution`. When the attribution table is empty
// for the window, falls back to per-day aggregation from `query_log` so the
// caller still gets the same column shape — never an empty mock.

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"time"
)

func (s *Server) costAttributionCSV(w http.ResponseWriter, r *http.Request) {
	cid, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	since := parseTimeQ(r, "since", time.Now().Add(-30*24*time.Hour))
	until := parseTimeQ(r, "until", time.Now())

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="cost-attribution.csv"`)
	w.WriteHeader(http.StatusOK)

	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{"period_start", "period_end", "warehouse_cost_usd", "duckdb_cost_usd", "estimated_savings_usd", "queries_total", "queries_duckdb"})

	rows, err := s.db.Pool().Query(r.Context(), `
		SELECT period_start, period_end,
		       warehouse_cost_usd, duckdb_cost_usd, estimated_savings_usd,
		       queries_total, queries_duckdb
		FROM cost_attribution
		WHERE customer_id = $1 AND period_start >= $2 AND period_end <= $3
		ORDER BY period_start ASC`, cid, since, until)
	if err != nil {
		s.log.Warn().Err(err).Msg("cost-attribution primary query failed")
		return
	}
	defer rows.Close()

	wrote := 0
	for rows.Next() {
		var ps, pe time.Time
		var wh, duck, sav float64
		var qt, qd int64
		if err := rows.Scan(&ps, &pe, &wh, &duck, &sav, &qt, &qd); err != nil {
			s.log.Warn().Err(err).Msg("cost-attribution scan failed mid-stream")
			return
		}
		_ = cw.Write([]string{
			ps.Format(time.RFC3339), pe.Format(time.RFC3339),
			fmt.Sprintf("%.6f", wh), fmt.Sprintf("%.6f", duck), fmt.Sprintf("%.6f", sav),
			fmt.Sprintf("%d", qt), fmt.Sprintf("%d", qd),
		})
		wrote++
	}
	if wrote > 0 {
		return
	}

	// Fallback: aggregate from query_log by day so empty-attribution windows
	// still produce a meaningful export (CLAUDE.md: no silent empty result).
	fb, err := s.db.Pool().Query(r.Context(), `
		SELECT date_trunc('day', started_at) AS day,
		       date_trunc('day', started_at) + interval '1 day' AS day_end,
		       COALESCE(SUM(actual_cost_usd) FILTER (WHERE route_decision <> 'duckdb'), 0)::float8 AS wh_cost,
		       COALESCE(SUM(actual_cost_usd) FILTER (WHERE route_decision  = 'duckdb'), 0)::float8 AS duck_cost,
		       COUNT(*) AS qt,
		       COUNT(*) FILTER (WHERE route_decision = 'duckdb') AS qd
		FROM query_log
		WHERE customer_id = $1 AND started_at >= $2 AND started_at < $3
		GROUP BY 1 ORDER BY 1 ASC`, cid, since, until)
	if err != nil {
		s.log.Warn().Err(err).Msg("cost-attribution fallback query failed")
		return
	}
	defer fb.Close()
	for fb.Next() {
		var ps, pe time.Time
		var wh, duck float64
		var qt, qd int64
		if err := fb.Scan(&ps, &pe, &wh, &duck, &qt, &qd); err != nil {
			s.log.Warn().Err(err).Msg("cost-attribution fallback scan failed")
			return
		}
		_ = cw.Write([]string{
			ps.Format(time.RFC3339), pe.Format(time.RFC3339),
			fmt.Sprintf("%.6f", wh), fmt.Sprintf("%.6f", duck), fmt.Sprintf("%.6f", wh-duck),
			fmt.Sprintf("%d", qt), fmt.Sprintf("%d", qd),
		})
	}
}

func parseTimeQ(r *http.Request, key string, fallback time.Time) time.Time {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t
	}
	return fallback
}
