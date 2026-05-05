# Storage Modes — Managed vs BYOS

Per-customer choice. Surfaced during onboarding (`POST /api/v1/connections`).

## Mode A: Managed (default)

Iceberg files live in Ultraviolet's S3 bucket, namespaced by customer.

```
s3://uv-data/{customer_id}/{table_name}/
  metadata/
  data/
```

Access: Ultraviolet's IAM role only.

Pros: zero customer setup; UV controls storage cost optimization; backups + versioning easy.
Cons: customer data leaves their cloud account (compliance concern for some); transit cost between source warehouse and UV bucket.

## Mode B: BYOS (Bring Your Own Storage)

Iceberg files live in the customer's S3 / GCS bucket.

```
s3://{customer_bucket}/uv/{table_name}/
  metadata/
  data/
```

Access: cross-account IAM role with strict policy (object PutObject + GetObject + DeleteObject scoped to `uv/*` prefix; no Bucket-level perms).

Onboarding flow:
1. UI generates a Terraform / CloudFormation / gcloud snippet that creates the cross-account role.
2. Customer applies it; pastes back the role ARN.
3. UV tests assumeRole + write+read+delete on a probe object.
4. On success, the connection is enabled.

Pros: data sovereignty (data never leaves customer cloud); compliance-friendly; transit cost stays inside customer's network for same-region warehouse.
Cons: customer responsible for storage cost + lifecycle; debugging cross-account access issues is harder.

## Mode C: Customer-managed Iceberg (BYO Iceberg — see `iceberg-modes.md`)

Strictly speaking a different axis (Iceberg mode rather than storage mode), but worth flagging here: in BYO Iceberg mode, *no* UV-managed storage exists for that table — UV just points DuckDB at the customer's catalog.

## Phase 1 default

Managed (Mode A) for fastest onboarding. BYOS (Mode B) supported but requires the cross-account flow.

## GCS specifics

For BigQuery customers, BYOS-on-GCS is preferred because:
- Same-region read from BQ to GCS is free (vs. cross-region or cross-cloud).
- Customer-controlled buckets fit existing GCP IAM patterns.
- BigLake Iceberg tables can co-locate.

## Sensitive data + encryption at rest

- All buckets enforce AES-256-SSE-S3 (managed) or customer-managed CMEK (BYOS supported).
- Object-level encryption with customer-supplied keys (`SSE-C`) supported only via Phase 2 (custom code path).

## Storage cost visibility

Storage cost isn't part of the "savings" calculation, but is itemized in the analytics dashboard so the customer sees the trade. UV pulls daily storage usage from S3 inventory reports / GCS storage stats and joins to `cost_attribution` per customer.
