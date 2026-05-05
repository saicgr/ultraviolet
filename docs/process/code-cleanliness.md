# Code Cleanliness — Search Before Write

## The duplication taxonomy

Before writing a new function, search:
- `rg -t go 'func.*<name>'` for similar names.
- `rg -t go '<keyword from intent>'` for similar concepts.

If you find a match: **use or extend it**. Don't duplicate.

| Type | Example | Where to put the canonical |
|---|---|---|
| Same util in two packages | `parseTableName` in router + sync | `internal/sql/parse.go` |
| Same SQL in two handlers | `INSERT INTO query_log` | sqlc query in `internal/store/queries/` |
| Same struct redefined | `auth.Principal` in middleware + handler | `internal/auth/principal.go` |
| Same constant magic number | `30 * time.Second` for DuckDB timeout in 3 files | `internal/workers/config.go` |
| Same error string | `"connection failed"` repeated | const in `internal/connectors/errors.go` |
| Same regex | API key shape `^uv_[A-Za-z0-9]{32}$` | const in `internal/auth/apikey.go` |

## The 6 process rules

1. **Search before write.** `rg`, `Glob`, IDE go-to-definition.
2. **Plan before refactor.** Even DRY refactors have cascading effects.
3. **One concern per file.** If you can't name the file in <5 words, split it.
4. **Tests as documentation.** A test name should describe what's being tested, not the function name.
5. **Comments only for WHY, never for WHAT.** Well-named identifiers carry the WHAT.
6. **Delete unused code.** Never leave commented-out code. Git history has it.

## Cross-boundary DRY

Some duplication is acceptable — even desirable — across boundaries:
- **Wire format vs internal struct.** PG-wire DataRow vs internal `Row` — keep separate; they evolve differently.
- **API request body vs DB row.** REST API uses JSON shape; sqlc uses Go struct. Don't merge.
- **Test fixtures.** Fixture data per test file is fine; share only via explicit `test/fixtures/` if reused 3+ times.

## Security hygiene (always-on)

- **Never store credentials in plaintext.** AES-256-GCM at rest (`internal/store/crypto.go`).
- **Never log credentials.** Even at debug level.
- **Never log raw SQL with literals in prod.** Only normalized SQL.
- **Never include stack traces in user-visible errors** (`UV_DEBUG=false` in prod).
- **Never trust input from clients** for routing decisions — re-derive customer ID from API key, never from a query parameter.

## When to break the rules

DRY taken to absurdity becomes its own anti-pattern (the wrong abstraction is more harmful than duplication). Three similar lines is fine; three identical 50-line blocks is not. Use judgment; the `code-organizer` agent has seen the codebase and can advise.
