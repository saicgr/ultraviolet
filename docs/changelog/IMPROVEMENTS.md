# Improvements — Open Observations

Items observed during scaffolding that aren't yet committed plans. Triage each before Phase 1 implementation begins.

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
