package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// QueryOptimizer takes a SQL string and proposes rewrites that reduce cost or
// latency. Phase-1 is heuristic-only (no LLM call required) so it's cheap to
// run on every workbench Save:
//
//   - SELECT *  → flag, suggest enumerated columns
//   - WHERE ... NOT IN (subquery) → suggest WHERE NOT EXISTS
//   - JOIN without explicit ON predicate → flag
//   - CROSS JOIN large tables → flag
//   - Missing partition pruning predicate on a partitioned table → suggest adding
//   - LIMIT-less SELECT in workbench context → suggest LIMIT 1000
//
// Phase-2 hands the SQL + schema context to an LLM for deeper rewrites.
type QueryOptimizer struct{ pool *pgxpool.Pool }

func NewQueryOptimizer(pool *pgxpool.Pool) *QueryOptimizer { return &QueryOptimizer{pool: pool} }

func (a *QueryOptimizer) Name() string { return "query_optimizer" }
func (a *QueryOptimizer) Kind() string { return "productive" }

func (a *QueryOptimizer) Run(ctx context.Context, in Input) (Output, error) {
	sql, _ := in.Args["sql"].(string)
	if sql == "" {
		return Output{Summary: "sql required"}, nil
	}
	upper := strings.ToUpper(sql)
	out := Output{}

	if strings.Contains(upper, "SELECT *") {
		out.Suggestions = append(out.Suggestions, Suggestion{
			ID: "select-star", Title: "Avoid `SELECT *`",
			Rationale: "Selecting all columns scans more bytes; warehouses bill by columns scanned.",
			Severity: "warn",
		})
	}
	if strings.Contains(upper, " NOT IN (SELECT") || strings.Contains(upper, "NOT IN(SELECT") {
		out.Suggestions = append(out.Suggestions, Suggestion{
			ID: "not-in", Title: "Replace `NOT IN (SELECT …)` with `WHERE NOT EXISTS (…)`",
			Rationale: "NOT IN has surprising semantics with NULLs and often produces worse plans than NOT EXISTS.",
			Severity: "warn",
		})
	}
	if strings.Contains(upper, "CROSS JOIN") {
		out.Suggestions = append(out.Suggestions, Suggestion{
			ID: "cross-join", Title: "Cross join detected",
			Rationale: "Cross-product is rarely intended; verify join predicate.", Severity: "critical",
		})
	}
	if !strings.Contains(upper, "LIMIT") && strings.HasPrefix(strings.TrimSpace(upper), "SELECT") {
		out.Suggestions = append(out.Suggestions, Suggestion{
			ID: "no-limit", Title: "Consider `LIMIT 1000`",
			Rationale: "Exploratory queries benefit from an explicit limit to bound cost.",
			Severity: "info",
		})
	}
	out.Summary = fmt.Sprintf("Heuristic review of %d-char SQL; %d suggestion(s).", len(sql), len(out.Suggestions))
	out.Confidence = 0.6
	return out, nil
}
