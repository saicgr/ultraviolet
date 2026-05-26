import { useState } from "react";
import { Button, Card, CardHeader, ErrorState } from "../components/ui";
import { api } from "../lib/api";

export function Workbench() {
  const [sql, setSQL] = useState("SELECT 1 AS x");
  const [out, setOut] = useState<unknown>(null);
  const [err, setErr] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  const run = async () => {
    setBusy(true);
    setErr(null);
    try {
      const r = await api.workbenchRun(sql);
      setOut(r);
    } catch (e) {
      setErr(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Workbench</h1>
      <Card>
        <CardHeader title="SQL" description="Runs through the same router that powers your BI tools." />
        <textarea
          className="w-full h-48 rounded-md border border-border bg-background p-3 font-mono text-xs"
          value={sql}
          onChange={(e) => setSQL(e.target.value)}
        />
        <div className="mt-3 flex gap-3">
          <Button onClick={run} disabled={busy}>
            {busy ? "Running…" : "Run"}
          </Button>
        </div>
        {err ? <ErrorState error={err} /> : null}
      </Card>
      {out !== null && (
        <Card>
          <CardHeader title="Result" />
          <pre className="text-xs overflow-x-auto whitespace-pre-wrap">{JSON.stringify(out, null, 2)}</pre>
        </Card>
      )}
    </div>
  );
}
