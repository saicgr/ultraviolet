-- Reverse of 0007_phase6_wave1.up.sql.
DROP TABLE IF EXISTS lineage_watch;
ALTER TABLE synced_tables DROP COLUMN IF EXISTS source_query;
