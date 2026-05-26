// Phase-6 Wave-1 fo-4: detect dashboards whose this-week warehouse cost is
// ≥2× the trailing week's. We do not have a dashboard_id column on query_log
// today, and no query_hash_cache exists yet (TODO: wire one in Phase-6 W2 so
// the join is exact). Until then we attribute cost by FQN/table-name match
// between the dashboard tile SQL and normalized SQL in query_log.
package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CostRegressionAlert is one dashboard's week-over-week warehouse cost
// regression. Multiplier == ThisWeekUSD / max(LastWeekUSD, epsilon).
type CostRegressionAlert struct {
	Dashboard    string  `json:"dashboard"`
	DashboardID  string  `json:"dashboard_id"`
	ThisWeekUSD  float64 `json:"this_week_usd"`
	LastWeekUSD  float64 `json:"last_week_usd"`
	Multiplier   float64 `json:"multiplier"`
}

// regressionMultiplierThreshold is the floor at which we emit an alert. Tuned
// to 2.0 so genuine doublings page; bumpable per-customer in Phase-6 W2.
const regressionMultiplierThreshold = 2.0

// fqnRE picks out `schema.table` and `db.schema.table` tokens from SQL. The
// match is intentionally permissive — we'd rather over-attribute cost than
// miss a regression.
var fqnRE = regexp.MustCompile(`(?i)\b[a-z_][a-z0-9_]*(?:\.[a-z_][a-z0-9_]*){1,2}\b`)

// Detect computes week-over-week dashboard cost regressions for one customer.
// Order: load dashboards → extract FQNs per dashboard → bucket query_log cost
// by FQN for this-week and last-week windows → emit when ratio ≥ threshold.
func Detect(ctx context.Context, pool *pgxpool.Pool, customerID uuid.UUID) ([]CostRegressionAlert, error) {
	dashboards, err := loadDashboardFQNs(ctx, pool, customerID)
	if err != nil {
		return nil, fmt.Errorf("load dashboards: %w", err)
	}
	if len(dashboards) == 0 {
		return []CostRegressionAlert{}, nil
	}

	thisWeek, err := costByFQN(ctx, pool, customerID, "now() - interval '7 days'", "now()")
	if err != nil {
		return nil, fmt.Errorf("this-week cost: %w", err)
	}
	lastWeek, err := costByFQN(ctx, pool, customerID, "now() - interval '14 days'", "now() - interval '7 days'")
	if err != nil {
		return nil, fmt.Errorf("last-week cost: %w", err)
	}

	out := []CostRegressionAlert{}
	for _, d := range dashboards {
		var thisSum, lastSum float64
		for fqn := range d.fqns {
			thisSum += thisWeek[fqn]
			lastSum += lastWeek[fqn]
		}
		// Avoid /0 noise — a brand-new dashboard with $0 last week and any
		// spend this week will trip; require both sides to have signal.
		if lastSum <= 0.0001 {
			continue
		}
		mult := thisSum / lastSum
		if mult >= regressionMultiplierThreshold {
			out = append(out, CostRegressionAlert{
				Dashboard:   d.name,
				DashboardID: d.id.String(),
				ThisWeekUSD: thisSum,
				LastWeekUSD: lastSum,
				Multiplier:  mult,
			})
		}
	}
	return out, nil
}

type dashboardFQNs struct {
	id   uuid.UUID
	name string
	fqns map[string]struct{}
}

func loadDashboardFQNs(ctx context.Context, pool *pgxpool.Pool, customerID uuid.UUID) ([]dashboardFQNs, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, name, tiles FROM dashboard WHERE customer_id = $1`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dashboardFQNs
	for rows.Next() {
		var id uuid.UUID
		var name string
		var tilesJSON []byte
		if err := rows.Scan(&id, &name, &tilesJSON); err != nil {
			return nil, err
		}
		var tiles []struct {
			SQL string `json:"sql"`
		}
		_ = json.Unmarshal(tilesJSON, &tiles)
		fqns := map[string]struct{}{}
		for _, t := range tiles {
			for _, m := range fqnRE.FindAllString(t.SQL, -1) {
				fqns[strings.ToLower(m)] = struct{}{}
			}
		}
		out = append(out, dashboardFQNs{id: id, name: name, fqns: fqns})
	}
	return out, rows.Err()
}

// costByFQN buckets actual_cost_usd by every FQN appearing in normalized_sql.
// A single query that touches N tables is double-counted across N FQNs; that
// is acceptable because we sum within a dashboard, and dashboards almost
// always pin one FQN per tile.
func costByFQN(ctx context.Context, pool *pgxpool.Pool, customerID uuid.UUID, since, until string) (map[string]float64, error) {
	q := fmt.Sprintf(`
		SELECT normalized_sql, COALESCE(actual_cost_usd, estimated_cost_usd, 0)::float8 AS cost
		FROM query_log
		WHERE customer_id = $1
		  AND started_at >= %s
		  AND started_at <  %s
		  AND route_decision = 'warehouse'`, since, until)
	rows, err := pool.Query(ctx, q, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var sql string
		var cost float64
		if err := rows.Scan(&sql, &cost); err != nil {
			return nil, err
		}
		seen := map[string]struct{}{}
		for _, m := range fqnRE.FindAllString(sql, -1) {
			k := strings.ToLower(m)
			if _, dup := seen[k]; dup {
				continue
			}
			seen[k] = struct{}{}
			out[k] += cost
		}
	}
	return out, rows.Err()
}
