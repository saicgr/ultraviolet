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

export function AccessReviews() {
  const qc = useQueryClient();
  const customer = useActiveCustomer();
  const cid = customer.data?.id ?? "";
  const [title, setTitle] = useState("");
  const [decisionReview, setDecisionReview] = useState<string | null>(null);
  const [subject, setSubject] = useState("");
  const [reason, setReason] = useState("");

  const list = useQuery({
    queryKey: ["access-reviews", cid],
    queryFn: () => api.listAccessReviews(cid),
    enabled: !!cid,
  });
  const open = useMutation({
    mutationFn: () => api.openAccessReview({ customer_id: cid, title }),
    onSuccess: () => {
      setTitle("");
      qc.invalidateQueries({ queryKey: ["access-reviews", cid] });
    },
  });
  const close = useMutation({
    mutationFn: (id: string) => api.closeAccessReview(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["access-reviews", cid] }),
  });
  const decide = useMutation({
    mutationFn: ({ id, decision }: { id: string; decision: "approve" | "revoke" }) =>
      api.recordAccessReviewDecision(id, { subject, decision, reason: reason || undefined }),
    onSuccess: () => {
      setSubject(""); setReason("");
    },
  });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Access Reviews</h1>
      <Card>
        <CardHeader title="Open a review" description="Periodic certification of who has access to which datasets." />
        <div className="flex gap-2">
          <Input placeholder="Q2 quarterly review" value={title} onChange={(e) => setTitle(e.target.value)} />
          <Button onClick={() => open.mutate()} disabled={!title || open.isPending}>Open</Button>
        </div>
        {open.isError && <div className="mt-2"><ErrorState error={open.error} /></div>}
      </Card>

      <Card>
        <CardHeader title="Reviews" />
        {(customer.isPending || (list.isPending && !!cid)) && <SkeletonRows count={3} />}
        {list.isError && <ErrorState error={list.error} onRetry={() => list.refetch()} />}
        {list.data && list.data.length === 0 && <EmptyState title="No reviews" />}
        {list.data && list.data.length > 0 && (
          <Table>
            <THead><TR><TH>Title</TH><TH>Status</TH><TH>Opened</TH><TH /></TR></THead>
            <tbody>
              {list.data.map((r) => (
                <TR key={r.id}>
                  <TD>{r.title}</TD>
                  <TD><Badge variant={r.status === "open" ? "warn" : "success"}>{r.status}</Badge></TD>
                  <TD>{new Date(r.created_at).toLocaleDateString()}</TD>
                  <TD>
                    <div className="flex gap-2">
                      <Button size="sm" variant="outline" onClick={() => setDecisionReview(r.id)} disabled={r.status === "closed"}>Decide</Button>
                      <Button size="sm" variant="ghost" onClick={() => close.mutate(r.id)} disabled={r.status === "closed" || close.isPending}>Close</Button>
                    </div>
                  </TD>
                </TR>
              ))}
            </tbody>
          </Table>
        )}
      </Card>

      {decisionReview && (
        <Card>
          <CardHeader title="Record decision" description={`Review: ${decisionReview}`} />
          <div className="space-y-3">
            <Input placeholder="user@team.com or role:analyst" value={subject} onChange={(e) => setSubject(e.target.value)} />
            <Input placeholder="Reason (optional)" value={reason} onChange={(e) => setReason(e.target.value)} />
            <div className="flex gap-2">
              <Button onClick={() => decide.mutate({ id: decisionReview, decision: "approve" })} disabled={!subject || decide.isPending}>Approve</Button>
              <Button variant="outline" onClick={() => decide.mutate({ id: decisionReview, decision: "revoke" })} disabled={!subject || decide.isPending}>Revoke</Button>
              <Button variant="ghost" onClick={() => setDecisionReview(null)}>Done</Button>
            </div>
            {decide.isError && <ErrorState error={decide.error} />}
          </div>
        </Card>
      )}
    </div>
  );
}
