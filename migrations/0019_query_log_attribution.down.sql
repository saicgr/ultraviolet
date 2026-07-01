DROP INDEX IF EXISTS idx_query_log_dashboard;
DROP INDEX IF EXISTS idx_query_log_user;
ALTER TABLE query_log DROP COLUMN IF EXISTS dashboard_id;
ALTER TABLE query_log DROP COLUMN IF EXISTS user_id;
