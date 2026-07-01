# Ultraviolet — Local Setup & Verification

How to build, verify, and run the Foundational features (Data Lineage, Pull Request Analysis, Dashboard, GitHub App, Cost Optimization) locally.

> **TL;DR** — fastest confidence with zero external services:
> ```bash
> make verify-localdb      # real end-to-end on an ephemeral in-process Postgres (no Docker)
> ```

---

## 1. Prerequisites

| Tool | Why | Notes |
|---|---|---|
| **Go 1.23+ with CGo** (`clang`/Xcode CLT on macOS) | DuckDB + `pg_query` are CGo | always build with `CGO_ENABLED=1` |
| **Node 18+** (`npm`) | frontend | |
| **Postgres 14+** *or* Docker | control-plane DB | the `verify-localdb` harness downloads its own Postgres, so this is optional for verification |
| BigQuery service-account JSON | only for *live* warehouse queries (runtime lineage, real cost) | optional |
| A GitHub App (id + private key + webhook secret) | only for *live* PR analysis | optional |

---

## 2. Verify (no external services needed)

```bash
# Real end-to-end against an ephemeral Postgres started in-process (no Docker).
# Proves: all migrations apply + reverse, lineage graph (recursive CTE), the
# cost-savings fix, GitHub install→customer routing + webhook idempotency, and
# the API handlers — all against real Postgres.
make verify-localdb

# Standard static/unit gates:
CGO_ENABLED=1 go build ./...
CGO_ENABLED=1 go vet ./...
CGO_ENABLED=1 go test -race ./...
cd frontend && npm install && npm run typecheck && npm run build
```

`make verify-localdb` is the single most useful check — it exercises the real control-plane code path end to end without Docker, BigQuery, or GitHub.

### See the whole webapp in a browser (no Docker, no cloud)

**One command** — boots embedded Postgres + seeded data + API + frontend, then Ctrl+C stops it all:

```bash
make up        # → open http://localhost:5173
```

<details><summary>Two terminals (to watch backend logs separately)</summary>

```bash
make demo                         # terminal 1: embedded Postgres + seeded demo data, API on :8080
cd frontend && npm run dev        # terminal 2: Vite on :5173 (proxies /api → :8080)
```
</details>

`make up` / `make demo` boots a real Postgres in-process and seeds the **structural** Foundational data: a multi-hop lineage graph (`samples.shakespeare → analytics.top_words → analytics.word_report → marts.exec_summary`), two analyzed PRs (one `action_required` with a failing data-quality test), and a connected GitHub installation.

The **cost Savings Dashboard deliberately starts at $0 / 0 queries** — no invented numbers. You populate it yourself: open **/workbench** and run a query (it executes on the *real* DuckDB engine), or run `make demo-traffic` to fire a batch. Each run flows through the real pipeline (`query_log → cost rollup → cost_attribution → dashboard`) and is auditable at **/queries**. To instead boot with the pre-populated screenshot demo (14 days of rollups, $585.20), set `UV_DEMO_SEED=full`.

The warehouse "datasource" is intentionally absent — it's what you swap in later (BigQuery/Snowflake) to populate lineage/cost from live BI traffic instead of the Workbench.

---

## 3. Run the full stack locally

### 3a. Database + migrations

**Option A — Docker:**
```bash
make dev                 # postgres + redis + localstack
export DATABASE_URL="postgres://uv:uv@localhost:5432/uv?sslmode=disable"
make migrate-up          # (the API also auto-migrates on boot)
```

**Option B — your own Postgres:** point `DATABASE_URL` at it; the API runs `MigrateUp` on boot.

> The risky migrations (`0020` constraint swap, `0024` PK swap) are proven reversible by `make verify-localdb`; to check by hand: `make migrate-up` then `make migrate-down` repeatedly.

### 3b. Services

```bash
export ENCRYPTION_KEY="0000000000000000000000000000000000000000000000000000000000000000"  # dev only
export JWT_SECRET="dev-jwt-secret-change-me"                                              # dev only

CGO_ENABLED=1 go run ./cmd/api          # :8080  REST control plane → curl localhost:8080/readyz
CGO_ENABLED=1 go run ./cmd/proxy        # :5000  PG-wire proxy (needs a warehouse connection for real queries)
CGO_ENABLED=1 go run ./cmd/lineage-bot  # :8090  GitHub App webhook receiver (needs UV_GITHUB_WEBHOOK_SECRET)

cd frontend && npm run dev              # :5173  Vite dev server, proxies /api → :8080
```

Open **http://localhost:5173**. The 5 Foundational features are grouped at the top of the sidebar.

> ⚠️ **Production safety:** set `UV_PROD=true` in any real deployment. It refuses to boot with the all-zero `ENCRYPTION_KEY` or the dev `JWT_SECRET`, and disables the `X-UV-Dev-Bypass` auth header. The lineage-bot refuses to boot without `UV_GITHUB_WEBHOOK_SECRET`.

---

## 4. Environment variables (new features)

| Var | Default | Purpose |
|---|---|---|
| `UV_PROD` | `false` | `true` disables dev backdoors + refuses weak secrets |
| `UV_BQ_USD_PER_TIB` | `6.25` | BigQuery cost rate |
| `UV_SF_USD_PER_TIB` | `5.0` | Snowflake cost rate |
| `UV_GITHUB_APP_ID` | — | GitHub App id |
| `UV_GITHUB_APP_SLUG` | — | App slug for the install link (`github.com/apps/<slug>/installations/new`) |
| `UV_GITHUB_PRIVATE_KEY` / `UV_GITHUB_PRIVATE_KEY_PATH` | — | App RSA private key (PEM inline or file path) |
| `UV_GITHUB_WEBHOOK_SECRET` | — | **required** for the bot; HMAC webhook verification |
| `UV_GITHUB_SETUP_REDIRECT_URL` | `http://localhost:5173/github` | post-install redirect |
| `UV_LINEAGE_BOT_PORT` | `8090` | bot listen port |

