# Claude Code Best Practices — 2026 Synthesis

Sources:
- Anthropic 2026 best-practices doc
- dev.to community: 30-line root CLAUDE.md rule
- Reppora's `docs/process/claude-code-best-practices.md` (FitWiz lineage)
- Claude Code 2026 release notes (agents/skills/hooks GA)

## Top-level principles

1. **Slim root CLAUDE.md.** Aim ≤50 lines. Long CLAUDE.md files cause Claude to ignore instructions. Move depth to per-folder CLAUDE.md + topical docs/.
2. **Per-folder CLAUDE.md auto-loads** when working in that folder. Use this aggressively — every meaningful subdir gets one.
3. **Conflict precedence** stated in root. When two docs disagree, the root tells which wins.
4. **Critical-every-turn invariants** in root only. Everything else can defer to topical docs.
5. **Agents for delegated isolated work; skills for on-demand domain knowledge; commands for slash invocation; hooks for deterministic always-must-happen.** Don't conflate.
6. **Plan before code.** Cheap insurance.
7. **Verify cleanly** before declaring done. `compile-cleanly.md`.
8. **Real integrations beat mocks.** Test against actual external systems.
9. **No silent fallback.** Fail loud + log.

## Layout pattern (Ultraviolet's adoption)

```
/CLAUDE.md                     ← ~40 lines, entry-point routing
/<folder>/CLAUDE.md            ← ~30 lines, folder-specific rules
/.claude/
  agents/<name>.md             ← isolated capabilities
  skills/<name>/SKILL.md       ← domain knowledge
  commands/<name>.md           ← slash invocations
  settings.json                ← hooks (deterministic)
/docs/
  CLAUDE.md                    ← doc discipline
  INDEX.md                     ← topic-organized listing
  <topic>/<doc>.md             ← deep content
```

## Skill vs. Agent decision

| If you need... | Use... |
|---|---|
| Reusable specialist sub-agent (DB ops, security audit, full-stack design) | **Agent** in `.claude/agents/` |
| Reusable knowledge/workflow Claude triggers via `/command` | **Command** in `.claude/commands/` |
| Codified knowledge Claude pulls in mid-reasoning | **Skill** in `.claude/skills/` |
| Always-must-happen action (lint after edit, block force-push) | **Hook** in `.claude/settings.json` |

Hooks are guaranteed by the harness; agents/skills/CLAUDE.md rules are advisory.

## Frontmatter shape

### Agent
```yaml
---
name: short-name
description: |
  When to invoke + 3–4 example contexts. The description is what Claude reads to decide whether to launch this agent.
model: opus | sonnet | haiku-4-5
color: red | blue | green | ...
allowedTools: [Bash, Read, Write, Edit, Glob, Grep, ...]
useExtendedThinking: true | false
swarmable: true | false
---
```

### Skill
```yaml
---
name: short-name
description: One-line — used at retrieval time.
---
```

### Command
Markdown only; no frontmatter required.

## Agent description quality

The description is the only thing Claude reads to decide whether to invoke. It should:
- Start with verb ("Verifies...", "Refactors...", "Diagnoses...").
- State the precondition (what files / context).
- Include 2–4 concrete invocation examples.
- State what's returned.

Bad: "Helps with database stuff."
Good: "Performs operations on the control-plane Postgres (migrations, indexes, FKs) and warehouse schemas (Snowflake/BQ INFORMATION_SCHEMA introspection). Use when adding tables, fixing slow queries on `query_log`, validating FK relationships, or generating seed data via `bigquery-public-data.*`."

## Memory-system discipline

Persistent memory at `~/.claude/projects/<repo>/memory/` for facts that should survive sessions:
- User preferences (file size limits, no fallback rule)
- Project context (what Ultraviolet is, what stage)
- References (where docs live)
- Feedback (corrections + confirmations)

**Not** for: ephemeral task state (use plans + tasks); code patterns derivable from the repo; git history.

## Anti-patterns observed in the wild

- **Massive root CLAUDE.md** with all rules → Claude ignores them.
- **Vague agent descriptions** ("a helpful agent") → never gets invoked.
- **Agents that overlap heavily** with each other → coordinator can't pick.
- **Skill content in CLAUDE.md** instead of `skills/` → bloats every conversation.
- **Plan-mode used for trivial tasks** → annoying friction.
- **Plans buried in chat** instead of in `~/.claude/plans/<slug>.md` → can't be re-read across sessions.

## Source citations

- [Anthropic Claude Code best practices 2026](https://code.claude.com/docs/en/best-practices)
- [dev.to — You don't need a CLAUDE.md](https://dev.to/byme8/you-dont-need-a-claudemd-jgf) (the 30-line rule)
- [Claude Code 2026 release notes](https://docs.claude.com/release-notes)
