---
name: bigquery-sample-datasets
description: Quick reference for choosing the right `bigquery-public-data.*` dataset for a given Ultraviolet integration test (size, schema, partition strategy, gotchas). Use when adding a new test/integration/*_test.go, when the warehouse-connector-tester agent picks fixtures, or when documenting why a particular dataset was chosen for a parity test.
---

# bigquery-sample-datasets skill

All datasets here are public, queryable with any GCP project that has `bigquery.jobs.create` permission. No data charges for the proxy operator (queries are billed to the *querying* project, but `bigquery-public-data` allows free use of the **first 1 TiB / month** of cross-dataset queries via the GCP free tier).

## Test scenario → dataset map

| Scenario | Dataset | Why |
|---|---|---|
| `ai_generate()` Path A (≤500 rows, text) | `bigquery-public-data.samples.shakespeare` | Small, text-heavy, no PII, deterministic content for snapshot tests |
| `ai_generate()` Path B (>500 rows, batch) | same dataset, larger LIMIT | tests batch threshold + result-rejoin |
| Type-mapping coverage (INT/FLOAT/BOOL/DATE) | `bigquery-public-data.samples.natality` | ~138M rows; NUMERIC + INT64 + FLOAT64 + DATE + BOOL all present |
| Streaming + Storage Read API | `bigquery-public-data.samples.natality` | row count exceeds Jobs API row-limit; forces Storage Read |
| CDC watermark sync (`_PARTITIONTIME`) | `bigquery-public-data.austin_bikeshare.bikeshare_trips` | partitioned, small enough to roundtrip in test |
| Iceberg attach parity (DuckDB ↔ direct BQ) | `bigquery-public-data.usa_names.usa_1910_current` | clustered, type-rich, deterministic ORDER BY for byte-equal asserts |
| Geospatial type round-trip (Phase 2) | `bigquery-public-data.geo_us_boundaries.states` | GEOGRAPHY type — currently unsupported, gates Phase 2 work |
| ARRAY / STRUCT round-trip (Phase 2) | `bigquery-public-data.github_repos.commits` | nested types |
| LARGE table benchmarking | `bigquery-public-data.wikipedia.pageviews_2024` | tens of TB; for cost/latency benchmarks only — never default |

## Gotchas

- `samples.shakespeare` has **no DATE column**. Don't pick it for date-handling tests.
- `austin_bikeshare.bikeshare_trips` does NOT have CDC enabled (so `APPENDS()` won't work). Use it specifically to test the watermark fallback path.
- `wikipedia.pageviews_2024` will burn through free-tier quota fast. Only use behind a `BQ_BENCHMARK=1` opt-in env flag.
- `usa_names.usa_1910_current` clustering means default scan order is non-deterministic. Always `ORDER BY year, name` for parity tests.
- The `bigquery-public-data` location is `US` — if your service-account project is in another region, queries that don't cross datasets work fine, but writing extracts requires a US-region staging dataset.

## Standard test header

```go
// Dataset: bigquery-public-data.samples.shakespeare
// Why: small + text-heavy, deterministic content, fits LIMIT 50 path A
// Cost: ~6 KiB scanned per run; well under free tier
// Auth: GOOGLE_APPLICATION_CREDENTIALS pointing at a service account
//       with roles/bigquery.jobUser + roles/bigquery.dataViewer on the
//       querying project (no special grant needed on bigquery-public-data).
```

## Auth setup

```bash
gcloud iam service-accounts create uv-test \
  --display-name "Ultraviolet integration test"
gcloud projects add-iam-policy-binding $PROJECT \
  --member="serviceAccount:uv-test@$PROJECT.iam.gserviceaccount.com" \
  --role="roles/bigquery.jobUser"
gcloud projects add-iam-policy-binding $PROJECT \
  --member="serviceAccount:uv-test@$PROJECT.iam.gserviceaccount.com" \
  --role="roles/bigquery.dataViewer"
gcloud iam service-accounts keys create uv-test-key.json \
  --iam-account="uv-test@$PROJECT.iam.gserviceaccount.com"
export GOOGLE_APPLICATION_CREDENTIALS="$(pwd)/uv-test-key.json"
```

Document chosen dataset + reason in the test file header. Never silently swap datasets between runs.
