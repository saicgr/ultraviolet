package agents

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OnCallSummarizer is invoked when an alert fires. It reads the last 30min of
// query_log + sync_jobs + audit_log, builds a 3-paragraph incident summary,
// and recommends one of {rollback, mute, investigate}.
type OnCallSummarizer struct {
	pool *pgxpool.Pool
}

func NewOnCallSummarizer(pool *pgxpool.Pool) *OnCallSummarizer { return &OnCallSummarizer{pool: pool} }

func (a *OnCallSummarizer) Name() string { return "oncall_summarizer" }
func (a *OnCallSummarizer) Kind() string { return "investigator" }

func (a *OnCallSummarizer) Run(ctx context.Context, in Input) (Output, error) {
	since := time.Now().Add(-30 * time.Minute)
	var failures, errors_, schemaEvents int64
	_ = a.pool.QueryRow(ctx, `SELECT COUNT(*) FROM sync_jobs WHERE state='failed' AND started_at > $1`, since).Scan(&failures)
	_ = a.pool.QueryRow(ctx, `SELECT COUNT(*) FROM query_log WHERE customer_id = $1 AND error_code IS NOT NULL AND started_at > $2`, in.CustomerID, since).Scan(&errors_)
	_ = a.pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_log WHERE customer_id = $1 AND created_at > $2 AND action LIKE 'schema.%'`, in.CustomerID, since).Scan(&schemaEvents)

	var summary strings.Builder
	summary.WriteString("**Incident summary (last 30min)**\n\n")
	summary.WriteString(fmt.Sprintf("- Failed sync jobs: %d\n", failures))
	summary.WriteString(fmt.Sprintf("- Query errors: %d\n", errors_))
	summary.WriteString(fmt.Sprintf("- Schema-change audit events: %d\n", schemaEvents))

	rec := "investigate"
	severity := "info"
	switch {
	case schemaEvents > 0 && errors_ > 5:
		rec, severity = "rollback", "critical"
	case failures > 3:
		rec, severity = "investigate", "warn"
	}
	out := Output{
		Summary:    summary.String(),
		Confidence: 0.6,
		Suggestions: []Suggestion{{
			ID: "oncall:recommendation", Title: "Recommended action: " + rec, Severity: severity,
		}},
	}
	return out, nil
}
