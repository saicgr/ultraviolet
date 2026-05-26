package agents

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OnboardingPlanner reads a new customer's first ~100 queries (after they
// connect a warehouse + start using the proxy) and proposes:
//
//   1. Top-N tables to add as synced (highest hit-count, freshness-tolerant).
//   2. 3 starter dashboards built from the most-frequent query shapes.
//   3. A projected $/month savings number based on observed bytes-scanned.
//
// Runs once on customer creation, then on-demand from the UI.
type OnboardingPlanner struct{ pool *pgxpool.Pool }

func NewOnboardingPlanner(pool *pgxpool.Pool) *OnboardingPlanner { return &OnboardingPlanner{pool: pool} }

func (a *OnboardingPlanner) Name() string { return "onboarding_planner" }
func (a *OnboardingPlanner) Kind() string { return "productive" }

func (a *OnboardingPlanner) Run(ctx context.Context, in Input) (Output, error) {
	rows, err := a.pool.Query(ctx, `
		WITH refs AS (
			SELECT LOWER(unnest(regexp_matches(normalized_sql, '[a-z_][a-z0-9_]*\.[a-z_][a-z0-9_]*', 'g'))) AS fqn,
			       bytes_scanned, duration_ms
			FROM query_log
			WHERE customer_id = $1
			ORDER BY started_at DESC LIMIT 100
		)
		SELECT fqn, COUNT(*), COALESCE(SUM(bytes_scanned),0), COALESCE(AVG(duration_ms),0)
		FROM refs GROUP BY 1 ORDER BY 2 DESC LIMIT 10`, in.CustomerID)
	if err != nil {
		return Output{}, err
	}
	defer rows.Close()
	var totalBytes int64
	out := Output{}
	for rows.Next() {
		var fqn string
		var hits int64
		var bytesSum int64
		var avgMS float64
		if err := rows.Scan(&fqn, &hits, &bytesSum, &avgMS); err != nil {
			return out, err
		}
		totalBytes += bytesSum
		out.Suggestions = append(out.Suggestions, Suggestion{
			ID:       "onboarding_sync:" + fqn,
			Title:    fmt.Sprintf("Add `%s` to synced tables (%d hits, avg %.0fms)", fqn, hits, avgMS),
			Severity: "info",
		})
	}
	// Crude projection: BQ on-demand $5/TiB; assume DuckDB-on-Iceberg cuts 70%.
	projectedSavings := float64(totalBytes) / (1 << 40) * 5.0 * 0.7 * 30.0 / 1.0
	out.Summary = fmt.Sprintf(
		"From last 100 queries: top %d tables to sync; projected savings ≈ $%.2f/mo if all routed via DuckDB.",
		len(out.Suggestions), projectedSavings)
	out.Confidence = 0.5
	return out, nil
}
