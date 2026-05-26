-- Phase 6 ux-7 + bu-9: dashboard versioning + shareable point-in-time snapshots.
-- ux-7 stores every dashboard mutation as a version row so the UI can show a
-- git-style history and restore prior states. bu-9 ties a dashboard view to a
-- specific Iceberg snapshot id so the shared link is reproducible via
-- DuckDB's `FOR VERSION AS OF` time-travel.

CREATE TABLE dashboard_version (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dashboard_id    UUID NOT NULL REFERENCES dashboard(id) ON DELETE CASCADE,
    version         INT NOT NULL,
    payload         JSONB NOT NULL,
    author          TEXT,
    message         TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (dashboard_id, version)
);
CREATE INDEX idx_dashboard_version_dashboard ON dashboard_version(dashboard_id, version DESC);

CREATE TABLE dashboard_snapshot (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dashboard_id        UUID NOT NULL REFERENCES dashboard(id) ON DELETE CASCADE,
    iceberg_snapshot_id TEXT NOT NULL,
    share_token         TEXT NOT NULL UNIQUE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_dashboard_snapshot_dashboard ON dashboard_snapshot(dashboard_id, created_at DESC);
