import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { Link, Route, Routes, useLocation } from "react-router-dom";
import { Dashboard } from "./pages/Dashboard";
import { Connections } from "./pages/Connections";
import { Sync } from "./pages/Sync";
import { Queries } from "./pages/Queries";
import { APIKeys } from "./pages/APIKeys";
import { ConnectionString } from "./pages/ConnectionString";
import { cn } from "./lib/cn";
const nav = [
    { to: "/", label: "Savings" },
    { to: "/connections", label: "Connections" },
    { to: "/sync", label: "Sync" },
    { to: "/queries", label: "Queries" },
    { to: "/api-keys", label: "API Keys" },
    { to: "/connection-string", label: "Connect" },
];
export default function App() {
    const loc = useLocation();
    return (_jsxs("div", { className: "min-h-screen flex", children: [_jsxs("aside", { className: "w-56 shrink-0 border-r border-border p-4", children: [_jsxs("div", { className: "font-semibold text-lg mb-6 flex items-center gap-2", children: [_jsx("span", { className: "inline-block h-3 w-3 rounded-full bg-accent" }), "Ultraviolet"] }), _jsx("nav", { className: "space-y-1", children: nav.map((n) => (_jsx(Link, { to: n.to, className: cn("block px-3 py-2 rounded-md text-sm hover:bg-muted", loc.pathname === n.to && "bg-muted font-medium"), children: n.label }, n.to))) })] }), _jsx("main", { className: "flex-1 p-8 max-w-6xl", children: _jsxs(Routes, { children: [_jsx(Route, { path: "/", element: _jsx(Dashboard, {}) }), _jsx(Route, { path: "/connections", element: _jsx(Connections, {}) }), _jsx(Route, { path: "/sync", element: _jsx(Sync, {}) }), _jsx(Route, { path: "/queries", element: _jsx(Queries, {}) }), _jsx(Route, { path: "/api-keys", element: _jsx(APIKeys, {}) }), _jsx(Route, { path: "/connection-string", element: _jsx(ConnectionString, {}) })] }) })] }));
}
