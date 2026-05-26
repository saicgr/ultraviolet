import { useQuery } from "@tanstack/react-query";
import { Card, CardHeader, EmptyState, ErrorState, SkeletonRows } from "../components/ui";
import { api } from "../lib/api";
import { useActiveCustomer } from "../lib/customer";

export function Dashboards() {
  const { data: customer } = useActiveCustomer();
  const list = useQuery({
    queryKey: ["dashboards", customer?.id],
    queryFn: () => api.listDashboards(customer!.id),
    enabled: !!customer,
  });

  if (!customer) return <EmptyState title="No active customer" />;
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Dashboards</h1>
      <Card>
        <CardHeader title="Your dashboards" description="Tile-cached, RLS-aware, scheduleable." />
        {list.isPending && <SkeletonRows count={3} />}
        {list.isError && <ErrorState error={list.error} onRetry={() => list.refetch()} />}
        {list.data?.length === 0 && <EmptyState title="No dashboards yet" hint="Use the CLI: `uv dashboard deploy file.yaml`" />}
        {!!list.data?.length && (
          <ul className="grid grid-cols-2 gap-3">
            {list.data.map((d) => (
              <li key={d.id} className="rounded-md border border-border p-3 hover:bg-muted">
                <p className="font-medium">{d.name}</p>
                <p className="text-xs text-foreground/60">{d.tiles?.length ?? 0} tiles</p>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}
