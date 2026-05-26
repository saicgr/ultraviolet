ALTER TABLE connections DROP CONSTRAINT connections_warehouse_type_check;
ALTER TABLE connections ADD CONSTRAINT connections_warehouse_type_check
    CHECK (warehouse_type IN ('bigquery','snowflake','databricks'));
