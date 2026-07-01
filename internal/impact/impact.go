// Package impact answers "if I drop column X (or table Y), what breaks?" by
// walking the lineage graph downstream until convergence. Powers the GitHub PR
// bot in cmd/lineage-bot.
package impact

import (
	"context"

	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/lineage"
)

type Change struct {
	Kind  string // drop_column|drop_table|rename_column|type_change
	FQN   string
	Detail string
}

type Hit struct {
	FQN      string
	Hops     int
	Why      string // chain of upstream FQNs joined by "→"
	Severity string // "breaking" | "warning" — derived from the change kind
}

type Analyzer struct{ writer *lineage.Writer }

func New(w *lineage.Writer) *Analyzer { return &Analyzer{writer: w} }

// granularityForKind picks which lineage edges to traverse. A column-scoped
// change (drop/rename/type) only propagates along column edges; a table-scoped
// change along table edges.
func granularityForKind(kind string) string {
	switch kind {
	case "drop_column", "rename_column", "type_change":
		return "column"
	default:
		return "table"
	}
}

// severityForKind weights the change so the PR bot's Check Run conclusion is
// driven by the kind of change, not a substring scan. Drops/type-changes/renames
// break downstream references; anything else is advisory.
func severityForKind(kind string) string {
	switch kind {
	case "drop_column", "drop_table", "rename_column", "rename_table", "type_change":
		return "breaking"
	default:
		return "warning"
	}
}

// Preview walks downstream from `change.FQN` up to maxDepth hops, returning
// every node that depends on it. It traverses only edges of the granularity
// implied by change.Kind (column changes follow column edges) so a dropped
// column's blast radius isn't diluted by unrelated table edges. Cycles guarded
// by `seen`.
func (a *Analyzer) Preview(ctx context.Context, customerID uuid.UUID, change Change, maxDepth int) ([]Hit, error) {
	if maxDepth <= 0 {
		maxDepth = 5
	}
	gran := granularityForKind(change.Kind)
	sev := severityForKind(change.Kind)
	seen := map[string]int{change.FQN: 0}
	queue := []string{change.FQN}
	var hits []Hit
	for depth := 0; depth < maxDepth && len(queue) > 0; depth++ {
		var next []string
		for _, fqn := range queue {
			edges, err := a.writer.Downstream(ctx, customerID, fqn)
			if err != nil {
				return nil, err
			}
			for _, e := range edges {
				if e.EdgeType != gran {
					continue
				}
				if _, ok := seen[e.DownstreamFQN]; ok {
					continue
				}
				seen[e.DownstreamFQN] = depth + 1
				hits = append(hits, Hit{FQN: e.DownstreamFQN, Hops: depth + 1, Why: fqn + " → " + e.DownstreamFQN, Severity: sev})
				next = append(next, e.DownstreamFQN)
			}
		}
		queue = next
	}
	return hits, nil
}
