-- Phase-6 Wave-1: hover detail, promote-to-synced-table, cost-regression alerts,
-- lineage watch lists.

-- de-2: workbench query → synced_table promotion. Record the source SQL so the
-- subsequent sync job can CTAS-refresh from a query, not just a base table.
ALTER TABLE synced_tables ADD COLUMN IF NOT EXISTS source_query TEXT;

-- co-6: per-user watch list on a lineage node.
CREATE TABLE lineage_watch (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    user_slug   TEXT NOT NULL,
    fqn         TEXT NOT NULL,
    notify_on   TEXT NOT NULL DEFAULT 'change'
                CHECK (notify_on IN ('change','freshness','cost','any')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (customer_id, user_slug, fqn)
);
CREATE INDEX idx_lineage_watch_user ON lineage_watch(customer_id, user_slug);
CREATE INDEX idx_lineage_watch_fqn  ON lineage_watch(customer_id, fqn);
