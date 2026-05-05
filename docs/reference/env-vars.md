# Environment Variables

Loaded by `internal/config`. `.env` for dev (gitignored); secret manager (AWS Secrets Manager / GCP Secret Manager) in prod.

## Core

| Var | Default | Meaning |
|---|---|---|
| `DATABASE_URL` | `postgres://uv:uv@localhost:5432/uv` | Control-plane Postgres |
| `REDIS_URL` | `redis://localhost:6379` | Pubsub + freshness cache |
| `UV_PROXY_PORT` | `5432` (prod), `5000` (dev/test) | PG-wire listener. Dev uses 5000 to avoid colliding with a local Postgres on 5432. |
| `UV_API_PORT` | `8080` | REST API |
| `UV_LOG_LEVEL` | `info` (prod), `debug` (dev) | zerolog level |
| `UV_LOG_FORMAT` | `json` (prod), `pretty` (dev) | output format |
| `UV_DEV_TLS` | `false` | auto-generate self-signed TLS cert |
| `UV_DEBUG` | `false` | enables stack traces in API responses (NEVER enable in prod) |

## Auth + crypto

| Var | Required? | Meaning |
|---|---|---|
| `JWT_SECRET` | yes | JWT signing for control-plane UI sessions |
| `ENCRYPTION_KEY` | yes | 32-byte hex; used to derive per-customer AES-256-GCM keys for credentials at rest |
| `TLS_CERT_PATH` | prod | TLS cert for proxy (PEM) |
| `TLS_KEY_PATH` | prod | TLS key for proxy (PEM) |
| `PROXY_REQUIRE_TLS` | `true` (prod) | reject non-SSL connections |

## AWS (managed storage mode)

| Var | Required? | Meaning |
|---|---|---|
| `AWS_REGION` | yes (managed) | S3 region |
| `AWS_ACCESS_KEY_ID` | yes (managed) | UV's AWS credentials |
| `AWS_SECRET_ACCESS_KEY` | yes (managed) | ditto |
| `S3_BUCKET` | yes (managed) | UV-managed bucket |
| `S3_ENDPOINT` | optional | LocalStack URL in dev |

## GCP (BigQuery + GCS BYOS)

| Var | Required? | Meaning |
|---|---|---|
| `GOOGLE_APPLICATION_CREDENTIALS` | yes for BQ tests | path to service-account JSON |
| `GCP_PROJECT` | yes | project for BQ jobs |

## LLM

| Var | Required? | Meaning |
|---|---|---|
| `OPENAI_API_KEY` | for ai_generate Path B (OpenAI) | platform-side key (only if you bill LLM separately) |
| `ANTHROPIC_API_KEY` | for ai_generate (Anthropic) | ditto |
| `GOOGLE_AI_API_KEY` | for ai_generate (Gemini) | ditto |

Per-customer LLM keys override platform keys when set in the control plane.

## Operations

| Var | Default | Meaning |
|---|---|---|
| `UV_MAX_DUCKDB_WORKERS` | `64` | hard cap across all customer pools |
| `UV_DUCKDB_WORKER_TIMEOUT` | `30s` | per-query DuckDB timeout |
| `UV_SYNC_POLL_INTERVAL` | `60s` | default Snowflake STREAM poll interval |
| `UV_FRESHNESS_DEFAULT_MAX_LAG` | `5m` | per-table override stored in DB |
| `UV_QUERY_LOG_BATCH_SIZE` | `100` | rows per batched insert |
| `UV_QUERY_LOG_BATCH_INTERVAL` | `5s` | max wait between batches |
| `UV_COST_BACKFILL_INTERVAL` | `1h` | nightly cron is on top |

## Test-only

| Var | Required? | Meaning |
|---|---|---|
| `UV_TEST_BQ_PROJECT` | for `make test-integration` | GCP project to bill BQ jobs to (free tier ok) |
| `UV_TEST_LOCALSTACK_ENDPOINT` | optional | override LocalStack URL |
| `BQ_BENCHMARK` | `0` | opt-in flag for expensive `wikipedia.pageviews_2024` benchmarks |

## Loading discipline

`internal/config.Load()` is the single entry. Validates required vars at startup; refuses to start with helpful errors. Never reads env vars elsewhere in the codebase.
