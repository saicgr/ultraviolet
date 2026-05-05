---
name: iceberg-spec-reference
description: Quick reference for Apache Iceberg v2 spec — manifest schema, snapshot JSON shape, deletion-vector format, partition transforms, REST catalog endpoints. Use when implementing internal/iceberg/ or internal/sync/, when the iceberg-spec-validator agent flags a spec violation, or when a downstream reader (DuckDB, pyiceberg, Spark) fails to parse a written table.
---

# iceberg-spec-reference skill

## Table layout on object storage

```
s3://<bucket>/<prefix>/<customer>/<table>/
  metadata/
    v1.metadata.json
    v2.metadata.json
    ...
    snap-<snapshot-id>-<short-hash>.avro      (manifest list)
    <commit-uuid>-m0.avro                     (manifest)
    version-hint.text                         (latest version int) — only if not using REST catalog
  data/
    <partition>=<val>/<uuid>.parquet
  delete/
    <uuid>.parquet                            (positional or equality deletes)
```

## table metadata JSON (v2) — key fields

```json
{
  "format-version": 2,
  "table-uuid": "...",
  "location": "s3://...",
  "last-updated-ms": 1746...,
  "last-column-id": 12,
  "schemas": [{ "schema-id": 0, "type": "struct", "fields": [...] }],
  "current-schema-id": 0,
  "partition-specs": [{ "spec-id": 0, "fields": [...] }],
  "default-spec-id": 0,
  "last-partition-id": 1000,
  "sort-orders": [...],
  "default-sort-order-id": 0,
  "snapshots": [
    {
      "snapshot-id": 1234567890,
      "parent-snapshot-id": null,
      "timestamp-ms": 1746...,
      "summary": { "operation": "append", "added-data-files": "3", ... },
      "manifest-list": "s3://.../snap-1234567890-abcd.avro",
      "schema-id": 0
    }
  ],
  "current-snapshot-id": 1234567890,
  "refs": { "main": { "snapshot-id": 1234567890, "type": "branch" } },
  "snapshot-log": [{ "timestamp-ms": ..., "snapshot-id": ... }]
}
```

## Manifest list (avro) — fields

`manifest_path` (string), `manifest_length` (long), `partition_spec_id` (int), `content` (int: 0=data, 1=deletes), `sequence_number` (long), `min_sequence_number` (long), `added_snapshot_id` (long), `added_files_count`, `existing_files_count`, `deleted_files_count`, `partitions[]` (lower/upper bounds per partition field).

## Manifest entry (avro) — fields

`status` (int: 0=existing, 1=added, 2=deleted), `snapshot_id`, `sequence_number`, `file_sequence_number`, `data_file` { `content`, `file_path`, `file_format`, `partition`, `record_count`, `file_size_in_bytes`, `column_sizes`, `value_counts`, `null_value_counts`, `nan_value_counts`, `lower_bounds`, `upper_bounds`, `key_metadata`, `split_offsets`, `equality_ids`, `sort_order_id` }.

## v2 deletion vectors

- **Positional deletes:** parquet file with columns `file_path` (string), `pos` (long).
- **Equality deletes:** parquet file with the columns whose equality defines the delete; `equality_ids` lists field IDs.

## Partition transforms

`identity` · `bucket[N]` (hash mod N) · `truncate[L]` (string/binary truncate) · `year` · `month` · `day` · `hour` · `void` (drop partition).

## REST catalog (key endpoints)

- `GET /v1/{prefix}/namespaces/{ns}/tables` — list
- `GET /v1/{prefix}/namespaces/{ns}/tables/{table}` — load table (returns metadata location)
- `POST /v1/{prefix}/namespaces/{ns}/tables` — create
- `POST /v1/{prefix}/namespaces/{ns}/tables/{table}` — commit (atomic — body has `requirements[]` for optimistic concurrency)
- `DELETE /v1/{prefix}/namespaces/{ns}/tables/{table}` — drop

## Atomic-commit protocol

1. Write new manifest list (`snap-<id>-<hash>.avro`).
2. Write new metadata JSON (`v<n+1>.metadata.json`).
3. Atomically swap `version-hint.text` (or POST to REST catalog `commit` endpoint).
4. On any failure before step 3, delete files written in steps 1–2 (orphan GC).

## See also

- [Iceberg v2 spec](https://iceberg.apache.org/spec/)
- [DuckDB-Iceberg extension](https://duckdb.org/docs/extensions/iceberg)
- `docs/architecture/iceberg.md` — Ultraviolet-specific writer strategy
