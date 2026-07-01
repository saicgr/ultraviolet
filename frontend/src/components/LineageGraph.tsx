import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { layout } from "../lib/dagLayout";
import { Button, EmptyState, ErrorState, SkeletonRows } from "./ui";

type Direction = "upstream" | "downstream" | "both";
type Granularity = "table" | "column";

export function LineageGraph({
  rootFqn,
  customerId,
  onReRoot,
}: {
  rootFqn: string;
  customerId: string | undefined;
  onReRoot: (fqn: string) => void;
}) {
  const [direction, setDirection] = useState<Direction>("both");
  const [granularity, setGranularity] = useState<Granularity>("table");
  const [depth, setDepth] = useState(3);

  const q = useQuery({
    // customerId is in the key so a tenant switch never serves stale lineage.
    queryKey: ["lineage-graph", customerId, rootFqn, direction, granularity, depth],
    queryFn: () => api.lineageGraph({ fqn: rootFqn, depth, direction, granularity }),
    enabled: !!rootFqn,
  });

  const laid = useMemo(() => (q.data ? layout(q.data) : null), [q.data]);

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-3">
        <div className="flex gap-1" role="group" aria-label="Direction">
          {(["upstream", "downstream", "both"] as Direction[]).map((d) => (
            <Button
              key={d}
              size="sm"
              variant={direction === d ? "default" : "outline"}
              onClick={() => setDirection(d)}
            >
              {d}
            </Button>
          ))}
        </div>
        <div className="flex gap-1" role="group" aria-label="Granularity">
          {(["table", "column"] as Granularity[]).map((g) => (
            <Button
              key={g}
              size="sm"
              variant={granularity === g ? "default" : "outline"}
              onClick={() => setGranularity(g)}
            >
              {g}
            </Button>
          ))}
        </div>
        <label className="flex items-center gap-2 text-xs text-foreground/70">
          Depth: {depth}
          <input
            type="range"
            min={1}
            max={6}
            value={depth}
            onChange={(e) => setDepth(Number(e.target.value))}
            aria-label="Graph depth"
          />
        </label>
        <Legend />
      </div>

      {q.isPending && <SkeletonRows count={4} />}
      {q.isError && <ErrorState error={q.error} onRetry={() => q.refetch()} />}
      {q.data && q.data.nodes.length <= 1 && q.data.edges.length === 0 && (
        <EmptyState
          title="No lineage observed yet"
          hint="Runtime lineage needs query traffic through the proxy; connect a repo for source-code lineage."
        />
      )}
      {laid && q.data && q.data.edges.length > 0 && (
        <div className="overflow-auto rounded-md border border-border bg-background/50" style={{ maxHeight: 520 }}>
          <svg
            width={laid.width}
            height={laid.height}
            role="img"
            aria-label={`Lineage graph rooted at ${rootFqn}`}
          >
            {laid.edges.map((le, i) => (
              <path
                key={i}
                d={le.path}
                fill="none"
                strokeWidth={1.5}
                className={le.edge.origin === "source_code" ? "stroke-border" : "stroke-accent"}
                strokeDasharray={le.edge.origin === "source_code" ? "5 4" : undefined}
                opacity={le.edge.confidence === "ambiguous" ? 0.45 : 0.85}
              />
            ))}
            {laid.nodes.map((n) => (
              <g
                key={n.id}
                transform={`translate(${n.x},${n.y})`}
                className="cursor-pointer"
                onClick={() => onReRoot(n.fqn)}
                tabIndex={0}
                role="button"
                aria-label={`Re-root at ${n.fqn}`}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") onReRoot(n.fqn);
                }}
              >
                <rect
                  width={n.w}
                  height={n.h}
                  rx={8}
                  className={
                    n.is_root
                      ? "fill-accent/20 stroke-accent"
                      : "fill-muted stroke-border hover:stroke-accent"
                  }
                  strokeWidth={n.is_root ? 2 : 1}
                />
                <text x={10} y={27} className="fill-foreground text-[11px] font-mono">
                  {truncate(n.fqn, 28)}
                </text>
              </g>
            ))}
          </svg>
        </div>
      )}
      {q.data?.truncated && (
        <p className="text-xs text-warn">Graph truncated at depth {q.data.depth}; increase depth to see more.</p>
      )}
    </div>
  );
}

function Legend() {
  return (
    <div className="flex items-center gap-3 text-xs text-foreground/60">
      <span className="flex items-center gap-1">
        <svg width="22" height="6">
          <line x1="0" y1="3" x2="22" y2="3" className="stroke-accent" strokeWidth="2" />
        </svg>
        runtime
      </span>
      <span className="flex items-center gap-1">
        <svg width="22" height="6">
          <line x1="0" y1="3" x2="22" y2="3" className="stroke-border" strokeWidth="2" strokeDasharray="4 3" />
        </svg>
        source-code
      </span>
    </div>
  );
}

function truncate(s: string, n: number): string {
  return s.length > n ? "…" + s.slice(-(n - 1)) : s;
}
