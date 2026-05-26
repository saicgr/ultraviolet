-- 0016: Quarterly RBAC access-review flow (go-6).
-- access_review tracks a per-customer quarterly review session.
-- access_review_decision captures per-user / per-role keep|revoke determinations.

CREATE TABLE access_review (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    period      TEXT NOT NULL,                                   -- e.g. "2026-Q2"
    status      TEXT NOT NULL DEFAULT 'open'
                CHECK (status IN ('open','completed')),
    opened_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at   TIMESTAMPTZ,
    UNIQUE (customer_id, period)
);
CREATE INDEX idx_access_review_customer ON access_review(customer_id, opened_at DESC);

CREATE TABLE access_review_decision (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    review_id   UUID NOT NULL REFERENCES access_review(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL,
    role        TEXT NOT NULL,
    action      TEXT NOT NULL CHECK (action IN ('keep','revoke')),
    reviewer    TEXT NOT NULL,
    decided_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_access_review_decision_review ON access_review_decision(review_id, decided_at DESC);
