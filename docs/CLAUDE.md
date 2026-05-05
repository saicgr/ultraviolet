# `docs/` — Documentation Discipline

## Read-first / write-first

1. **Before any non-trivial task,** read `INDEX.md` to find relevant topical docs.
2. **Before any architectural change,** read the canonical doc(s) listed in conflict precedence below.
3. **After any architectural change,** invoke the `changelog-curator` skill to append `changelog/CHANGELOG.md`. Open observations go to `changelog/IMPROVEMENTS.md`.

## Conflict precedence (when docs disagree)

`reference/product-brief.md` (canonical for product scope) > `architecture/*` (canonical for system design) > `process/*` (canonical for workflow) > `conventions/*` (canonical for style) > root `/CLAUDE.md` (canonical for entry-point routing).

## Topic map

| Folder | Use for |
|---|---|
| `strategy/` | Pricing, competitors, market positioning |
| `architecture/` | How the system is built (frequent reads) |
| `process/` | How we work — ultrathink, plan-before-code, testing, swarm |
| `conventions/` | Naming, style, error mapping, logging format |
| `reference/` | Product brief, env vars, requirements |
| `changelog/` | CHANGELOG (decisions) + IMPROVEMENTS (open observations) |

When adding a doc, update `INDEX.md` and the `doc-finder` skill's routing table.
