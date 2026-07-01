package lineage

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// Graph traversal bounds. Depth is capped so an adversarial or densely-connected
// graph can't run an unbounded recursive CTE.
const (
	DefaultGraphDepth = 5
	MaxGraphDepth     = 10
)

// GraphNode is one vertex in a lineage graph (a table FQN or a column FQN).
type GraphNode struct {
	ID     string `json:"id"` // == FQN; stable key for the frontend
	FQN    string `json:"fqn"`
	Kind   string `json:"kind"` // "table" | "column"
	Depth  int    `json:"depth"`
	IsRoot bool   `json:"is_root"`
}

// GraphEdge is a directed dependency carrying its evidence origin + confidence.
type GraphEdge struct {
	Source     string `json:"source"`
	Target     string `json:"target"`
	EdgeType   string `json:"edge_type"`
	Origin     string `json:"origin"`
	Confidence string `json:"confidence"`
}

// GraphResult is the multi-hop lineage graph rooted at an FQN.
type GraphResult struct {
	Root        string      `json:"root"`
	Direction   string      `json:"direction"`
	Granularity string      `json:"granularity"`
	Depth       int         `json:"depth"`
	Truncated   bool        `json:"truncated"`
	Nodes       []GraphNode `json:"nodes"`
	Edges       []GraphEdge `json:"edges"`
}

// Recursive CTEs walk lineage_edge with a visited-path array as a cycle guard so
// A→B→A (INSERT…SELECT loops) and self-referential CTEs terminate. The two
// directions are static SQL (no interpolation) so the only knobs are bound
// params: $1 customer, $2 root fqn, $3 depth cap, $4 edge_type, $5 anchor LIKE
// pattern. $5 lets a column-granularity graph rooted at a TABLE expand to that
// table's columns (column edges are keyed on schema.table.col, not the table);
// it is ” for table granularity (matches nothing extra).
const downstreamWalkSQL = `
WITH RECURSIVE walk AS (
    SELECT upstream_fqn, downstream_fqn, edge_type, origin, confidence, 1 AS depth,
           ARRAY[upstream_fqn, downstream_fqn] AS path
    FROM lineage_edge
    WHERE customer_id = $1 AND edge_type = $4
      AND (upstream_fqn = $2 OR ($5 <> '' AND upstream_fqn LIKE $5))
  UNION ALL
    SELECT e.upstream_fqn, e.downstream_fqn, e.edge_type, e.origin, e.confidence, w.depth + 1,
           w.path || e.downstream_fqn
    FROM lineage_edge e
    JOIN walk w ON e.upstream_fqn = w.downstream_fqn
    WHERE e.customer_id = $1 AND e.edge_type = $4
      AND w.depth < $3 AND NOT e.downstream_fqn = ANY(w.path)
)
SELECT DISTINCT upstream_fqn, downstream_fqn, edge_type, origin, confidence, depth FROM walk`

const upstreamWalkSQL = `
WITH RECURSIVE walk AS (
    SELECT upstream_fqn, downstream_fqn, edge_type, origin, confidence, 1 AS depth,
           ARRAY[downstream_fqn, upstream_fqn] AS path
    FROM lineage_edge
    WHERE customer_id = $1 AND edge_type = $4
      AND (downstream_fqn = $2 OR ($5 <> '' AND downstream_fqn LIKE $5))
  UNION ALL
    SELECT e.upstream_fqn, e.downstream_fqn, e.edge_type, e.origin, e.confidence, w.depth + 1,
           w.path || e.upstream_fqn
    FROM lineage_edge e
    JOIN walk w ON e.downstream_fqn = w.upstream_fqn
    WHERE e.customer_id = $1 AND e.edge_type = $4
      AND w.depth < $3 AND NOT e.upstream_fqn = ANY(w.path)
)
SELECT DISTINCT upstream_fqn, downstream_fqn, edge_type, origin, confidence, depth FROM walk`

// Graph returns the multi-hop lineage graph around `fqn`. direction is
// "upstream" | "downstream" | "both"; granularity is "table" | "column". depth
// is clamped to [1, MaxGraphDepth].
func (w *Writer) Graph(ctx context.Context, customerID uuid.UUID, fqn, direction, granularity string, depth int) (GraphResult, error) {
	if depth <= 0 {
		depth = DefaultGraphDepth
	}
	if depth > MaxGraphDepth {
		depth = MaxGraphDepth
	}
	if granularity != "column" {
		granularity = "table"
	}
	switch direction {
	case "upstream", "downstream", "both":
	default:
		direction = "both"
	}

	res := GraphResult{Root: fqn, Direction: direction, Granularity: granularity, Depth: depth}
	edgeSeen := map[string]struct{}{}

	// For a column-granularity graph rooted at a table (the FQN has no column
	// segment), expand the anchor to that table's columns so the toggle works
	// without making the user re-root on a specific column.
	anchorLike := ""
	if granularity == "column" {
		anchorLike = fqn + ".%"
	}

	// Seed the root node only when it is itself a graph vertex (exact match), so
	// the searched node shows even with zero edges. When we expand a table → its
	// columns, the table isn't a column vertex, so don't seed a disconnected node.
	nodeDepth := map[string]int{} // min depth seen per node
	if anchorLike == "" {
		nodeDepth[fqn] = 0
	}

	run := func(sql string) error {
		rows, err := w.pool.Query(ctx, sql, customerID, fqn, depth, granularity, anchorLike)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e GraphEdge
			var d int
			if err := rows.Scan(&e.Source, &e.Target, &e.EdgeType, &e.Origin, &e.Confidence, &d); err != nil {
				return err
			}
			if d >= depth {
				res.Truncated = true
			}
			key := e.Source + "\x00" + e.Target + "\x00" + e.Origin
			if _, ok := edgeSeen[key]; !ok {
				edgeSeen[key] = struct{}{}
				res.Edges = append(res.Edges, e)
			}
			recordNodeDepth(nodeDepth, e.Source, d)
			recordNodeDepth(nodeDepth, e.Target, d)
		}
		return rows.Err()
	}

	if direction == "downstream" || direction == "both" {
		if err := run(downstreamWalkSQL); err != nil {
			return GraphResult{}, fmt.Errorf("downstream walk: %w", err)
		}
	}
	if direction == "upstream" || direction == "both" {
		if err := run(upstreamWalkSQL); err != nil {
			return GraphResult{}, fmt.Errorf("upstream walk: %w", err)
		}
	}

	for id, d := range nodeDepth {
		res.Nodes = append(res.Nodes, GraphNode{
			ID: id, FQN: id, Kind: granularity, Depth: d, IsRoot: id == fqn,
		})
	}
	if res.Edges == nil {
		res.Edges = []GraphEdge{}
	}
	return res, nil
}

func recordNodeDepth(m map[string]int, fqn string, d int) {
	if cur, ok := m[fqn]; !ok || d < cur {
		m[fqn] = d
	}
}