---

## 5. Exercise each Foundational feature

### Cost Optimization ✅ (build the numbers yourself)
- `/` starts at **$0 / 0 queries**. Run queries in **/workbench** (real DuckDB) or `make demo-traffic`, then watch `/` fill: savings, DuckDB hit-rate, route breakdown, query count — all from your runs, auditable in `/queries`.
- `/cost-preflight` estimates a query's bytes/USD before running (returns `estimated_bytes_scanned`, `estimated_cost_usd`, `would_block_if_over_budget`, and any `unknown_tables`).
- Savings come from `cost_attribution`, rolled up from `query_log` by the cost backfiller (nightly cron / `cmd/sync`, and on demand after each Workbench run). The CSV export (`/api/v1/customers/{id}/cost-attribution.csv`) derives live from `query_log` when no rollup exists yet and marks the source with an `X-UV-Cost-Source` header.
- `UV_DEMO_SEED=full` pre-seeds 14 days of rollups for a populated screenshot view.

### Dashboard ✅
- `/dashboards`, `/subscriptions`, `/dashboard-versions` — already complete; confirm they load.

### Data Lineage ✅
- `/lineage` → search a table FQN (e.g. `analytics.top_words`). Toggle **direction** (upstream/downstream/both), **granularity** (table/column), and **depth**. Click a node to re-root. Solid edges = runtime-observed; dashed = source-code-derived.
- **Populate it:** run write queries through the proxy so runtime lineage captures (off the hot path):
  ```sql
  CREATE TABLE analytics.top_words AS
    SELECT word AS term, word_count AS freq FROM samples.shakespeare WHERE word_count > 100;
  ```
  (Requires a connected warehouse. Without one, `make verify-localdb` exercises the same extract→persist→graph path directly.)

### GitHub App ✅ (settings work; live PR analysis needs a real App)
- `/github` → install state, installations, and repo connect/disconnect.
- Connecting a repo binds it to the active customer (this is what gives the bot a real tenant instead of the old zero-UUID).

### Pull Request Analysis ✅ (history UI works; live needs a real App)
- `/pull-requests` lists analyzed PRs with impacted-node count, a **data-quality** badge, and a conclusion; click a PR for impacted downstream nodes + DQ status + a "View on GitHub" link.

#### Local webhook smoke (no real GitHub):
```bash
export UV_GITHUB_WEBHOOK_SECRET="dev-bot-secret"
BODY='{"action":"opened","pull_request":{"number":7,"html_url":"https://github.com/acme/dw/pull/7","head":{"sha":"abc1234"}},"repository":{"id":456,"full_name":"acme/dw"},"installation":{"id":123}}'
SIG="sha256=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$UV_GITHUB_WEBHOOK_SECRET" | awk '{print $2}')"
curl -s -X POST localhost:8090/webhook \
  -H "X-GitHub-Event: pull_request" \
  -H "X-GitHub-Delivery: $(uuidgen)" \
  -H "X-Hub-Signature-256: $SIG" \
  -H "Content-Type: application/json" -d "$BODY"
```
This verifies signature → delivery-dedup → tenant routing. **Note:** fetching the PR diff + posting the comment/check-run requires a real GitHub App token (`UV_GITHUB_APP_ID` + `UV_GITHUB_PRIVATE_KEY`); without it those steps log a "github app not configured" warning. For a full live test, register a GitHub App, point its webhook at the bot via `smee.io`/`ngrok`, install it, connect the repo in `/github`, and open a PR.

---

## 6. What is and isn't auto-verified

**Verified by `make verify-localdb` (real Postgres):** migrations + roundtrip, lineage extract→persist→graph (table + column), the cost-savings fix, per-user cost column, GitHub install→customer routing + fail-closed + webhook idempotency + `pr_analysis` persistence, and the `/lineage/graph`, `/pr-analysis`, `/github/*` HTTP handlers.

**Verified by `make verify-external`** (no warehouse / GitHub needed):
- **Live DuckDB query execution** through the actual worker pool — runs real SQL on an in-memory DuckDB and encodes PG result frames (the Iceberg ATTACH is non-fatal, so no S3 needed).
- **Full PR-analysis pipeline on a real `git diff`** — `git diff --no-index` → `ParseDiff` → `DetectChanges` → column-level `impact.Preview` → `dq` cross-ref → the actual rendered GitHub PR comment + Check Run JSON. This is everything `AnalyzePR` does after the diff is fetched.

**Still needs real external services (the only remaining gap):**
- Proxy's live path against a **real warehouse** (BigQuery/Snowflake) reading **Iceberg on S3** — needs warehouse credentials + S3/LocalStack. (DuckDB execution itself is verified above.)
- GitHub's **HTTP transport** — a real App fetching the PR diff and POSTing the comment/check-run over the network. The webhook signature/dedup/routing and the entire analysis+render pipeline are verified; only the network calls to `api.github.com` are not.

---

## 7. Known follow-ups
- dbt-manifest metadata sync, PII propagation along lineage, cross-warehouse lineage, per-customer cost timezone, full RBAC rollout.
- `cmd/lineage-bot` dashboards-as-code push handler still routes to a placeholder customer (separate from PR analysis, which is fully tenant-correct).
