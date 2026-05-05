# `internal/iceberg/` — Iceberg Reader / Writer

Canonical: `docs/architecture/iceberg.md`. Required gate: `iceberg-spec-validator` agent.

## Strategy

- **Default:** thin wrapper over the DuckDB-Iceberg extension (full v2 DML support, transactional, well-tested).
- **Escape hatch:** custom Go writer for partition layouts / catalog modes the extension doesn't yet support.

## Invariants

- **Iceberg v2 spec conformance** — manifest format, snapshot JSON shape, deletion-vector format. See skill `iceberg-spec-reference`.
- **Atomic snapshot commit** — write all manifests + commit pointer file last; on failure, garbage-collect orphan manifests.
- **REST catalog server** (`iceberg_catalog.go`) — implements Iceberg REST spec for DuckDB to discover tables.
- **No deletion-vector silent fallback** — if v2 deletes aren't supported by the target reader, error explicitly.

## Files

`writer.go` (DuckDB-Iceberg shim) · `writer_custom.go` (escape hatch) · `catalog.go` (REST server) · `snapshot.go` · `reader.go`.
