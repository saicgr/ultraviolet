package agents

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ultraviolet-dev/ultraviolet/internal/ai"
	"github.com/ultraviolet-dev/ultraviolet/internal/metadata"
)

// AutoDocumenter finds tables in the catalog without descriptions, generates
// candidate descriptions from {column names + N recent queries that touch the
// table + a sample of distinct values}, and inserts them as `source='manual'`
// rows for human review (PR-style).
type AutoDocumenter struct {
	pool     *pgxpool.Pool
	rewriter *ai.Rewriter
	enrich   *metadata.Enricher
}

func NewAutoDocumenter(pool *pgxpool.Pool, r *ai.Rewriter, e *metadata.Enricher) *AutoDocumenter {
	return &AutoDocumenter{pool: pool, rewriter: r, enrich: e}
}

func (a *AutoDocumenter) Name() string { return "auto_documenter" }
func (a *AutoDocumenter) Kind() string { return "productive" }

func (a *AutoDocumenter) Run(ctx context.Context, in Input) (Output, error) {
	rows, err := a.pool.Query(ctx, `
		SELECT t.id, t.schema_name || '.' || t.table_name AS fqn
		FROM synced_tables t JOIN connections c ON c.id = t.connection_id
		WHERE c.customer_id = $1
		  AND NOT EXISTS (
		      SELECT 1 FROM table_metadata m WHERE m.customer_id = $1 AND m.fqn = t.schema_name || '.' || t.table_name
		  )
		LIMIT 25`, in.CustomerID)
	if err != nil {
		return Output{}, err
	}
	defer rows.Close()
	out := Output{}
	for rows.Next() {
		var id uuid.UUID
		var fqn string
		if err := rows.Scan(&id, &fqn); err != nil {
			return out, err
		}
		ctxQueries := a.recentQueriesFor(ctx, in.CustomerID, fqn)
		desc := a.generateDescription(ctx, in.UserSlug, fqn, ctxQueries)
		out.Suggestions = append(out.Suggestions, Suggestion{
			ID:       "doc:" + fqn,
			Title:    fmt.Sprintf("Propose description for `%s`", fqn),
			Diff:     desc,
			Severity: "info",
		})
		// In auto-apply mode, insert as proposal so a human can review:
		if !in.DryRun && a.enrich != nil {
			_ = a.enrich.Apply(ctx, in.CustomerID, []metadata.TableMeta{{
				FQN: fqn, Description: desc, Source: "manual",
			}})
		}
	}
	out.Summary = fmt.Sprintf("Generated %d table description proposal(s).", len(out.Suggestions))
	out.Confidence = 0.5
	return out, nil
}

func (a *AutoDocumenter) recentQueriesFor(ctx context.Context, customerID uuid.UUID, fqn string) []string {
	rows, err := a.pool.Query(ctx,
		`SELECT normalized_sql FROM query_log
		 WHERE customer_id = $1 AND normalized_sql ILIKE '%' || $2 || '%'
		 ORDER BY started_at DESC LIMIT 5`, customerID, fqn)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err == nil {
			out = append(out, s)
		}
	}
	return out
}

func (a *AutoDocumenter) generateDescription(ctx context.Context, userSlug, fqn string, queries []string) string {
	if a.rewriter == nil {
		return fmt.Sprintf("Auto-doc placeholder for %s (no LLM provider configured).", fqn)
	}
	prompt := fmt.Sprintf(
		`You are a data analyst writing concise table documentation.
Table: %s
Recent queries touching this table:
%v

Write a 2-sentence description: what this table represents + the most-likely grain (one row per X).`,
		fqn, queries)
	out, err := a.rewriter.CompleteOne(ctx, "gpt-4o-mini", prompt, userSlug)
	if err != nil {
		return fmt.Sprintf("(LLM unavailable: %v)", err)
	}
	return out
}
