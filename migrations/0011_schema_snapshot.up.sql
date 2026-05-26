-- Phase 6 Wave 2 (de-3): schema-snapshot store for the schema-diff viewer.
-- Each row is a point-in-time capture of an introspected connector's table list
-- serialized to JSONB; the API diffs two snapshots in Go.

CREATE TABLE schema_snapshot (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id   UUID NOT NULL REFERENCES customers(id)   ON DELETE CASCADE,
    connection_id UUID NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    captured_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    payload       JSONB NOT NULL
);

CREATE UNIQUE INDEX idx_schema_snapshot_conn_captured
    ON schema_snapshot (connection_id, captured_at);
