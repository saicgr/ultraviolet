-- W2 webhook idempotency: GitHub delivers at-least-once and retries on a slow
-- or crashed handler. Recording the delivery id lets us no-op a replay instead
-- of re-running impact analysis and double-posting comments/check-runs.
CREATE TABLE github_webhook_delivery (
    delivery_id  TEXT        PRIMARY KEY,   -- X-GitHub-Delivery
    event        TEXT        NOT NULL,
    received_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Reap old delivery markers (kept ~30d) so the table doesn't grow unbounded.
CREATE INDEX idx_github_webhook_delivery_received ON github_webhook_delivery(received_at);
