import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useActiveCustomer } from "../lib/customer";
import {
  Badge,
  Card,
  CardHeader,
  EmptyState,
  ErrorState,
  SkeletonRows,
} from "../components/ui";

function fmtLag(s: number): string {
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.round(s / 60)}m`;
  return `${(s / 3600).toFixed(1)}h`;
}

export function SyncDAG() {
  const { data: customer } = useActiveCustomer();
  const dag = useQuery({
    queryKey: ["sync-dag", customer?.id],
    queryFn: () => api.syncDAG(customer!.id),
    enabled: !!customer,
  });

  if (!customer) return <EmptyState title="No active customer" />;

  const sorted = (dag.data ?? []).slice().sort((a, b) => b.lag_seconds - a.lag_seconds);

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Sync DAG</h1>
      <Card>
        <CardHeader title="Pipelines" description="Synced tables and their upstream / downstream relationships. Sorted by lag." />
        {dag.isPending && <SkeletonRows count={6} />}
        {dag.isError && <ErrorState error={dag.error} onRetry={() => dag.refetch()} />}
        {dag.data && dag.data.length === 0 && <EmptyState title="No synced tables" />}
        {sorted.length > 0 && (
          <div className="grid grid-cols-2 lg:grid-cols-3 gap-4">
            {sorted.map((n) => (
              <div key={n.id} className="rounded-md border border-border bg-background p-3 space-y-2">
                <div className="flex items-center justify-between">
                  <span className="font-mono text-sm truncate">{n.fqn}</span>
                  <Badge
                    variant={
                      n.state === "live" ? "success" : n.state === "error" ? "danger" : "warn"
                    }
                  >
                    {n.state}
                  </Badge>
                </div>
                <div className="text-xs text-foreground/60">
                  Lag: {fmtLag(n.lag_seconds)} · Last: {n.last_sync_at ? new Date(n.last_sync_at).toLocaleString() : "—"}
                </div>
                {n.upstream.length > 0 && (
                  <div className="text-xs font-mono space-y-0.5">
                    {n.upstream.map((u) => (
                      <div key={u} className="text-foreground/70">{u} → {n.fqn}</div>
                    ))}
                  </div>
                )}
                {n.downstream.length > 0 && (
                  <div className="text-xs font-mono space-y-0.5">
                    {n.downstream.map((d) => (
                      <div key={d} className="text-foreground/70">{n.fqn} → {d}</div>
                    ))}
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}
