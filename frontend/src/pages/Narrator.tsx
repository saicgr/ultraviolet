import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { Card, CardHeader, EmptyState, ErrorState, SkeletonRows } from "../components/ui";
import { useActiveCustomer } from "../lib/customer";

// Minimal inline markdown renderer covering bold/italic/code + paragraphs.
// Avoids pulling in a markdown library for ~3 paragraphs of generated prose.
function renderMd(src: string): React.ReactNode {
  const paragraphs = src.split(/\n\s*\n/).map((p) => p.trim()).filter(Boolean);
  return paragraphs.map((para, i) => {
    const parts: React.ReactNode[] = [];
    let rest = para;
    const re = /(\*\*[^*]+\*\*|\*[^*]+\*|`[^`]+`)/g;
    let m: RegExpExecArray | null;
    let last = 0;
    while ((m = re.exec(rest))) {
      if (m.index > last) parts.push(rest.slice(last, m.index));
      const tok = m[0];
      if (tok.startsWith("**")) parts.push(<strong key={`${i}-${m.index}`}>{tok.slice(2, -2)}</strong>);
      else if (tok.startsWith("`")) parts.push(<code key={`${i}-${m.index}`} className="font-mono text-xs bg-muted px-1 py-0.5 rounded">{tok.slice(1, -1)}</code>);
      else parts.push(<em key={`${i}-${m.index}`}>{tok.slice(1, -1)}</em>);
      last = m.index + tok.length;
    }
    if (last < rest.length) parts.push(rest.slice(last));
    return <p key={i} className="text-sm leading-relaxed text-foreground/85 mb-4">{parts}</p>;
  });
}

export function Narrator() {
  const customer = useActiveCustomer();
  const cid = customer.data?.id ?? "";
  const q = useQuery({
    queryKey: ["narrative", cid],
    queryFn: () => api.catalogNarrative(cid),
    enabled: !!cid,
  });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Catalog Narrator</h1>
      <Card>
        <CardHeader title="What this warehouse is doing" description="LLM-generated prose summary of your active datasets, hot paths, and recent shifts." />
        {(customer.isPending || (q.isPending && !!cid)) && <SkeletonRows count={6} />}
        {q.isError && <ErrorState error={q.error} onRetry={() => q.refetch()} />}
        {q.data && !q.data.narrative && <EmptyState title="Nothing to narrate yet" hint="The narrator needs query history and metadata to summarize." />}
        {q.data && q.data.narrative && (
          <div className="prose-narrative">{renderMd(q.data.narrative)}</div>
        )}
      </Card>
    </div>
  );
}
