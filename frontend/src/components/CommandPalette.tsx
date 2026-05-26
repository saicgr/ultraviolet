// ux-3 — Cmd/Ctrl+K command palette.
//
// Triggered by a window keydown listener registered in App.tsx (sets `open`).
// Fuzzy-filters across:
//   1. Static page navigation entries (instant, no fetch).
//   2. Catalog table FQNs — `GET /api/v1/catalog/search?q=`, debounced.
//   3. Recent queries — `GET /api/v1/customers/{id}/queries?limit=20`.
// Selecting:
//   - Page entry → navigate.
//   - Catalog entry → copy FQN to clipboard.
//   - Recent query entry → copy SQL to clipboard.

import * as React from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useActiveCustomer } from "../lib/customer";

type PageEntry = { kind: "page"; label: string; to: string };
type CatalogEntry = { kind: "catalog"; label: string; fqn: string };
type QueryEntry = { kind: "query"; label: string; sql: string };
type Entry = PageEntry | CatalogEntry | QueryEntry;

const PAGES: PageEntry[] = [
  { kind: "page", label: "Savings", to: "/" },
  { kind: "page", label: "Connections", to: "/connections" },
  { kind: "page", label: "Sync", to: "/sync" },
  { kind: "page", label: "Queries", to: "/queries" },
  { kind: "page", label: "Catalog", to: "/catalog" },
  { kind: "page", label: "Lineage", to: "/lineage" },
  { kind: "page", label: "Dashboards", to: "/dashboards" },
  { kind: "page", label: "Workbench", to: "/workbench" },
  { kind: "page", label: "Impact", to: "/impact" },
  { kind: "page", label: "Agents", to: "/agents" },
  { kind: "page", label: "Activity", to: "/activity" },
  { kind: "page", label: "Inbox", to: "/inbox" },
  { kind: "page", label: "Subscriptions", to: "/subscriptions" },
  { kind: "page", label: "Webhooks", to: "/webhooks" },
  { kind: "page", label: "Scheduled Queries", to: "/scheduled-queries" },
  { kind: "page", label: "Access Reviews", to: "/access-reviews" },
  { kind: "page", label: "Copilot", to: "/copilot" },
  { kind: "page", label: "Narrator", to: "/narrator" },
  { kind: "page", label: "Lineage Watches", to: "/watches" },
  { kind: "page", label: "API Keys", to: "/api-keys" },
  { kind: "page", label: "Connect", to: "/connection-string" },
];

function fuzzy(q: string, label: string): boolean {
  if (!q) return true;
  const l = label.toLowerCase();
  const needle = q.toLowerCase();
  let i = 0;
  for (const ch of l) {
    if (ch === needle[i]) i++;
    if (i === needle.length) return true;
  }
  return false;
}

export function CommandPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  const nav = useNavigate();
  const customer = useActiveCustomer();
  const cid = customer.data?.id ?? "";
  const [q, setQ] = React.useState("");
  const [active, setActive] = React.useState(0);
  const [debounced, setDebounced] = React.useState("");

  React.useEffect(() => {
    if (!open) { setQ(""); setActive(0); }
  }, [open]);

  React.useEffect(() => {
    const t = setTimeout(() => setDebounced(q), 150);
    return () => clearTimeout(t);
  }, [q]);

  const catalog = useQuery({
    queryKey: ["palette-catalog", debounced],
    queryFn: () => api.catalogSearch(debounced),
    enabled: open && debounced.length >= 2,
  });
  const recent = useQuery({
    queryKey: ["palette-recent", cid],
    queryFn: () => api.recentQueries(cid, 20),
    enabled: open && !!cid,
  });

  const entries: Entry[] = React.useMemo(() => {
    const pages: Entry[] = PAGES.filter((p) => fuzzy(q, p.label));
    const cat: Entry[] = (catalog.data ?? []).slice(0, 10).map((r) => ({
      kind: "catalog" as const,
      label: r.fqn,
      fqn: r.fqn,
    }));
    const recents: Entry[] = (recent.data ?? [])
      .filter((r) => fuzzy(q, r.sql))
      .slice(0, 10)
      .map((r) => ({ kind: "query" as const, label: r.sql.replace(/\s+/g, " ").slice(0, 80), sql: r.sql }));
    return [...pages, ...cat, ...recents];
  }, [q, catalog.data, recent.data]);

  React.useEffect(() => { setActive(0); }, [entries.length]);

  if (!open) return null;

  function pick(e: Entry) {
    if (e.kind === "page") nav(e.to);
    else if (e.kind === "catalog") void navigator.clipboard?.writeText(e.fqn);
    else void navigator.clipboard?.writeText(e.sql);
    onClose();
  }

  function onKey(ev: React.KeyboardEvent) {
    if (ev.key === "Escape") onClose();
    else if (ev.key === "ArrowDown") { ev.preventDefault(); setActive((a) => Math.min(entries.length - 1, a + 1)); }
    else if (ev.key === "ArrowUp") { ev.preventDefault(); setActive((a) => Math.max(0, a - 1)); }
    else if (ev.key === "Enter") { ev.preventDefault(); if (entries[active]) pick(entries[active]); }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center pt-24 bg-black/60 backdrop-blur-sm" onClick={onClose}>
      <div className="w-full max-w-xl rounded-lg border border-border bg-background shadow-2xl" onClick={(e) => e.stopPropagation()}>
        <input
          autoFocus
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={onKey}
          placeholder="Jump to page, search catalog, or recall a query…"
          className="w-full bg-transparent px-4 py-3 text-sm placeholder:text-foreground/40 focus:outline-none border-b border-border"
        />
        <ul className="max-h-96 overflow-y-auto py-1">
          {entries.length === 0 && (
            <li className="px-4 py-6 text-center text-sm text-foreground/50">No matches</li>
          )}
          {entries.map((e, i) => (
            <li
              key={`${e.kind}-${i}-${e.label}`}
              onMouseEnter={() => setActive(i)}
              onClick={() => pick(e)}
              className={`px-4 py-2 text-sm cursor-pointer flex items-center justify-between gap-3 ${i === active ? "bg-muted" : ""}`}
            >
              <span className={e.kind === "page" ? "" : "font-mono text-xs truncate"}>{e.label}</span>
              <span className="text-[10px] uppercase tracking-wide text-foreground/40">
                {e.kind === "page" ? "go" : e.kind === "catalog" ? "copy fqn" : "copy sql"}
              </span>
            </li>
          ))}
        </ul>
        <div className="border-t border-border px-4 py-2 text-[11px] text-foreground/40 flex justify-between">
          <span>↑↓ navigate · ↵ select · esc close</span>
          <span>{entries.length} result{entries.length === 1 ? "" : "s"}</span>
        </div>
      </div>
    </div>
  );
}
