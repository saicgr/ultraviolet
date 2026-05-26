import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { Button, Card, CardHeader, EmptyState, ErrorState, SkeletonRows, Badge } from "../components/ui";
import { useActiveCustomer } from "../lib/customer";

export function Inbox() {
  const qc = useQueryClient();
  const customer = useActiveCustomer();
  const cid = customer.data?.id ?? "";

  const inbox = useQuery({
    queryKey: ["inbox", cid],
    queryFn: () => api.listInbox(cid),
    enabled: !!cid,
  });

  const markOne = useMutation({
    mutationFn: (id: string) => api.markInboxRead(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["inbox", cid] }),
  });
  const markAll = useMutation({
    mutationFn: () => api.markAllInboxRead(cid),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["inbox", cid] }),
  });

  const unread = inbox.data?.filter((i) => !i.read_at).length ?? 0;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold tracking-tight">Inbox</h1>
        <Button
          variant="outline"
          size="sm"
          disabled={!unread || markAll.isPending}
          onClick={() => markAll.mutate()}
        >
          Mark all read
        </Button>
      </div>
      <Card>
        <CardHeader title={unread ? `${unread} unread` : "All caught up"} description="Notifications about anomalies, run failures, and review requests." />
        {(customer.isPending || (inbox.isPending && !!cid)) && <SkeletonRows count={5} />}
        {inbox.isError && <ErrorState error={inbox.error} onRetry={() => inbox.refetch()} />}
        {inbox.data && inbox.data.length === 0 && <EmptyState title="Inbox empty" />}
        {inbox.data && inbox.data.length > 0 && (
          <ul className="space-y-2">
            {inbox.data.map((i) => (
              <li
                key={i.id}
                className={`rounded-md border border-border/60 p-3 ${i.read_at ? "bg-background/20 text-foreground/60" : "bg-muted/30"}`}
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="flex-1">
                    <div className="flex items-center gap-2">
                      <Badge variant={i.read_at ? "default" : "warn"}>{i.kind}</Badge>
                      <span className="font-medium text-sm">{i.title}</span>
                    </div>
                    {i.body && <p className="text-xs mt-1 text-foreground/70">{i.body}</p>}
                    <p className="text-xs mt-1 text-foreground/50">{new Date(i.created_at).toLocaleString()}</p>
                  </div>
                  {!i.read_at && (
                    <Button size="sm" variant="ghost" onClick={() => markOne.mutate(i.id)} disabled={markOne.isPending}>
                      Mark read
                    </Button>
                  )}
                </div>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}
