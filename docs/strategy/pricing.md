# Pricing — Cost Model + Tiers (TBD)

> Marketing claim: 70–90% warehouse cost reduction. This doc backs the math.

## Customer cost stack (before Ultraviolet)

For a typical mid-market BI workload on Snowflake:
- **80–95% of queries** are repeat reads on slow-changing dimension/fact tables (BI dashboards, dbt incremental models, ad-hoc analysts re-running the same SELECT).
- These read queries hit the warehouse credit meter every time.
- A `Medium` Snowflake warehouse running 10h/day = ~10 credits/day × $3/credit ≈ **$900/month** per warehouse, before storage / cloud-services overhead.

## Cost stack with Ultraviolet (managed mode)

- **Reads routed to DuckDB** (≥80% target): warehouse credit consumption drops proportionally.
- **DuckDB compute**: a single 8-vCPU worker runs ~3 customers, so amortized infra cost ~$0.05/hr/customer.
- **Sync layer**: incremental CDC poll keeps Snowflake credits low (small SELECTs every 60s).
- **Iceberg storage on S3**: ~$0.023/GiB-month; for a 100 GB customer table = $2.30/month.

Net: warehouse spend cut 70–90%; Ultraviolet infra cost <5% of original spend; net savings to customer ~70–85%.

## Pricing tiers (placeholder)

| Tier | Monthly | Includes |
|---|---|---|
| Starter | $200 | 1 warehouse, 50 GB synced, 1M routed queries |
| Growth | $1,500 | 3 warehouses, 1 TB synced, 50M routed queries |
| Scale | custom | unlimited; BYOC option; SOC 2 evidence |

Above is illustrative — finalize after first 10 paying customers reveal price elasticity. Compare against `competitive-landscape.md`.

## Take rate sanity check

Greybeam (Snowflake-only) lists "pay per hour of compute." Keebo charges a percentage of savings. Espresso bundles into broader observability spend. Ultraviolet's wedge is **multi-warehouse + warehouse-agnostic AI** — pricing should reward landing the second warehouse (per-warehouse fee) rather than per-query metering (which becomes adversarial when the customer's BI traffic spikes for legitimate reasons).
