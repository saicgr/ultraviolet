-- W1 column-level lineage: distinguish runtime-observed vs source-code-derived
-- edges, and record binding confidence so an ambiguous or table-only-fallback
-- edge is never presented as exact.
ALTER TABLE lineage_edge
    ADD COLUMN origin     TEXT NOT NULL DEFAULT 'runtime'
        CHECK (origin IN ('runtime','source_code')),
    ADD COLUMN confidence TEXT NOT NULL DEFAULT 'exact'
        CHECK (confidence IN ('exact','ambiguous','table_only'));

-- Replace the 4-column unique with one that also keys on origin, so a
-- runtime-observed edge and a source-code-derived edge for the same
-- (upstream, downstream, type) coexist as independent evidence. The original
-- constraint name is auto-generated (and truncated to 63 chars), so drop it by
-- lookup rather than hard-coding the name.
DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT conname FROM pg_constraint
            WHERE conrelid = 'lineage_edge'::regclass AND contype = 'u' LOOP
    EXECUTE 'ALTER TABLE lineage_edge DROP CONSTRAINT ' || quote_ident(r.conname);
  END LOOP;
END $$;
ALTER TABLE lineage_edge
    ADD CONSTRAINT lineage_edge_unique
        UNIQUE (customer_id, upstream_fqn, downstream_fqn, edge_type, origin);

-- Column-granularity edges drive the recursive graph CTE; partial indexes keep
-- them selective (table edges already covered by idx_lineage_upstream/downstream).
CREATE INDEX idx_lineage_upstream_col   ON lineage_edge(customer_id, upstream_fqn)   WHERE edge_type = 'column';
CREATE INDEX idx_lineage_downstream_col ON lineage_edge(customer_id, downstream_fqn) WHERE edge_type = 'column';
