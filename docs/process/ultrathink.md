# Ultrathink — Mandatory Edge-Case Checklist

> Before writing any non-trivial plan or code, run this checklist. The 11 axes catch the failure modes Claude (and humans) most often miss.

## The 11 axes

1. **Empty / null / zero.** What happens with `nil`, `[]`, `""`, `0`, `0001-01-01`? Each branch tested.
2. **Boundary.** Off-by-one on lengths, indices, time ranges, partition cutoffs.
3. **Concurrency.** Two goroutines hit the same map/slice; cancellation arrives mid-write; pubsub event arrives before subscription.
4. **Failure.** Network blip, warehouse 5xx, DuckDB OOM, disk-full on Iceberg write, S3 eventual consistency. What does the *user* see?
5. **Auth boundary.** Customer A's API key on customer B's connection? Per-customer namespacing enforced at every layer (router, workers, sync, cost)?
6. **Time.** UTC vs local; DST; clock skew between proxy and warehouse; query that runs at midnight UTC and crosses partitions.
7. **Encoding.** UTF-8 vs Latin-1 in column names; emoji in `ai_generate` prompts; binary data in JSON.
8. **Scale.** What at 1k customers? 10k? Per-customer pool cap; total goroutine count; Postgres connection limit.
9. **Backwards-compat.** Will existing customer queries still work after this change? Is there a migration path?
10. **Cost / quota.** LLM provider rate limit hits mid-batch; warehouse credit budget exhausted; S3 PUT rate limit.
11. **Observability.** Will the operator know when this fails? Metric, log, alert?

## Application

For every plan:
- Walk the 11 axes explicitly. For each, write either:
  - "Handled by: <specific code path or test>"
  - "Not applicable because: <reason>"
  - "Open question: <what>"
- Flag every "open question" before starting code.

For every PR review (especially via `swarm-coordinator`):
- The plan diff (if present) must show the 11-axis pass.
- The code diff must implement the handlers identified.
- The test diff must cover the handlers.

## Why mandatory

The largest production incidents trace to "I didn't think about X." Writing it down forces the thinking before the code.

This rule is taken from the Reppora project's `docs/process/ultrathink.md` (FitWiz lineage — 5 months of codified rules). Adapted for Ultraviolet's data-plane realities (concurrency, scale, cost) over Reppora's mobile-app realities (offline, build flavors, codegen).
