package agents

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SyncAutopilot decides per-table whether to use watermark / CDC / full reload,
// adjusts sync cadence based on observed query patterns, pauses cold tables,
// resumes when first query hits.
//
// Heuristic (Phase-1):
//   - queried > 10×/hour and < 5min staleness ok  → CDC
//   - queried 1–10×/hour with 1h staleness ok      → watermark @ 5min cadence
//   - queried < 1×/hour                            → watermark @ 1h cadence
//   - not queried in 7d                            → pause sync; resume on first hit
type SyncAutopilot struct{ pool *pgxpool.Pool }

func NewSyncAutopilot(pool *pgxpool.Pool) *SyncAutopilot { return &SyncAutopilot{pool: pool} }

func (a *SyncAutopilot) Name() string { return "sync_autopilot" }
func (a *SyncAutopilot) Kind() string { return "loop_closing" }

func (a *SyncAutopilot) Run(ctx context.Context, in Input) (Output, error) {
	rows, err := a.pool.Query(ctx, `
		SELECT
			t.id,
			t.schema_name || '.' || t.table_name AS fqn,
			t.sync_mode,
			COALESCE((SELECT COUNT(*) FROM query_log q
			          WHERE q.customer_id = $1 AND q.normalized_sql ILIKE '%' || t.schema_name || '.' || t.table_name || '%'
			            AND q.started_at > now() - interval '24 hours'), 0) AS hits_24h,
			COALESCE(EXTRACT(EPOCH FROM (now() - t.last_sync_at))::bigint, 999999999) AS seconds_stale
		FROM synced_tables t JOIN connections c ON c.id = t.connection_id
		WHERE c.customer_id = $1 AND t.state = 'live'`, in.CustomerID)
	if err != nil {
		return Output{}, err
	}
	defer rows.Close()
	out := Output{NextRunHint: time.Hour}
	for rows.Next() {
		var id, fqn, mode string
		var hits24h, secsStale int64
		if err := rows.Scan(&id, &fqn, &mode, &hits24h, &secsStale); err != nil {
			return out, err
		}
		hitsPerHr := float64(hits24h) / 24.0
		desired := desiredMode(hitsPerHr)
		if desired == mode {
			continue
		}
		out.Suggestions = append(out.Suggestions, Suggestion{
			ID:       "sync_mode:" + id,
			Title:    fmt.Sprintf("Switch `%s` from %s → %s (%.1f hits/hr)", fqn, mode, desired, hitsPerHr),
			Rationale: fmt.Sprintf("Last 24h: %d queries hit this table. Cadence/mode mismatch with usage.", hits24h),
			Severity: "info",
		})
	}
	out.Summary = fmt.Sprintf("Reviewed sync cadence for live tables; %d adjustments suggested.", len(out.Suggestions))
	out.Confidence = 0.6
	return out, nil
}

func desiredMode(hitsPerHr float64) string {
	switch {
	case hitsPerHr >= 10:
		return "cdc_native"
	case hitsPerHr >= 1:
		return "watermark"
	default:
		return "manual"
	}
}
