-- Phase-4 ent-5: column-level masking rules. The proxy reads matching rows at
-- query rewrite time and wraps the SELECT's masked columns in the chosen strategy.
CREATE TABLE column_mask (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id     UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    schema_name     TEXT NOT NULL,
    table_name      TEXT NOT NULL,
    column_name     TEXT NOT NULL,
    role            TEXT NOT NULL,
    strategy        TEXT NOT NULL CHECK (strategy IN ('null','hash','redact','first_4')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (customer_id, schema_name, table_name, column_name, role)
);
CREATE INDEX idx_column_mask_lookup ON column_mask(customer_id, role, schema_name, table_name);
