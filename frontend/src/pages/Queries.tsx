import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useActiveCustomer } from "../lib/customer";
import { Card, CardHeader, EmptyState, ErrorState, SkeletonRows, Table, TD, TH, THead, TR } from "../components/ui";

export function Queries() {
  const { data: customer } = useActiveCustomer();
  const q = useQuery({
    queryKey: ["queries-page", customer?.id],
    queryFn: () => api.queryAnalytics(customer!.id),
    enabled: !!customer,
    refetchInterval: 5000,
  });

  if (!customer) return <EmptyState title="No active customer" />;
  if (q.isError) return <ErrorState error={q.error} onRetry={() => q.refetch()} />;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Query Analytics</h1>
      <Card>
        <CardHeader title="Last 24 hours" description="Live-refreshing every 5s" />
        {q.isPending ? (
          <SkeletonRows count={5} />
        ) : !q.data || q.data.length === 0 ? (
          <EmptyState title="No queries yet" hint="Connect a BI tool via the Postgres wire on :5000" />
        ) : (
          <Table>
            <THead>
              <TR>
                <TH>Route</TH>
                <TH>Count</TH>
                <TH>Avg ms</TH>
                <TH>Rows</TH>
              </TR>
            </THead>
            <tbody>
              {q.data.map((r) => (
                <TR key={r.route}>
                  <TD className="font-mono">{r.route}</TD>
                  <TD>{r.count.toLocaleString()}</TD>
                  <TD>{r.avg_ms}</TD>
                  <TD>{r.rows_returned.toLocaleString()}</TD>
                </TR>
              ))}
            </tbody>
          </Table>
        )}
      </Card>
    </div>
  );
}
