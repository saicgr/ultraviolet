import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
// Minimal shadcn-flavored primitives — Button, Card, Input, Table, Badge.
// Hand-rolled rather than shadcn-cli generated to keep the scaffold self-contained.
import * as React from "react";
import { cn } from "../lib/cn";
export const Button = React.forwardRef(({ className, variant = "default", size = "md", ...props }, ref) => (_jsx("button", { ref: ref, className: cn("inline-flex items-center justify-center rounded-md font-medium transition-colors disabled:opacity-50 disabled:pointer-events-none focus:outline-none focus-visible:ring-2 focus-visible:ring-accent", size === "sm" ? "h-8 px-3 text-sm" : "h-10 px-4 text-sm", variant === "default" && "bg-accent text-white hover:bg-accent/90", variant === "outline" && "border border-border hover:bg-muted", variant === "ghost" && "hover:bg-muted", className), ...props })));
Button.displayName = "Button";
export function Card({ className, ...props }) {
    return (_jsx("div", { className: cn("rounded-lg border border-border bg-muted/30 backdrop-blur p-5 shadow-sm", className), ...props }));
}
export function CardHeader({ title, description }) {
    return (_jsxs("div", { className: "mb-4", children: [_jsx("h2", { className: "text-lg font-semibold tracking-tight", children: title }), description && _jsx("p", { className: "text-sm text-foreground/60 mt-1", children: description })] }));
}
export const Input = React.forwardRef(({ className, ...props }, ref) => (_jsx("input", { ref: ref, className: cn("flex h-10 w-full rounded-md border border-border bg-background px-3 py-2 text-sm placeholder:text-foreground/40 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent", className), ...props })));
Input.displayName = "Input";
export function Badge({ children, variant = "default", }) {
    const colors = {
        default: "bg-muted text-foreground/80",
        success: "bg-emerald-900/40 text-emerald-300 border border-emerald-700/40",
        warn: "bg-amber-900/40 text-amber-300 border border-amber-700/40",
        danger: "bg-rose-900/40 text-rose-300 border border-rose-700/40",
    };
    return (_jsx("span", { className: cn("inline-flex items-center rounded-md px-2 py-0.5 text-xs", colors[variant]), children: children }));
}
export function Table({ children }) {
    return _jsx("table", { className: "w-full text-sm border-collapse", children: children });
}
export function THead({ children }) {
    return _jsx("thead", { className: "border-b border-border text-left text-xs uppercase tracking-wide text-foreground/60", children: children });
}
export function TR({ children, className }) {
    return _jsx("tr", { className: cn("border-b border-border/50", className), children: children });
}
export function TD({ children, className }) {
    return _jsx("td", { className: cn("py-2 pr-4", className), children: children });
}
export function TH({ children, className }) {
    return _jsx("th", { className: cn("py-2 pr-4 font-medium", className), children: children });
}
export function TD2({ children, className }) {
    return _jsx("td", { className: cn("py-2 pr-4", className), children: children });
}
export function EmptyState({ title, hint }) {
    return (_jsxs("div", { className: "text-center py-12 text-foreground/60", children: [_jsx("p", { className: "font-medium", children: title }), hint && _jsx("p", { className: "text-sm mt-1", children: hint })] }));
}
export function ErrorState({ error }) {
    return (_jsx("div", { className: "rounded-md border border-rose-900/40 bg-rose-950/30 p-4 text-sm text-rose-200", children: error instanceof Error ? error.message : String(error) }));
}
