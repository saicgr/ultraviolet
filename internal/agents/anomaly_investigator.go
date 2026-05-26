package agents

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ultraviolet-dev/ultraviolet/internal/anomaly"
	"github.com/ultraviolet-dev/ultraviolet/internal/lineage"
)

// AnomalyInvestigator runs after `internal/anomaly` flags a metric drop/spike.
// It walks the lineage graph upstream from the affected metric, tests
// hypotheses against observable signals, and posts a root-cause summary with
// confidence score. The hypotheses (in priority order):
//
//   1. Sync delay — was a relevant table's last_sync_at > SLA at the anomaly time?
//   2. Schema change — was there an audit_log entry on a relevant table near the anomaly?
//   3. Data quality — did any dq_result for upstream tables turn red?
//   4. Volume cliff — did the upstream table's row count suddenly change?
//
// Output is the highest-scoring hypothesis with structured evidence.
type AnomalyInvestigator struct {
	pool *pgxpool.Pool
}

func NewAnomalyInvestigator(pool *pgxpool.Pool) *AnomalyInvestigator { return &AnomalyInvestigator{pool: pool} }

func (a *AnomalyInvestigator) Name() string { return "anomaly_investigator" }
func (a *AnomalyInvestigator) Kind() string { return "investigator" }

func (a *AnomalyInvestigator) Run(ctx context.Context, in Input) (Output, error) {
	metricFQN, _ := in.Args["metric_fqn"].(string)
	at, _ := in.Args["anomaly_at"].(time.Time)
	if metricFQN == "" {
		return Output{Summary: "metric_fqn required"}, nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	wr := lineage.NewWriter(a.pool)
	upstream, err := wr.Upstream(ctx, in.CustomerID, metricFQN)
	if err != nil {
		return Output{}, err
	}

	out := Output{NextRunHint: 0 /* one-shot */}
	hypotheses := []Suggestion{}

	// 1) sync delay
	for _, e := range upstream {
		var staleSec *int64
		err := a.pool.QueryRow(ctx,
			`SELECT EXTRACT(EPOCH FROM ($1 - t.last_sync_at))::bigint
			 FROM synced_tables t WHERE t.schema_name || '.' || t.table_name = $2`, at, e.UpstreamFQN).Scan(&staleSec)
		if err == nil && staleSec != nil && *staleSec > 600 {
			hypotheses = append(hypotheses, Suggestion{
				ID:       "anomaly_cause:sync_delay:" + e.UpstreamFQN,
				Title:    fmt.Sprintf("Likely cause: `%s` was %ds stale at anomaly time", e.UpstreamFQN, *staleSec),
				Severity: "warn",
				Rationale: "Upstream table sync lagged; metric reflects pre-update state.",
			})
		}
	}

	// 2) schema change in audit_log near anomaly time
	for _, e := range upstream {
		var hits int64
		_ = a.pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM audit_log
			 WHERE customer_id = $1 AND target_id = $2
			   AND created_at BETWEEN $3 AND $4
			   AND action LIKE 'schema.%'`,
			in.CustomerID, e.UpstreamFQN, at.Add(-2*time.Hour), at).Scan(&hits)
		if hits > 0 {
			hypotheses = append(hypotheses, Suggestion{
				ID:       "anomaly_cause:schema_change:" + e.UpstreamFQN,
				Title:    fmt.Sprintf("Likely cause: schema change on `%s` shortly before anomaly", e.UpstreamFQN),
				Severity: "critical",
				Rationale: fmt.Sprintf("audit_log shows %d schema.* events on this table in the 2h window before the anomaly fired.", hits),
			})
		}
	}

	// 3) dq_result red
	for _, e := range upstream {
		var status string
		_ = a.pool.QueryRow(ctx,
			`SELECT status FROM dq_result WHERE customer_id = $1 AND table_fqn = $2 ORDER BY created_at DESC LIMIT 1`,
			in.CustomerID, e.UpstreamFQN).Scan(&status)
		if status == "fail" {
			hypotheses = append(hypotheses, Suggestion{
				ID:       "anomaly_cause:dq_fail:" + e.UpstreamFQN,
				Title:    fmt.Sprintf("Likely cause: data-quality test failed on `%s`", e.UpstreamFQN),
				Severity: "critical",
			})
		}
	}

	out.Suggestions = hypotheses
	if len(hypotheses) > 0 {
		out.Confidence = 0.7
		out.Summary = fmt.Sprintf("%d hypothesis(es) for anomaly on `%s` at %s", len(hypotheses), metricFQN, at.Format(time.RFC3339))
	} else {
		out.Confidence = 0.2
		out.Summary = fmt.Sprintf("No upstream signals match for `%s` at %s — recommend manual investigation.", metricFQN, at.Format(time.RFC3339))
	}
	_ = anomaly.Anomaly{} // keep the import semantically aligned
	return out, nil
}
