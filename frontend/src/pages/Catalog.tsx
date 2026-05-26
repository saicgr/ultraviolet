import { useState } from "react";
import { Card, CardHeader, EmptyState, ErrorState, Input, SkeletonRows, Table, TD, TH, THead, TR } from "../components/ui";
import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";

export function Catalog() {
  const [q, setQ] = useState("");
  const search = useQuery({
    queryKey: ["catalog", q],
    queryFn: () => api.catalogSearch(q),
    enabled: q.length >= 2,
  });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Catalog</h1>
      <Card>
        <CardHeader title="Search" description="Type ≥2 characters to search descriptions, owners, tags, FQNs." />
        <Input placeholder="orders, fct_*, owner:data-platform" value={q} onChange={(e) => setQ(e.target.value)} />
      </Card>
      <Card>
        <CardHeader title="Results" />
        {q.length < 2 && <EmptyState title="Start typing" hint="Search across all enriched metadata sources (dbt / Tableau / Looker / Hex / Metaplane / Atlan)." />}
        {search.isPending && q.length >= 2 && <SkeletonRows count={4} />}
        {search.isError && <ErrorState error={search.error} onRetry={() => search.refetch()} />}
        {search.data?.length === 0 && <EmptyState title="No matches" />}
        {!!search.data?.length && (
          <Table>
            <THead><TR><TH>FQN</TH><TH>Owner</TH><TH>Source</TH><TH>SLA</TH></TR></THead>
            <tbody>
              {search.data.map((r) => (
                <TR key={r.fqn + r.source}>
                  <TD className="font-mono">{r.fqn}</TD>
                  <TD>{r.owner ?? "—"}</TD>
                  <TD>{r.source}</TD>
                  <TD>{r.sla_minutes ? `${r.sla_minutes}m` : "—"}</TD>
                </TR>
              ))}
            </tbody>
          </Table>
        )}
      </Card>
    </div>
  );
}
