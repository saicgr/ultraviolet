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

export function ScheduledQueries() {
  const qc = useQueryClient();
  const customer = useActiveCustomer();
  const cid = customer.data?.id ?? "";
  const [name, setName] = useState("");
  const [sql, setSql] = useState("");
  const [cron, setCron] = useState("0 6 * * *");
  const [selected, setSelected] = useState<string | null>(null);

  const list = useQuery({
    queryKey: ["scheduled", cid],
    queryFn: () => api.listScheduledQueries(cid),
    enabled: !!cid,
  });
  const runs = useQuery({
    queryKey: ["scheduled-runs", selected],
    queryFn: () => api.scheduledQueryRuns(selected!),
    enabled: !!selected,
  });
  const create = useMutation({
    mutationFn: () => api.createScheduledQuery({ customer_id: cid, name, sql, cron }),
    onSuccess: () => {
      setName(""); setSql("");
      qc.invalidateQueries({ queryKey: ["scheduled", cid] });
    },
  });
  const del = useMutation({
    mutationFn: (id: string) => api.deleteScheduledQuery(id),
    onSuccess: () => {
      if (selected) setSelected(null);
      qc.invalidateQueries({ queryKey: ["scheduled", cid] });
    },
  });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Scheduled Queries</h1>
      <Card>
        <CardHeader title="New schedule" description="Cron-driven SQL with run history." />
        <div className="space-y-3">
          <Input placeholder="Name" value={name} onChange={(e) => setName(e.target.value)} />
          <textarea
            className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm font-mono h-28"
            placeholder="SELECT … FROM …"
            value={sql}
            onChange={(e) => setSql(e.target.value)}
          />
          <Input placeholder="cron" value={cron} onChange={(e) => setCron(e.target.value)} />
          <Button onClick={() => create.mutate()} disabled={!name || !sql || create.isPending}>Create</Button>
          {create.isError && <ErrorState error={create.error} />}
        </div>
      </Card>

      <Card>
        <CardHeader title="Schedules" />
        {(customer.isPending || (list.isPending && !!cid)) && <SkeletonRows count={3} />}
        {list.isError && <ErrorState error={list.error} onRetry={() => list.refetch()} />}
        {list.data && list.data.length === 0 && <EmptyState title="No scheduled queries" />}
        {list.data && list.data.length > 0 && (
          <Table>
            <THead><TR><TH>Name</TH><TH>Cron</TH><TH>Last run</TH><TH /></TR></THead>
            <tbody>
              {list.data.map((s) => (
                <TR key={s.id}>
                  <TD>{s.name}</TD>
                  <TD className="font-mono">{s.cron}</TD>
                  <TD>{s.last_run_at ? new Date(s.last_run_at).toLocaleString() : "—"}</TD>
                  <TD>
                    <div className="flex gap-2">
                      <Button size="sm" variant="outline" onClick={() => setSelected(s.id)}>History</Button>
                      <Button size="sm" variant="ghost" onClick={() => del.mutate(s.id)} disabled={del.isPending}>Delete</Button>
                    </div>
                  </TD>
                </TR>
              ))}
            </tbody>
          </Table>
        )}
      </Card>

      {selected && (
        <Card>
          <CardHeader title="Run history" />
          {runs.isPending && <SkeletonRows count={3} />}
          {runs.isError && <ErrorState error={runs.error} />}
          {runs.data && runs.data.length === 0 && <EmptyState title="No runs yet" />}
          {runs.data && runs.data.length > 0 && (
            <Table>
              <THead><TR><TH>Started</TH><TH>Finished</TH><TH>Status</TH><TH>Error</TH></TR></THead>
              <tbody>
                {runs.data.map((r) => (
                  <TR key={r.id}>
                    <TD>{new Date(r.started_at).toLocaleString()}</TD>
                    <TD>{r.finished_at ? new Date(r.finished_at).toLocaleString() : "—"}</TD>
                    <TD><Badge variant={r.status === "success" ? "success" : r.status === "error" ? "danger" : "warn"}>{r.status}</Badge></TD>
                    <TD className="text-xs text-rose-200">{r.error ?? ""}</TD>
                  </TR>
                ))}
              </tbody>
            </Table>
          )}
        </Card>
      )}
    </div>
  );
}
