import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "../lib/api";
import { useActiveCustomer } from "../lib/customer";
import { Button, Card, CardHeader, EmptyState, ErrorState, Input, SkeletonRows, Table, TD, TH, THead, TR, Badge } from "../components/ui";

export function APIKeys() {
  const qc = useQueryClient();
  const { data: customer } = useActiveCustomer();
  const keys = useQuery({
    queryKey: ["api-keys", customer?.id],
    queryFn: () => api.listAPIKeys(customer!.id),
    enabled: !!customer,
  });
  const [desc, setDesc] = useState("");
  const [created, setCreated] = useState<string | null>(null);
  const create = useMutation({
    mutationFn: () => api.createAPIKey(customer!.id, desc || undefined),
    onSuccess: (res) => {
      setCreated(res.api_key);
      setDesc("");
      qc.invalidateQueries({ queryKey: ["api-keys"] });
    },
  });
  const revoke = useMutation({
    mutationFn: (id: string) => api.revokeAPIKey(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["api-keys"] }),
  });

  if (!customer) return <EmptyState title="No active customer" />;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">API Keys</h1>

      <Card>
        <CardHeader title="Create new" description="The full key is shown once and never again." />
        <div className="flex gap-3">
          <Input placeholder="Description (optional)" value={desc} onChange={(e) => setDesc(e.target.value)} />
          <Button onClick={() => create.mutate()} disabled={create.isPending}>
            Create
          </Button>
        </div>
        {created && (
          <div className="mt-4 rounded-md border border-emerald-700/40 bg-emerald-950/40 p-3 font-mono text-sm">
            {created}
            <p className="text-xs mt-2 text-emerald-300/80">Copy this now. It will not be shown again.</p>
          </div>
        )}
        {create.isError && <ErrorState error={create.error} />}
      </Card>

      <Card>
        <CardHeader title="Existing" />
        {keys.isPending && <SkeletonRows count={3} />}
        {keys.isError && <ErrorState error={keys.error} onRetry={() => keys.refetch()} />}
        {keys.data && keys.data.length === 0 && (
          <EmptyState title="No keys yet" hint="Create one above to authenticate the proxy or REST API." />
        )}
        {keys.data && keys.data.length > 0 && (
          <Table>
            <THead>
              <TR>
                <TH>Prefix</TH>
                <TH>Description</TH>
                <TH>Last used</TH>
                <TH>Status</TH>
                <TH></TH>
              </TR>
            </THead>
            <tbody>
              {keys.data.map((k) => (
                <TR key={k.id}>
                  <TD className="font-mono">{k.key_prefix}…</TD>
                  <TD>{k.description ?? "—"}</TD>
                  <TD>{k.last_used_at ? new Date(k.last_used_at).toLocaleString() : "—"}</TD>
                  <TD>{k.revoked_at ? <Badge variant="danger">revoked</Badge> : <Badge variant="success">active</Badge>}</TD>
                  <TD>
                    {!k.revoked_at && (
                      <Button size="sm" variant="outline" onClick={() => revoke.mutate(k.id)}>
                        Revoke
                      </Button>
                    )}
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
