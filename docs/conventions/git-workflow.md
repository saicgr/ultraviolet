# Git Workflow

## Branch naming

```
<type>/<scope>/<short-slug>
```

Types: `feat` · `fix` · `docs` · `refactor` · `test` · `chore` · `perf` · `sec`.

Scope: `proxy` · `router` · `workers` · `connectors` · `sync` · `iceberg` · `ai` · `api` · `frontend` · `infra` · `repo`.

Examples:
- `feat/router/pushdown-detection`
- `fix/workers/iceberg-attach-race`
- `docs/architecture/iceberg-modes`
- `sec/api/api-key-rotation`

## Conventional commits

```
<type>(<scope>): <short summary, imperative present>

<optional body>

<optional footer: BREAKING CHANGE, Closes #N, Co-Authored-By>
```

Examples:
- `feat(router): detect pushdown candidates and log`
- `fix(sync): handle watermark boundary without dupes`
- `docs(architecture): add iceberg-modes.md covering passthrough + BYO`

## PR rules

- **Every PR has a description.** What, why, how to test, screenshots if UI.
- **Single concern per PR.** A "split file + fix bug + add test" PR rejected; split into three.
- **Tests with the PR.** New code = new test. Bug fix = regression test. Refactor = preserved coverage.
- **CHANGELOG entry** if architectural — added by `changelog-curator` skill on merge.
- **No merge to main with red CI.** Hard rule.

## Swarm worktree branches

`scripts/swarm_spawn.sh` creates `feat/swarm-<run-id>/<branch-name>` worktrees. These get squashed on merge — never merge a swarm branch as multiple commits to main; one commit per branch.

## What never goes to main

- `// TODO` without a follow-up issue
- `console.log` / `fmt.Println` (use logger)
- Commented-out code
- Mock data outside `test/fixtures/`
- Plaintext credentials anywhere

`compile-checker` + `code-organizer` + `security-auditor` agents catch most of these.

## History discipline

- **Always create new commits.** Never `--amend` after push.
- **Never `git push --force` to main.** Settings.json hook blocks this.
- **`git push --force-with-lease`** ok on personal branches.
- **Rebase before merge** to keep history linear; squash if commits don't tell a useful story.

## Tags + releases

Phase 1.5+ once CI/CD pipeline exists. Format: `v1.2.3` semver. Tag on main only.
