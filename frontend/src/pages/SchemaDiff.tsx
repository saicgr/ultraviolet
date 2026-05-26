import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useActiveCustomer } from "../lib/customer";
import {
  Button,
  Card,
  CardHeader,
  EmptyState,
  ErrorState,
  Input,
  SkeletonRows,
} from "../components/ui";

export function SchemaDiff() {
  const { data: customer } = useActiveCustomer();
  const conns = useQuery({
    queryKey: ["connections", customer?.id],
    queryFn: () => api.listConnections(customer!.id),
    enabled: !!customer,
  });
  const [connId, setConnId] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [enabled, setEnabled] = useState(false);

  const diff = useQuery({
    queryKey: ["schema-diff", connId, from, to],
    queryFn: () => api.schemaDiff(connId, from, to),
    enabled: enabled && !!connId && !!from && !!to,
  });

  if (!customer) return <EmptyState title="No active customer" />;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Schema Diff</h1>
      <Card>
        <CardHeader title="Inputs" description="Compare two schema snapshots for a connection." />
        <div className="grid grid-cols-4 gap-3">
          <select
            className="h-10 rounded-md border border-border bg-background px-3 text-sm"
            value={connId}
            onChange={(e) => setConnId(e.target.value)}
          >
            <option value="">Connection…</option>
            {(conns.data ?? []).map((c) => (
              <option key={c.id} value={c.id}>{c.name}</option>
            ))}
          </select>
          <Input placeholder="from snapshot id" value={from} onChange={(e) => setFrom(e.target.value)} />
          <Input placeholder="to snapshot id" value={to} onChange={(e) => setTo(e.target.value)} />
          <Button onClick={() => setEnabled(true)} disabled={!connId || !from || !to}>
            Diff
          </Button>
        </div>
      </Card>

      {enabled && diff.isPending && (
        <Card><SkeletonRows count={5} /></Card>
      )}
      {diff.isError && (
        <Card><ErrorState error={diff.error} onRetry={() => diff.refetch()} /></Card>
      )}
      {diff.data && (
        <div className="grid grid-cols-3 gap-4">
          <Card>
            <CardHeader title={`Added (${diff.data.added.length})`} />
            {diff.data.added.length === 0 ? (
              <EmptyState title="None" />
            ) : (
              <ul className="space-y-1 text-sm font-mono">
                {diff.data.added.map((a) => (
                  <li key={a.fqn} className="text-emerald-300">{a.fqn} <span className="text-foreground/50">({a.type})</span></li>
                ))}
              </ul>
            )}
          </Card>
          <Card>
            <CardHeader title={`Dropped (${diff.data.dropped.length})`} />
            {diff.data.dropped.length === 0 ? (
              <EmptyState title="None" />
            ) : (
              <ul className="space-y-1 text-sm font-mono">
                {diff.data.dropped.map((d) => (
                  <li key={d.fqn} className="text-rose-300">{d.fqn} <span className="text-foreground/50">({d.type})</span></li>
                ))}
              </ul>
            )}
          </Card>
          <Card>
            <CardHeader title={`Type changes (${diff.data.type_changes.length})`} />
            {diff.data.type_changes.length === 0 ? (
              <EmptyState title="None" />
            ) : (
              <ul className="space-y-1 text-sm font-mono">
                {diff.data.type_changes.map((c) => (
                  <li key={c.fqn} className="text-amber-300">
                    {c.fqn}: {c.old_type} → {c.new_type}
                  </li>
                ))}
              </ul>
            )}
          </Card>
        </div>
      )}
    </div>
  );
}
