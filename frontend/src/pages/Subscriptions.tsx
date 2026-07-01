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
} from "../components/ui";
import { useActiveCustomer } from "../lib/customer";

export function Subscriptions() {
  const qc = useQueryClient();
  const customer = useActiveCustomer();
  const cid = customer.data?.id ?? "";
  const [dashboardId, setDashboardId] = useState("");
  const [email, setEmail] = useState("");
  const [cron, setCron] = useState("0 9 * * 1");
  const [channel, setChannel] = useState("email");

  const dashboards = useQuery({
    queryKey: ["dashboards", cid],
    queryFn: () => api.listDashboards(cid),
    enabled: !!cid,
  });
  const subs = useQuery({
    queryKey: ["subscriptions", dashboardId],
    queryFn: () => api.listSubscriptions(dashboardId),
    enabled: !!dashboardId,
  });

  const create = useMutation({
    mutationFn: () =>
      api.createSubscription(dashboardId, { user_email: email, schedule_cron: cron, channel }),
    onSuccess: () => {
      setEmail("");
      qc.invalidateQueries({ queryKey: ["subscriptions", dashboardId] });
    },
  });
  const del = useMutation({
    mutationFn: (id: string) => api.deleteSubscription(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["subscriptions", dashboardId] }),
  });
  const test = useMutation({
    mutationFn: (id: string) => api.testSendSubscription(id),
  });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Subscriptions</h1>
      <Card>
        <CardHeader title="Pick a dashboard" description="Email subscriptions deliver rendered dashboards on a cron." />
        {dashboards.isPending && <SkeletonRows count={3} />}
        {dashboards.isError && <ErrorState error={dashboards.error} />}
        {dashboards.data && dashboards.data.length === 0 && <EmptyState title="No dashboards" />}
        {dashboards.data && dashboards.data.length > 0 && (
          <select
            className="h-10 w-full rounded-md border border-border bg-background px-3 text-sm"
            value={dashboardId}
            onChange={(e) => setDashboardId(e.target.value)}
          >
            <option value="">Select a dashboard…</option>
            {dashboards.data.map((d) => (
              <option key={d.id} value={d.id}>{d.name}</option>
            ))}
          </select>
        )}
      </Card>

      {dashboardId && (
        <>
          <Card>
            <CardHeader title="Add subscription" />
            <div className="grid grid-cols-3 gap-3">
              <Input placeholder="email@team.com" value={email} onChange={(e) => setEmail(e.target.value)} />
              <Input placeholder="cron e.g. 0 9 * * 1" value={cron} onChange={(e) => setCron(e.target.value)} />
              <select
                className="h-10 rounded-md border border-border bg-background px-3 text-sm"
                value={channel}
                onChange={(e) => setChannel(e.target.value)}
              >
                <option value="email">Email</option>
                <option value="slack">Slack</option>
                <option value="webhook">Webhook</option>
              </select>
            </div>
            <div className="mt-3">
              <Button onClick={() => create.mutate()} disabled={!email || create.isPending}>Create</Button>
            </div>
            {create.isError && <div className="mt-3"><ErrorState error={create.error} /></div>}
          </Card>

          <Card>
            <CardHeader title="Existing subscriptions" />
            {subs.isPending && <SkeletonRows count={3} />}
            {subs.isError && <ErrorState error={subs.error} onRetry={() => subs.refetch()} />}
            {subs.data && subs.data.length === 0 && <EmptyState title="No subscriptions yet" />}
            {subs.data && subs.data.length > 0 && (
              <Table>
                <THead><TR><TH>Address</TH><TH>Cron</TH><TH>Channel</TH><TH /></TR></THead>
                <tbody>
                  {subs.data.map((s) => (
                    <TR key={s.id}>
                      <TD>{s.address}</TD>
                      <TD className="font-mono">{s.schedule_cron}</TD>
                      <TD>{s.channel}</TD>
                      <TD>
                        <div className="flex gap-2">
                          <Button size="sm" variant="outline" onClick={() => test.mutate(s.id)} disabled={test.isPending}>Test send</Button>
                          <Button size="sm" variant="ghost" onClick={() => del.mutate(s.id)} disabled={del.isPending}>Delete</Button>
                        </div>
                      </TD>
                    </TR>
                  ))}
                </tbody>
              </Table>
            )}
          </Card>
        </>
      )}
    </div>
  );
}
