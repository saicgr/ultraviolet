import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "../lib/api";
import { useActiveCustomer } from "../lib/customer";
import { Button, Card, CardHeader, EmptyState, ErrorState, Input, Table, TD, TH, THead, TR, Badge } from "../components/ui";
export function Sync() {
    const qc = useQueryClient();
    const { data: customer } = useActiveCustomer();
    const conns = useQuery({
        queryKey: ["connections", customer?.id],
        queryFn: () => api.listConnections(customer.id),
        enabled: !!customer,
    });
    const [connId, setConnId] = useState("");
    const tables = useQuery({
        queryKey: ["tables", connId],
        queryFn: () => api.listSyncedTables(connId),
        enabled: !!connId,
    });
    const [schema, setSchema] = useState("");
    const [table, setTable] = useState("");
    const [mode, setMode] = useState("watermark");
    const create = useMutation({
        mutationFn: () => api.createSyncedTable(connId, { schema_name: schema, table_name: table, sync_mode: mode }),
        onSuccess: () => {
            setSchema("");
            setTable("");
            qc.invalidateQueries({ queryKey: ["tables"] });
        },
    });
    if (!customer)
        return _jsx(EmptyState, { title: "No active customer" });
    return (_jsxs("div", { className: "space-y-6", children: [_jsx("h1", { className: "text-2xl font-semibold tracking-tight", children: "Table Sync" }), _jsxs(Card, { children: [_jsx(CardHeader, { title: "Connection" }), _jsxs("select", { className: "h-10 rounded-md border border-border bg-background px-3 text-sm", value: connId, onChange: (e) => setConnId(e.target.value), children: [_jsx("option", { value: "", children: "Select\u2026" }), (conns.data ?? []).map((c) => (_jsxs("option", { value: c.id, children: [c.name, " (", c.warehouse_type, ")"] }, c.id)))] })] }), connId && (_jsxs(_Fragment, { children: [_jsxs(Card, { children: [_jsx(CardHeader, { title: "Add synced table" }), _jsxs("div", { className: "grid grid-cols-4 gap-3", children: [_jsx(Input, { placeholder: "schema", value: schema, onChange: (e) => setSchema(e.target.value) }), _jsx(Input, { placeholder: "table", value: table, onChange: (e) => setTable(e.target.value) }), _jsxs("select", { className: "h-10 rounded-md border border-border bg-background px-3 text-sm", value: mode, onChange: (e) => setMode(e.target.value), children: [_jsx("option", { value: "watermark", children: "watermark" }), _jsx("option", { value: "cdc_native", children: "cdc_native" }), _jsx("option", { value: "manual", children: "manual" })] }), _jsx(Button, { onClick: () => create.mutate(), disabled: !schema || !table || create.isPending, children: "Add" })] }), create.isError && _jsx(ErrorState, { error: create.error })] }), _jsxs(Card, { children: [_jsx(CardHeader, { title: "Synced" }), tables.isError && _jsx(ErrorState, { error: tables.error }), tables.data && tables.data.length === 0 && _jsx(EmptyState, { title: "None yet" }), tables.data && tables.data.length > 0 && (_jsxs(Table, { children: [_jsx(THead, { children: _jsxs(TR, { children: [_jsx(TH, { children: "Schema.Table" }), _jsx(TH, { children: "Mode" }), _jsx(TH, { children: "State" }), _jsx(TH, { children: "Rows" }), _jsx(TH, { children: "Last sync" })] }) }), _jsx("tbody", { children: tables.data.map((t) => (_jsxs(TR, { children: [_jsxs(TD, { className: "font-mono", children: [t.schema_name, ".", t.table_name] }), _jsx(TD, { children: t.sync_mode }), _jsx(TD, { children: _jsx(Badge, { variant: t.state === "live" ? "success" : t.state === "error" ? "danger" : "warn", children: t.state }) }), _jsx(TD, { children: t.row_count.toLocaleString() }), _jsx(TD, { children: t.last_sync_at ? new Date(t.last_sync_at).toLocaleString() : "—" })] }, t.id))) })] }))] })] }))] }));
}
