import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from "react";
import { useActiveCustomer } from "../lib/customer";
import { Card, CardHeader, EmptyState, Input, Button } from "../components/ui";
export function ConnectionString() {
    const { data: customer } = useActiveCustomer();
    const [warehouse, setWarehouse] = useState("bigquery");
    const [host, setHost] = useState("localhost");
    const [port, setPort] = useState("5000");
    if (!customer)
        return _jsx(EmptyState, { title: "No active customer" });
    const url = `postgresql://<API_KEY>@${host}:${port}/${customer.slug}_${warehouse}`;
    return (_jsxs("div", { className: "space-y-6", children: [_jsx("h1", { className: "text-2xl font-semibold tracking-tight", children: "Connection String" }), _jsxs(Card, { children: [_jsx(CardHeader, { title: "Build a Postgres connection URL", description: "Paste into psql, dbt, Looker, Tableau, etc." }), _jsxs("div", { className: "grid grid-cols-3 gap-3", children: [_jsx(Input, { value: host, onChange: (e) => setHost(e.target.value), placeholder: "host" }), _jsx(Input, { value: port, onChange: (e) => setPort(e.target.value), placeholder: "port" }), _jsxs("select", { className: "h-10 rounded-md border border-border bg-background px-3 text-sm", value: warehouse, onChange: (e) => setWarehouse(e.target.value), children: [_jsx("option", { value: "bigquery", children: "bigquery" }), _jsx("option", { value: "snowflake", children: "snowflake" })] })] }), _jsx("div", { className: "mt-4 rounded-md border border-border bg-background p-3 font-mono text-sm break-all", children: url }), _jsx(Button, { className: "mt-3", onClick: () => navigator.clipboard.writeText(url), children: "Copy" })] }), _jsxs(Card, { children: [_jsx(CardHeader, { title: "psql one-liner" }), _jsxs("div", { className: "font-mono text-sm", children: ["psql \"", url, "\" -c \"SELECT 1\""] })] })] }));
}
