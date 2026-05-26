import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import {
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

export function SemanticSearch() {
  const [q, setQ] = useState("");
  const search = useQuery({
    queryKey: ["semantic-search", q],
    queryFn: () => api.semanticSearch(q, 10),
    enabled: q.length >= 3,
    retry: false,
  });

  const errMsg = search.error instanceof Error ? search.error.message : "";
  const notDeployed = /404|not found|not deployed/i.test(errMsg);

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Semantic Search</h1>
      <Card>
        <CardHeader title="Query" description="Natural-language search over catalog metadata embeddings (top-10)." />
        <Input
          placeholder="tables tracking monthly recurring revenue"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
      </Card>
      <Card>
        <CardHeader title="Top hits" />
        {q.length < 3 && <EmptyState title="Enter a query" hint="At least 3 characters." />}
        {search.isPending && q.length >= 3 && <SkeletonRows count={5} />}
        {search.isError && notDeployed && (
          <EmptyState
            title="Endpoint not deployed"
            hint="The semantic-search endpoint is not available in this environment."
          />
        )}
        {search.isError && !notDeployed && (
          <ErrorState error={search.error} onRetry={() => search.refetch()} />
        )}
        {search.data && search.data.length === 0 && <EmptyState title="No matches" />}
        {search.data && search.data.length > 0 && (
          <Table>
            <THead>
              <TR>
                <TH>FQN</TH>
                <TH>Score</TH>
                <TH>Snippet</TH>
              </TR>
            </THead>
            <tbody>
              {search.data.map((h) => (
                <TR key={h.fqn}>
                  <TD className="font-mono">{h.fqn}</TD>
                  <TD>{h.score.toFixed(3)}</TD>
                  <TD className="text-foreground/70">{h.snippet ?? "—"}</TD>
                </TR>
              ))}
            </tbody>
          </Table>
        )}
      </Card>
    </div>
  );
}
