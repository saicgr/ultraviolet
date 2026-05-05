---
name: changelog-curator
description: Auto-appends entries to docs/changelog/CHANGELOG.md after architectural / rule / spec changes. Enforces the Decision / Why / Updated / Origin format. Invoke when the swarm-coordinator's gate detects an architectural change in a diff, or before merging a PR that adds/removes rules or shifts semantics.
---

# changelog-curator skill

Append-only curator for `docs/changelog/CHANGELOG.md`.

## When to invoke

- After any merge that touches: `CLAUDE.md` (any folder), `docs/architecture/`, `docs/process/` rules, `docs/conventions/` rules, schema migrations, cross-service API contracts, pricing config, env var additions with semantic meaning.
- When swarm-coordinator's gate step 6 detects architectural content in the diff and requires a CHANGELOG entry before merge.

## What does NOT need an entry

- Individual code commits (git log handles that).
- Trivial edits (typos, formatting).
- Bug fixes without architectural implications.
- Test fixture updates.
- Dep version bumps (unless behavior shifts).

## Entry format (strict)

```
## YYYY-MM-DD — short title
- **Decision:** one-line what (specific, active voice).
- **Why:** one-line why (durable reason — references incident / driver / constraint).
- **Updated:** list of files + sections (e.g., `docs/architecture/routing.md §pushdown`, `internal/router/decision.go`).
- **Origin:** PR # OR session date + topic OR user request quoted verbatim.
```

## Workflow

1. Detect change type from diff: architectural decision / new rule / removed rule / schema change / cross-product impact.
2. Extract the **Decision** — what was chosen (not what was considered). One sentence, active voice.
3. Extract the **Why** — reason that ages well.
4. Enumerate files updated with section refs.
5. Identify **Origin** — PR, conversation date, or quoted request.
6. Append to TOP of `docs/changelog/CHANGELOG.md` (reverse chronological).
7. Verify format compliance.

## Append-only discipline

- **Never edit historical entries** — corrections go as new entries that supersede.
- **Never reorder** — entries stay in chronological order written.
- **Never compress** — archive old entries to `CHANGELOG-<year>.md` if the file gets long.

## Quality rules

- Decision lines must be specific. "Updated docs" is not a Decision. "Moved no-fallback rule from root CLAUDE.md to `docs/process/no-fallback-data.md`" is.
- Why lines must age well. "Per user request" is too thin without the actual request quoted.
- Updated lists must be actionable — someone reading 6 months later should grep + find every file.

## Anti-patterns

- ❌ "Made some improvements"
- ❌ Editing a historical entry instead of appending a supersession
- ❌ Appending to bottom (should be top)
- ❌ Skipping CHANGELOG because "it was small" — if it changed an architectural rule, it's not small
