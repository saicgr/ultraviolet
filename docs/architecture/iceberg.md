# Iceberg Writer Strategy

`internal/iceberg/`. Reference: skill `iceberg-spec-reference`.

## Strategy: thin wrapper over DuckDB-Iceberg extension

The DuckDB-Iceberg extension supports v2 DML (INSERT/UPDATE/DELETE) with transactional consistency since late-2025 releases ([MotherDuck Jan 2026 newsletter](https://motherduck.com/blog/duckdb-ecosystem-newsletter-january-2026/)). Use it as the default writer rather than hand-rolling Parquet + manifest Avro in Go.

Default write path:
```
sync layer pulls CDC rows
   ↓
serialize to Arrow record batches
   ↓
INSERT INTO {customer}_{table} BY NAME SELECT * FROM read_arrow_array(...)
   ↓
DuckDB-Iceberg extension writes Parquet + manifest + commits snapshot
```

Benefits: leverages well-tested DuckDB code; extension authors handle spec evolution; we don't write Avro manifest serialization.

## Escape hatch: custom Go writer

Some scenarios may need a hand-rolled writer (`internal/iceberg/writer_custom.go`):
- Partition layouts not yet in DuckDB-Iceberg (e.g., custom truncate transforms).
- Catalog modes not yet supported by the extension.
- Performance-critical paths where extension overhead is measured to be too high.

Custom writer uses `apache/arrow-go` for Parquet write + `goavro` for Avro manifest serialization.

**Default to extension. Only switch to custom when a concrete need is documented.**

## REST catalog server

`internal/iceberg/catalog.go` implements the [Iceberg REST catalog OpenAPI spec](https://iceberg.apache.org/concepts/catalog/) — exposes Ultraviolet's per-customer tables to DuckDB workers (via `ATTACH ... TYPE ICEBERG, REST 'true'`) and to external readers (Spark, Trino, pyiceberg).

Endpoints (Phase 1 minimum):
- `GET /v1/{prefix}/namespaces` — list per-customer namespaces
- `GET /v1/{prefix}/namespaces/{ns}/tables` — list tables
- `GET /v1/{prefix}/namespaces/{ns}/tables/{table}` — load table metadata
- `POST /v1/{prefix}/namespaces/{ns}/tables/{table}` — atomic commit (with `requirements[]` for OCC)

## Atomic snapshot commit

```
1. Write data files (parquet) to s3://.../data/
2. Write manifest entries to s3://.../metadata/<commit>-m0.avro
3. Write manifest list to s3://.../metadata/snap-<id>-<hash>.avro
4. Write metadata JSON to s3://.../metadata/v<n+1>.metadata.json
5. POST commit to REST catalog (or atomic swap version-hint.text)
6. On any failure before step 5: delete files written in 1–4 (orphan GC).
7. Publish Redis event customer:{id}:table:{name}:refreshed:{snapshot_id}.
```

`iceberg-spec-validator` agent verifies cross-reader compatibility by spawning a `pyiceberg` subprocess against LocalStack S3 after writes.

## Schema evolution

Iceberg field IDs preserved on rename / type-promotion. We never reuse a field ID. Schema changes propagated via `internal/sync` after detecting new columns in source warehouse.

## Deletion vectors (v2 positional)

For `DELETE` and pre-image of `UPDATE` from CDC, write a delete file with columns `(file_path, pos)` referencing the data file rows being shadowed. Snapshot-summary tracks `removed-data-files` count.

## Files

| File | Purpose |
|---|---|
| `writer.go` | DuckDB-Iceberg shim |
| `writer_custom.go` | Hand-rolled writer (escape hatch) |
| `catalog.go` | REST catalog HTTP server |
| `snapshot.go` | Snapshot/commit helpers |
| `reader.go` | Standalone reader (tests + diagnostics) |
| `metadata.go` | Iceberg metadata JSON marshaling |
