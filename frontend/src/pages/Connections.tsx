import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "../lib/api";
import { useActiveCustomer } from "../lib/customer";
import { Button, Card, CardHeader, EmptyState, ErrorState, Input, SkeletonRows, Table, TD, TH, THead, TR, Badge } from "../components/ui";

export function Connections() {
  const qc = useQueryClient();
  const { data: customer } = useActiveCustomer();
  const conns = useQuery({
    queryKey: ["connections", customer?.id],
    queryFn: () => api.listConnections(customer!.id),
    enabled: !!customer,
  });
  const [name, setName] = useState("");
  const [warehouse, setWarehouse] = useState("bigquery");
  const [creds, setCreds] = useState('{"project_id":"my-project"}');
  const create = useMutation({
    mutationFn: () =>
      api.createConnection(customer!.id, {
        warehouse_type: warehouse,
        name,
        credentials: JSON.parse(creds),
      }),
    onSuccess: () => {
      setName("");
      qc.invalidateQueries({ queryKey: ["connections"] });
    },
  });

  if (!customer) return <EmptyState title="No active customer" />;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Warehouse Connections</h1>

      <Card>
        <CardHeader title="Add connection" description="Credentials are encrypted with AES-256-GCM at rest." />
        <div className="grid grid-cols-3 gap-3">
          <Input placeholder="Connection name" value={name} onChange={(e) => setName(e.target.value)} />
          <select
            className="h-10 rounded-md border border-border bg-background px-3 text-sm"
            value={warehouse}
            onChange={(e) => setWarehouse(e.target.value)}
          >
            <option value="bigquery">BigQuery</option>
            <option value="snowflake">Snowflake</option>
          </select>
          <Button onClick={() => create.mutate()} disabled={!name || create.isPending}>
            {create.isPending ? "Saving..." : "Add"}
          </Button>
        </div>
        <textarea
          className="mt-3 w-full h-32 rounded-md border border-border bg-background p-3 font-mono text-xs"
          value={creds}
          onChange={(e) => setCreds(e.target.value)}
          placeholder="Credentials JSON"
        />
        {create.isError && <ErrorState error={create.error} />}
      </Card>

      <Card>
        <CardHeader title="Existing" />
        {conns.isPending && <SkeletonRows count={4} />}
        {conns.isError && <ErrorState error={conns.error} onRetry={() => conns.refetch()} />}
        {conns.data && conns.data.length === 0 && (
          <EmptyState title="No connections yet" hint="Add a BigQuery or Snowflake connection above to start syncing." />
        )}
        {conns.data && conns.data.length > 0 && (
          <Table>
            <THead>
              <TR>
                <TH>Name</TH>
                <TH>Warehouse</TH>
                <TH>Storage</TH>
                <TH>Created</TH>
              </TR>
            </THead>
            <tbody>
              {conns.data.map((c) => (
                <TR key={c.id}>
                  <TD>{c.name}</TD>
                  <TD><Badge>{c.warehouse_type}</Badge></TD>
                  <TD>{c.storage_mode}</TD>
                  <TD>{new Date(c.created_at).toLocaleString()}</TD>
                </TR>
              ))}
            </tbody>
          </Table>
        )}
      </Card>
    </div>
  );
}
