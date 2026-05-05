# `migrations/` — Control-Plane Schema

Tool: `golang-migrate`. Naming: `NNNN_description.up.sql` + `NNNN_description.down.sql` (zero-padded, monotonic).

## Discipline

- **Never edit applied migrations.** Add a new migration. `code-organizer` agent rejects edits to files already present in `schema_migrations`.
- **Down migrations must actually revert.** Tested by `make migrate-roundtrip` in CI.
- **No data migrations in schema files.** Data backfills live in `cmd/sync` jobs or one-off scripts under `scripts/`.
- **One concern per migration.** Splitting helps when a migration needs to roll back.
- **Run `make sqlc` after any schema change.** Generated query funcs live in `internal/store/gen/`.

## Apply order in dev

`make migrate-up` (forward) · `make migrate-down` (one step back) · `make migrate-reset` (drop + reapply, dev only).
