#!/usr/bin/env bash
# backup-control-plane.sh — pg_dump the control-plane Postgres, gzip, and
# optionally upload to S3 (UV_BACKUP_S3_BUCKET). Designed to be run from a
# cron / Kubernetes CronJob.
#
# Required env:
#   DATABASE_URL           — postgres://user:pass@host:port/db
# Optional env:
#   UV_BACKUP_DIR          — local output dir (default: /var/backups/ultraviolet)
#   UV_BACKUP_S3_BUCKET    — if set, gzip is uploaded to s3://$bucket/control-plane/
#   UV_BACKUP_RETAIN_DAYS  — local retention in days (default: 7)

set -euo pipefail

: "${DATABASE_URL:?DATABASE_URL is required}"

BACKUP_DIR="${UV_BACKUP_DIR:-/var/backups/ultraviolet}"
RETAIN_DAYS="${UV_BACKUP_RETAIN_DAYS:-7}"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${BACKUP_DIR}/control-plane-${TS}.sql.gz"

mkdir -p "${BACKUP_DIR}"

echo "[backup] pg_dump → ${OUT}"
pg_dump --no-owner --no-privileges --format=plain "${DATABASE_URL}" | gzip -9 > "${OUT}"

SIZE_BYTES=$(wc -c < "${OUT}")
echo "[backup] wrote ${OUT} (${SIZE_BYTES} bytes)"

if [[ -n "${UV_BACKUP_S3_BUCKET:-}" ]]; then
  S3_DEST="s3://${UV_BACKUP_S3_BUCKET}/control-plane/$(basename "${OUT}")"
  echo "[backup] upload → ${S3_DEST}"
  aws s3 cp "${OUT}" "${S3_DEST}" --only-show-errors
fi

# Local retention — prune dumps older than RETAIN_DAYS.
find "${BACKUP_DIR}" -name 'control-plane-*.sql.gz' -mtime "+${RETAIN_DAYS}" -delete

echo "[backup] done"
