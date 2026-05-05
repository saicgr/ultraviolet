# scripts/

Operational helpers + swarm orchestration shims. Stubs for now; filled as Phase 1 lands.

| Script | Purpose | Status |
|---|---|---|
| `swarm_spawn.sh <run-id>` | Create worktrees from `.swarm/manifest-<run-id>.json` | stub |
| `swarm_gate.sh <run-id> <branch>` | Run the agent gate before a swarm branch merges | stub |
| `swarm_merge.sh <run-id> <branch>` | Squash-merge a passing swarm branch | stub |
| `swarm_cleanup.sh <run-id>` | Delete worktrees + archive manifest | stub |
| `swarm_recover.sh <run-id>` | Resume a coordinator that crashed | stub |
| `bq_test_setup.sh` | Provision the GCP service account for `bigquery-public-data.*` tests | stub |

See `.claude/agents/swarm-coordinator.md` + `docs/process/agents-and-swarm.md`.
