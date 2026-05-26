-- 0014: Index supporting activity feed queries (customer_id + created_at DESC).
-- The activity_event table already exists from 0005_user_features; this is an
-- idempotent index addition for the per-customer activity feed endpoint.
CREATE INDEX IF NOT EXISTS idx_activity_event_customer_created
    ON activity_event(customer_id, created_at DESC);
