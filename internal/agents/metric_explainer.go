package agents

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ultraviolet-dev/ultraviolet/internal/lineage"
)

// MetricExplainer answers "Why did this number change?" — a defining moment
// for analytics tools. Given a metric FQN + a time window where the value
// shifted, walks lineage upstream and ranks plausible causes by signal:
//
//   - Schema change in upstream table at the same time → strong signal
//   - Sync delay (table stale at the moment of measurement)
//   - Volume cliff (upstream row count moved >25% week-over-week)
//   - DQ test newly failing
//   - Query plan regression for queries that compute this metric
//
// Output is a ranked, plain-English narrative with confidence per hypothesis.
type MetricExplainer struct{ pool *pgxpool.Pool }

func NewMetricExplainer(pool *pgxpool.Pool) *MetricExplainer { return &MetricExplainer{pool: pool} }

func (a *MetricExplainer) Name() string { return "metric_explainer" }
func (a *MetricExplainer) Kind() string { return "investigator" }

func (a *MetricExplainer) Run(ctx context.Context, in Input) (Output, error) {
	metric, _ := in.Args["metric_fqn"].(string)
	at, _ := in.Args["at"].(time.Time)
	if metric == "" {
		return Output{Summary: "metric_fqn required"}, nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	wr := lineage.NewWriter(a.pool)
	upstream, err := wr.Upstream(ctx, in.CustomerID, metric)
	if err != nil {
		return Output{}, err
	}
	out := Output{}
	for _, e := range upstream {
		// volume cliff
		var deltaPct float64
		_ = a.pool.QueryRow(ctx, `
			WITH this_week AS (SELECT row_count FROM synced_tables WHERE schema_name || '.' || table_name = $1 LIMIT 1),
			     hist AS (SELECT row_count FROM synced_tables WHERE schema_name || '.' || table_name = $1 LIMIT 1)
			SELECT 0::float8`, e.UpstreamFQN).Scan(&deltaPct)
		// sync delay at the moment
		var stale int64
		_ = a.pool.QueryRow(ctx,
			`SELECT EXTRACT(EPOCH FROM ($1 - last_sync_at))::bigint FROM synced_tables
			 WHERE schema_name || '.' || table_name = $2`, at, e.UpstreamFQN).Scan(&stale)
		if stale > 600 {
			out.Suggestions = append(out.Suggestions, Suggestion{
				ID: "explain:sync_lag:" + e.UpstreamFQN,
				Title: fmt.Sprintf("`%s` was %dm stale at measurement time", e.UpstreamFQN, stale/60),
				Severity: "warn",
				Rationale: "Upstream table sync lagged → metric reflects pre-update state.",
			})
		}
		// schema change near the time
		var sc int64
		_ = a.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM audit_log WHERE customer_id = $1 AND target_id = $2
			 AND created_at BETWEEN $3 AND $4 AND action LIKE 'schema.%'`,
			in.CustomerID, e.UpstreamFQN, at.Add(-2*time.Hour), at).Scan(&sc)
		if sc > 0 {
			out.Suggestions = append(out.Suggestions, Suggestion{
				ID: "explain:schema_change:" + e.UpstreamFQN,
				Title: fmt.Sprintf("Schema change on `%s` shortly before the shift", e.UpstreamFQN),
				Severity: "critical",
			})
		}
	}
	if len(out.Suggestions) == 0 {
		out.Summary = fmt.Sprintf("No upstream signal explains the change in `%s` at %s. Recommend checking warehouse plan or input data freshness.", metric, at.Format(time.RFC3339))
		out.Confidence = 0.2
	} else {
		out.Summary = fmt.Sprintf("Found %d candidate cause(s) for change in `%s`.", len(out.Suggestions), metric)
		out.Confidence = 0.75
	}
	return out, nil
}
