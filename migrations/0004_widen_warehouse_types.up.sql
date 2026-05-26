-- W4 connector matrix: relax the warehouse_type CHECK so new connector types
-- (Databricks, Fabric, Redshift, ClickHouse, MotherDuck, PG/MySQL/Mongo/Dynamo
-- sources, Trino, local_duckdb) can be persisted. Each stub satisfies the
-- Connector interface in internal/connectors/stubs.go.
ALTER TABLE connections DROP CONSTRAINT connections_warehouse_type_check;
ALTER TABLE connections ADD CONSTRAINT connections_warehouse_type_check
    CHECK (warehouse_type IN (
        'bigquery','snowflake','databricks',
        'redshift','clickhouse','motherduck','fabric',
        'pg_source','mysql','trino','mongo','dynamo','local_duckdb'
    ));
