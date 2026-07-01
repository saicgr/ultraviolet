import { useEffect, useState } from "react";
import { Link, Route, Routes, useLocation } from "react-router-dom";
import { ErrorBoundary } from "./components/ErrorBoundary";
import { Dashboard } from "./pages/Dashboard";
import { Connections } from "./pages/Connections";
import { Sync } from "./pages/Sync";
import { Queries } from "./pages/Queries";
import { APIKeys } from "./pages/APIKeys";
import { ConnectionString } from "./pages/ConnectionString";
import { Lineage } from "./pages/Lineage";
import { Catalog } from "./pages/Catalog";
import { Dashboards } from "./pages/Dashboards";
import { Workbench } from "./pages/Workbench";
import { Impact } from "./pages/Impact";
import { Login } from "./pages/Login";
import { Agents } from "./pages/Agents";
import { CatalogHover } from "./pages/CatalogHover";
import { Quotas } from "./pages/Quotas";
import { Approvals } from "./pages/Approvals";
import { SchemaDiff } from "./pages/SchemaDiff";
import { SyncDAG } from "./pages/SyncDAG";
import { DashboardVersions } from "./pages/DashboardVersions";
import { CostPreflight } from "./pages/CostPreflight";
import { SemanticSearch } from "./pages/SemanticSearch";
import { Activity } from "./pages/Activity";
import { Inbox } from "./pages/Inbox";
import { Subscriptions } from "./pages/Subscriptions";
import { Webhooks } from "./pages/Webhooks";
import { ScheduledQueries } from "./pages/ScheduledQueries";
import { AccessReviews } from "./pages/AccessReviews";
import { Copilot } from "./pages/Copilot";
import { Narrator } from "./pages/Narrator";
import { Watches } from "./pages/Watches";
import { PullRequests } from "./pages/PullRequests";
import { GitHubApp } from "./pages/GitHubApp";
import { CommandPalette } from "./components/CommandPalette";
import { cn } from "./lib/cn";
import { BRANDING } from "./branding";
import { SUPPORTED_LOCALES, useI18n, useT } from "./i18n";

function LocaleSwitcher() {
  const { locale, setLocale } = useI18n();
  return (
    <select
      value={locale}
      onChange={(e) => setLocale(e.target.value as typeof locale)}
      className="h-8 rounded-md border border-border bg-background px-2 text-xs"
      aria-label="Locale"
    >
      {SUPPORTED_LOCALES.map((l) => (
        <option key={l.code} value={l.code}>{l.label}</option>
      ))}
    </select>
  );
}

