package agents

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CostOptimizer reads query_log + sync state + lineage and proposes specific
// dollar-quantified changes:
//
//   1. Stop syncing tables not queried in N days (saves storage + sync cost).
//   2. Materialize hot multi-join queries (cuts repeated bytes-scanned).
//   3. Add filter pushdown on dashboards consistently scanning > X bytes.
//   4. Promote tables from warehouse to DuckDB-Iceberg routing when freshness allows.
//
// The agent only proposes; AutoApply mode opens a draft PR-style proposal in
// `agent_proposal` rather than mutating sync config silently.
type CostOptimizer struct {
	pool *pgxpool.Pool
}

func NewCostOptimizer(pool *pgxpool.Pool) *CostOptimizer { return &CostOptimizer{pool: pool} }

func (a *CostOptimizer) Name() string { return "cost_optimizer" }
func (a *CostOptimizer) Kind() string { return "loop_closing" }

func (a *CostOptimizer) Run(ctx context.Context, in Input) (Output, error) {
	window := in.Window
	if window == 0 {
		window = 30 * 24 * time.Hour
	}

	out := Output{NextRunHint: 24 * time.Hour}

	// 1) Tables not queried in `window` but still being synced.
	cold, err := a.coldSyncedTables(ctx, in.CustomerID, window)
	if err != nil {
		return out, err
	}
	for _, t := range cold {
		out.Suggestions = append(out.Suggestions, Suggestion{
			ID:       fmt.Sprintf("cold-sync:%s", t.FQN),
			Title:    fmt.Sprintf("Stop syncing `%s` — last queried %s ago", t.FQN, humanDuration(t.SinceLast)),
			Rationale: fmt.Sprintf("query_log shows zero hits in %s; sync still ticking every 60s.", humanDuration(window)),
			Severity: "warn",
			Estimate: Estimate{
				SavingsUSDPerMonth: t.MonthlyCostUSD,
				BytesScannedDelta:  -t.LastSyncBytes,
			},
		})
	}

	// 2) Hot repeated joins that aren't materialized.
	hot, err := a.hotRepeatedJoins(ctx, in.CustomerID, window)
	if err != nil {
		return out, err
	}
	for _, h := range hot {
		out.Suggestions = append(out.Suggestions, Suggestion{
			ID:       fmt.Sprintf("materialize:%s", h.QueryHash),
			Title:    fmt.Sprintf("Materialize a view for query hit %d×/week", h.Hits),
			Rationale: fmt.Sprintf("Same join shape (`%s`) ran %d times scanning %d MB total. Materializing cuts repeated cost.", h.QueryHash, h.Hits, h.BytesScanned/1e6),
			Severity: "info",
			Estimate: Estimate{
				SavingsUSDPerMonth: h.MonthlyCostUSD * 0.85, // assume 85% of cost goes away
			},
		})
	}

	out.Summary = fmt.Sprintf("Reviewed %d cold tables and %d hot query shapes; %d actionable suggestions.",
		len(cold), len(hot), len(out.Suggestions))
	out.Confidence = 0.7
	return out, nil
}

type coldTable struct {
	FQN            string
	SinceLast      time.Duration
	MonthlyCostUSD float64
	LastSyncBytes  int64
}

func (a *CostOptimizer) coldSyncedTables(ctx context.Context, customerID uuid.UUID, window time.Duration) ([]coldTable, error) {
	rows, err := a.pool.Query(ctx, `
		WITH last_query AS (
			SELECT
				LOWER(unnest(regexp_matches(normalized_sql, '[a-z_][a-z0-9_]*\.[a-z_][a-z0-9_]*', 'g'))) AS fqn,
				MAX(started_at) AS last_at
			FROM query_log WHERE customer_id = $1 GROUP BY 1
		)
		SELECT
			(t.schema_name || '.' || t.table_name) AS fqn,
			COALESCE(EXTRACT(EPOCH FROM (now() - lq.last_at))::bigint, 999999999) AS seconds_since,
			COALESCE(t.row_count * 0.000005, 0)::float8 AS monthly_cost_usd
		FROM synced_tables t
		JOIN connections c ON c.id = t.connection_id
		LEFT JOIN last_query lq ON lq.fqn = LOWER(t.schema_name || '.' || t.table_name)
		WHERE c.customer_id = $1
		  AND (lq.last_at IS NULL OR lq.last_at < now() - $2::interval)
		LIMIT 100`, customerID, window)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []coldTable
	for rows.Next() {
		var c coldTable
		var secs int64
		if err := rows.Scan(&c.FQN, &secs, &c.MonthlyCostUSD); err != nil {
			return nil, err
		}
		c.SinceLast = time.Duration(secs) * time.Second
		out = append(out, c)
	}
	return out, rows.Err()
}

type hotQuery struct {
	QueryHash       string
	Hits            int64
	BytesScanned    int64
	MonthlyCostUSD  float64
}

func (a *CostOptimizer) hotRepeatedJoins(ctx context.Context, customerID uuid.UUID, window time.Duration) ([]hotQuery, error) {
	rows, err := a.pool.Query(ctx, `
		SELECT query_hash, COUNT(*), COALESCE(SUM(bytes_scanned),0), COALESCE(SUM(actual_cost_usd),0)
		FROM query_log
		WHERE customer_id = $1 AND started_at > now() - $2::interval
		GROUP BY 1
		HAVING COUNT(*) > 50
		ORDER BY 4 DESC NULLS LAST
		LIMIT 10`, customerID, window)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []hotQuery
	for rows.Next() {
		var h hotQuery
		if err := rows.Scan(&h.QueryHash, &h.Hits, &h.BytesScanned, &h.MonthlyCostUSD); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func humanDuration(d time.Duration) string {
	switch {
	case d > 30*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/24/30))
	case d > 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d > time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
}
