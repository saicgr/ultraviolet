import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useActiveCustomer } from "../lib/customer";
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

export function Quotas() {
  const qc = useQueryClient();
  const { data: customer } = useActiveCustomer();
  const quotas = useQuery({
    queryKey: ["quotas", customer?.id],
    queryFn: () => api.listQuotas(customer!.id),
    enabled: !!customer,
  });
  const [userId, setUserId] = useState("");
  const [bytes, setBytes] = useState("");
  const [cost, setCost] = useState("");
  const create = useMutation({
    mutationFn: () =>
      api.createQuota({
        customer_id: customer!.id,
        user_id: userId || null,
        daily_bytes_cap: Number(bytes),
        daily_cost_cap_usd: Number(cost),
      }),
    onSuccess: () => {
      setUserId("");
      setBytes("");
      setCost("");
      qc.invalidateQueries({ queryKey: ["quotas"] });
    },
  });

  if (!customer) return <EmptyState title="No active customer" />;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Quotas</h1>

      <Card>
        <CardHeader title="Add quota" description="Per-user daily caps. Leave user blank for customer-wide." />
        <div className="grid grid-cols-4 gap-3">
          <Input placeholder="user_id (optional)" value={userId} onChange={(e) => setUserId(e.target.value)} />
          <Input placeholder="daily bytes cap" value={bytes} onChange={(e) => setBytes(e.target.value)} />
          <Input placeholder="daily cost USD cap" value={cost} onChange={(e) => setCost(e.target.value)} />
          <Button
            onClick={() => create.mutate()}
            disabled={!bytes || !cost || create.isPending}
          >
            Add
          </Button>
        </div>
        {create.isError && <div className="mt-3"><ErrorState error={create.error} /></div>}
      </Card>

      <Card>
        <CardHeader title="Current quotas" />
        {quotas.isPending && <SkeletonRows count={3} />}
        {quotas.isError && <ErrorState error={quotas.error} onRetry={() => quotas.refetch()} />}
        {quotas.data && quotas.data.length === 0 && <EmptyState title="No quotas configured" />}
        {quotas.data && quotas.data.length > 0 && (
          <Table>
            <THead>
              <TR>
                <TH>User</TH>
                <TH>Daily bytes</TH>
                <TH>Daily USD</TH>
                <TH>Created</TH>
              </TR>
            </THead>
            <tbody>
              {quotas.data.map((q) => (
                <TR key={q.id}>
                  <TD className="font-mono">{q.user_id ?? "(all)"}</TD>
                  <TD>{q.daily_bytes_cap.toLocaleString()}</TD>
                  <TD>${q.daily_cost_cap_usd.toFixed(2)}</TD>
                  <TD>{new Date(q.created_at).toLocaleString()}</TD>
                </TR>
              ))}
            </tbody>
          </Table>
        )}
      </Card>
    </div>
  );
}
