ALTER TABLE pr_suppression DROP CONSTRAINT pr_suppression_pkey;
ALTER TABLE pr_suppression DROP COLUMN IF EXISTS finding_fp;
ALTER TABLE pr_suppression DROP COLUMN IF EXISTS repo_id;
ALTER TABLE pr_suppression DROP COLUMN IF EXISTS customer_id;
ALTER TABLE pr_suppression ADD PRIMARY KEY (repo, pr_number);
