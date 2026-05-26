import { useState } from "react";
import { Card, CardHeader, EmptyState, ErrorState, Input, SkeletonRows, Table, TD, TH, THead, TR } from "../components/ui";
import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";

export function Lineage() {
  const [fqn, setFqn] = useState("");
  const upstream = useQuery({
    queryKey: ["lineage", "upstream", fqn],
    queryFn: () => api.lineageUpstream(fqn),
    enabled: !!fqn,
  });
  const downstream = useQuery({
    queryKey: ["lineage", "downstream", fqn],
    queryFn: () => api.lineageDownstream(fqn),
    enabled: !!fqn,
  });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Lineage</h1>
      <Card>
        <CardHeader title="Find a table" description="Enter the fully-qualified name (schema.table)." />
        <Input
          placeholder="e.g. samples.shakespeare"
          value={fqn}
          onChange={(e) => setFqn(e.target.value)}
          className="max-w-md"
        />
      </Card>
      {fqn && (
        <div className="grid grid-cols-2 gap-6">
          <Card>
            <CardHeader title="Upstream" description="What this table depends on." />
            {upstream.isPending && <SkeletonRows count={4} />}
            {upstream.isError && <ErrorState error={upstream.error} onRetry={() => upstream.refetch()} />}
            {upstream.data?.length === 0 && <EmptyState title="No upstream observed" hint="Run a query that joins this table to populate the graph." />}
            {!!upstream.data?.length && (
              <Table>
                <THead><TR><TH>FQN</TH><TH>Type</TH></TR></THead>
                <tbody>
                  {upstream.data.map((e) => (
                    <TR key={e.upstream_fqn}><TD className="font-mono">{e.upstream_fqn}</TD><TD>{e.edge_type}</TD></TR>
                  ))}
                </tbody>
              </Table>
            )}
          </Card>
          <Card>
            <CardHeader title="Downstream" description="What depends on this table." />
            {downstream.isPending && <SkeletonRows count={4} />}
            {downstream.isError && <ErrorState error={downstream.error} onRetry={() => downstream.refetch()} />}
            {downstream.data?.length === 0 && <EmptyState title="No downstream observed" hint="As soon as a dashboard or saved query references it, edges land here." />}
            {!!downstream.data?.length && (
              <Table>
                <THead><TR><TH>FQN</TH><TH>Type</TH></TR></THead>
                <tbody>
                  {downstream.data.map((e) => (
                    <TR key={e.downstream_fqn}><TD className="font-mono">{e.downstream_fqn}</TD><TD>{e.edge_type}</TD></TR>
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
