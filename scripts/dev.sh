#!/usr/bin/env bash
#
# One command to run the whole Ultraviolet webapp locally:
#   embedded Postgres + seeded demo data + API (:8080) + frontend (:5173).
# No Docker, no warehouse, no GitHub. Press Ctrl+C to stop everything.
#
# Usage:  ./scripts/dev.sh    (or: make up)
set -euo pipefail
set -m   # job control: each background job becomes its own process group leader,
         # so cleanup can signal the whole tree (npm→vite, uv-demo→postgres).

# repo root regardless of where this is invoked from
cd "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

API_PORT=8080
UI_PORT=5173
PG_DIR="$HOME/.uv-demo-pg"
BACKEND_PID=""
FRONTEND_PID=""

kill_port() {
  local pids
  pids=$(lsof -ti :"$1" 2>/dev/null || true)
  if [ -n "$pids" ]; then
    # shellcheck disable=SC2086
    kill -9 $pids 2>/dev/null || true
  fi
}

cleanup() {
  echo ""
  echo "==> shutting down…"
  # Negative PID = kill the whole process group (set -m made each job a leader),
  # so vite/esbuild and the embedded Postgres go down with their parents.
  [ -n "$FRONTEND_PID" ] && kill -TERM "-$FRONTEND_PID" 2>/dev/null || true
  [ -n "$BACKEND_PID" ]  && kill -TERM "-$BACKEND_PID"  2>/dev/null || true
  # belt-and-suspenders for any stragglers from THIS demo
  pkill -f "$PG_DIR" 2>/dev/null || true
  pkill -f "bin/uv-demo" 2>/dev/null || true
  kill_port "$API_PORT"
  kill_port "$UI_PORT"
}
trap cleanup EXIT INT TERM

echo "==> cleaning previous run"
pkill -f "$PG_DIR" 2>/dev/null || true
kill_port "$API_PORT"
rm -rf "$PG_DIR"

echo "==> building backend (CGo: DuckDB + pg_query)"
CGO_ENABLED=1 go build -tags demo -o bin/uv-demo ./test/demo

echo "==> starting backend: embedded Postgres + seeded data + API on :$API_PORT"
./bin/uv-demo &
BACKEND_PID=$!

echo "==> waiting for the API to come up (first run downloads a Postgres binary)…"
ready=0
for _ in $(seq 1 120); do
  if curl -sf "http://localhost:$API_PORT/readyz" >/dev/null 2>&1; then ready=1; break; fi
  if ! kill -0 "$BACKEND_PID" 2>/dev/null; then echo "!! backend exited early — check output above"; exit 1; fi
  sleep 1
done
[ "$ready" = 1 ] || { echo "!! API did not become ready in time"; exit 1; }
echo "==> API ready on :$API_PORT"

if [ ! -d frontend/node_modules ]; then
  echo "==> installing frontend deps (first run)"
  ( cd frontend && npm install )
fi

echo "==> starting frontend on :$UI_PORT"
( cd frontend && npm run dev ) &
FRONTEND_PID=$!

cat <<EOF

===========================================================
  Ultraviolet is up  →  http://localhost:$UI_PORT
  API:                  http://localhost:$API_PORT
  (seeded demo data for all 5 Foundational features)

  Press Ctrl+C to stop everything.
===========================================================
EOF

# Block until either process dies (or Ctrl+C fires the trap). Portable to the
# bash 3.2 that ships on macOS (no `wait -n`).
while kill -0 "$BACKEND_PID" 2>/dev/null && kill -0 "$FRONTEND_PID" 2>/dev/null; do
  sleep 1
done
