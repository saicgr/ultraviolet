# Improvements — Open Observations

Items observed during scaffolding that aren't yet committed plans. Triage each before Phase 1 implementation begins.

---

## Phase-1 deferred (added 2026-05-05 after batches 0–8 implementation pass)

### Completed in fill-in-gaps pass (same day)

- ~~`ai_generate` Path A/B selection~~ — done. Heuristic in `internal/ai/path_b.go::looksScalarOrLimited`; `Rewriter.Rewrite` now logs path A/B; both paths defer to DuckDB `llm` extension. External-API rewrite still future work.
- ~~DuckDB pool Iceberg pubsub refresh~~ — done. `internal/workers/pool.go::subscribeRefresh` PSUBSCRIBEs to `iceberg.snapshot.{slug}.*` and logs on receipt.
- ~~Snowflake cost backfill~~ — done. `internal/cost/snowflake_cost.go` reads `ACCOUNT_USAGE.QUERY_HISTORY` via gosnowflake (Snowflake-account-required at runtime).
- ~~REST API JWT middleware~~ — done. `internal/api/auth.go::authMiddleware` validates HS256 Bearer tokens; dev bypass via `X-UV-Dev-Bypass: 1` header until SSO lands in Phase 1.5.
- ~~Iceberg writer real-format attempt~~ — done. `internal/iceberg/writer.go` now tries `COPY (...) TO 's3://...' (FORMAT 'iceberg', OVERWRITE_OR_IGNORE TRUE)` first, falls back to Parquet with a loud warning when the extension is unavailable.
- ~~DuckDB llm extension auto-load~~ — done. `internal/workers/pool.go::installExtensions` adds optional `INSTALL llm FROM community; LOAD llm`.
- ~~Integration smoke test~~ — done. `test/integration/pgwire_smoke_test.go` connects via real `pgx` driver and verifies `SELECT 1` round-trip without external services.
- ~~Unit tests for ai/connectors/logger~~ — done.

### Completed in second fill-in-gaps pass (2026-05-07)

- ~~golang-migrate auto-apply~~ — done. `internal/store/migrate.go` + boot-time call in `cmd/proxy` and `cmd/api`. `UV_MIGRATIONS_SOURCE` env override.
- ~~TLS self-signed cert auto-gen~~ — done. `internal/protocols/pgwire/tls.go::LoadOrGenerateTLS` produces a P-256 in-memory cert when no paths configured; loads files when `TLS_CERT_PATH`/`TLS_KEY_PATH` set. Wired into `cmd/proxy` via `UV_DEV_TLS=true`.
- ~~ai_generate Path B external providers~~ — done. `internal/ai/provider.go` adds OpenAI + Anthropic HTTP clients; `Rewriter.CompleteOne` returns `ErrNoProvider` when no key, otherwise hits the real API.
- ~~Snowflake STREAM CDC syncer~~ — done. `internal/sync/snowflake_syncer.go` opens gosnowflake, queries `uv_<schema>_<table>_stream`, appends to Iceberg, publishes pubsub. Returns nil quietly when account unreachable.
- ~~Per-table CHECKPOINT on pubsub~~ — done. `Pool.subscribeRefresh` issues `CHECKPOINT` on every snapshot event so subsequent ATTACH sees fresh metadata.
- ~~Production JWT enforcement toggle~~ — done. `UV_API_REQUIRE_AUTH=true` makes missing/invalid Bearer return 401; default false for dev.
- ~~OpenAPI spec~~ — done. `internal/api/openapi.yaml` embedded; served at `GET /openapi.yaml`. Frontend codegen path is now unblocked.
- ~~goroutine-leak tests~~ — done. `goleak.VerifyTestMain` in `internal/{ai,router,store}` test packages; passes with `-race`.
- ~~Iceberg writer atomicity test~~ — done. `internal/iceberg/writer_test.go` verifies fail-closed behavior on invalid source query.

