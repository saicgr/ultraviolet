#!/usr/bin/env bash
#
# Generate REAL query traffic against a running demo (`make up`). Each query is
# executed on the actual DuckDB engine via POST /api/v1/workbench/run, logged to
# query_log, and rolled into cost_attribution — so the Savings Dashboard fills
# with activity YOU generated (and can audit row-by-row in /queries). Nothing is
# inserted directly; every number traces to a real executed query.
#
# Usage:  ./scripts/gen-traffic.sh [rounds]   (default 3 rounds → 24 queries)
#         make demo-traffic
set -euo pipefail

API="${UV_API:-http://localhost:8080}"
ROUNDS="${1:-3}"

if ! curl -sf "$API/readyz" >/dev/null 2>&1; then
  echo "!! API not reachable at $API — start the stack first with: make up" >&2
  exit 1
fi

# A spread of real DuckDB queries over varying scan sizes. SELECT * over range()
# means bytes returned ≈ bytes scanned, so the warehouse-equivalent cost is an
# honest reflection of the work done. No quote literals → trivial JSON.
QUERIES=(
  "SELECT range AS n, range * range AS square FROM range(1, 250000)"
  "SELECT i, i % 100 AS bucket FROM range(1, 750000) t(i)"
  "SELECT i, i * 2 AS d, i * i AS sq FROM range(1, 1200000) t(i)"
  "SELECT i % 7 AS bucket, count(*) AS n FROM range(1, 2000000) t(i) GROUP BY 1 ORDER BY 1"
  "SELECT i, i + 1 AS nxt FROM range(1, 1800000) t(i)"
  "SELECT range AS n FROM range(1, 500000)"
  "SELECT i, i / 3.0 AS third FROM range(1, 900000) t(i)"
  "SELECT i, i * i AS sq, i * i * i AS cube FROM range(1, 1500000) t(i)"
)

total=0
err=0
echo "==> generating $((ROUNDS * ${#QUERIES[@]})) real DuckDB queries against $API"
r=1
while [ "$r" -le "$ROUNDS" ]; do
  for q in "${QUERIES[@]}"; do
    body=$(printf '{"sql":"%s"}' "$q")
    code=$(curl -s -o /tmp/uv_traffic_resp.json -w '%{http_code}' \
      -X POST "$API/api/v1/workbench/run" \
      -H 'Content-Type: application/json' -d "$body" || echo 000)
    if [ "$code" = "200" ]; then
      total=$((total + 1))
      rows=$(python3 -c "import json;print(json.load(open('/tmp/uv_traffic_resp.json')).get('row_count','?'))" 2>/dev/null || echo "?")
      printf '  ok  rows=%-9s %s\n' "$rows" "$q"
    else
      err=$((err + 1))
      printf '  ERR http=%s %s\n' "$code" "$q"
    fi
  done
  r=$((r + 1))
done

echo ""
echo "==> ran $total queries ($err errors). Open http://localhost:5173/ — the Savings"
echo "    Dashboard now reflects them (Queries, DuckDB hit-rate, route mix, savings)."
echo "    Every query is listed at http://localhost:5173/queries."
