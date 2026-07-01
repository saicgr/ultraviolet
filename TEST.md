# Ultraviolet — How to Test

Everything here runs **without Docker, a warehouse, or GitHub** — using a real Postgres started in-process and a real DuckDB engine. Reproduces exactly what was used to verify the Foundational features (Data Lineage, Pull Request Analysis, Dashboard, GitHub App, Cost Optimization).

> Prereq: Go 1.23+ with **CGo** (`clang`/Xcode CLT on macOS — DuckDB + `pg_query` are CGo), Node 18+. Always build/test with `CGO_ENABLED=1`. First run of any `verify-*` / `demo` target downloads a small Postgres binary (cached after).

## TL;DR

```bash
make up                 # ⭐ ONE COMMAND: whole webapp (DB + API + frontend + seed) → http://localhost:5173
make verify-localdb     # real-Postgres end-to-end: migrations, lineage graph, cost, GitHub, API
make verify-external    # live DuckDB query engine + full PR analysis on a real git diff
```

`make up` runs everything in the foreground; press **Ctrl+C** to stop the whole stack (it tears down the API, frontend, and embedded Postgres cleanly).

---

## 1. Static + unit gates (fast, no DB)

```bash
CGO_ENABLED=1 go build ./...
CGO_ENABLED=1 go vet ./...
CGO_ENABLED=1 go test -race ./...
cd frontend && npm install && npm run typecheck && npm run build
```

Unit tests worth knowing (pure logic, no DB):

```bash
CGO_ENABLED=1 go test ./internal/lineage/      -run TestExtractColumnLineage -v   # column resolver
CGO_ENABLED=1 go test ./internal/srcanalysis/  -v                                 # dbt/Jinja rendering
CGO_ENABLED=1 go test ./internal/githubapp/    -v                                 # diff parsing + conclusion
```

---

## 2. Real-Postgres end-to-end — `make verify-localdb`

Boots an ephemeral Postgres in-process, then exercises the real control-plane code.

```bash
make verify-localdb
```

**Asserts (each prints a `✓`):**
- All 6 new migrations apply **and** fully reverse (down×6 → up) — including the risky `0020` constraint swap and `0024` primary-key swap.
- Lineage: extract → persist → **recursive-CTE graph** returns real table + column edges (from both a table root and a column root).
- Cost: the savings bug is fixed — `savings=$1.00` while `duckdb_cost=$0.00` (they used to be equal); `query_log.user_id` works.
- GitHub: install→customer routing, **fail-closed** on an unknown installation, webhook **idempotency**, `pr_analysis` persistence.
- API: real HTTP handlers `/lineage/graph`, `/pr-analysis`, `/github/install-info` return 200 with correct shapes.

Expected tail: `--- PASS: TestEndToEnd` / `END-TO-END PASSED`.

Source: `test/localpg/e2e_test.go` (build tag `localpg`; excluded from normal `go test`).

---

## 3. DuckDB engine + PR-on-real-diff — `make verify-external`

```bash
make verify-external
```

**Part 1 — live DuckDB:** runs a real query through the actual proxy worker pool and encodes Postgres wire frames:

```
query:   SELECT range AS n, range * range AS square FROM range(1, 6)
rows:    5
pg wire: 153 bytes of RowDescription + DataRow + CommandComplete frames encoded
✓ DuckDB executed real SQL via the proxy's worker pool and encoded PG result frames
```

**Part 2 — PR analysis on a REAL `git diff`:** feeds genuine `git diff` output (a migration dropping a column) through the whole pipeline and prints the actual GitHub comment + Check Run it would post:

```
detected: drop_column analytics.top_words.freq
impacted downstream: 2   data-quality refs: 3   conclusion: action_required

## 🔎 Ultraviolet impact analysis
| Downstream | Hops | DQ status | Severity |
| `analytics.word_report.freq` | 1 | ❌ fail | breaking |
| `marts.exec_summary.volume`  | 2 | ✅ pass | breaking |
```

Source: `test/external/main.go` (build tag `external`).

---

## 4. Browser end-to-end — `make up`

One command boots the whole webapp; open the browser and click through it. Ctrl+C stops everything.

```bash
make up
# open http://localhost:5173
```

### See the numbers come from YOUR activity (no seeded $)

By default the cost **Savings Dashboard starts at $0 / 0 queries** — nothing is invented. You populate it by running real queries, then watch the numbers appear:

1. `make up`, open <http://localhost:5173/> → savings **$0.00**, "No queries yet".
2. Go to **/workbench**, click **Run** on a sample (it executes on the *real* DuckDB engine the proxy uses). The result card shows the rows, bytes scanned, and the warehouse-cost it avoided.
3. Or, in a second terminal, fire a batch of real queries at once:
   ```bash
   make demo-traffic        # runs ~24 real DuckDB queries through /workbench/run
   ```
