# SLO Definitions (ops-7)

Service-level objectives for the Ultraviolet platform. Every SLO has: target,
measurement source, Prometheus counter, Grafana query, and error-budget burn
policy. Burn alerts fire to PagerDuty (`uv-prod-primary`).

> SLO precedence: when two SLOs conflict (e.g. availability vs query-success),
> protect **availability first** — a 502 is worse than a slow query.

## 1. Availability — 99.9 % (both proxy and api)

- **Definition:** ratio of non-5xx responses to total responses on
  `:5432` (proxy) and `:8080` (api) measured over a rolling 30-day window.
- **Error budget:** 43.2 minutes of downtime / 30 days (4.32 min / 3 days for
  the fast-burn window).
- **Prometheus source:** `uv_http_requests_total{status!~"5.."}` (api) +
  `uv_pg_connections_total{outcome="ok"}` (proxy).
- **Grafana query (availability %):**
  ```promql
  sum(rate(uv_http_requests_total{status!~"5.."}[30d]))
    / sum(rate(uv_http_requests_total[30d]))
  ```
- **Burn alerts:**
  - **Fast burn:** 14.4× budget in 1h → page primary on-call.
  - **Slow burn:** 6× budget in 6h → ticket + Slack `#uv-ops`.

## 2. Query success rate — ≥ 99 % (excluding warehouse errors)

- **Definition:** ratio of `query_log` rows with `error IS NULL` *OR*
  `error_source IN ('proxy','duckdb')` excluded, to total rows. We exclude
  warehouse-side errors (`error_source='warehouse'`) because those are outside
  our control surface.
- **Error budget:** 1 % of query volume / 30 days.
- **Prometheus source:** `uv_route_decision_total{outcome="ok"}` /
  `uv_route_decision_total`.
- **Grafana query:**
  ```promql
  sum(rate(uv_route_decision_total{outcome="ok"}[30d]))
    / sum(rate(uv_route_decision_total{outcome!="warehouse_error"}[30d]))
  ```
- **Burn alerts:** fast-burn paging at 14.4× budget over 1h.

## 3. Proxy overhead — p95 < 100 ms

- **Definition:** wall-time *added* by the proxy layer beyond the downstream
  warehouse / DuckDB query time. Measured as
  `proxy_total_ms − downstream_exec_ms`.
- **Prometheus source:** `uv_proxy_overhead_ms` histogram, label `route`.
- **Grafana query:**
  ```promql
  histogram_quantile(0.95, sum(rate(uv_proxy_overhead_ms_bucket[5m])) by (le))
  ```
- **Burn alert:** p95 > 100 ms for 10 min → Slack `#uv-ops` warning.
  Sustained > 250 ms for 30 min → page.
- **Notes:** the DuckDB-route path includes Arrow→pg-wire encoding; budget
  most of the 100 ms there. Snowflake-passthrough should sit well under 20 ms.

## 4. Sync lag — p95 < 2 min

- **Definition:** time between a CDC row landing in the source warehouse and
  being queryable in Iceberg via DuckDB (`now() - synced_tables.last_synced_at`).
- **Prometheus source:** `uv_sync_lag_seconds` gauge per `(customer, table)`.
- **Grafana query:**
  ```promql
  quantile_over_time(0.95, uv_sync_lag_seconds[30d])
  ```
- **Burn alert:** any per-table p95 > 5 min for 10 min → page sync on-call.
- **Notes:** per-table; aggregated 99th percentile is reported but not paged
  on (a single straggler shouldn't wake anyone).

## 5. AI cost guardrail — per-customer

- **Definition:** spend on LLM provider calls (`ai_call_log.cost_usd`) stays
  below the customer's configured monthly cap.
- **Prometheus source:** `uv_ai_cost_usd_total{customer=…}`.
- **Grafana query:**
  ```promql
  sum by (customer) (increase(uv_ai_cost_usd_total[30d]))
  ```
- **Burn alert:** > 80 % of cap → Slack `#uv-ops`; > 100 % → block calls
  (handled in code, `Rewriter.takeBudget`).

## Error-budget policy

When any 30-day error budget is **fully consumed**:

1. **Halt all non-fix deploys** until budget recovers.
2. Open a "budget exhaustion" doc (RCA + remediation plan) within 24h.
3. Promote any related IMPROVEMENTS items to top-of-backlog.

Budget refreshes weekly on a rolling window; do not "reset" budgets manually.

## Dashboards

- `dashboards/uv-slo-overview.json` — all five SLOs at a glance.
- `dashboards/uv-slo-burn-rate.json` — fast/slow burn for availability + success.
- `dashboards/uv-sync-lag-by-customer.json` — per-customer sync lag distribution.

## Adding a new SLO

1. Define target + measurement window in this file.
2. Add Prometheus counter / gauge / histogram in `internal/metrics/`.
3. Add Grafana query + dashboard panel.
4. Wire burn-rate alert in `monitoring/alerts.yaml` (fast + slow).
5. Append `docs/changelog/CHANGELOG.md`.
