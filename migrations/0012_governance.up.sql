-- Phase 6 Wave 3 governance: PII auto-tagging output + query approval workflow.
-- Sibling to the older `column_tag` table (pii regex tags) — pii_tag adds the
-- fully-qualified-name shape that the workbench privacy-preview joins against.

CREATE TABLE IF NOT EXISTS pii_tag (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  customer_id uuid NOT NULL REFERENCES customers(id),
  fqn text NOT NULL,
  column_name text NOT NULL,
  kind text NOT NULL,
  confidence numeric(3,2) NOT NULL,
  detected_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(customer_id, fqn, column_name, kind)
);

CREATE TABLE IF NOT EXISTS query_approval (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  customer_id uuid NOT NULL REFERENCES customers(id),
  requester text NOT NULL,
  sql text NOT NULL,
  pii_columns text[] NOT NULL DEFAULT '{}',
  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','denied')),
  approver text,
  decided_at timestamptz,
  requested_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_query_approval_status ON query_approval(customer_id, status);
