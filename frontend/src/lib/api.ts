// Hand-typed API client. The OpenAPI-generated client lands in Phase 1.5.

export interface Customer {
  id: string;
  slug: string;
  display_name: string;
  created_at: string;
}

export interface Connection {
  id: string;
  customer_id: string;
  warehouse_type: "bigquery" | "snowflake" | "databricks";
  name: string;
  storage_mode: "managed" | "byos";
  s3_bucket?: string | null;
  s3_prefix?: string | null;
  created_at: string;
}

export interface APIKey {
  id: string;
  customer_id: string;
  key_prefix: string;
  description: string | null;
  created_at: string;
  last_used_at: string | null;
  revoked_at: string | null;
}

export interface SyncedTable {
  id: string;
  connection_id: string;
  schema_name: string;
  table_name: string;
  sync_mode: "cdc_native" | "watermark" | "manual";
  watermark_column: string | null;
  last_sync_at: string | null;
  iceberg_path: string | null;
  row_count: number;
  state: "pending" | "initial_load" | "live" | "error" | "paused";
  last_error: string | null;
}

export interface QueryAnalyticsRow {
  route: string;
  count: number;
  avg_ms: number;
  rows_returned: number;
}

export interface SavingsRow {
  period_start: string;
  period_end: string;
  warehouse_cost_usd: number;
  duckdb_cost_usd: number;
  estimated_savings_usd: number;
  queries_total: number;
  queries_duckdb: number;
}

const BASE = "/api/v1";

export interface LineageEdge {
  upstream_fqn: string;
  downstream_fqn: string;
  edge_type: string;
  origin?: string;     // "runtime" | "source_code"
  confidence?: string; // "exact" | "ambiguous" | "table_only"
  query_hash: string;
}

// W1 recursive lineage graph (GET /lineage/graph)
export interface LineageGraphNode {
  id: string;
  fqn: string;
  kind: string; // "table" | "column"
  depth: number;
  is_root: boolean;
}
export interface LineageGraphEdge {
  source: string;
  target: string;
  edge_type: string;
  origin: string;     // "runtime" | "source_code"
  confidence: string; // "exact" | "ambiguous" | "table_only"
}
export interface LineageGraph {
  root: string;
  direction: string;
  granularity: string;
  depth: number;
  truncated: boolean;
  nodes: LineageGraphNode[];
  edges: LineageGraphEdge[];
}

// W2 PR analysis history
export interface PRAnalysisRow {
  id: number;
  repo: string;
  pr_number: number;
  head_sha: string;
  pull_request_url: string;
  conclusion: string; // success | neutral | action_required
  impacted_count: number;
  dq_impact: "none" | "touched" | "failing";
  created_at: string;
}
export interface PRAnalysisDetail extends PRAnalysisRow {
  changes: { Kind: string; FQN: string; Detail?: string }[];
  hits: { FQN: string; Hops: number; Why: string; Severity: string }[];
  dq_refs: { table_fqn: string; status: string }[];
}

// W2 GitHub App settings
export interface GitHubInstallInfo {
  install_url: string;
  installed: boolean;
}
export interface GitHubInstallation {
  installation_id: number;
  account_login: string;
  suspended: boolean;
}
export interface GitHubRepo {
  repo_id: number;
  full_name: string;
  installation_id: number;
  connected: boolean;
}

export interface CatalogRow {
  fqn: string;
  description: string | null;
  owner: string | null;
  sla_minutes: number | null;
  source: string;
}

export interface DashboardSummary {
  id: string;
  name: string;
  tiles: { id: string }[];
}

export interface ImpactHit {
  fqn: string;
  hops: number;
  why: string;
}

export interface CatalogHover {
  fqn: string;
  owner: string | null;
  sla_minutes: number | null;
  columns: { name: string; type: string }[];
  recent_queries: { query_hash: string; sql: string; last_seen_at: string }[];
  dashboards: { id: string; name: string }[];
}

export interface Quota {
  id: string;
  customer_id: string;
  user_id: string | null;
  daily_bytes_cap: number;
  daily_cost_cap_usd: number;
  created_at: string;
}

export interface QueryApproval {
  id: string;
  customer_id: string;
  user_id: string;
  sql: string;
  estimated_bytes: number;
  estimated_cost_usd: number;
  status: "pending" | "approved" | "denied";
  created_at: string;
  decided_at: string | null;
  reason: string | null;
}

