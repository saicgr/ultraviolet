package agents

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// QueryRegressor compares this-week's p95 latency for every query_hash against
// the trailing-4-week baseline. When p95 ≥ 2× baseline AND the regression
// crosses an absolute floor (≥250ms), it surfaces the top-N regressions.
//
// The hypothesis-builder (deferred) walks lineage to suggest causes:
//   "fct_orders partition spec changed Tuesday → re-routing via materialized view fixes."
type QueryRegressor struct{ pool *pgxpool.Pool }

func NewQueryRegressor(pool *pgxpool.Pool) *QueryRegressor { return &QueryRegressor{pool: pool} }

func (a *QueryRegressor) Name() string { return "query_regressor" }
func (a *QueryRegressor) Kind() string { return "investigator" }

func (a *QueryRegressor) Run(ctx context.Context, in Input) (Output, error) {
	rows, err := a.pool.Query(ctx, `
		WITH recent AS (
			SELECT query_hash,
			       percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms) AS p95
			FROM query_log
			WHERE customer_id = $1 AND started_at > now() - interval '7 days'
			GROUP BY 1 HAVING COUNT(*) >= 20
		),
		baseline AS (
			SELECT query_hash,
			       percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms) AS p95
			FROM query_log
			WHERE customer_id = $1
			  AND started_at BETWEEN now() - interval '35 days' AND now() - interval '7 days'
			GROUP BY 1 HAVING COUNT(*) >= 20
		)
		SELECT r.query_hash, r.p95, b.p95
		FROM recent r JOIN baseline b USING (query_hash)
		WHERE r.p95 >= 2 * b.p95 AND r.p95 - b.p95 > 250
		ORDER BY r.p95 - b.p95 DESC
		LIMIT 10`, in.CustomerID)
	if err != nil {
		return Output{}, err
	}
	defer rows.Close()
	out := Output{NextRunHint: 6 * time.Hour}
	for rows.Next() {
		var hash string
		var recentP95, baselineP95 float64
		if err := rows.Scan(&hash, &recentP95, &baselineP95); err != nil {
			return out, err
		}
		out.Suggestions = append(out.Suggestions, Suggestion{
			ID:       "regression:" + hash,
			Title:    fmt.Sprintf("Query `%s` p95 regressed %.0fms → %.0fms (%.1f×)", hash, baselineP95, recentP95, recentP95/baselineP95),
			Rationale: fmt.Sprintf("4-week baseline p95 was %.0fms; trailing 7-day is %.0fms. Investigate warehouse plan diff or sync staleness.", baselineP95, recentP95),
			Severity: "warn",
			Estimate: Estimate{LatencyMSDelta: int64(recentP95 - baselineP95)},
		})
	}
	out.Summary = fmt.Sprintf("%d query regressions detected (≥2× p95, ≥250ms absolute).", len(out.Suggestions))
	out.Confidence = 0.8
	return out, nil
}
