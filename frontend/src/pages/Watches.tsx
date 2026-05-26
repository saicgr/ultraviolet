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

export function Watches() {
  const qc = useQueryClient();
  const customer = useActiveCustomer();
  const cid = customer.data?.id ?? "";
  const [fqn, setFqn] = useState("");
  const [email, setEmail] = useState("");

  const list = useQuery({
    queryKey: ["watches", cid],
    queryFn: () => api.listLineageWatches(cid),
    enabled: !!cid,
  });
  const create = useMutation({
    mutationFn: () => api.createLineageWatch({ customer_id: cid, fqn, notify_email: email || undefined }),
    onSuccess: () => {
      setFqn(""); setEmail("");
      qc.invalidateQueries({ queryKey: ["watches", cid] });
    },
  });
  const del = useMutation({
    mutationFn: (id: string) => api.deleteLineageWatch(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["watches", cid] }),
  });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Lineage Watches</h1>
      <Card>
        <CardHeader title="Watch a table" description="Get notified when an upstream or downstream of a watched FQN changes." />
        <div className="flex gap-2">
          <Input placeholder="warehouse.schema.table" value={fqn} onChange={(e) => setFqn(e.target.value)} />
          <Input placeholder="notify-email (optional)" value={email} onChange={(e) => setEmail(e.target.value)} />
          <Button onClick={() => create.mutate()} disabled={!fqn || create.isPending}>Watch</Button>
        </div>
        {create.isError && <div className="mt-2"><ErrorState error={create.error} /></div>}
      </Card>
      <Card>
        <CardHeader title="Active watches" />
        {(customer.isPending || (list.isPending && !!cid)) && <SkeletonRows count={3} />}
        {list.isError && <ErrorState error={list.error} onRetry={() => list.refetch()} />}
        {list.data && list.data.length === 0 && <EmptyState title="No watches yet" />}
        {list.data && list.data.length > 0 && (
          <Table>
            <THead><TR><TH>FQN</TH><TH>Notify</TH><TH>Created</TH><TH /></TR></THead>
            <tbody>
              {list.data.map((w) => (
                <TR key={w.id}>
                  <TD className="font-mono">{w.fqn}</TD>
                  <TD>{w.notify_email ?? "—"}</TD>
                  <TD>{new Date(w.created_at).toLocaleDateString()}</TD>
                  <TD><Button size="sm" variant="ghost" onClick={() => del.mutate(w.id)} disabled={del.isPending}>Stop</Button></TD>
                </TR>
              ))}
            </tbody>
          </Table>
        )}
      </Card>
    </div>
  );
}
