import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { Card, CardHeader, EmptyState, ErrorState, Input, SkeletonRows, Table, TD, TH, THead, TR } from "../components/ui";
import { LineageGraph } from "../components/LineageGraph";
import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useActiveCustomer } from "../lib/customer";

export function Lineage() {
  const [params, setParams] = useSearchParams();
  const customer = useActiveCustomer();
  const [fqn, setFqn] = useState(params.get("fqn") ?? "");
  const [showTables, setShowTables] = useState(false);

  // Keep the URL in sync so the graph is deep-linkable (e.g. from PR analysis).
  useEffect(() => {
    const urlFqn = params.get("fqn") ?? "";
    if (urlFqn !== fqn) setFqn(urlFqn);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params]);

  function reRoot(next: string) {
    setFqn(next);
    setParams(next ? { fqn: next } : {});
  }

  const upstream = useQuery({
    queryKey: ["lineage", "upstream", customer.data?.id, fqn],
    queryFn: () => api.lineageUpstream(fqn),
    enabled: !!fqn && showTables,
  });
  const downstream = useQuery({
    queryKey: ["lineage", "downstream", customer.data?.id, fqn],
    queryFn: () => api.lineageDownstream(fqn),
    enabled: !!fqn && showTables,
  });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Lineage</h1>
      <Card>
        <CardHeader title="Find a table" description="Enter the fully-qualified name (schema.table)." />
        <Input
          placeholder="e.g. analytics.top_words"
          value={fqn}
          onChange={(e) => reRoot(e.target.value)}
          className="max-w-md"
        />
      </Card>

      {!fqn && <EmptyState title="Search for a table" hint="Then explore its column-level lineage graph." />}

      {fqn && (
        <Card>
          <CardHeader title="Lineage graph" description="Click any node to re-root. Toggle table vs column granularity." />
          <LineageGraph rootFqn={fqn} customerId={customer.data?.id} onReRoot={reRoot} />
          <button
            className="mt-4 text-xs text-foreground/60 underline"
            onClick={() => setShowTables((v) => !v)}
          >
            {showTables ? "Hide" : "Show"} table view (accessible fallback)
          </button>
        </Card>
      )}

      {fqn && showTables && (
        <div className="grid grid-cols-2 gap-6">
          <Card>
            <CardHeader title="Upstream" description="What this table depends on." />
            {upstream.isPending && <SkeletonRows count={4} />}
            {upstream.isError && <ErrorState error={upstream.error} onRetry={() => upstream.refetch()} />}
            {upstream.data?.length === 0 && <EmptyState title="No upstream observed" />}
            {!!upstream.data?.length && (
              <Table>
                <THead><TR><TH>FQN</TH><TH>Type</TH><TH>Origin</TH></TR></THead>
                <tbody>
                  {upstream.data.map((e) => (
                    <TR key={e.upstream_fqn + e.edge_type}>
                      <TD className="font-mono">{e.upstream_fqn}</TD>
                      <TD>{e.edge_type}</TD>
                      <TD>{e.origin ?? "runtime"}</TD>
                    </TR>
                  ))}
                </tbody>
              </Table>
            )}
          </Card>
          <Card>
            <CardHeader title="Downstream" description="What depends on this table." />
            {downstream.isPending && <SkeletonRows count={4} />}
            {downstream.isError && <ErrorState error={downstream.error} onRetry={() => downstream.refetch()} />}
            {downstream.data?.length === 0 && <EmptyState title="No downstream observed" />}
            {!!downstream.data?.length && (
              <Table>
                <THead><TR><TH>FQN</TH><TH>Type</TH><TH>Origin</TH></TR></THead>
                <tbody>
                  {downstream.data.map((e) => (
                    <TR key={e.downstream_fqn + e.edge_type}>
                      <TD className="font-mono">{e.downstream_fqn}</TD>
                      <TD>{e.edge_type}</TD>
                      <TD>{e.origin ?? "runtime"}</TD>
                    </TR>
                  ))}
                </tbody>
              </Table>
            )}
          </Card>
        </div>
      )}
    </div>
  );
}
