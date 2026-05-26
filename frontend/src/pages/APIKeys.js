import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "../lib/api";
import { useActiveCustomer } from "../lib/customer";
import { Button, Card, CardHeader, EmptyState, ErrorState, Input, Table, TD, TH, THead, TR, Badge } from "../components/ui";
export function APIKeys() {
    const qc = useQueryClient();
    const { data: customer } = useActiveCustomer();
    const keys = useQuery({
        queryKey: ["api-keys", customer?.id],
        queryFn: () => api.listAPIKeys(customer.id),
        enabled: !!customer,
    });
    const [desc, setDesc] = useState("");
    const [created, setCreated] = useState(null);
    const create = useMutation({
        mutationFn: () => api.createAPIKey(customer.id, desc || undefined),
        onSuccess: (res) => {
            setCreated(res.api_key);
            setDesc("");
            qc.invalidateQueries({ queryKey: ["api-keys"] });
        },
    });
    const revoke = useMutation({
        mutationFn: (id) => api.revokeAPIKey(id),
        onSuccess: () => qc.invalidateQueries({ queryKey: ["api-keys"] }),
    });
    if (!customer)
        return _jsx(EmptyState, { title: "No active customer" });
    return (_jsxs("div", { className: "space-y-6", children: [_jsx("h1", { className: "text-2xl font-semibold tracking-tight", children: "API Keys" }), _jsxs(Card, { children: [_jsx(CardHeader, { title: "Create new", description: "The full key is shown once and never again." }), _jsxs("div", { className: "flex gap-3", children: [_jsx(Input, { placeholder: "Description (optional)", value: desc, onChange: (e) => setDesc(e.target.value) }), _jsx(Button, { onClick: () => create.mutate(), disabled: create.isPending, children: "Create" })] }), created && (_jsxs("div", { className: "mt-4 rounded-md border border-emerald-700/40 bg-emerald-950/40 p-3 font-mono text-sm", children: [created, _jsx("p", { className: "text-xs mt-2 text-emerald-300/80", children: "Copy this now. It will not be shown again." })] })), create.isError && _jsx(ErrorState, { error: create.error })] }), _jsxs(Card, { children: [_jsx(CardHeader, { title: "Existing" }), keys.isError && _jsx(ErrorState, { error: keys.error }), keys.data && keys.data.length === 0 && _jsx(EmptyState, { title: "No keys yet" }), keys.data && keys.data.length > 0 && (_jsxs(Table, { children: [_jsx(THead, { children: _jsxs(TR, { children: [_jsx(TH, { children: "Prefix" }), _jsx(TH, { children: "Description" }), _jsx(TH, { children: "Last used" }), _jsx(TH, { children: "Status" }), _jsx(TH, {})] }) }), _jsx("tbody", { children: keys.data.map((k) => (_jsxs(TR, { children: [_jsxs(TD, { className: "font-mono", children: [k.key_prefix, "\u2026"] }), _jsx(TD, { children: k.description ?? "—" }), _jsx(TD, { children: k.last_used_at ? new Date(k.last_used_at).toLocaleString() : "—" }), _jsx(TD, { children: k.revoked_at ? _jsx(Badge, { variant: "danger", children: "revoked" }) : _jsx(Badge, { variant: "success", children: "active" }) }), _jsx(TD, { children: !k.revoked_at && (_jsx(Button, { size: "sm", variant: "outline", onClick: () => revoke.mutate(k.id), children: "Revoke" })) })] }, k.id))) })] }))] })] }));
}
