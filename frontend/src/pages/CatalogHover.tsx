import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useActiveCustomer } from "../lib/customer";
import {
  Card,
  CardHeader,
  EmptyState,
  ErrorState,
  Input,
  SkeletonRows,
  Table,
  TD,
  TH,
  THead,
  TR,
} from "../components/ui";

export function CatalogHover() {
  const { data: customer } = useActiveCustomer();
  const [fqn, setFqn] = useState("");
  const hover = useQuery({
    queryKey: ["catalog-hover", fqn, customer?.id],
    queryFn: () => api.catalogHover(fqn, customer!.id),
    enabled: !!customer && fqn.length >= 3,
  });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Catalog Hover</h1>
      <Card>
        <CardHeader title="Lookup" description="Fully-qualified name (e.g. project.dataset.table)." />
        <Input
          placeholder="analytics.public.orders"
          value={fqn}
          onChange={(e) => setFqn(e.target.value)}
        />
      </Card>

      {!customer && <EmptyState title="No active customer" />}
      {customer && fqn.length < 3 && (
        <EmptyState title="Enter an FQN" hint="At least 3 characters." />
      )}
      {hover.isPending && fqn.length >= 3 && customer && (
        <Card>
          <SkeletonRows count={5} />
        </Card>
      )}
      {hover.isError && (
        <Card>
          <ErrorState error={hover.error} onRetry={() => hover.refetch()} />
        </Card>
      )}
      {hover.data && (
        <Card>
          <CardHeader title={hover.data.fqn} description={`Owner: ${hover.data.owner ?? "—"} · SLA: ${hover.data.sla_minutes ? hover.data.sla_minutes + "m" : "—"}`} />
          <div className="space-y-6">
            <div>
              <h3 className="text-sm font-medium mb-2">Columns</h3>
              {hover.data.columns.length === 0 ? (
                <EmptyState title="No columns" />
              ) : (
                <Table>
                  <THead><TR><TH>Name</TH><TH>Type</TH></TR></THead>
                  <tbody>
                    {hover.data.columns.map((c) => (
                      <TR key={c.name}>
                        <TD className="font-mono">{c.name}</TD>
                        <TD>{c.type}</TD>
                      </TR>
                    ))}
                  </tbody>
                </Table>
              )}
            </div>
            <div>
              <h3 className="text-sm font-medium mb-2">Recent queries</h3>
              {hover.data.recent_queries.length === 0 ? (
                <EmptyState title="No recent queries" />
              ) : (
                <ul className="space-y-2 text-sm">
                  {hover.data.recent_queries.map((q) => (
                    <li key={q.query_hash} className="font-mono text-xs text-foreground/70 truncate">
                      {q.sql}
                    </li>
                  ))}
                </ul>
              )}
            </div>
            <div>
              <h3 className="text-sm font-medium mb-2">Consuming dashboards</h3>
              {hover.data.dashboards.length === 0 ? (
                <EmptyState title="No consumers" />
              ) : (
                <ul className="space-y-1 text-sm">
                  {hover.data.dashboards.map((d) => (
                    <li key={d.id}>{d.name}</li>
                  ))}
                </ul>
              )}
            </div>
          </div>
        </Card>
      )}
    </div>
  );
}
