// Dependency-free layered-DAG layout for the lineage graph. We deliberately
// avoid reactflow/dagre/elk (bundle size + the "shadcn primitives only" rule):
// a depth-bounded lineage graph is a thin layered DAG that lays out cleanly with
// a BFS layering + per-layer stacking in ~100 lines we own.
import type { LineageGraph, LineageGraphNode, LineageGraphEdge } from "./api";

export interface PositionedNode extends LineageGraphNode {
  x: number;
  y: number;
  w: number;
  h: number;
}
export interface LaidOutEdge {
  edge: LineageGraphEdge;
  path: string;
}
export interface Layout {
  nodes: PositionedNode[];
  edges: LaidOutEdge[];
  width: number;
  height: number;
}

const NODE_W = 210;
const NODE_H = 46;
const COL_GAP = 70;
const ROW_GAP = 18;
const PAD = 24;

// layerOf assigns a signed layer to each node: 0 at the root, +1 per downstream
// hop (right), -1 per upstream hop (left), so an "upstream + downstream" graph
// fans out both ways instead of piling on one side.
function layerOf(graph: LineageGraph): Map<string, number> {
  const layer = new Map<string, number>();
  if (graph.nodes.length === 0) return layer;
  const root = graph.nodes.find((n) => n.is_root)?.id ?? graph.nodes[0].id;
  layer.set(root, 0);
  const queue = [root];
  // Adjacency for both directions.
  const out = new Map<string, string[]>();
  const inc = new Map<string, string[]>();
  for (const e of graph.edges) {
    if (!out.has(e.source)) out.set(e.source, []);
    out.get(e.source)!.push(e.target);
    if (!inc.has(e.target)) inc.set(e.target, []);
    inc.get(e.target)!.push(e.source);
  }
  while (queue.length) {
    const cur = queue.shift()!;
    const l = layer.get(cur)!;
    for (const nxt of out.get(cur) ?? []) {
      if (!layer.has(nxt)) {
        layer.set(nxt, l + 1);
        queue.push(nxt);
      }
    }
    for (const prv of inc.get(cur) ?? []) {
      if (!layer.has(prv)) {
        layer.set(prv, l - 1);
        queue.push(prv);
      }
    }
  }
  // Any disconnected nodes default to layer 0.
  for (const n of graph.nodes) if (!layer.has(n.id)) layer.set(n.id, 0);
  return layer;
}

export function layout(graph: LineageGraph): Layout {
  const layer = layerOf(graph);
  const layers = [...new Set([...layer.values()])].sort((a, b) => a - b);
  const colIndex = new Map<number, number>();
  layers.forEach((l, i) => colIndex.set(l, i));

  const byCol = new Map<number, LineageGraphNode[]>();
  for (const n of graph.nodes) {
    const c = colIndex.get(layer.get(n.id)!)!;
    if (!byCol.has(c)) byCol.set(c, []);
    byCol.get(c)!.push(n);
  }

  const pos = new Map<string, PositionedNode>();
  let maxRows = 0;
  byCol.forEach((nodes, col) => {
    maxRows = Math.max(maxRows, nodes.length);
    nodes.forEach((n, row) => {
      pos.set(n.id, {
        ...n,
        x: PAD + col * (NODE_W + COL_GAP),
        y: PAD + row * (NODE_H + ROW_GAP),
        w: NODE_W,
        h: NODE_H,
      });
    });
  });

  const edges: LaidOutEdge[] = [];
  for (const e of graph.edges) {
    const s = pos.get(e.source);
    const t = pos.get(e.target);
    if (!s || !t) continue;
    const x1 = s.x + s.w;
    const y1 = s.y + s.h / 2;
    const x2 = t.x;
    const y2 = t.y + t.h / 2;
    const mx = (x1 + x2) / 2;
    edges.push({ edge: e, path: `M ${x1} ${y1} C ${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}` });
  }

  return {
    nodes: [...pos.values()],
    edges,
    width: Math.max(PAD * 2 + layers.length * (NODE_W + COL_GAP), 400),
    height: Math.max(PAD * 2 + maxRows * (NODE_H + ROW_GAP), 160),
  };
}