4. Back on **/** the cards now move — **Queries (24h)**, **DuckDB hit rate**, **Route breakdown**, and **Estimated savings** — all derived from those runs. Every query is listed at **/queries**, so you can audit exactly where each number came from. The pipeline is the real one: `query_log → cost rollup → cost_attribution → dashboard`.

> Want the pre-populated screenshot demo (14 days of rollups, $585.20) instead? Boot with `UV_DEMO_SEED=full`:
> ```bash
> UV_DEMO_SEED=full CGO_ENABLED=1 go run -tags demo ./test/demo   # then: cd frontend && npm run dev
> ```

<details><summary>Prefer two terminals? (e.g. to watch backend logs separately)</summary>

```bash
make demo                       # terminal 1: embedded Postgres + seeded data, API on :8080
cd frontend && npm run dev      # terminal 2: Vite on :5173 (proxies /api → :8080)
```
</details>

What to check per feature:

| Page | Verify |
|---|---|
| `/` Cost Optimization | starts at **$0 / 0 queries**; after `/workbench` runs (or `make demo-traffic`) the savings $, DuckDB hit-rate %, and route breakdown reflect exactly those queries |
| `/workbench` | type SQL → **Run** executes on real DuckDB; result card shows rows + bytes scanned + warehouse-cost avoided |
| `/dashboards` Dashboard | seeded dashboards list |
| `/lineage?fqn=analytics.top_words` Data Lineage | multi-hop graph (`samples.shakespeare → top_words → word_report → exec_summary`); toggle direction / table↔column / depth; click a node to re-root |
| `/pull-requests` PR Analysis | 2 PRs; open #42 → impacted columns, **DQ status `analytics.word_report → fail`**, GitHub link, per-node "lineage" links |
| `/github` GitHub App | "Installed" badge, installation, repos; click **Connect** on `acme-labs/etl` → flips to connected |

The seed stands in for live warehouse traffic; swap a real BigQuery/Snowflake connection in later to populate lineage/cost from actual queries.

> Driving it programmatically: this was verified with the Playwright MCP (`browser_navigate` → `browser_snapshot` → `browser_click`). Any Playwright/headless setup pointed at `http://localhost:5173` works.

---

## 5. What each test proves

| Feature | Unit | `verify-localdb` | `verify-external` | Browser |
|---|:--:|:--:|:--:|:--:|
| Data Lineage (column-level, graph) | ✅ resolver | ✅ graph CTE | — | ✅ renders |
| Source-code / dbt lineage | ✅ | — | — | — |
| Pull Request Analysis (+ data-quality) | ✅ diff | ✅ persist | ✅ real diff → comment | ✅ detail |
| GitHub App (routing, idempotency) | — | ✅ | (signature in localpg) | ✅ connect |
| Cost Optimization (savings fix) | — | ✅ formula | — | ✅ dashboard |
| Dashboard | — | — | — | ✅ list |
| Migrations (apply + reverse) | — | ✅ roundtrip | — | — |

---

## 6. Not covered (needs real external services)

- **Proxy → real warehouse** (BigQuery/Snowflake) reading **Iceberg on S3** — needs warehouse credentials + S3/LocalStack. *DuckDB execution itself is covered by `verify-external`.* To try it: set `GOOGLE_APPLICATION_CREDENTIALS`, add a connection in `/connections`, run queries through `cmd/proxy` (:5000).
- **GitHub's HTTP transport** — a real App fetching the PR diff and POSTing the comment/check-run over the network. *Signature/dedup/routing + the full analysis→render pipeline are covered.* To try it: register a GitHub App, set `UV_GITHUB_APP_ID`/`UV_GITHUB_PRIVATE_KEY`/`UV_GITHUB_WEBHOOK_SECRET`, tunnel the bot (`:8090`) via `smee.io`/`ngrok`, install it, connect the repo in `/github`, open a PR. (Local signed-webhook smoke without GitHub is in SETUP.md §5.)

---

## 7. Troubleshooting

- **`process already listening on port 5432/5456`** — a previous run left an embedded Postgres orphaned (a failed run `os.Exit`s before its cleanup). Free it: `lsof -ti :5456 | xargs kill -9` (or `:5432`), then `rm -rf ~/.uv-ext-pg ~/.uv-demo-pg`.
- **`make demo` port 8080/5173 in use** — stop a prior `make demo` / `npm run dev`, or change ports.
- **CGo / DuckDB build errors** — ensure `CGO_ENABLED=1` and a C toolchain (`xcode-select --install` on macOS).
- The `localpg`/`external`/`demo` harnesses are **build-tagged**, so they never run in normal `go build ./...` or `go test ./...`.