### Still open (deferred to Phase 1.5+)

- **Live-warehouse integration tests** — replace placeholder GCP creds in `.env` and run `test/integration/*` against `bigquery-public-data.samples.shakespeare`.
- **LocalStack iceberg snapshot end-to-end + pyiceberg cross-read** — sync writer call path executes but no real rows materialize via `read_bigquery(...)` yet; needs the DuckDB BigQuery extension (or an Arrow-bridged temp file).
- **`ai_generate` Path B SQL rewrite** — provider client exists but the rewriter still passes the SQL through unchanged. Phase 1.5: rewrite `ai_generate(...)` calls to a UV-provided UDF that batches via `Rewriter.CompleteOne`.
- **SCRAM-SHA-256 auth** — pgwire still cleartext-over-TLS only.
- **Real Snowflake STREAM row materialization** — current syncer counts STREAM rows but the iceberg `Append` source query is a placeholder; pipe rows through DuckDB.
- **Gate-agent runs** — invoke `pg-protocol-validator`, `warehouse-connector-tester`, `iceberg-spec-validator`, `security-auditor`, `code-organizer` over the new code.
- **pg_query_go v5→v6 switch** — v5 fails to compile on macOS Tahoe SDK (`strchrnul` redeclaration). Sticking with v6 for Phase 1.

---

## Architectural

- **ADBC protocol surface (Phase 1.5)** — design only; Phase 1 ships PG-wire alone. Track when first BI tool customer requests Arrow throughput.
- **Snowflake-wire emulation (Phase 2)** — drop-in compatibility for existing Snowflake clients. Hard. Defer until 3+ Snowflake customers ask.
- **Hybrid pushdown routing (Phase 2)** — router decomposes a query into "DuckDB part + warehouse-pushdown part." Phase 1 router logs `pushdown.candidate` without acting. Promote to active routing once we have telemetry on how often it would save bytes.
- **Iceberg catalog passthrough mode** — read Snowflake-managed Iceberg via Horizon Catalog without a sync step. Cuts ~weeks off Phase 1 for Iceberg-native customers. Spec it before first Snowflake customer onboards.
- **BYO Iceberg mode** — customer already has Iceberg on their S3/GCS; just point DuckDB. Easiest path; should be MVP, not Phase 2.

## Build / verification

- `compile-checker` agent assumes `golangci-lint` config exists. Add `.golangci.yml` with at least: `errcheck`, `govet`, `staticcheck`, `gosec`, `revive`. Phase 1 step 0.
- `make migrate-roundtrip` target needs to detect when there are no pending migrations and skip with explicit `SKIP`, not `PASS`.
- Pre-commit hook in `.claude/settings.json` only runs `gofmt`; should also run `go vet ./<changed-pkg>` and possibly `golangci-lint run --new-from-rev=HEAD~1` for incremental lint. Verify perf cost before enabling.
- Hook commands embed JSON parsing in shell — fragile. Replace with a small Go binary `tools/uv-hook` once the toolchain exists.

## Documentation

- `docs/architecture/observability.md` doesn't yet specify metric names. Pin them before first Prometheus exporter is wired (`uv_proxy_query_duration_seconds`, `uv_router_decision_total{routed_to=...}`, etc.).
- `docs/architecture/cost-attribution.md` lacks SQL examples for each warehouse view query. Add when wiring `internal/cost`.
- No `docs/strategy/STRATEGY.md` master doc yet (Reppora has one). Defer until pricing finalized.

## Open product questions

- LLM API keys: per-customer (decentralized, customer pays directly) vs platform (centralized, we mark up)? Brief says per-customer; revisit when pricing tiers are set.
- Storage mode default: managed (our S3) vs BYOS? Brief implies managed default; data-sensitive customers will demand BYOS — make BYOS first-class in onboarding flow.
- Phase 1 launch: BigQuery first per the user's launch strategy. Snowflake architecture proven, code stubbed.
