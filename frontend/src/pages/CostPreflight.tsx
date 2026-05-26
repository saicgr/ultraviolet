import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useActiveCustomer } from "../lib/customer";
import {
  Badge,
  Button,
  Card,
  CardHeader,
  EmptyState,
  ErrorState,
} from "../components/ui";

export function CostPreflight() {
  const { data: customer } = useActiveCustomer();
  const [sql, setSql] = useState("");
  const preflight = useMutation({
    mutationFn: () => api.costPreflight({ customer_id: customer!.id, sql }),
  });

  if (!customer) return <EmptyState title="No active customer" />;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Cost Preflight</h1>
      <Card>
        <CardHeader title="Estimate" description="Dry-run a query and see the estimated bytes / cost before execution." />
        <textarea
          value={sql}
          onChange={(e) => setSql(e.target.value)}
          placeholder="SELECT count(*) FROM analytics.orders WHERE created_at > current_date - interval '7 days'"
          className="w-full h-40 rounded-md border border-border bg-background p-3 font-mono text-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        />
        <div className="mt-3">
          <Button onClick={() => preflight.mutate()} disabled={!sql.trim() || preflight.isPending}>
            Estimate
          </Button>
        </div>
        {preflight.isError && <div className="mt-3"><ErrorState error={preflight.error} onRetry={() => preflight.mutate()} /></div>}
      </Card>
      {preflight.data && (
        <Card>
          <CardHeader title="Result" />
          <div className="space-y-3 text-sm">
            <div className="flex justify-between">
              <span className="text-foreground/60">Estimated bytes</span>
              <span className="font-mono">{preflight.data.estimated_bytes.toLocaleString()}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-foreground/60">Estimated cost</span>
              <span className="font-mono">${preflight.data.estimated_cost_usd.toFixed(4)}</span>
            </div>
            <div className="flex justify-between items-center">
              <span className="text-foreground/60">Would block</span>
              <Badge variant={preflight.data.would_block ? "danger" : "success"}>
                {preflight.data.would_block ? "yes" : "no"}
              </Badge>
            </div>
            {preflight.data.reason && (
              <div className="pt-2 text-foreground/70">{preflight.data.reason}</div>
            )}
          </div>
        </Card>
      )}
    </div>
  );
}