export default function App() {
  const loc = useLocation();
  const t = useT();
  const [paletteOpen, setPaletteOpen] = useState(false);

  useEffect(() => {
    document.title = BRANDING.name;
  }, []);

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPaletteOpen((v) => !v);
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // The 5 JUUL "Foundational" features are grouped at the top; everything else
  // stays in an un-headed section below so existing muscle memory is preserved.
  const sections = [
    {
      heading: t("nav.foundational"),
      items: [
        { to: "/", label: t("nav.cost_optimization") },
        { to: "/dashboards", label: t("nav.dashboards") },
        { to: "/lineage", label: t("nav.lineage") },
        { to: "/pull-requests", label: t("nav.pull_requests") },
        { to: "/github", label: t("nav.github_app") },
      ],
    },
    {
      heading: null as string | null,
      items: [
        { to: "/connections", label: t("nav.connections") },
        { to: "/sync", label: t("nav.sync") },
        { to: "/queries", label: t("nav.queries") },
        { to: "/catalog", label: t("nav.catalog") },
        { to: "/workbench", label: t("nav.workbench") },
        { to: "/impact", label: t("nav.impact") },
        { to: "/agents", label: t("nav.agents") },
        { to: "/activity", label: t("nav.activity") },
        { to: "/inbox", label: t("nav.inbox") },
        { to: "/subscriptions", label: t("nav.subscriptions") },
        { to: "/webhooks", label: t("nav.webhooks") },
        { to: "/scheduled-queries", label: t("nav.scheduled_queries") },
        { to: "/access-reviews", label: t("nav.access_reviews") },
        { to: "/copilot", label: t("nav.copilot") },
        { to: "/narrator", label: t("nav.narrator") },
        { to: "/watches", label: t("nav.watches") },
        { to: "/catalog/hover", label: "Catalog Hover" },
        { to: "/semantic-search", label: "Semantic Search" },
        { to: "/quotas", label: "Quotas" },
        { to: "/approvals", label: "Approvals" },
        { to: "/schema-diff", label: "Schema Diff" },
        { to: "/sync-dag", label: "Sync DAG" },
        { to: "/dashboard-versions", label: "Dashboard Versions" },
        { to: "/cost-preflight", label: "Cost Preflight" },
        { to: "/api-keys", label: t("nav.api_keys") },
        { to: "/connection-string", label: t("nav.connect") },
      ],
    },
  ];

  return (
    <div className="min-h-screen flex">
      <aside className="w-56 shrink-0 border-r border-border p-4">
        <div className="font-semibold text-lg mb-6 flex items-center gap-2">
          <span className="inline-block h-3 w-3 rounded-full bg-accent" />
          {BRANDING.name}
        </div>
        <nav className="space-y-1">
          {sections.map((section, si) => (
            <div key={si} className={cn(si > 0 && "pt-3")}>
              {section.heading && (
                <div className="px-3 pb-1 pt-2 text-xs font-medium uppercase tracking-wide text-foreground/50">
                  {section.heading}
                </div>
              )}
              {section.items.map((n) => (
                <Link
                  key={n.to}
                  to={n.to}
                  className={cn(
                    "block px-3 py-2 rounded-md text-sm hover:bg-muted",
                    loc.pathname === n.to && "bg-muted font-medium",
                  )}
                >
                  {n.label}
                </Link>
              ))}
            </div>
          ))}
        </nav>
      </aside>
      <main className="flex-1 p-8 max-w-6xl">
        <header className="flex items-center justify-end gap-3 mb-4">
          <button
            onClick={() => setPaletteOpen(true)}
            className="h-8 rounded-md border border-border px-3 text-xs text-foreground/70 hover:bg-muted"
            aria-label="Open command palette"
          >
            Search… <span className="ml-2 opacity-60">⌘K</span>
          </button>
          <LocaleSwitcher />
        </header>
        <ErrorBoundary resetKey={loc.pathname}>
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/connections" element={<Connections />} />
          <Route path="/sync" element={<Sync />} />
          <Route path="/queries" element={<Queries />} />
          <Route path="/api-keys" element={<APIKeys />} />
          <Route path="/connection-string" element={<ConnectionString />} />
          <Route path="/lineage" element={<Lineage />} />
          <Route path="/catalog" element={<Catalog />} />
          <Route path="/dashboards" element={<Dashboards />} />
          <Route path="/workbench" element={<Workbench />} />
          <Route path="/impact" element={<Impact />} />
          <Route path="/login" element={<Login />} />
          <Route path="/agents" element={<Agents />} />
          <Route path="/catalog/hover" element={<CatalogHover />} />
          <Route path="/semantic-search" element={<SemanticSearch />} />
          <Route path="/quotas" element={<Quotas />} />
          <Route path="/approvals" element={<Approvals />} />
          <Route path="/schema-diff" element={<SchemaDiff />} />
          <Route path="/sync-dag" element={<SyncDAG />} />
          <Route path="/dashboard-versions" element={<DashboardVersions />} />
          <Route path="/cost-preflight" element={<CostPreflight />} />
          <Route path="/activity" element={<Activity />} />
          <Route path="/inbox" element={<Inbox />} />
          <Route path="/subscriptions" element={<Subscriptions />} />
          <Route path="/webhooks" element={<Webhooks />} />
          <Route path="/scheduled-queries" element={<ScheduledQueries />} />
          <Route path="/access-reviews" element={<AccessReviews />} />
          <Route path="/copilot" element={<Copilot />} />
          <Route path="/narrator" element={<Narrator />} />
          <Route path="/watches" element={<Watches />} />
          <Route path="/pull-requests" element={<PullRequests />} />
          <Route path="/github" element={<GitHubApp />} />
        </Routes>
        </ErrorBoundary>
      </main>
      <CommandPalette open={paletteOpen} onClose={() => setPaletteOpen(false)} />
    </div>
  );
}
