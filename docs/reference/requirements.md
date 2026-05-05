# System Requirements

## Toolchain

| Tool | Version | Why |
|---|---|---|
| Go | 1.22+ | Generics, range-over-int, vet improvements |
| Node.js | 20 LTS | frontend tooling |
| pnpm | 9+ | frontend package manager |
| Docker + Compose | 24+ | dev services, integration tests |
| Make | any | task runner |
| `golang-migrate` | 4.17+ | DB migrations |
| `sqlc` | 1.27+ | Go code from SQL |
| `golangci-lint` | 1.60+ | linter |
| `shellcheck` | 0.10+ | bash linter |
| `gh` (GitHub CLI) | optional | PR creation |

Install:
```bash
# macOS
brew install go node pnpm docker make golang-migrate sqlc golangci-lint shellcheck gh

# Linux (ubuntu)
# Use https://go.dev/dl/, https://nodejs.org/, official deb repos
```

## CGo (DuckDB)

`marcboeker/go-duckdb` requires CGo enabled.

```bash
export CGO_ENABLED=1
export CC=clang   # macOS; on Linux gcc works
```

### Apple Silicon (M-series) caveats

- DuckDB binaries auto-detected for arm64 by go-duckdb v1.x.
- For race-detector builds, `CGO_CFLAGS="-g -O0"` recommended for clearer crash diagnostics.
- `CGO_LDFLAGS_ALLOW='.*'` not needed in v1.x; if you see linker warnings, file an issue with `go-duckdb`.

### Linux build

Standard. `go-duckdb` ships statically-linked DuckDB; no system DuckDB install needed.

## Cloud accounts

### GCP

For integration tests against `bigquery-public-data.*`:
- A GCP project (free tier ok).
- Service account with `roles/bigquery.jobUser` + `roles/bigquery.dataViewer`.
- JSON key in `GOOGLE_APPLICATION_CREDENTIALS`.
- Quota: integration tests use ≤1 GiB scanned per run; well under the free 1 TiB/month.

Setup script in `.claude/skills/bigquery-sample-datasets/SKILL.md`.

### AWS

For dev: LocalStack (Docker) — no real AWS account needed.

For prod managed-storage: an S3 bucket + IAM user/role with `s3:ListBucket`, `s3:GetObject`, `s3:PutObject`, `s3:DeleteObject` scoped to the bucket.

### Snowflake / Databricks

Not required for Phase 1 development. When wiring Phase 2, customer-provided.

## RAM + disk

Dev:
- ≥16 GB RAM (DuckDB + LocalStack + Postgres + Redis + Node).
- ≥10 GB free disk (DuckDB workers spill, LocalStack storage).

CI:
- 8 GB RAM ok for Go tests.
- 16 GB recommended for `test-integration` with LocalStack.

## Editor / IDE

Anything that talks LSP. VS Code + Go extension or GoLand fine. The `.claude/settings.json` hooks work with Claude Code; other editors get gofmt-on-save via the editor's own config.

## Browser

Frontend dev: any evergreen Chrome/Firefox/Safari.

## Operating systems supported

- macOS 13+ (Apple Silicon + Intel)
- Ubuntu 22.04 / Debian 12+
- Other Linux distros likely fine; not regularly tested

Windows: WSL2 only. Native Windows not supported (CGo + LocalStack quirks).
