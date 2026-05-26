import { useQuery, useMutation } from "@tanstack/react-query";
import { useState } from "react";
import { Button, Card, CardHeader, EmptyState, ErrorState, SkeletonRows, Badge } from "../components/ui";
import { api } from "../lib/api";

export function Agents() {
  const list = useQuery({
    queryKey: ["agents"],
    queryFn: () => api.listAgents(),
  });
  const [last, setLast] = useState<{ name: string; out: unknown } | null>(null);
  const run = useMutation({
    mutationFn: (n: string) => api.runAgent(n, { dry_run: true }),
    onSuccess: (out, name) => setLast({ name, out }),
  });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Agents</h1>
      <p className="text-sm text-foreground/70 max-w-2xl">
        Loop-closing AI that acts on Ultraviolet's runtime signal. Cost optimizer reads query_log + sync state; schema safeguard runs on every PR commit; query regressor diffs week-over-week p95; on-call summarizer reads audit_log + sync_jobs; and more. Every run is audited + billed as one usage event.
      </p>
      <Card>
        <CardHeader title="Available agents" description="Click run to invoke in dry-run mode and inspect suggestions." />
        {list.isPending && <SkeletonRows count={6} />}
        {list.isError && <ErrorState error={list.error} onRetry={() => list.refetch()} />}
        {list.data?.length === 0 && <EmptyState title="No agents registered" />}
        {!!list.data?.length && (
          <ul className="grid grid-cols-2 gap-3">
            {list.data.map((a) => (
              <li key={a.name} className="rounded-md border border-border p-3">
                <div className="flex items-center justify-between gap-2">
                  <div>
                    <p className="font-medium font-mono text-sm">{a.name}</p>
                    <Badge variant={a.kind === "loop_closing" ? "success" : a.kind === "investigator" ? "warn" : undefined}>
                      {a.kind}
                    </Badge>
                  </div>
                  <Button size="sm" variant="outline" onClick={() => run.mutate(a.name)} disabled={run.isPending}>
                    {run.isPending && run.variables === a.name ? "Running…" : "Run"}
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </Card>
      {last && (
        <Card>
          <CardHeader title={`Last run: ${last.name}`} />
          <pre className="text-xs overflow-x-auto whitespace-pre-wrap">{JSON.stringify(last.out, null, 2)}</pre>
        </Card>
      )}
    </div>
  );
}
