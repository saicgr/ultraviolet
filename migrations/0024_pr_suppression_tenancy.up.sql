-- W2 PR suppression: add tenancy + per-finding scope. The previous PK
-- (repo, pr_number) collided across customers (one tenant's /uv suppress
-- silenced another tenant's same-named repo+PR) and was all-or-nothing per PR.
-- Existing rows predate customer scoping (zero-UUID routing) and can't be
-- safely attributed, so they are cleared as part of this constraint change.
DELETE FROM pr_suppression;
ALTER TABLE pr_suppression
    ADD COLUMN customer_id UUID   NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    ADD COLUMN repo_id     BIGINT,
    ADD COLUMN finding_fp  TEXT   NOT NULL DEFAULT '';  -- '' = suppress all findings on the PR
ALTER TABLE pr_suppression DROP CONSTRAINT pr_suppression_pkey;
ALTER TABLE pr_suppression ADD PRIMARY KEY (customer_id, repo, pr_number, finding_fp);
