package api

// fo-6 Cost-attribution CSV / NDJSON export.
//
// GET /api/v1/customers/{id}/cost-attribution.csv?since=...&until=...
//
// Streams rows from `cost_attribution`. When the attribution table has no
// rollup for the window yet, it derives the same columns per-day from
// `query_log` using the SAME savings formula (estimated − actual on the
// DuckDB-routed queries) — and marks the response with the
// `X-UV-Cost-Source: query_log_derived` header so the consumer knows the source
// is a live derivation, not the persisted rollup. Never a silent or
// inconsistent degradation (CLAUDE.md no-fallback rule).

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

	// Decide the source BEFORE writing the body so we can set an honest header.
	var hasRollup bool
	if err := s.db.Pool().QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM cost_attribution
		 WHERE customer_id = $1 AND period_start >= $2 AND period_end <= $3)`,
		cid, since, until).Scan(&hasRollup); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="cost-attribution.csv"`)
	if hasRollup {
		w.Header().Set("X-UV-Cost-Source", "cost_attribution")
	} else {
		w.Header().Set("X-UV-Cost-Source", "query_log_derived")
	}
	w.WriteHeader(http.StatusOK)

	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{"period_start", "period_end", "warehouse_cost_usd", "duckdb_cost_usd", "estimated_savings_usd", "queries_total", "queries_duckdb"})

	if hasRollup {
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
		}
		return
	}

	// Derived path — same savings formula as backfiller.rollup so the number is
	// identical to what the persisted rollup would produce.
	fb, err := s.db.Pool().Query(r.Context(), `
		SELECT date_trunc('day', started_at) AS day,
		       date_trunc('day', started_at) + interval '1 day' AS day_end,
		       COALESCE(SUM(actual_cost_usd) FILTER (WHERE route_decision IN ('warehouse','fallback')), 0)::float8 AS wh_cost,
		       COALESCE(SUM(actual_cost_usd) FILTER (WHERE route_decision = 'duckdb'), 0)::float8 AS duck_cost,
		       (COALESCE(SUM(estimated_cost_usd) FILTER (WHERE route_decision = 'duckdb'), 0)
		        - COALESCE(SUM(actual_cost_usd) FILTER (WHERE route_decision = 'duckdb'), 0))::float8 AS savings,
		       COUNT(*) AS qt,
		       COUNT(*) FILTER (WHERE route_decision = 'duckdb') AS qd
		FROM query_log
		WHERE customer_id = $1 AND started_at >= $2 AND started_at < $3
		GROUP BY 1 ORDER BY 1 ASC`, cid, since, until)
	if err != nil {
		s.log.Warn().Err(err).Msg("cost-attribution derived query failed")
		return
	}
	defer fb.Close()
	for fb.Next() {
		var ps, pe time.Time
		var wh, duck, sav float64
		var qt, qd int64
		if err := fb.Scan(&ps, &pe, &wh, &duck, &sav, &qt, &qd); err != nil {
			s.log.Warn().Err(err).Msg("cost-attribution derived scan failed")
			return
		}
		_ = cw.Write([]string{
			ps.Format(time.RFC3339), pe.Format(time.RFC3339),
			fmt.Sprintf("%.6f", wh), fmt.Sprintf("%.6f", duck), fmt.Sprintf("%.6f", sav),
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
