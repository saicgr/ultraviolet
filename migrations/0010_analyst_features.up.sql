-- 0010_analyst_features
-- Trigram index on normalized_sql for fast ILIKE prefix lookups powering the
-- workbench autocomplete endpoint (an-2 / ai-u9).
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX IF NOT EXISTS idx_query_log_normalized_prefix
    ON query_log USING gin (normalized_sql gin_trgm_ops);