export interface SchemaDiff {
  added: { fqn: string; type: string }[];
  dropped: { fqn: string; type: string }[];
  type_changes: { fqn: string; old_type: string; new_type: string }[];
}

export interface SyncDAGNode {
  id: string;
  fqn: string;
  state: string;
  lag_seconds: number;
  last_sync_at: string | null;
  upstream: string[];
  downstream: string[];
}

export interface DashboardVersion {
  id: string;
  dashboard_id: string;
  version: number;
  created_at: string;
  author: string | null;
  note: string | null;
}

export interface CostPreflight {
  estimated_bytes_scanned: number;
  estimated_cost_usd: number;
  warehouse: string;
  would_block_if_over_budget: boolean;
  budget_reason?: string;
  unknown_tables?: string[];
}

export interface WorkbenchResult {
  columns: string[];
  rows: string[][];
  row_count: number;
  truncated: boolean;
  route: string;
  duration_ms: number;
  bytes_scanned: number;
  estimated_cost_usd: number;
  hint?: string;
}

export interface SemanticHit {
  fqn: string;
  score: number;
  snippet: string | null;
}

export interface ActivityEvent {
  id: string;
  kind: string;
  target: string;
  summary: string;
  created_at: string;
}

export interface InboxItem {
  id: string;
  customer_id: string;
  kind: string;
  title: string;
  body: string | null;
  read_at: string | null;
  created_at: string;
}

export interface DashboardSubscription {
  id: string;
  customer_id: string;
  target_id: string;
  schedule_cron: string;
  channel: string;
  address: string;
  enabled: boolean;
}

export interface Webhook {
  id: string;
  customer_id: string;
  url: string;
  events: string[];
  secret_prefix: string | null;
  created_at: string;
}

export interface ScheduledQuery {
  id: string;
  customer_id: string;
  name: string;
  sql: string;
  cron: string;
  created_at: string;
  last_run_at: string | null;
}

export interface ScheduledQueryRun {
  id: string;
  scheduled_query_id: string;
  started_at: string;
  finished_at: string | null;
  status: string;
  error: string | null;
}

export interface AccessReview {
  id: string;
  customer_id: string;
  title: string;
  status: "open" | "closed";
  created_at: string;
  closed_at: string | null;
}

export interface AccessReviewDecision {
  id: string;
  review_id: string;
  subject: string;
  decision: "approve" | "revoke";
  reason: string | null;
  decided_at: string;
}

export interface CopilotMessage {
  role: "user" | "assistant";
  content: string;
}

export interface CopilotResponse {
  message: string;
  suggested_actions: { id: string; label: string; payload?: Record<string, unknown> }[];
}

export interface LineageWatch {
  id: string;
  customer_id: string;
  fqn: string;
  notify_email: string | null;
  created_at: string;
}

