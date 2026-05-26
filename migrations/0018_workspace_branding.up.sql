-- 0018: row-level annotations (an-10) + per-workspace branding (bu-11).
--
-- an-10: annotations need to point at a specific row within a result set.
-- We extend the existing `annotation` table additively with a nullable
-- `row_key` column. Existing rows keep target_type ∈ {dashboard,tile,...}
-- semantics. New writes via /api/v1/annotations may set target_kind to
-- 'query_result' / 'dashboard_tile' / 'lineage_node' and populate row_key
-- with an opaque tuple identifier (typically a hash of the PK columns).
--
-- bu-11: per-workspace custom branding (logo, primary color, name override).
-- One row per workspace. Falls back to env-driven defaults in internal/branding
-- when no row exists.

ALTER TABLE annotation ADD COLUMN IF NOT EXISTS row_key text;

-- Relax the CHECK constraint on target_type so the new row-level annotation
-- target kinds ('query_result', 'dashboard_tile') are accepted alongside the
-- existing values. 'lineage_node' is already in the original set.
ALTER TABLE annotation DROP CONSTRAINT IF EXISTS annotation_target_type_check;
ALTER TABLE annotation ADD CONSTRAINT annotation_target_type_check
    CHECK (target_type IN ('dashboard','tile','lineage_node','query','table','metric','query_result','dashboard_tile'));

CREATE TABLE IF NOT EXISTS workspace_branding (
    workspace_id  UUID PRIMARY KEY REFERENCES workspace(id) ON DELETE CASCADE,
    name          TEXT,
    tagline       TEXT,
    logo_url      TEXT,
    primary_hex   TEXT,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
