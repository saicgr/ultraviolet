import { useState } from "react";
import { Button, Card, CardHeader, EmptyState, ErrorState, Input, SkeletonRows, Table, TD, TH, THead, TR } from "../components/ui";
import { useMutation } from "@tanstack/react-query";
import { api } from "../lib/api";

export function Impact() {
  const [fqn, setFqn] = useState("");
  const [kind, setKind] = useState("drop_column");
  const m = useMutation({
    mutationFn: () => api.impactPreview({ kind, fqn }),
  });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Impact analysis</h1>
      <Card>
        <CardHeader title="Preview a change" description="See what dashboards / dbt models / saved queries break." />
        <div className="grid grid-cols-3 gap-3">
          <Input placeholder="schema.table.column" value={fqn} onChange={(e) => setFqn(e.target.value)} />
          <select className="h-10 rounded-md border border-border bg-background px-3 text-sm" value={kind} onChange={(e) => setKind(e.target.value)}>
            <option value="drop_column">drop column</option>
            <option value="drop_table">drop table</option>
            <option value="rename_column">rename column</option>
            <option value="type_change">type change</option>
          </select>
          <Button onClick={() => m.mutate()} disabled={!fqn || m.isPending}>{m.isPending ? "Analyzing…" : "Preview"}</Button>
        </div>
        {m.isError && <ErrorState error={m.error} />}
      </Card>
      <Card>
        <CardHeader title="Blast radius" />
        {m.isPending && <SkeletonRows count={3} />}
        {!m.data && !m.isPending && <EmptyState title="Run a preview" />}
        {m.data?.length === 0 && <EmptyState title="No downstream consumers" hint="Safe to ship." />}
        {!!m.data?.length && (
          <Table>
            <THead><TR><TH>FQN</TH><TH>Hops</TH><TH>Path</TH></TR></THead>
            <tbody>
              {m.data.map((h, i) => (
                <TR key={i}><TD className="font-mono">{h.fqn}</TD><TD>{h.hops}</TD><TD className="text-xs text-foreground/70">{h.why}</TD></TR>
              ))}
            </tbody>
          </Table>
        )}
      </Card>
    </div>
  );
}
