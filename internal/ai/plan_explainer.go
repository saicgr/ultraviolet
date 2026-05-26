// Package ai — plan_explainer.go: LLM-backed diff explainer for two query_log rows.
//
// Use case: an operator sees two runs of "the same" query (same query_hash) with
// wildly different cost or latency and wants a human-readable explanation. We
// fetch both rows, compute a textual diff of the key cost/perf fields, and ask
// the LLM for a 3-sentence summary.
//
// Hard guardrail: if the two rows have different query_hash values we refuse —
// the comparison would be apples-to-oranges. Returning an error keeps callers
// honest (project rule: no silent degradation).
package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PlanExplainer struct {
	rw   *Rewriter
	pool *pgxpool.Pool
}

func NewPlanExplainer(rw *Rewriter, pool *pgxpool.Pool) *PlanExplainer {
	return &PlanExplainer{rw: rw, pool: pool}
}

type planRow struct {
	queryHash     string
	normalizedSQL string
	durationMS    int
	bytesScanned  *int64
	actualCost    *float64
	routeDecision string
}

// Explain fetches the latest two query_log rows for the given hashes and returns
// an LLM-generated 3-sentence explanation of the cost/perf delta.
func (p *PlanExplainer) Explain(ctx context.Context, customerID uuid.UUID, queryHashA, queryHashB string) (string, error) {
	if p.rw == nil || p.rw.provider == nil {
		return "", ErrNoProvider
	}
	if p.pool == nil {
		return "", errors.New("plan_explainer: nil pool")
	}
	if strings.TrimSpace(queryHashA) == "" || strings.TrimSpace(queryHashB) == "" {
		return "", errors.New("plan_explainer: empty query hash")
	}

	a, err := p.fetchRow(ctx, customerID, queryHashA)
	if err != nil {
		return "", fmt.Errorf("plan_explainer: fetch A: %w", err)
	}
	b, err := p.fetchRow(ctx, customerID, queryHashB)
	if err != nil {
		return "", fmt.Errorf("plan_explainer: fetch B: %w", err)
	}

	// Refuse cross-query diffs. query_hash is the canonical identity of a
	// normalized statement; mismatched hashes mean the operator is comparing
	// two different queries and the LLM answer would be misleading.
	if a.queryHash != b.queryHash {
		return "", errors.New("plan_explainer: diff requested across different queries")
	}

	prompt := buildPlanExplainerPrompt(a, b)
	out, err := p.rw.CompleteOne(ctx, p.rw.cfg.AIDefaultModel, prompt, customerID.String())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (p *PlanExplainer) fetchRow(ctx context.Context, customerID uuid.UUID, qhash string) (planRow, error) {
	var r planRow
	err := p.pool.QueryRow(ctx, `
		SELECT query_hash, normalized_sql, duration_ms, bytes_scanned, actual_cost_usd, route_decision
		FROM query_log
		WHERE customer_id = $1 AND query_hash = $2
		ORDER BY started_at DESC
		LIMIT 1`, customerID, qhash).Scan(
		&r.queryHash, &r.normalizedSQL, &r.durationMS, &r.bytesScanned, &r.actualCost, &r.routeDecision)
	return r, err
}

func buildPlanExplainerPrompt(a, b planRow) string {
	var sb strings.Builder
	sb.WriteString("Two runs of the same query had different cost. Explain in 3 sentences why.\n\n")
	sb.WriteString("Normalized SQL:\n")
	sb.WriteString(a.normalizedSQL)
	sb.WriteString("\n\nRun A:\n")
	writeRun(&sb, a)
	sb.WriteString("\nRun B:\n")
	writeRun(&sb, b)
	sb.WriteString("\nDeltas:\n")
	fmt.Fprintf(&sb, "  duration_ms: %d -> %d (delta %+d)\n", a.durationMS, b.durationMS, b.durationMS-a.durationMS)
	fmt.Fprintf(&sb, "  bytes_scanned: %s -> %s\n", fmtNullableInt(a.bytesScanned), fmtNullableInt(b.bytesScanned))
	fmt.Fprintf(&sb, "  actual_cost_usd: %s -> %s\n", fmtNullableFloat(a.actualCost), fmtNullableFloat(b.actualCost))
	fmt.Fprintf(&sb, "  route_decision: %s -> %s\n", a.routeDecision, b.routeDecision)
	sb.WriteString("\nKeep the answer to exactly 3 sentences.")
	return sb.String()
}

func writeRun(sb *strings.Builder, r planRow) {
	fmt.Fprintf(sb, "  duration_ms=%d\n", r.durationMS)
	fmt.Fprintf(sb, "  bytes_scanned=%s\n", fmtNullableInt(r.bytesScanned))
	fmt.Fprintf(sb, "  actual_cost_usd=%s\n", fmtNullableFloat(r.actualCost))
	fmt.Fprintf(sb, "  route_decision=%s\n", r.routeDecision)
}

func fmtNullableInt(v *int64) string {
	if v == nil {
		return "null"
	}
	return fmt.Sprintf("%d", *v)
}

func fmtNullableFloat(v *float64) string {
	if v == nil {
		return "null"
	}
	return fmt.Sprintf("%.6f", *v)
}
