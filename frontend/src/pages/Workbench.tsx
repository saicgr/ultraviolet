import { useState, type ReactNode } from "react";
import { Link } from "react-router-dom";
import { Badge, Button, Card, CardHeader, ErrorState, Table, TD, TH, THead, TR } from "../components/ui";
import { api, type WorkbenchResult } from "../lib/api";

const SAMPLES = [
  "SELECT range AS n, range * range AS square FROM range(1, 11)",
  "SELECT count(*) AS rows, sum(i) AS total FROM range(1, 5000000) t(i)",
  "SELECT i % 7 AS bucket, count(*) AS n FROM range(1, 2000000) t(i) GROUP BY 1 ORDER BY 1",
];

export function Workbench() {
  const [sql, setSQL] = useState(SAMPLES[0]);
  const [out, setOut] = useState<WorkbenchResult | null>(null);
  const [err, setErr] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  const run = async () => {
    setBusy(true);
    setErr(null);
    try {
      setOut(await api.workbenchRun(sql));
    } catch (e) {
      setErr(e);
      setOut(null);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Workbench</h1>
      <Card>
        <CardHeader
          title="SQL"
          description="Runs on the real DuckDB engine that powers the proxy. Every run is logged and feeds the Savings Dashboard."
        />
        <textarea
          className="w-full h-40 rounded-md border border-border bg-background p-3 font-mono text-xs"
          value={sql}
          onChange={(e) => setSQL(e.target.value)}
        />
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <Button onClick={run} disabled={busy || !sql.trim()}>
            {busy ? "Running…" : "Run"}
          </Button>
          {SAMPLES.map((s, i) => (
            <button
              key={i}
              type="button"
              onClick={() => setSQL(s)}
              className="h-8 rounded-md border border-border px-2 text-xs text-foreground/70 hover:bg-muted"
            >
              sample {i + 1}
            </button>
          ))}
        </div>
        {err ? <div className="mt-3"><ErrorState error={err} /></div> : null}
      </Card>

      {out && (
        <>
          <Card>
            <CardHeader title="Run stats" description="This is exactly what gets added to your dashboard." />
            <div className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
              <Stat label="Route" value={<Badge variant="success">{out.route}</Badge>} />
              <Stat label="Rows" value={out.row_count.toLocaleString()} />
              <Stat label="Bytes scanned" value={out.bytes_scanned.toLocaleString()} />
              <Stat label="Warehouse cost avoided" value={`$${out.estimated_cost_usd.toFixed(6)}`} />
            </div>
            <p className="mt-3 text-xs text-foreground/60">
              Ran in {out.duration_ms}ms on DuckDB. The cost shown is the warehouse-equivalent ($/TiB ×
              bytes) you avoided — i.e. the saving. Open the{" "}
              <Link to="/" className="underline">Savings Dashboard</Link> or{" "}
              <Link to="/queries" className="underline">Queries</Link> to see it counted.
            </p>
          </Card>

          <Card>
            <CardHeader title={`Result${out.truncated ? ` (showing first ${out.rows.length} of ${out.row_count.toLocaleString()})` : ""}`} />
            {out.rows.length === 0 ? (
              <p className="text-sm text-foreground/60">No rows.</p>
            ) : (
              <div className="overflow-x-auto">
                <Table>
                  <THead><TR>{out.columns.map((c) => <TH key={c}>{c}</TH>)}</TR></THead>
                  <tbody>
                    {out.rows.map((row, ri) => (
                      <TR key={ri}>
                        {row.map((cell, ci) => <TD key={ci} className="font-mono">{cell}</TD>)}
                      </TR>
                    ))}
                  </tbody>
                </Table>
              </div>
            )}
          </Card>
        </>
      )}
    </div>
  );
}

function Stat({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="rounded-md border border-border p-3">
      <div className="text-xs text-foreground/60">{label}</div>
      <div className="mt-1 text-lg font-semibold">{value}</div>
    </div>
  );
}
