import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useActiveCustomer } from "../lib/customer";
import { Card, CardHeader, EmptyState, ErrorState, Table, TD, TH, THead, TR } from "../components/ui";
export function Queries() {
    const { data: customer } = useActiveCustomer();
    const q = useQuery({
        queryKey: ["queries-page", customer?.id],
        queryFn: () => api.queryAnalytics(customer.id),
        enabled: !!customer,
        refetchInterval: 5000,
    });
    if (!customer)
        return _jsx(EmptyState, { title: "No active customer" });
    if (q.isError)
        return _jsx(ErrorState, { error: q.error });
    return (_jsxs("div", { className: "space-y-6", children: [_jsx("h1", { className: "text-2xl font-semibold tracking-tight", children: "Query Analytics" }), _jsxs(Card, { children: [_jsx(CardHeader, { title: "Last 24 hours", description: "Live-refreshing every 5s" }), !q.data || q.data.length === 0 ? (_jsx(EmptyState, { title: "No queries yet", hint: "Connect a BI tool via the Postgres wire on :5000" })) : (_jsxs(Table, { children: [_jsx(THead, { children: _jsxs(TR, { children: [_jsx(TH, { children: "Route" }), _jsx(TH, { children: "Count" }), _jsx(TH, { children: "Avg ms" }), _jsx(TH, { children: "Rows" })] }) }), _jsx("tbody", { children: q.data.map((r) => (_jsxs(TR, { children: [_jsx(TD, { className: "font-mono", children: r.route }), _jsx(TD, { children: r.count.toLocaleString() }), _jsx(TD, { children: r.avg_ms }), _jsx(TD, { children: r.rows_returned.toLocaleString() })] }, r.route))) })] }))] })] }));
}
