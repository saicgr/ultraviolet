# Compile Cleanly — Per-Language Verification

> Build clean, vet clean, test clean, lint clean. Before declaring done.

The `compile-checker` agent automates this. Run manually any time.

## Go

```bash
# Build (catches obvious errors)
go build ./...

# Vet (catches the import-error trap, copy locks, format string mismatches)
go vet ./...

# Test with race detector (catches concurrency bugs)
go test -race -count=1 -timeout=120s ./...

# Lint (catches style + common bugs not in vet)
golangci-lint run --timeout=5m
```

### The import-error trap

`go build` won't catch errors in tests if those packages compile but the test files have unused imports. Always run `go vet ./...` AND `go test -count=1 ./...` (the latter forces test compilation).

`go test -count=1` (not `go test`) bypasses the test cache so you re-run on every check.

## TypeScript (frontend)

```bash
cd frontend
pnpm tsc --noEmit          # type check, no output
pnpm lint                  # eslint (TBD config)
pnpm test                  # vitest
```

## SQL (migrations)

```bash
make migrate-roundtrip     # apply all up; apply all down; apply all up again
```

This catches: missing down migration, irreversible up, FK ordering issues. If no migrations changed in the diff, the target should `SKIP` explicitly (not `PASS`).

## Bash (scripts)

```bash
shellcheck scripts/*.sh
bash -n scripts/*.sh       # syntax-only parse
```

## Aggregate target

```bash
make verify     # runs all four languages; exits non-zero on any failure
```

The `Makefile` `verify` target is the single command CI runs.

## Pre-commit

`.claude/settings.json` hooks:
- `gofmt -w` on every Go file edit (deterministic — no manual step).
- `go vet` on the changed package after every edit (Phase 1.5 once measured fast enough).

## When to skip a language

If the diff genuinely doesn't touch that language's surface (no `.go` files changed → skip Go), the agent reports `SKIP <reason>`. **Never skip a language just because its check is failing on something that "looks unrelated."** Investigate.

## Common failure modes

| Symptom | Likely cause | Fix |
|---|---|---|
| `go vet`: composite literal uses unkeyed fields | struct field added recently | use field names |
| `go vet`: range loop captures variable | passing loop var to goroutine | shadow `tc := tc` (or Go 1.22+ no longer needed) |
| `go test -race`: data race | shared mutable state | mutex / channel / per-goroutine copy |
| `tsc`: no overload matches | shadcn dep update | bump types; re-run install |
| `migrate up && down && up`: orphan FK | down didn't drop the FK | fix down migration |

## The single rule

**Never declare done if `make verify` is red.**
