-- Cost-backfill hot path: SELECT … FROM query_log WHERE connection_id = $1 AND
-- actual_cost IS NULL ORDER BY started_at DESC. The partial index lets the planner
-- skip already-attributed rows and avoids the seq scan flagged in AUDIT.md §migrations.
CREATE INDEX IF NOT EXISTS idx_query_log_conn_unattributed
    ON query_log (connection_id, started_at DESC)
    WHERE actual_cost_usd IS NULL;

-- Routing-decision dashboard hot path:
-- GROUP BY route_decision WHERE customer_id = $1 AND started_at > now() - '24h'.
CREATE INDEX IF NOT EXISTS idx_query_log_customer_started
    ON query_log (customer_id, started_at DESC);
