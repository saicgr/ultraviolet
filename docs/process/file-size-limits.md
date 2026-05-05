# File Size Limits

## Hard caps

| Limit | Value | Rationale |
|---|---|---|
| Max lines per file | 1000 | hard cap; refuse to push past |
| Target for new files | <500 | room to grow without immediate split |
| Max lines per Go function | 100 | most functions <40 |
| Max lines per package directory | 5000 | otherwise split into sub-packages |

Enforced by `code-organizer` agent. CI lint check (Phase 1.5) via `revive` rule `function-length`.

## Why

Large files cause:
- Claude (and humans) lose track of invariants → inconsistent edits.
- Test coverage fragments — hard to tell what test covers what code.
- Merge conflicts increase quadratically with file size.
- Fast scans (rg, IDE outline) miss content past ~screen-3.

## Splitting strategy

When a file approaches 500 lines, look for:
1. **Distinct concerns.** A file mixing parsing + validation + execution → split per concern.
2. **Distinct types.** Each type with >5 methods → its own file (`pool.go`, `worker.go`, not `workers.go`).
3. **Test/non-test bleed.** Move helpers shared across multiple test files into `*_test_helpers.go`.

Naming convention: keep package; split on dimension. `pool.go` + `pool_eviction.go` + `pool_metrics.go` is fine; `pool1.go` + `pool2.go` is not.

## When to break a package, not just a file

If the package itself is >2000 lines or >10 files of mixed concerns, create a sub-package. E.g., `internal/connectors/` getting big → `internal/connectors/snowflake/` + `internal/connectors/bigquery/`.

## Refactor discipline

- **Plan first** (per `plan-before-code.md`).
- **One refactor at a time.** Don't bundle "split file" with "fix bug X."
- **Tests pass at every commit.** Don't leave the codebase in a broken intermediate state.
- **Imports + uses get audited** post-split — `go build ./...` and `go vet ./...` clean.

## Anti-patterns

- ❌ "I'll split it once it hits 1100" — hard cap is hard. Split BEFORE hitting it.
- ❌ Splitting on alphabet boundaries (`a-h.go`, `i-p.go`) — meaningless to readers.
- ❌ Splitting and leaving cyclic imports between halves — that's not a split, it's a tangle.
