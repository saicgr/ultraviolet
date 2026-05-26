import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "../lib/api";
import { useActiveCustomer } from "../lib/customer";
import { Button, Card, CardHeader, EmptyState, ErrorState, Input, Table, TD, TH, THead, TR, Badge } from "../components/ui";
export function Connections() {
    const qc = useQueryClient();
    const { data: customer } = useActiveCustomer();
    const conns = useQuery({
        queryKey: ["connections", customer?.id],
        queryFn: () => api.listConnections(customer.id),
        enabled: !!customer,
    });
    const [name, setName] = useState("");
    const [warehouse, setWarehouse] = useState("bigquery");
    const [creds, setCreds] = useState('{"project_id":"my-project"}');
    const create = useMutation({
        mutationFn: () => api.createConnection(customer.id, {
            warehouse_type: warehouse,
            name,
            credentials: JSON.parse(creds),
        }),
        onSuccess: () => {
            setName("");
            qc.invalidateQueries({ queryKey: ["connections"] });
        },
    });
    if (!customer)
        return _jsx(EmptyState, { title: "No active customer" });
    return (_jsxs("div", { className: "space-y-6", children: [_jsx("h1", { className: "text-2xl font-semibold tracking-tight", children: "Warehouse Connections" }), _jsxs(Card, { children: [_jsx(CardHeader, { title: "Add connection", description: "Credentials are encrypted with AES-256-GCM at rest." }), _jsxs("div", { className: "grid grid-cols-3 gap-3", children: [_jsx(Input, { placeholder: "Connection name", value: name, onChange: (e) => setName(e.target.value) }), _jsxs("select", { className: "h-10 rounded-md border border-border bg-background px-3 text-sm", value: warehouse, onChange: (e) => setWarehouse(e.target.value), children: [_jsx("option", { value: "bigquery", children: "BigQuery" }), _jsx("option", { value: "snowflake", children: "Snowflake" })] }), _jsx(Button, { onClick: () => create.mutate(), disabled: !name || create.isPending, children: create.isPending ? "Saving..." : "Add" })] }), _jsx("textarea", { className: "mt-3 w-full h-32 rounded-md border border-border bg-background p-3 font-mono text-xs", value: creds, onChange: (e) => setCreds(e.target.value), placeholder: "Credentials JSON" }), create.isError && _jsx(ErrorState, { error: create.error })] }), _jsxs(Card, { children: [_jsx(CardHeader, { title: "Existing" }), conns.isError && _jsx(ErrorState, { error: conns.error }), conns.data && conns.data.length === 0 && _jsx(EmptyState, { title: "No connections yet" }), conns.data && conns.data.length > 0 && (_jsxs(Table, { children: [_jsx(THead, { children: _jsxs(TR, { children: [_jsx(TH, { children: "Name" }), _jsx(TH, { children: "Warehouse" }), _jsx(TH, { children: "Storage" }), _jsx(TH, { children: "Created" })] }) }), _jsx("tbody", { children: conns.data.map((c) => (_jsxs(TR, { children: [_jsx(TD, { children: c.name }), _jsx(TD, { children: _jsx(Badge, { children: c.warehouse_type }) }), _jsx(TD, { children: c.storage_mode }), _jsx(TD, { children: new Date(c.created_at).toLocaleString() })] }, c.id))) })] }))] })] }));
}
