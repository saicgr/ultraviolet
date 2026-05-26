import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { Card, CardHeader, EmptyState, ErrorState, SkeletonRows, Badge } from "../components/ui";
import { useActiveCustomer } from "../lib/customer";

export function Activity() {
  const customer = useActiveCustomer();
  const cid = customer.data?.id ?? "";
  const q = useQuery({
    queryKey: ["activity", cid],
    queryFn: () => api.listActivity(cid, 50),
    enabled: !!cid,
  });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Activity</h1>
      <Card>
        <CardHeader title="Recent events" description="Audit and operations feed for this customer." />
        {(customer.isPending || (q.isPending && !!cid)) && <SkeletonRows count={6} />}
        {q.isError && <ErrorState error={q.error} onRetry={() => q.refetch()} />}
        {q.data && q.data.length === 0 && <EmptyState title="No activity yet" hint="Events will appear here once your team begins querying." />}
        {q.data && q.data.length > 0 && (
          <ul className="space-y-3">
            {q.data.map((e) => (
              <li key={e.id} className="rounded-md border border-border/60 bg-background/40 p-3">
                <div className="flex items-center justify-between gap-3">
                  <div className="flex items-center gap-2">
                    <Badge>{e.kind}</Badge>
                    <span className="font-mono text-xs text-foreground/70">{e.target}</span>
                  </div>
                  <span className="text-xs text-foreground/50">{new Date(e.created_at).toLocaleString()}</span>
                </div>
                <p className="text-sm mt-2 text-foreground/80">{e.summary}</p>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}
