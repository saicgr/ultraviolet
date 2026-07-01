import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api, PRAnalysisRow } from "../lib/api";
import { useActiveCustomer } from "../lib/customer";
import { Badge, Card, CardHeader, EmptyState, ErrorState, SkeletonRows, Table, TD, TH, THead, TR } from "../components/ui";

function dqBadge(impact: PRAnalysisRow["dq_impact"]) {
  if (impact === "failing") return <Badge variant="danger">failing</Badge>;
  if (impact === "touched") return <Badge variant="warn">touched</Badge>;
  return <Badge>none</Badge>;
}

function conclusionBadge(c: string) {
  if (c === "action_required") return <Badge variant="danger">action required</Badge>;
  if (c === "neutral") return <Badge variant="warn">neutral</Badge>;
  return <Badge variant="success">success</Badge>;
}

export function PullRequests() {
  const customer = useActiveCustomer();
  const [selected, setSelected] = useState<number | null>(null);

  const list = useQuery({
    queryKey: ["pr-analysis", customer.data?.id],
    queryFn: () => api.listPRAnalysis(),
    enabled: !!customer.data,
  });

  if (!customer.data) return <EmptyState title="No active customer" />;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Pull Requests</h1>
      <Card>
        <CardHeader title="Analyzed pull requests" description="Impact + data-quality analysis posted to GitHub on each PR." />
        {list.isPending && <SkeletonRows count={5} />}
        {list.isError && <ErrorState error={list.error} onRetry={() => list.refetch()} />}
        {list.data?.length === 0 && (
          <EmptyState title="No PRs analyzed yet" hint="Install the GitHub App and open a PR touching your models." />
        )}
        {!!list.data?.length && (
          <Table>
            <THead>
              <TR>
                <TH>Repo</TH><TH>PR</TH><TH>Commit</TH><TH>Impacted</TH><TH>DQ</TH><TH>Conclusion</TH><TH>Analyzed</TH>
              </TR>
            </THead>
            <tbody>
              {list.data.map((row) => (
                <TR key={row.id} className="cursor-pointer hover:bg-muted">
                  <TD className="font-mono">{row.repo}</TD>
                  <TD>
                    <button className="text-accent underline" onClick={() => setSelected(row.id)}>
                      #{row.pr_number}
                    </button>
                  </TD>
                  <TD className="font-mono">{row.head_sha.slice(0, 7)}</TD>
                  <TD className="tabular-nums">{row.impacted_count}</TD>
                  <TD>{dqBadge(row.dq_impact)}</TD>
                  <TD>{conclusionBadge(row.conclusion)}</TD>
                  <TD>{new Date(row.created_at).toLocaleString()}</TD>
                </TR>
              ))}
            </tbody>
          </Table>
        )}
      </Card>

      {selected !== null && <PRDetail id={selected} onClose={() => setSelected(null)} />}
    </div>
  );
}

function PRDetail({ id, onClose }: { id: number; onClose: () => void }) {
  const detail = useQuery({
    queryKey: ["pr-analysis-detail", id],
    queryFn: () => api.getPRAnalysis(id),
  });
  return (
    <Card>
      <div className="flex items-center justify-between">
        <CardHeader title="PR detail" description="Impacted downstream nodes and data-quality status." />
        <button className="text-xs text-foreground/60 underline" onClick={onClose}>close</button>
      </div>
      {detail.isPending && <SkeletonRows count={4} />}
      {detail.isError && <ErrorState error={detail.error} onRetry={() => detail.refetch()} />}
      {detail.data && (
        <div className="space-y-4">
          <a
            href={detail.data.pull_request_url}
            target="_blank"
            rel="noreferrer"
            className="inline-block text-sm text-accent underline"
          >
            View PR on GitHub ↗
          </a>
          <div>
            <h3 className="mb-2 text-sm font-medium">Detected changes</h3>
            {detail.data.changes?.length ? (
              <ul className="list-disc pl-5 text-sm">
                {detail.data.changes.map((c, i) => (
                  <li key={i}><span className="font-mono">{c.FQN}</span> — {c.Kind}</li>
                ))}
              </ul>
            ) : <p className="text-sm text-foreground/60">none</p>}
          </div>
          <div>
            <h3 className="mb-2 text-sm font-medium">Impacted downstream</h3>
            {detail.data.hits?.length ? (
              <Table>
                <THead><TR><TH>Downstream</TH><TH>Hops</TH><TH>Severity</TH><TH /></TR></THead>
                <tbody>
                  {detail.data.hits.map((h, i) => (
                    <TR key={i}>
                      <TD className="font-mono">{h.FQN}</TD>
                      <TD className="tabular-nums">{h.Hops}</TD>
                      <TD>{h.Severity === "breaking" ? <Badge variant="danger">breaking</Badge> : <Badge variant="warn">{h.Severity}</Badge>}</TD>
                      <TD>
                        <Link className="text-accent underline" to={`/lineage?fqn=${encodeURIComponent(h.FQN)}`}>
                          lineage
                        </Link>
                      </TD>
                    </TR>
                  ))}
                </tbody>
              </Table>
            ) : <p className="text-sm text-foreground/60">no downstream consumers — safe to merge</p>}
          </div>
          {!!detail.data.dq_refs?.length && (
            <div>
              <h3 className="mb-2 text-sm font-medium">Data-quality status</h3>
              <ul className="list-disc pl-5 text-sm">
                {detail.data.dq_refs.map((d, i) => (
                  <li key={i}>
                    <span className="font-mono">{d.table_fqn}</span> →{" "}
                    {d.status === "fail" || d.status === "error"
                      ? <Badge variant="danger">{d.status}</Badge>
                      : <Badge variant="success">{d.status}</Badge>}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}
    </Card>
  );
}
