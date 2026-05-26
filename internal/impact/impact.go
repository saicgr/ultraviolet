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
	FQN   string
	Hops  int
	Why   string // chain of upstream FQNs joined by "→"
}

type Analyzer struct{ writer *lineage.Writer }

func New(w *lineage.Writer) *Analyzer { return &Analyzer{writer: w} }

// Preview walks downstream from `change.FQN` up to maxDepth hops, returning
// every node that depends on it. Phase-1 simple BFS; cycles guarded by `seen`.
func (a *Analyzer) Preview(ctx context.Context, customerID uuid.UUID, change Change, maxDepth int) ([]Hit, error) {
	if maxDepth <= 0 {
		maxDepth = 5
	}
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
				if _, ok := seen[e.DownstreamFQN]; ok {
					continue
				}
				seen[e.DownstreamFQN] = depth + 1
				hits = append(hits, Hit{FQN: e.DownstreamFQN, Hops: depth + 1, Why: fqn + " → " + e.DownstreamFQN})
				next = append(next, e.DownstreamFQN)
			}
		}
		queue = next
	}
	return hits, nil
}
