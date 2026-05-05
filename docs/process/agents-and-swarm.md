# Agents + Swarm — Triggers, Decomposition, Gate

Canonical reference for `swarm-coordinator` agent. Agents listed in `.claude/CLAUDE.md`.

## When to swarm

Two of these → delegate to `swarm-coordinator`:
- ≥2 subsystems touched (e.g., proxy + sync + frontend)
- ≥4 files modified
- Estimated >30 min single-thread wall time
- Task-type match: "implement Phase X", "ship all IMPROVEMENTS", "refactor across packages", "audit + fix"

If only 1 fires: default single-thread.

## When NOT to swarm

- Typo fix
- Single-file edit
- Debugging a single stack trace (use `error-debugger` instead)
- Read-only exploration (use Explore subagent)

## Decomposition rules

The coordinator splits a task into N parallel branches. Each gets:
- A **git worktree** at `.swarm/worktrees/<run-id>/<branch-name>/`.
- A **file-ownership manifest** at `.swarm/manifest-<run-id>.json` declaring which files this branch may touch. **Two branches must NEVER claim the same file.**
- A **goal statement** (1–2 sentences) and a **scope statement** (file list + non-goals).
- A **completion criterion** (test name(s) + manual check if any).

Maximum 5 parallel agents per swarm. Hard budget 1M tokens / 90 min wall per swarm. Beyond that: split the swarm into two sequential ones.

## Manifest schema

```json
{
  "run_id": "uuid",
  "started_at": "2026-05-05T...",
  "branches": [
    {
      "name": "feat/router-pushdown",
      "agent": "full-stack-architect",
      "owns": ["internal/router/**", "internal/router/pushdown.go"],
      "blocks": [],
      "goal": "Add pushdown candidate detection to router; Phase 1 logs only.",
      "criterion": "TestRouter_DetectsPushdownCandidate passes"
    }
  ]
}
```

## Gate (run before each branch merge — all must pass)

| Step | Agent / command | Required when |
|---|---|---|
| 1 | `compile-checker` | always |
| 2 | `pg-protocol-validator` | `internal/protocols/pgwire/` touched |
| 3 | `warehouse-connector-tester` | `internal/connectors/` touched |
| 3a | `iceberg-spec-validator` | `internal/iceberg/` or `internal/sync/` touched |
| 4 | `make test-integration` | always (real BQ public-data tests) |
| 5 | `security-auditor` | auth / crypto / SQL-rewrite / external connector touched |
| 6 | `code-organizer` | always (file size + duplication) |
| 7 | `changelog-curator` | architectural change in diff |

A branch merges to main only if all required gates pass. If any fail, branch stays open; coordinator surfaces the report to the user, never auto-fixes.

## Failure recovery

- **One branch fails gate:** other branches continue. Failed branch surfaces report; the user decides retry / abandon / split further.
- **Two branches conflict on a file** (manifest violation): swarm aborts both; coordinator reports the conflict; user decomposes more carefully.
- **Coordinator itself fails** (token budget, wall time, OOM): `scripts/swarm_recover.sh <run-id>` resumes from the manifest snapshot.

## Cleanup

After all branches merge or abort:
```bash
scripts/swarm_cleanup.sh <run-id>
```
- Removes `.swarm/worktrees/<run-id>/`.
- Archives `.swarm/manifest-<run-id>.json` to `.swarm/archive/<run-id>.json`.
- Pushes coordinator log + per-branch logs to `.swarm/logs/<run-id>/`.

## Discipline

- **Never run a swarm without `swarm-coordinator`.** Don't manually spawn agents in parallel — the manifest discipline is what prevents collisions.
- **Always use the Plan tool first** for the decomposition; let user approve the manifest before spawning.
- **Branches are short-lived** — a swarm should finish within 90 min, then cleanup. Don't leave worktrees around.
