# `.claude/` — Agents + Skills + Commands Extensibility

Ultraviolet's Claude Code extension layer. Inheriting Anthropic's 2026 best practices: agents for delegated isolated work, skills for on-demand domain knowledge, commands for slash invocation, hooks for deterministic always-must-happen actions.

## Workflow

1. **For new isolated capability** with structured tool needs → **agent** in `agents/<name>.md` (frontmatter: name/description/model/color/allowedTools/swarmable).
2. **For domain knowledge / reusable workflow** invoked on demand → **skill** at `skills/<name>/SKILL.md` (frontmatter: name/description).
3. **For slash command** users invoke explicitly → **command** at `commands/<name>.md`.
4. **For deterministic always-must-happen** (gofmt after every edit, block writes to applied migrations) → **hook** in `settings.json` — NOT an agent (hooks are guaranteed; agents/CLAUDE.md rules are advisory).

## Agent inventory (12)

| Agent | Purpose | Swarmable |
|---|---|---|
| `swarm-coordinator` | Orchestrates parallel agent execution with git worktrees + manifest. | n/a |
| `compile-checker` | Go (`go build/vet/test -race`, `golangci-lint`), TS (`tsc --noEmit`), SQL (golang-migrate dry-run), Bash (`shellcheck`). | yes |
| `pg-protocol-validator` | PG wire v3 conformance. Required gate after `internal/protocols/pgwire/` changes. | yes |
| `warehouse-connector-tester` | Golden-suite vs `bigquery-public-data.*` + dbt/Looker/Tableau/Hex compat. Gate after `internal/connectors/`. | yes |
| `iceberg-spec-validator` | Iceberg v2 conformance. Gate after `internal/iceberg/` or `internal/sync/`. | yes |
| `cost-attribution-backfiller` | Nightly backfill of `actual_cost` from per-warehouse history APIs. | yes |
| `code-organizer` | Enforces ≤1000-line cap + Go package conventions + table-driven tests. | yes |
| `error-debugger` | Go panics + warehouse driver errors + DuckDB CGo crashes. | yes |
| `security-auditor` | Credential encryption, TLS, SQL injection, API-key leak detection, CGo memory safety. | yes |
| `database-operations-specialist` | Control-plane Postgres + warehouse schema introspection. | yes |
| `full-stack-architect` | Comprehensive architecture for cross-stack features. | yes |
| `ui-ux-reviewer` | React + Tailwind + shadcn polish. | yes |

## Skill inventory (5)

- `doc-finder/SKILL.md` — keyword → `docs/` subfolder routing
- `changelog-curator/SKILL.md` — auto-append `docs/changelog/CHANGELOG.md`
- `pg-wire-reference/SKILL.md` — PG wire v3 message reference + OID table
- `iceberg-spec-reference/SKILL.md` — Iceberg v2 spec quick-reference
- `bigquery-sample-datasets/SKILL.md` — which `bigquery-public-data.*` table to use for which test scenario

## Command inventory (1)

- `commands/frontend-design.md` — shadcn-based design system reference

## Swarm trigger rules

See `docs/process/agents-and-swarm.md`. Any 2 of {≥2 subsystems, ≥4 files, ≥30min wall-time, task-type match} → delegate to `swarm-coordinator`. Default ceiling 5 parallel agents. Hard budget 1M tokens / 90 min wall per swarm.

## When adding new capabilities

- Audit existing agents/skills first — never duplicate.
- Frontmatter `swarmable: true` for any agent the coordinator may spawn.
- Add to inventory tables above.
