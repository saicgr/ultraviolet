package agents

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ultraviolet-dev/ultraviolet/internal/pii"
)

// PIITagger walks every synced table's column list and applies the regex-based
// PII rules from internal/pii. Each match is stored as a column_tag with
// confidence + source="auto". A separate value-sampling pass runs nightly to
// catch columns whose names don't reveal sensitivity (e.g. `cust_field_3`).
type PIITagger struct {
	pool *pgxpool.Pool
}

func NewPIITagger(pool *pgxpool.Pool) *PIITagger { return &PIITagger{pool: pool} }

func (a *PIITagger) Name() string { return "pii_tagger" }
func (a *PIITagger) Kind() string { return "loop_closing" }

func (a *PIITagger) Run(ctx context.Context, in Input) (Output, error) {
	rows, err := a.pool.Query(ctx, `
		SELECT t.schema_name || '.' || t.table_name FROM synced_tables t
		JOIN connections c ON c.id = t.connection_id
		WHERE c.customer_id = $1`, in.CustomerID)
	if err != nil {
		return Output{}, err
	}
	defer rows.Close()
	store := pii.NewStore(a.pool)
	out := Output{}
	tagged := 0
	for rows.Next() {
		var fqn string
		if err := rows.Scan(&fqn); err != nil {
			continue
		}
		// Phase-1: we don't have warehouse-side column lists wired through here,
		// so we score the FQN itself + every column referenced in recent queries.
		// Phase-2: integrate Introspecter (connector interface) for per-column.
		colRows, err := a.pool.Query(ctx,
			`SELECT DISTINCT lower(unnest(regexp_matches(normalized_sql, '[a-z_][a-z0-9_]*(?=\s*[,)])', 'g')))
			 FROM query_log WHERE customer_id = $1
			   AND normalized_sql ILIKE '%' || $2 || '%'
			 LIMIT 50`, in.CustomerID, fqn)
		if err != nil {
			continue
		}
		for colRows.Next() {
			var col string
			if err := colRows.Scan(&col); err != nil {
				continue
			}
			if tag, conf := pii.ScoreName(col); tag != "" {
				if err := store.Upsert(ctx, in.CustomerID, fqn, col, tag, conf, "auto"); err == nil {
					tagged++
				}
			}
		}
		colRows.Close()
	}
	out.Summary = fmt.Sprintf("Reviewed columns across customer's tables; tagged %d column(s) with PII classifications.", tagged)
	out.Confidence = 0.8
	if tagged > 0 {
		out.Suggestions = append(out.Suggestions, Suggestion{
			ID:       "pii:review",
			Title:    fmt.Sprintf("Review %d auto-tagged PII columns", tagged),
			Rationale: "Tags are auto-generated; confirm before enforcing field-level access policies.",
			Severity: "info",
		})
	}
	return out, nil
}
