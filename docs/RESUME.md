# Resume Notes

Last touched: 2026-05-05.

## Current state

- Phase 0 (scaffold) complete: per-folder CLAUDE.md, `.claude/` agents/skills/commands, `docs/` topical tree, Makefile / docker-compose stubs.
- Phase 1 (MVP) not started. Build order in `reference/phase-1-build-order.md`.

## Next session

Default first task: Phase 1 step 1 — Postgres wire protocol server. Read in order:
1. Root `CLAUDE.md`
2. `docs/INDEX.md`
3. `docs/reference/phase-1-build-order.md`
4. `docs/architecture/pg-wire-protocol.md`
5. `internal/protocols/pgwire/CLAUDE.md`
6. Skill `pg-wire-reference`

Then propose a plan respecting:
- No fallback data
- ≤1000-line file ceiling
- Plan before code
- Test against `bigquery-public-data.*` (not yet relevant for step 1; will matter from step 4)

## Open IMPROVEMENTS

See `changelog/IMPROVEMENTS.md`.
