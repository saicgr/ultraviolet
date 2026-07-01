DROP INDEX IF EXISTS idx_lineage_downstream_col;
DROP INDEX IF EXISTS idx_lineage_upstream_col;
ALTER TABLE lineage_edge DROP CONSTRAINT IF EXISTS lineage_edge_unique;
-- Restore the original 4-column uniqueness (auto-named, matching pre-0020).
ALTER TABLE lineage_edge
    ADD UNIQUE (customer_id, upstream_fqn, downstream_fqn, edge_type);
ALTER TABLE lineage_edge DROP COLUMN IF EXISTS confidence;
ALTER TABLE lineage_edge DROP COLUMN IF EXISTS origin;
