# Deployment Models — SaaS / BYOC / Self-Hosted

Customer-facing deployment choice. Affects data flow, billing, security model.

## Mode 1: SaaS (Phase 1 default)

UV runs in Ultraviolet's cloud account (us-east-1). Customer points BI tool at `proxy.ultraviolet.dev:5432` with their API key. Storage in UV-managed S3 (or BYOS — `storage-modes.md`).

```
Customer BI tool ─PG-wire/TLS─▶ UV proxy (UV cloud)
                                    │
                                    ├─▶ DuckDB pool (UV cloud)
                                    │      │
                                    │      └─▶ S3 (UV-managed) ─Iceberg─┐
                                    │                                    │
                                    └─▶ Warehouse (customer's account) ──┘
                                                                         │
                                Sync worker ◀─CDC─────────────────────────┘
                                    │
                                    └─▶ S3
```

Pros: fastest onboarding (just an API key); UV controls reliability.
Cons: customer data passes through UV cloud (compliance + DPA needed).

## Mode 2: BYOC (Phase 2)

UV proxy + DuckDB workers + sync run in the customer's VPC (their AWS / GCP account). Control plane (UI, billing, account mgmt) stays in UV cloud.

```
Customer BI tool ─PG-wire─▶ UV proxy (CUSTOMER VPC) ─▶ DuckDB pool (customer VPC)
                                                          │
                                                          └─▶ S3/GCS (customer account)
Customer admin UI ─HTTPS─▶ UV control plane (UV cloud) ──reports/usage──▶ same proxy
```

Onboarding: UV provides a Terraform module / Helm chart that the customer applies in their account. UV control plane writes config + reads usage telemetry; never touches data.

Pros: data sovereignty; FedRAMP / SOC 2 / HIPAA story improves.
Cons: deployment + upgrade ops shared with customer.

Pricing: per-VM hourly rate, billed via UV invoice.

## Mode 3: Self-hosted (Phase 3)

UV ships a full air-gapped distribution. No outbound calls to UV control plane. Customer runs their own admin UI. License-key gated.

Onboarding: download Helm chart + signed image bundle; apply in customer cluster.

Pros: total isolation (regulated industries, gov).
Cons: hardest to support; UV gets no telemetry; upgrade discipline on customer.

Pricing: annual subscription, larger ACV.

## Mode comparison

| Aspect | SaaS | BYOC | Self-hosted |
|---|---|---|---|
| Time to value | minutes | hours | days |
| Data leaves customer | Yes (proxy traffic) | No | No |
| UV reads telemetry | Yes | Yes (anonymized) | No |
| UV pushes upgrades | Yes | Pull (Helm/Terraform) | Customer-driven |
| Compliance fit | SOC 2 | SOC 2 + HIPAA + DPA | FedRAMP / IL5 |
| Pricing | per warehouse | per VM-hour | annual subscription |
| Phase | 1 (live) | 2 (Q3 2026 target) | 3 (TBD) |

## Architecture invariant

The proxy + sync + connector code is identical across all three modes. Only the deployment harness (Helm/Terraform/Docker Compose) differs. This keeps the codebase honest and avoids per-mode bug surface.

## Control-plane separability

Even in self-hosted, the control-plane Postgres schema is identical to SaaS. This means UV can offer "managed control plane + self-hosted data plane" as a hybrid — most regulated customers actually want this (data stays in their VPC, but they don't want to operate the admin UI themselves).