export interface RecentQuery {
  query_hash: string;
  sql: string;
  last_seen_at: string;
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const tok = typeof localStorage !== "undefined" ? localStorage.getItem("uv_token") : null;
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (tok) headers["Authorization"] = `Bearer ${tok}`;
  const res = await fetch(BASE + path, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`${res.status}: ${text}`);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  listCustomers: () => request<Customer[]>("GET", "/customers"),
  createCustomer: (slug: string, display_name: string) =>
    request<Customer>("POST", "/customers", { slug, display_name }),
  listConnections: (customerId: string) =>
    request<Connection[]>("GET", `/customers/${customerId}/connections`),
  createConnection: (
    customerId: string,
    body: {
      warehouse_type: string;
      name: string;
      credentials: Record<string, unknown>;
      storage_mode?: string;
    },
  ) => request<Connection>("POST", `/customers/${customerId}/connections`, body),
  listAPIKeys: (customerId: string) =>
    request<APIKey[]>("GET", `/customers/${customerId}/api-keys`),
  createAPIKey: (customerId: string, description?: string) =>
    request<{ api_key: string; key: APIKey }>(
      "POST",
      `/customers/${customerId}/api-keys`,
      { description },
    ),
  revokeAPIKey: (id: string) => request<void>("DELETE", `/api-keys/${id}`),
  listSyncedTables: (connectionId: string) =>
    request<SyncedTable[]>("GET", `/connections/${connectionId}/synced-tables`),
  createSyncedTable: (
    connectionId: string,
    body: { schema_name: string; table_name: string; sync_mode: string },
  ) => request<{ id: string }>("POST", `/connections/${connectionId}/synced-tables`, body),
  queryAnalytics: (customerId: string) =>
    request<QueryAnalyticsRow[]>("GET", `/customers/${customerId}/queries`),
  savings: (customerId: string) =>
    request<SavingsRow[]>("GET", `/customers/${customerId}/savings`),
  lineageUpstream: (fqn: string) =>
    request<LineageEdge[]>("GET", `/lineage/upstream?fqn=${encodeURIComponent(fqn)}`),
  lineageDownstream: (fqn: string) =>
    request<LineageEdge[]>("GET", `/lineage/downstream?fqn=${encodeURIComponent(fqn)}`),
  lineageGraph: (p: { fqn: string; depth?: number; direction?: string; granularity?: string }) =>
    request<LineageGraph>(
      "GET",
      `/lineage/graph?fqn=${encodeURIComponent(p.fqn)}&depth=${p.depth ?? 3}` +
        `&direction=${p.direction ?? "both"}&granularity=${p.granularity ?? "table"}`,
    ),
  listPRAnalysis: () => request<PRAnalysisRow[]>("GET", `/pr-analysis`),
  getPRAnalysis: (id: number) => request<PRAnalysisDetail>("GET", `/pr-analysis/${id}`),
  githubInstallInfo: () => request<GitHubInstallInfo>("GET", `/github/install-info`),
  listGitHubInstallations: () => request<GitHubInstallation[]>("GET", `/github/installations`),
  listGitHubRepos: () => request<GitHubRepo[]>("GET", `/github/repos`),
  connectGitHubRepo: (repo_full_name: string) =>
    request<{ connected: boolean }>("POST", `/github/connect`, { repo_full_name }),
  disconnectGitHubRepo: (repo_full_name: string) =>
    request<{ connected: boolean }>("POST", `/github/disconnect`, { repo_full_name }),
  catalogSearch: (q: string) =>
    request<CatalogRow[]>("GET", `/catalog/search?q=${encodeURIComponent(q)}`),
  listDashboards: (customerId: string) =>
    request<DashboardSummary[]>("GET", `/customers/${customerId}/dashboards`),
  workbenchRun: (sql: string) =>
    request<WorkbenchResult>("POST", `/workbench/run`, { sql }),
  impactPreview: (body: { kind: string; fqn: string }) =>
    request<ImpactHit[]>("POST", `/impact/preview`, body),
  listAgents: () => request<{ name: string; kind: string }[]>("GET", `/agents`),
  runAgent: (name: string, body: { dry_run?: boolean; args?: Record<string, unknown> }) =>
    request<{
      summary: string;
      suggestions: { id: string; title: string; rationale?: string; severity?: string }[];
      confidence: number;
    }>("POST", `/agents/${name}/run`, body),
  catalogHover: (fqn: string, customerId: string) =>
    request<CatalogHover>(
      "GET",
      `/catalog/hover?fqn=${encodeURIComponent(fqn)}&customer_id=${encodeURIComponent(customerId)}`,
    ),
  listQuotas: (customerId: string) =>
    request<Quota[]>("GET", `/quotas?customer_id=${encodeURIComponent(customerId)}`),
  createQuota: (body: {
    customer_id: string;
    user_id?: string | null;
    daily_bytes_cap: number;
    daily_cost_cap_usd: number;
  }) => request<Quota>("POST", `/quotas`, body),
  listApprovals: (status: string) =>
    request<QueryApproval[]>("GET", `/queries/approvals?status=${encodeURIComponent(status)}`),
  decideApproval: (id: string, decision: "approve" | "deny", reason?: string) =>
    request<QueryApproval>("POST", `/queries/approvals/${id}/decide`, { decision, reason }),
  schemaDiff: (connectionId: string, from: string, to: string) =>
    request<SchemaDiff>(
      "GET",
      `/connections/${connectionId}/schema/diff?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`,
    ),
  syncDAG: (customerId: string) =>
    request<SyncDAGNode[]>("GET", `/customers/${customerId}/sync/dag`),
  dashboardVersions: (dashboardId: string) =>
    request<DashboardVersion[]>("GET", `/dashboards/${dashboardId}/versions`),
  restoreDashboardVersion: (dashboardId: string, versionId: string) =>
    request<{ id: string }>("POST", `/dashboards/${dashboardId}/versions/${versionId}/restore`),
  costPreflight: (body: { customer_id: string; sql: string }) =>
    request<CostPreflight>("POST", `/cost/preflight`, body),
  semanticSearch: (q: string, k = 10) =>
    request<SemanticHit[]>(
      "GET",
      `/catalog/semantic-search?q=${encodeURIComponent(q)}&k=${k}`,
    ),

