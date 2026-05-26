import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "../lib/api";
import { useActiveCustomer } from "../lib/customer";
import { Button, Card, CardHeader, EmptyState, ErrorState, Input, SkeletonRows, Table, TD, TH, THead, TR, Badge } from "../components/ui";

export function Sync() {
  const qc = useQueryClient();
  const { data: customer } = useActiveCustomer();
  const conns = useQuery({
    queryKey: ["connections", customer?.id],
    queryFn: () => api.listConnections(customer!.id),
    enabled: !!customer,
  });
  const [connId, setConnId] = useState<string>("");
  const tables = useQuery({
    queryKey: ["tables", connId],
    queryFn: () => api.listSyncedTables(connId),
    enabled: !!connId,
  });
  const [schema, setSchema] = useState("");
  const [table, setTable] = useState("");
  const [mode, setMode] = useState("watermark");
  const create = useMutation({
    mutationFn: () =>
      api.createSyncedTable(connId, { schema_name: schema, table_name: table, sync_mode: mode }),
    onSuccess: () => {
      setSchema("");
      setTable("");
      qc.invalidateQueries({ queryKey: ["tables"] });
    },
  });

  if (!customer) return <EmptyState title="No active customer" />;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Table Sync</h1>

      <Card>
        <CardHeader title="Connection" />
        <select
          className="h-10 rounded-md border border-border bg-background px-3 text-sm"
          value={connId}
          onChange={(e) => setConnId(e.target.value)}
        >
          <option value="">Select…</option>
          {(conns.data ?? []).map((c) => (
            <option key={c.id} value={c.id}>
              {c.name} ({c.warehouse_type})
            </option>
          ))}
        </select>
      </Card>

      {connId && (
        <>
          <Card>
            <CardHeader title="Add synced table" />
            <div className="grid grid-cols-4 gap-3">
              <Input placeholder="schema" value={schema} onChange={(e) => setSchema(e.target.value)} />
              <Input placeholder="table" value={table} onChange={(e) => setTable(e.target.value)} />
              <select
                className="h-10 rounded-md border border-border bg-background px-3 text-sm"
                value={mode}
                onChange={(e) => setMode(e.target.value)}
              >
                <option value="watermark">watermark</option>
                <option value="cdc_native">cdc_native</option>
                <option value="manual">manual</option>
              </select>
              <Button onClick={() => create.mutate()} disabled={!schema || !table || create.isPending}>
                Add
              </Button>
            </div>
            {create.isError && <ErrorState error={create.error} />}
          </Card>

          <Card>
            <CardHeader title="Synced" />
            {tables.isPending && <SkeletonRows count={3} />}
            {tables.isError && <ErrorState error={tables.error} onRetry={() => tables.refetch()} />}
            {tables.data && tables.data.length === 0 && (
              <EmptyState title="No synced tables" hint="Add a table above to sync from the warehouse to Iceberg." />
            )}
            {tables.data && tables.data.length > 0 && (
              <Table>
                <THead>
                  <TR>
                    <TH>Schema.Table</TH>
                    <TH>Mode</TH>
                    <TH>State</TH>
                    <TH>Rows</TH>
                    <TH>Last sync</TH>
                  </TR>
                </THead>
                <tbody>
                  {tables.data.map((t) => (
                    <TR key={t.id}>
                      <TD className="font-mono">{t.schema_name}.{t.table_name}</TD>
                      <TD>{t.sync_mode}</TD>
                      <TD>
                        <Badge variant={t.state === "live" ? "success" : t.state === "error" ? "danger" : "warn"}>
                          {t.state}
                        </Badge>
                      </TD>
                      <TD>{t.row_count.toLocaleString()}</TD>
                      <TD>{t.last_sync_at ? new Date(t.last_sync_at).toLocaleString() : "—"}</TD>
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
