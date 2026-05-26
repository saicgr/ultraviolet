import { useState } from "react";
import { useActiveCustomer } from "../lib/customer";
import { Card, CardHeader, EmptyState, Input, Button } from "../components/ui";

export function ConnectionString() {
  const { data: customer } = useActiveCustomer();
  const [warehouse, setWarehouse] = useState("bigquery");
  const [host, setHost] = useState("localhost");
  const [port, setPort] = useState("5000");
  if (!customer) return <EmptyState title="No active customer" />;
  const url = `postgresql://<API_KEY>@${host}:${port}/${customer.slug}_${warehouse}`;
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold tracking-tight">Connection String</h1>
      <Card>
        <CardHeader title="Build a Postgres connection URL" description="Paste into psql, dbt, Looker, Tableau, etc." />
        <div className="grid grid-cols-3 gap-3">
          <Input value={host} onChange={(e) => setHost(e.target.value)} placeholder="host" />
          <Input value={port} onChange={(e) => setPort(e.target.value)} placeholder="port" />
          <select
            className="h-10 rounded-md border border-border bg-background px-3 text-sm"
            value={warehouse}
            onChange={(e) => setWarehouse(e.target.value)}
          >
            <option value="bigquery">bigquery</option>
            <option value="snowflake">snowflake</option>
          </select>
        </div>
        <div className="mt-4 rounded-md border border-border bg-background p-3 font-mono text-sm break-all">
          {url}
        </div>
        <Button className="mt-3" onClick={() => navigator.clipboard.writeText(url)}>
          Copy
        </Button>
      </Card>
      <Card>
        <CardHeader title="psql one-liner" />
        <div className="font-mono text-sm">psql "{url}" -c "SELECT 1"</div>
      </Card>
    </div>
  );
}
