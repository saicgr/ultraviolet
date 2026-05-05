# Plan Before Code

> Even small fixes have cascading effects. Write the plan first.

## What "plan" means

For tasks ≥ 10 min wall: a 5–20 line summary in chat or in a `~/.claude/plans/<slug>.md` file containing:
1. **Goal** — what's the user-visible change?
2. **Affected files** — explicit paths.
3. **Approach** — 2–5 bullets.
4. **Edge cases** — the 11-axis pass from `ultrathink.md`.
5. **Test plan** — what verifies done.

For tasks ≥ 30 min wall OR ≥ 4 files: full plan via Plan Mode → ExitPlanMode for user approval.

## When to skip the plan

- Single-line typo fix.
- One-file readme tweak.
- Trivial rename (search-and-replace, ≤10 occurrences).

Even then: **read the file first, confirm the change is what you think it is, then edit.** Never edit blind.

## When to invoke `swarm-coordinator`

Two of these → swarm:
- ≥2 subsystems (e.g., proxy + sync, or connector + frontend)
- ≥4 files modified
- >30 min single-thread estimate
- Bulk task ("ship all open IMPROVEMENTS", "implement Phase 1 step N")

The coordinator decomposes, spawns worktrees, runs the gate, merges. See `agents-and-swarm.md`.

## Plan rejection signals

If a plan reviewer (or the agent itself) catches:
- "TODO: handle later" with no follow-up issue
- "we'll fall back to X" without explicit logging (violates no-fallback rule)
- Files >1000 lines after the change
- Test plan missing real warehouse integration where one is reasonable

→ revise the plan; don't start coding.

## Why

Programmer instinct says "small change → just do it." But "small" is a lie when:
- The proxy goroutine model means a small bug = silent data corruption.
- The router is on the hot path; a single regex breaks 80% of customer queries.
- A migration mistake is unrecoverable in prod.

Plan-before-code is cheap insurance. The cost is 5 minutes; the savings is one prod incident.
