-- fo-3: per-user / per-dashboard cost attribution.
-- query_log gains the two dimensions spend_breakdown groups by. Both are
-- nullable: raw BI-tool traffic arriving over the pgwire proxy carries no
-- app-user or dashboard context, so these populate only for query paths that
-- know it (dashboard tile execution, user-scoped sessions). spend_breakdown
-- groups by them honestly — rows with a NULL dimension aggregate as
-- 'unattributed', never fabricated.
ALTER TABLE query_log
    ADD COLUMN user_id      UUID REFERENCES app_user(id)  ON DELETE SET NULL,
    ADD COLUMN dashboard_id UUID REFERENCES dashboard(id) ON DELETE SET NULL;

CREATE INDEX idx_query_log_user
    ON query_log(customer_id, user_id, started_at DESC)      WHERE user_id IS NOT NULL;
CREATE INDEX idx_query_log_dashboard
    ON query_log(customer_id, dashboard_id, started_at DESC) WHERE dashboard_id IS NOT NULL;
