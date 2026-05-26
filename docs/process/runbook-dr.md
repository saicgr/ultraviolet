# Disaster Recovery Runbook (ops-6)

When the world is on fire. Each section is self-contained — read top-to-bottom
of the relevant scenario, do not skip steps. RTOs are conservative.

> Precedence during incident response: **stop further damage first** (rotate
> credentials, halt syncs), **restore read traffic second**, **backfill
> writes / cost data last**.

## Inventory of stateful surfaces

| Surface | Backing store | RPO | RTO |
|---|---|---|---|
| Control-plane DB | Render Postgres (auto-backup every 6h) | 6h | 30 min |
| Iceberg lake | S3 versioned bucket (`uv-prod-iceberg`) | 0 (versioned) | 60 min |
| DuckDB worker pool | Stateless | n/a | 5 min (redeploy) |
| Encrypted credentials | Control-plane DB column (`connections.encrypted_credentials`) | tied to control-plane | tied to control-plane |
| Query log | Control-plane DB | 6h | 30 min |

## 1. Control-plane Postgres — restore

**Symptoms:** API 500s on every endpoint; `pg_isready` fails;
`uv_pg_connections_total` flat-lines.

### Path A — Render-managed auto-restore (preferred)

1. Render dashboard → `uv-control-plane` service → **Backups** tab.
2. Pick the latest pre-incident backup (timestamps shown in UTC).
3. Click **Restore** — creates a new database with `-restored-YYYYMMDD` suffix.
4. Repoint `DATABASE_URL` env on `uv-api` and `uv-proxy` services to the new DSN.
5. Trigger redeploy on both services. Verify `GET /healthz` and a
   `SELECT 1 FROM customers LIMIT 1` from `psql`.

### Path B — Manual `pg_restore`

Use when you have a `.dump` file (nightly cron pushes to `s3://uv-prod-backups/pg/`).

```bash
aws s3 cp s3://uv-prod-backups/pg/uv-control-YYYYMMDD.dump .
createdb -h <host> -U postgres uv_restored
pg_restore -h <host> -U postgres -d uv_restored -j 4 --no-owner --no-acl uv-control-YYYYMMDD.dump
```

Repoint `DATABASE_URL`, redeploy, verify as in Path A.

**Post-restore:** run `cmd/sync` backfill to recover any query_log rows lost
between the backup and incident; cost-attribution backfill will pick up
`actual_cost_usd` on its nightly cycle.

## 2. Iceberg-on-S3 recovery

**Symptoms:** DuckDB workers return `Cannot open Iceberg table` / `Manifest
not found` errors; cost spike on Snowflake/BigQuery (fallback engaged).

S3 bucket `uv-prod-iceberg` has versioning enabled and a 90-day noncurrent
retention. Recovery options:

### A) Object-level restore (single corrupt manifest)

1. AWS Console → S3 → `uv-prod-iceberg` → enable **Show versions**.
2. Navigate to the corrupted prefix (e.g. `customer_xyz/orders/metadata/`).
3. For each affected file: delete the latest version's **delete marker**, or
   use **Actions → Restore** on the prior version.

### B) Bucket-wide rollback (broad corruption)

```bash
# Replays object versions at or before a target timestamp using the AWS CLI.
# DO NOT run on the live bucket without coordination — pause sync first.
scripts/iceberg-rollback.sh --bucket uv-prod-iceberg --at "2026-05-09T03:00Z"
```

Pause `cmd/sync` (set `UV_SYNC_PAUSED=1`) for the affected customers before
rollback so new writes don't race the restore. Resume after verification.

### C) Per-customer rebuild from source warehouse

If S3 history is gone: re-trigger initial sync via
`POST /api/v1/connections/{id}/synced-tables` with `sync_mode='full_refresh'`.
RTO scales with source-warehouse scan cost — budget accordingly.

## 3. DuckDB worker pool — redeploy

Workers are stateless (Iceberg is the source of truth). Recovery is just a
fresh deploy.

1. Render → `uv-duckdb-workers` → **Manual Deploy → Latest commit**.
2. Wait for health checks; new workers register with the proxy automatically
   via Consul.
3. If the prior worker pool is leaking goroutines / CGo-side memory, set
   `UV_DUCKDB_RECYCLE_AFTER=300` (queries) and roll one more time.

No data restoration needed. If `~/.duckdb/<customer>.duckdb` cache files were
custom-mounted, they will rebuild lazily on next read.

## 4. Credential re-encryption / `ENCRYPTION_KEY` rotation

**When:** suspected key compromise, scheduled annual rotation, or operator
turnover.

The key is an AES-256-GCM master used to wrap `connections.encrypted_credentials`.
Rotation is a **two-phase** procedure: dual-read, single-write.

### Phase 1 — dual-read deploy

1. Generate new key: `openssl rand -hex 32`. Store in Render secret manager as
   `UV_ENCRYPTION_KEY_NEXT`.
2. Deploy `uv-api` + `uv-sync` with both `UV_ENCRYPTION_KEY` (current) and
   `UV_ENCRYPTION_KEY_NEXT` set. `store.Encryptor` accepts either for decrypt;
   encrypt still uses the current key.
3. Verify a known connection still decrypts:
   `curl -X GET /api/v1/connections/{id}/test`.

### Phase 2 — re-encrypt and promote

Run the one-shot rotation script from a trusted operator host:

```bash
go run ./scripts/rotate-encryption-key \
    --database-url "$DATABASE_URL" \
    --current "$UV_ENCRYPTION_KEY" \
    --next    "$UV_ENCRYPTION_KEY_NEXT" \
    --batch-size 50
```

The script reads every `connections` row, decrypts with current, re-encrypts
with next, and updates the row in a single transaction per batch. Idempotent
on failure — re-run safely.

After completion:

1. Swap env vars: `UV_ENCRYPTION_KEY ← UV_ENCRYPTION_KEY_NEXT`, clear
   `UV_ENCRYPTION_KEY_NEXT`.
2. Redeploy `uv-api`, `uv-proxy`, `uv-sync`.
3. Append `docs/changelog/CHANGELOG.md` with the rotation date and operator.
4. Spot-check 3 random customers via `/connections/{id}/test`.

## Drill cadence

- **Quarterly:** Path A control-plane restore against staging.
- **Semi-annually:** Iceberg per-customer rebuild against staging.
- **Annually:** full ENCRYPTION_KEY rotation (real).

Every drill produces a `docs/runbook-drills/YYYY-QN.md` post-mortem.
