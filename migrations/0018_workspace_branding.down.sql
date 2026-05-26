-- 0018 rollback.
--
-- We drop workspace_branding entirely. We deliberately do NOT drop the
-- `annotation.row_key` column on rollback: the column is additive and
-- nullable, downstream readers tolerate its absence in the JSON payload,
-- and dropping it would silently destroy any per-row annotations that
-- forward-rolled customers have already written. Additive-only down
-- migrations for non-destructive columns is the project convention
-- (see docs/changelog/CHANGELOG.md).

DROP TABLE IF EXISTS workspace_branding;
