-- Keep pg_trgm — other code may depend on it.
DROP INDEX IF EXISTS idx_query_log_normalized_prefix;
