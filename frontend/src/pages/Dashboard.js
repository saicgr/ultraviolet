import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useActiveCustomer } from "../lib/customer";
import { Card, CardHeader, EmptyState, ErrorState } from "../components/ui";
export function Dashboard() {
    const { data: customer } = useActiveCustomer();
    const customerId = customer?.id;
    const savings = useQuery({
        queryKey: ["savings", customerId],
        queryFn: () => api.savings(customerId),
        enabled: !!customerId,
    });
    const queries = useQuery({
        queryKey: ["queries", customerId],
        queryFn: () => api.queryAnalytics(customerId),
        enabled: !!customerId,
    });
    if (!customer) {
        return (_jsxs(Card, { children: [_jsx(CardHeader, { title: "Welcome to Ultraviolet", description: "Create a customer record to begin." }), _jsxs("p", { className: "text-sm text-foreground/60", children: ["POST ", _jsx("code", { className: "font-mono", children: "/api/v1/customers" }), " with ", _jsx("code", { children: `{"slug":"acme","display_name":"Acme"}` }), "."] })] }));
    }
    if (savings.isError)
        return _jsx(ErrorState, { error: savings.error });
    const total = savings.data?.reduce((a, r) => a + r.estimated_savings_usd, 0) ?? 0;
    const totalQ = queries.data?.reduce((a, r) => a + r.count, 0) ?? 0;
    const duckQ = queries.data?.find((r) => r.route === "duckdb")?.count ?? 0;
    const pct = totalQ ? ((duckQ / totalQ) * 100).toFixed(1) : "0";
    return (_jsxs("div", { className: "space-y-6", children: [_jsx("h1", { className: "text-2xl font-semibold tracking-tight", children: "Savings Dashboard" }), _jsxs("div", { className: "grid grid-cols-3 gap-4", children: [_jsxs(Card, { children: [_jsx(CardHeader, { title: "Estimated 30-day savings" }), _jsxs("div", { className: "text-3xl font-semibold", children: ["$", total.toFixed(2)] })] }), _jsxs(Card, { children: [_jsx(CardHeader, { title: "DuckDB hit rate" }), _jsxs("div", { className: "text-3xl font-semibold", children: [pct, "%"] }), _jsx("p", { className: "text-xs text-foreground/60 mt-1", children: "Phase 1 target \u226580%" })] }), _jsxs(Card, { children: [_jsx(CardHeader, { title: "Queries (24h)" }), _jsx("div", { className: "text-3xl font-semibold", children: totalQ.toLocaleString() })] })] }), _jsxs(Card, { children: [_jsx(CardHeader, { title: "Route breakdown (24h)" }), !queries.data || queries.data.length === 0 ? (_jsx(EmptyState, { title: "No queries yet", hint: "Run a query through the proxy to see metrics." })) : (_jsx("ul", { className: "space-y-2 text-sm", children: queries.data.map((r) => (_jsxs("li", { className: "flex items-center justify-between", children: [_jsx("span", { className: "font-mono", children: r.route }), _jsxs("span", { children: [r.count.toLocaleString(), " \u00B7 avg ", r.avg_ms, "ms"] })] }, r.route))) }))] })] }));
}
