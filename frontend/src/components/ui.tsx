// Minimal shadcn-flavored primitives — Button, Card, Input, Table, Badge.
// Hand-rolled rather than shadcn-cli generated to keep the scaffold self-contained.

import * as React from "react";
import { cn } from "../lib/cn";

export const Button = React.forwardRef<
  HTMLButtonElement,
  React.ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "default" | "ghost" | "outline"; size?: "sm" | "md" }
>(({ className, variant = "default", size = "md", ...props }, ref) => (
  <button
    ref={ref}
    className={cn(
      "inline-flex items-center justify-center rounded-md font-medium transition-colors disabled:opacity-50 disabled:pointer-events-none focus:outline-none focus-visible:ring-2 focus-visible:ring-accent",
      size === "sm" ? "h-8 px-3 text-sm" : "h-10 px-4 text-sm",
      variant === "default" && "bg-accent text-white hover:bg-accent/90",
      variant === "outline" && "border border-border hover:bg-muted",
      variant === "ghost" && "hover:bg-muted",
      className,
    )}
    {...props}
  />
));
Button.displayName = "Button";

export function Card({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn("rounded-lg border border-border bg-muted/30 backdrop-blur p-5 shadow-sm", className)}
      {...props}
    />
  );
}

export function CardHeader({ title, description }: { title: string; description?: string }) {
  return (
    <div className="mb-4">
      <h2 className="text-lg font-semibold tracking-tight">{title}</h2>
      {description && <p className="text-sm text-foreground/60 mt-1">{description}</p>}
    </div>
  );
}

export const Input = React.forwardRef<HTMLInputElement, React.InputHTMLAttributes<HTMLInputElement>>(
  ({ className, ...props }, ref) => (
    <input
      ref={ref}
      className={cn(
        "flex h-10 w-full rounded-md border border-border bg-background px-3 py-2 text-sm placeholder:text-foreground/40 focus:outline-none focus-visible:ring-2 focus-visible:ring-accent",
        className,
      )}
      {...props}
    />
  ),
);
Input.displayName = "Input";

export function Badge({
  children,
  variant = "default",
}: {
  children: React.ReactNode;
  variant?: "default" | "success" | "warn" | "danger";
}) {
  const colors = {
    default: "bg-muted text-foreground/80",
    success: "bg-emerald-900/40 text-emerald-300 border border-emerald-700/40",
    warn: "bg-amber-900/40 text-amber-300 border border-amber-700/40",
    danger: "bg-rose-900/40 text-rose-300 border border-rose-700/40",
  };
  return (
    <span className={cn("inline-flex items-center rounded-md px-2 py-0.5 text-xs", colors[variant])}>
      {children}
    </span>
  );
}

export function Table({ children }: { children: React.ReactNode }) {
  return <table className="w-full text-sm border-collapse">{children}</table>;
}
export function THead({ children }: { children: React.ReactNode }) {
  return <thead className="border-b border-border text-left text-xs uppercase tracking-wide text-foreground/60">{children}</thead>;
}
export function TR({ children, className }: { children: React.ReactNode; className?: string }) {
  return <tr className={cn("border-b border-border/50", className)}>{children}</tr>;
}
export function TD({ children, className }: { children: React.ReactNode; className?: string }) {
  return <td className={cn("py-2 pr-4", className)}>{children}</td>;
}
export function TH({ children, className }: { children?: React.ReactNode; className?: string }) {
  return <th className={cn("py-2 pr-4 font-medium", className)}>{children}</th>;
}
export function TD2({ children, className }: { children?: React.ReactNode; className?: string }) {
  return <td className={cn("py-2 pr-4", className)}>{children}</td>;
}

export function EmptyState({ title, hint }: { title: string; hint?: string }) {
  return (
    <div className="text-center py-12 text-foreground/60">
      <p className="font-medium">{title}</p>
      {hint && <p className="text-sm mt-1">{hint}</p>}
    </div>
  );
}

export function ErrorState({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  return (
    <div className="rounded-md border border-rose-900/40 bg-rose-950/30 p-4 text-sm text-rose-200">
      <p>{error instanceof Error ? error.message : String(error)}</p>
      {onRetry && (
        <button
          onClick={onRetry}
          className="mt-2 inline-flex items-center rounded-md border border-rose-700/60 bg-rose-900/40 px-2 py-1 text-xs hover:bg-rose-900/60"
        >
          Retry
        </button>
      )}
    </div>
  );
}

// Skeleton — animated placeholder for loading states. Replaces the bare
// "Loading…" text flagged in AUDIT.md §frontend gap. Use rows={N} for tables.
export function Skeleton({ className }: { className?: string }) {
  return <div className={cn("animate-pulse rounded-md bg-muted/50 h-4 w-full", className)} />;
}

export function SkeletonRows({ count = 5, className }: { count?: number; className?: string }) {
  return (
    <div className={cn("space-y-2", className)} role="status" aria-busy="true" aria-label="Loading">
      {Array.from({ length: count }).map((_, i) => (
        <Skeleton key={i} className={i % 3 === 0 ? "h-5 w-2/3" : "h-4 w-full"} />
      ))}
    </div>
  );
}