  // Round 7
  listActivity: (customerId: string, limit = 50) =>
    request<ActivityEvent[]>(
      "GET",
      `/customers/${customerId}/activity?limit=${limit}`,
    ),

  listInbox: (customerId: string) =>
    request<InboxItem[]>("GET", `/inbox?customer_id=${encodeURIComponent(customerId)}`),
  markInboxRead: (id: string) =>
    request<void>("POST", `/inbox/${id}/read`),
  markAllInboxRead: (customerId: string) =>
    request<void>("POST", `/inbox/read-all`, { customer_id: customerId }),

  listSubscriptions: (dashboardId: string) =>
    request<DashboardSubscription[]>(
      "GET",
      `/dashboards/${dashboardId}/subscriptions`,
    ),
  createSubscription: (
    dashboardId: string,
    body: { user_email: string; schedule_cron: string; channel?: string },
  ) =>
    request<{ id: string }>(
      "POST",
      `/dashboards/${dashboardId}/subscriptions`,
      body,
    ),
  deleteSubscription: (id: string) =>
    request<void>("DELETE", `/subscriptions/${id}`),
  testSendSubscription: (id: string) =>
    request<{ sent: boolean }>("POST", `/subscriptions/${id}/test-send`),

  listWebhooks: (customerId: string) =>
    request<Webhook[]>("GET", `/webhooks?customer_id=${encodeURIComponent(customerId)}`),
  createWebhook: (body: { customer_id: string; url: string; events: string[] }) =>
    request<Webhook>("POST", `/webhooks`, body),
  deleteWebhook: (id: string) => request<void>("DELETE", `/webhooks/${id}`),

  listScheduledQueries: (customerId: string) =>
    request<ScheduledQuery[]>(
      "GET",
      `/scheduled-queries?customer_id=${encodeURIComponent(customerId)}`,
    ),
  createScheduledQuery: (body: {
    customer_id: string;
    name: string;
    sql: string;
    cron: string;
  }) => request<ScheduledQuery>("POST", `/scheduled-queries`, body),
  deleteScheduledQuery: (id: string) =>
    request<void>("DELETE", `/scheduled-queries/${id}`),
  scheduledQueryRuns: (id: string) =>
    request<ScheduledQueryRun[]>("GET", `/scheduled-queries/${id}/runs`),

  listAccessReviews: (customerId: string) =>
    request<AccessReview[]>(
      "GET",
      `/access-reviews?customer_id=${encodeURIComponent(customerId)}`,
    ),
  openAccessReview: (body: { customer_id: string; title: string }) =>
    request<AccessReview>("POST", `/access-reviews`, body),
  closeAccessReview: (id: string) =>
    request<AccessReview>("POST", `/access-reviews/${id}/close`),
  recordAccessReviewDecision: (
    id: string,
    body: { subject: string; decision: "approve" | "revoke"; reason?: string },
  ) =>
    request<AccessReviewDecision>(
      "POST",
      `/access-reviews/${id}/decisions`,
      body,
    ),

  copilotChat: (body: {
    customer_id: string;
    messages: CopilotMessage[];
  }) => request<CopilotResponse>("POST", `/copilot/chat`, body),

  catalogNarrative: (customerId: string) =>
    request<{ narrative: string }>(
      "GET",
      `/customers/${customerId}/catalog/narrative`,
    ),

  listLineageWatches: (customerId: string) =>
    request<LineageWatch[]>(
      "GET",
      `/lineage/watches?customer_id=${encodeURIComponent(customerId)}`,
    ),
  createLineageWatch: (body: {
    customer_id: string;
    fqn: string;
    notify_email?: string;
  }) => request<LineageWatch>("POST", `/lineage/watches`, body),
  deleteLineageWatch: (id: string) =>
    request<void>("DELETE", `/lineage/watches/${id}`),

  recentQueries: (customerId: string, limit = 20) =>
    request<RecentQuery[]>(
      "GET",
      `/customers/${customerId}/queries?limit=${limit}`,
    ),

  i18nMessages: (locale: string) =>
    request<Record<string, string>>("GET", `/i18n/${locale}/messages.json`),
};
