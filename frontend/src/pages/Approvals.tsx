import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import {
  Button,
  Card,
  CardHeader,
  EmptyState,
  ErrorState,
  SkeletonRows,
  Table,
  TD,
  TH,
  THead,
  TR,
} from "../components/ui";

export function Approvals() {
  const qc = useQueryClient();
  const approvals = useQuery({
    queryKey: ["approvals", "pending"],
    queryFn: () => api.listApprovals("pending"),
  });
  const decide = useMutation({
    mutationFn: ({ id, decision }: { id: string; decision: "approve" | "deny" }) =>
      api.decideApproval(id, decision),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["approvals"] }),
  });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Query Approvals</h1>
      <Card>
        <CardHeader title="Pending" description="Queries that exceeded a guardrail and require human approval." />
        {approvals.isPending && <SkeletonRows count={4} />}
        {approvals.isError && <ErrorState error={approvals.error} onRetry={() => approvals.refetch()} />}
        {approvals.data && approvals.data.length === 0 && <EmptyState title="No pending approvals" />}
        {approvals.data && approvals.data.length > 0 && (
          <Table>
            <THead>
              <TR>
                <TH>User</TH>
                <TH>SQL</TH>
                <TH>Est. bytes</TH>
                <TH>Est. cost</TH>
                <TH>Requested</TH>
                <TH />
              </TR>
            </THead>
            <tbody>
              {approvals.data.map((a) => (
                <TR key={a.id}>
                  <TD className="font-mono">{a.user_id}</TD>
                  <TD className="font-mono text-xs max-w-md truncate">{a.sql}</TD>
                  <TD>{a.estimated_bytes.toLocaleString()}</TD>
                  <TD>${a.estimated_cost_usd.toFixed(2)}</TD>
                  <TD>{new Date(a.created_at).toLocaleString()}</TD>
                  <TD>
                    <div className="flex gap-2">
                      <Button
                        size="sm"
                        onClick={() => decide.mutate({ id: a.id, decision: "approve" })}
                        disabled={decide.isPending}
                      >
                        Approve
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => decide.mutate({ id: a.id, decision: "deny" })}
                        disabled={decide.isPending}
                      >
                        Deny
                      </Button>
                    </div>
                  </TD>
                </TR>
              ))}
            </tbody>
          </Table>
        )}
        {decide.isError && <div className="mt-3"><ErrorState error={decide.error} /></div>}
      </Card>
    </div>
  );
}
