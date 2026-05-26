DROP TABLE IF EXISTS webhook_endpoint;
DROP TABLE IF EXISTS scheduled_query_run;
ALTER TABLE scheduled_query DROP COLUMN IF EXISTS next_run_at;
