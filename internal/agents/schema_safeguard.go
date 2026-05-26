package agents

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ultraviolet-dev/ultraviolet/internal/impact"
	"github.com/ultraviolet-dev/ultraviolet/internal/lineage"
)

// SchemaSafeguard runs on every PR commit (driven by cmd/lineage-bot's
// synchronize webhook). Walks the diff, extracts schema-changing statements,
// computes blast radius via lineage, returns a structured PR comment block.
type SchemaSafeguard struct {
	pool *pgxpool.Pool
}

func NewSchemaSafeguard(pool *pgxpool.Pool) *SchemaSafeguard { return &SchemaSafeguard{pool: pool} }

func (a *SchemaSafeguard) Name() string { return "schema_safeguard" }
func (a *SchemaSafeguard) Kind() string { return "loop_closing" }

func (a *SchemaSafeguard) Run(ctx context.Context, in Input) (Output, error) {
	diff, _ := in.Args["diff"].(string)
	out := Output{}
	changes := extractSchemaChanges(diff)
	if len(changes) == 0 {
		out.Summary = "No schema-changing statements detected in diff."
		return out, nil
	}
	wr := lineage.NewWriter(a.pool)
	an := impact.New(wr)
	for _, ch := range changes {
		hits, err := an.Preview(ctx, in.CustomerID, ch, 5)
		if err != nil {
			continue
		}
		sev := "info"
		if len(hits) > 0 {
			sev = "warn"
		}
		if len(hits) >= 5 {
			sev = "critical"
		}
		out.Suggestions = append(out.Suggestions, Suggestion{
			ID:       fmt.Sprintf("schema:%s:%s", ch.Kind, ch.FQN),
			Title:    fmt.Sprintf("`%s %s` impacts %d downstream node(s)", ch.Kind, ch.FQN, len(hits)),
			Rationale: renderHits(hits),
			Severity: sev,
		})
	}
	out.Summary = fmt.Sprintf("Detected %d schema change(s); produced %d suggestion(s).", len(changes), len(out.Suggestions))
	out.Confidence = 0.85
	return out, nil
}

var (
	dropColRe = regexp.MustCompile(`(?i)ALTER\s+TABLE\s+(\S+)\s+DROP\s+COLUMN\s+(\w+)`)
	dropTblRe = regexp.MustCompile(`(?i)DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?(\S+)`)
	renameRe  = regexp.MustCompile(`(?i)ALTER\s+TABLE\s+(\S+)\s+RENAME\s+COLUMN\s+(\w+)\s+TO\s+(\w+)`)
	typeRe    = regexp.MustCompile(`(?i)ALTER\s+TABLE\s+(\S+)\s+ALTER\s+COLUMN\s+(\w+)\s+(?:SET\s+DATA\s+)?TYPE`)
)

func extractSchemaChanges(diff string) []impact.Change {
	var out []impact.Change
	for _, m := range dropColRe.FindAllStringSubmatch(diff, -1) {
		out = append(out, impact.Change{Kind: "drop_column", FQN: m[1] + "." + m[2]})
	}
	for _, m := range dropTblRe.FindAllStringSubmatch(diff, -1) {
		out = append(out, impact.Change{Kind: "drop_table", FQN: strings.TrimSuffix(m[1], ";")})
	}
	for _, m := range renameRe.FindAllStringSubmatch(diff, -1) {
		out = append(out, impact.Change{Kind: "rename_column", FQN: m[1] + "." + m[2], Detail: "→ " + m[3]})
	}
	for _, m := range typeRe.FindAllStringSubmatch(diff, -1) {
		out = append(out, impact.Change{Kind: "type_change", FQN: m[1] + "." + m[2]})
	}
	return out
}

func renderHits(hits []impact.Hit) string {
	if len(hits) == 0 {
		return "_No downstream consumers — safe._"
	}
	var b strings.Builder
	for _, h := range hits {
		b.WriteString(fmt.Sprintf("- `%s` (%d hops): %s\n", h.FQN, h.Hops, h.Why))
	}
	return b.String()
}
