import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useActiveCustomer } from "../lib/customer";
import { Card, CardHeader, EmptyState, ErrorState } from "../components/ui";
import { BRANDING } from "../branding";

export function Dashboard() {
  const { data: customer } = useActiveCustomer();
  const customerId = customer?.id;
  const savings = useQuery({
    queryKey: ["savings", customerId],
    queryFn: () => api.savings(customerId!),
    enabled: !!customerId,
  });
  const queries = useQuery({
    queryKey: ["queries", customerId],
    queryFn: () => api.queryAnalytics(customerId!),
    enabled: !!customerId,
  });

  if (!customer) {
    return (
      <Card>
        <CardHeader title={`Welcome to ${BRANDING.name}`} description="Create a customer record to begin." />
        <p className="text-sm text-foreground/60">
          POST <code className="font-mono">/api/v1/customers</code> with <code>{`{"slug":"acme","display_name":"Acme"}`}</code>.
        </p>
      </Card>
    );
  }

  if (savings.isError) return <ErrorState error={savings.error} />;
  const total = savings.data?.reduce((a, r) => a + r.estimated_savings_usd, 0) ?? 0;
  const totalQ = queries.data?.reduce((a, r) => a + r.count, 0) ?? 0;
  const duckQ = queries.data?.find((r) => r.route === "duckdb")?.count ?? 0;
  const pct = totalQ ? ((duckQ / totalQ) * 100).toFixed(1) : "0";

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Savings Dashboard</h1>
      <div className="grid grid-cols-3 gap-4">
        <Card>
          <CardHeader title="Estimated 30-day savings" />
          <div className="text-3xl font-semibold">${total.toFixed(2)}</div>
        </Card>
        <Card>
          <CardHeader title="DuckDB hit rate" />
          <div className="text-3xl font-semibold">{pct}%</div>
          <p className="text-xs text-foreground/60 mt-1">Phase 1 target ≥80%</p>
        </Card>
        <Card>
          <CardHeader title="Queries (24h)" />
          <div className="text-3xl font-semibold">{totalQ.toLocaleString()}</div>
        </Card>
      </div>

      <Card>
        <CardHeader title="Route breakdown (24h)" />
        {!queries.data || queries.data.length === 0 ? (
          <EmptyState title="No queries yet" hint="Run a query through the proxy to see metrics." />
        ) : (
          <ul className="space-y-2 text-sm">
            {queries.data.map((r) => (
              <li key={r.route} className="flex items-center justify-between">
                <span className="font-mono">{r.route}</span>
                <span>{r.count.toLocaleString()} · avg {r.avg_ms}ms</span>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}
