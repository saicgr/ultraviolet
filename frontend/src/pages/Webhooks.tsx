import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import {
  Button,
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
  Badge,
} from "../components/ui";
import { useActiveCustomer } from "../lib/customer";

const EVENT_TYPES = ["query.completed", "sync.failed", "anomaly.detected", "review.opened"];

export function Webhooks() {
  const qc = useQueryClient();
  const customer = useActiveCustomer();
  const cid = customer.data?.id ?? "";
  const [url, setUrl] = useState("");
  const [events, setEvents] = useState<string[]>([]);

  const list = useQuery({
    queryKey: ["webhooks", cid],
    queryFn: () => api.listWebhooks(cid),
    enabled: !!cid,
  });
  const create = useMutation({
    mutationFn: () => api.createWebhook({ customer_id: cid, url, events }),
    onSuccess: () => {
      setUrl("");
      setEvents([]);
      qc.invalidateQueries({ queryKey: ["webhooks", cid] });
    },
  });
  const del = useMutation({
    mutationFn: (id: string) => api.deleteWebhook(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["webhooks", cid] }),
  });

  const toggle = (e: string) =>
    setEvents((cur) => (cur.includes(e) ? cur.filter((x) => x !== e) : [...cur, e]));

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Webhooks</h1>
      <Card>
        <CardHeader title="Register webhook" description="POSTs an HMAC-signed JSON payload on subscribed events." />
        <div className="space-y-3">
          <Input placeholder="https://hooks.your.app/uv" value={url} onChange={(e) => setUrl(e.target.value)} />
          <div className="flex flex-wrap gap-2">
            {EVENT_TYPES.map((e) => (
              <button
                key={e}
                onClick={() => toggle(e)}
                className={`rounded-md border px-2 py-1 text-xs ${events.includes(e) ? "bg-accent text-white border-accent" : "border-border hover:bg-muted"}`}
              >
                {e}
              </button>
            ))}
          </div>
          <Button onClick={() => create.mutate()} disabled={!url || events.length === 0 || create.isPending}>
            Create webhook
          </Button>
          {create.isError && <ErrorState error={create.error} />}
        </div>
      </Card>
      <Card>
        <CardHeader title="Registered" />
        {(customer.isPending || (list.isPending && !!cid)) && <SkeletonRows count={3} />}
        {list.isError && <ErrorState error={list.error} onRetry={() => list.refetch()} />}
        {list.data && list.data.length === 0 && <EmptyState title="No webhooks yet" />}
        {list.data && list.data.length > 0 && (
          <Table>
            <THead><TR><TH>URL</TH><TH>Events</TH><TH>Secret</TH><TH>Created</TH><TH /></TR></THead>
            <tbody>
              {list.data.map((w) => (
                <TR key={w.id}>
                  <TD className="font-mono text-xs max-w-md truncate">{w.url}</TD>
                  <TD>
                    <div className="flex flex-wrap gap-1">
                      {w.events.map((ev) => <Badge key={ev}>{ev}</Badge>)}
                    </div>
                  </TD>
                  <TD className="font-mono text-xs">{w.secret_prefix ?? "—"}</TD>
                  <TD>{new Date(w.created_at).toLocaleDateString()}</TD>
                  <TD>
                    <Button size="sm" variant="ghost" onClick={() => del.mutate(w.id)} disabled={del.isPending}>Delete</Button>
                  </TD>
                </TR>
              ))}
            </tbody>
          </Table>
        )}
      </Card>
    </div>
  );
}
