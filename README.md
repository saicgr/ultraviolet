# ultraviolet

You are building a production-grade multi-warehouse query proxy called "Prism" (working name). 

Prism sits in front of data warehouses (Snowflake, BigQuery, Databricks) and acts as a transparent Postgres-wire-protocol proxy. It intercepts SQL queries, routes cheap/cacheable queries to managed DuckDB workers instead of the expensive warehouse, and provides warehouse-agnostic AI SQL functions like ai_generate(). The product saves users 70-90% on warehouse costs with zero code changes required in their BI tools or SQL clients.

---

## HIGH-LEVEL ARCHITECTURE
BI Tool / SQL Client (Looker, Tableau, dbt, psql)
│
│  Postgres wire protocol (port 5432)
▼
┌─────────────────────────────────────────────────────┐
│                  PROXY LAYER (Go)                    │
│  - Listens on :5432                                  │
│  - Postgres wire protocol parser                     │
│  - SQL parser + query classifier                     │
│  - Routes to DuckDB worker OR warehouse              │
│  - Rewrites AI SQL functions before forwarding       │
│  - Per-customer connection pool management           │
└──────────┬──────────────────────────┬────────────────┘
│                          │
[Cache hit /                [Cache miss /
read-only query]            write query /
│                    warehouse-only]
▼                          ▼
┌──────────────────┐     ┌─────────────────────────────┐
│  DUCKDB WORKER   │     │    WAREHOUSE CONNECTORS      │
│  POOL (Go)       │     │                              │
│                  │     │  - Snowflake (gosnowflake)   │
│  - DuckDB 1.x    │     │  - BigQuery (cloud.google)   │
│  - Iceberg ext.  │     │  - Databricks (JDBC)         │
│  - Reads from    │     │                              │
│    customer S3/  │     │  Fallback: forward raw SQL   │
│    GCS Iceberg   │     │  to warehouse unmodified     │
│  - LLM ext. for  │     └─────────────────────────────┘
│    ai_generate() │
└──────────┬───────┘
│
▼
┌─────────────────────────────────────────────────────┐
│                  SYNC LAYER (Go)                     │
│                                                      │
│  Per-warehouse CDC workers:                          │
│  - Snowflake: poll STREAM objects → Iceberg writes  │
│  - BigQuery: BigQuery CDC / INFORMATION_SCHEMA      │
│  - Databricks: Delta log tailing → Iceberg          │
│                                                      │
│  Writes: Apache Iceberg files to S3 or GCS          │
│  Catalog: REST catalog (Iceberg REST spec)          │
└─────────────────────────────────────────────────────┘
│
▼
┌─────────────────────────────────────────────────────┐
│               STORAGE LAYER                          │
│                                                      │
│  Mode A (managed):  Your S3 bucket                  │
│    s3://prism-data/{customer_id}/{table_name}/       │
│                                                      │
│  Mode B (BYOS):     Customer's own S3/GCS bucket    │
│    Accessed via IAM cross-account role               │
└─────────────────────────────────────────────────────┘
│
▼
┌─────────────────────────────────────────────────────┐
│               CONTROL PLANE (Go + React)             │
│                                                      │
│  - REST API: /api/v1/...                            │
│  - Customer onboarding (warehouse credentials)       │
│  - Table sync configuration UI                       │
│  - Query analytics dashboard                         │
│  - Savings calculator (estimated vs actual cost)     │
│  - Postgres connection string generator              │
└─────────────────────────────────────────────────────┘

---

## PHASE 1 — BUILD THIS FIRST (MVP)

Focus only on Snowflake + BigQuery for now. Databricks is Phase 2.

### 1. Postgres Wire Protocol Proxy

Build a Postgres wire protocol server in Go. This is the entry point for all client connections.

Requirements:
- Listen on configurable port (default 5432)
- Implement the Postgres frontend/backend protocol (v3)
- Handle: Startup message, Authentication (md5 or cleartext), Query (simple), Parse/Bind/Execute (extended), Describe, Sync, Terminate
- Extract the customer credentials from the connection: the "database" field in the startup message = customer_id + warehouse_type encoded as "{customer_id}_{warehouse}" e.g. "acme_snowflake" or "acme_bigquery"
- Parse the "user" field as the customer's API key for authentication
- Maintain session state per connection (current schema, open transactions, pending parameters)
- Handle PostgreSQL error codes and map warehouse-specific errors to their PG equivalents
- Support TLS (generate self-signed cert for dev, support customer-provided certs for prod)

The connection string a customer uses looks like:
postgresql://API_KEY@proxy.prism.dev:5432/acme_snowflake

File structure:
/proxy
server.go          # TCP listener, connection acceptor
protocol.go        # Postgres wire protocol parser/serializer
session.go         # Per-connection session state
router.go          # Query routing logic (DuckDB vs warehouse)
rewriter.go        # SQL rewriting (ai_generate, dialect normalization)
auth.go            # API key validation against control plane

### 2. SQL Router

When a query arrives, the router decides: DuckDB worker or warehouse?

Routing rules (implement in order):
1. If query is a DDL statement (CREATE, DROP, ALTER, TRUNCATE) → always forward to warehouse
2. If query is a write (INSERT, UPDATE, DELETE, MERGE) → always forward to warehouse
3. If query contains `ai_generate(` → rewrite (see AI section) then route to DuckDB worker
4. If query references a table not yet synced to Iceberg → forward to warehouse
5. If synced table's Iceberg snapshot is stale (>N minutes, configurable per table) → forward to warehouse
6. Otherwise → route to DuckDB worker

The router must parse SQL to extract referenced table names. Use the `pganalyze/pg_query_go` library for SQL parsing (it uses the actual PostgreSQL parser via CGo).

Stale check: maintain an in-memory map of `{customer_id}:{table_name} → last_synced_at`. This map is updated by the sync layer via Redis pub/sub.
/router
classifier.go      # DDL/DML/read detection
table_extractor.go # Extract referenced tables from SQL AST
freshness.go       # Check Iceberg snapshot freshness
decision.go        # Final routing decision + logging

### 3. DuckDB Worker Pool

A pool of DuckDB worker processes. Each worker is a long-running process with DuckDB loaded and customer Iceberg tables pre-attached.

Implementation:
- Use `marcboeker/go-duckdb` (CGo bindings) to embed DuckDB in Go
- Each worker is a goroutine with its own DuckDB in-process database
- On worker startup for a customer: run ATTACH for each of the customer's synced tables from their Iceberg catalog
- Use a worker pool with configurable concurrency per customer (default: 3 workers per customer)
- Workers are pre-warmed: spin up when a customer connects, not on first query
- Worker reuse: a worker can serve multiple sequential queries for the same customer
- Iceberg attach syntax:
```sql
  -- For S3 (managed mode):
  ATTACH 's3://prism-data/{customer_id}/{table_name}/' 
    AS {table_name} (TYPE ICEBERG, ENDPOINT_URL 's3.amazonaws.com');

  -- For GCS (BigQuery customers):
  ATTACH 'gs://{customer_bucket}/{table_name}/'
    AS {table_name} (TYPE ICEBERG);
```
- On Iceberg snapshot refresh (triggered by sync layer): run `CHECKPOINT` and re-attach
/workers
pool.go            # Worker pool management, checkout/checkin
worker.go          # Single DuckDB worker lifecycle
attach.go          # Iceberg table attach/detach logic
executor.go        # Execute SQL on DuckDB, return ResultSet
refresh.go         # Handle Iceberg snapshot refresh events

### 4. Warehouse Connectors

Two connectors for Phase 1.

**Snowflake connector:**
- Use `snowflakedb/gosnowflake` driver
- Connection pool per customer (max 5 connections)
- Execute raw SQL and stream results back as Postgres wire protocol rows
- Support query cancellation (via Snowflake query ID)
- Extract query ID from response for cost tracking

**BigQuery connector:**
- Use `cloud.google.com/go/bigquery` client
- Support both service account JSON and OAuth2 credentials
- Execute queries via Jobs API (async, poll for completion)
- Use BigQuery Storage Read API for streaming large results efficiently
- Map BigQuery types to Postgres types in the response
/connectors
snowflake.go       # Snowflake connector
bigquery.go        # BigQuery connector
result.go          # Unified ResultSet type → Postgres RowDescription + DataRow
types.go           # Type mapping (warehouse types → Postgres OIDs)

### 5. CDC Sync Layer — Snowflake

The sync worker keeps Iceberg files on S3 fresh from Snowflake.

How it works:
1. When a customer adds a table to sync in the UI, your backend runs:
```sql
   CREATE STREAM prism_stream_{table_name} ON TABLE {schema}.{table_name};
```
2. A sync worker polls this stream every N seconds (configurable, default 60s):
```sql
   SELECT * FROM prism_stream_{table_name};
```
3. The stream returns rows with system columns: `METADATA$ACTION` (INSERT/DELETE), `METADATA$ISUPDATE`, `METADATA$ROW_ID`
4. Apply the changes to the Iceberg files:
   - For INSERT: append new Parquet files to the Iceberg table
   - For DELETE/UPDATE: write deletion vectors (Iceberg v2 row-level deletes)
5. Commit a new Iceberg snapshot
6. Publish a `{customer_id}:{table_name}:refreshed:{snapshot_id}` event to Redis

For initial load (first sync of a table):
```sql
SELECT * FROM {schema}.{table_name};
```
Stream this in batches of 100k rows, write as Parquet files to S3, create initial Iceberg table metadata.
/sync
snowflake_syncer.go   # Snowflake CDC poller
bigquery_syncer.go    # BigQuery CDC (see below)
iceberg_writer.go     # Write Parquet + Iceberg metadata to S3/GCS
iceberg_catalog.go    # REST catalog server (Iceberg REST spec)
snapshot.go           # Iceberg snapshot management
scheduler.go          # Per-table sync schedule management

### 6. CDC Sync Layer — BigQuery

BigQuery doesn't have Streams like Snowflake. Use BigQuery's native CDC (available for BigQuery tables with row-level change tracking enabled):
```sql
SELECT * FROM APPENDS(TABLE `project.dataset.table`, NULL, NULL)
```
Or for tables without CDC enabled, use a timestamp-based watermark approach:
```sql
SELECT * FROM `project.dataset.table`
WHERE _PARTITIONTIME > TIMESTAMP('{last_sync_time}')
```
Fall back to full table scan on first sync.

Write output as Iceberg on GCS using the same `iceberg_writer.go` module.

### 7. AI SQL Functions — ai_generate()

Users can write SQL like:
```sql
SELECT 
  order_id,
  ai_generate('Classify this complaint as positive/negative/neutral: ' || complaint_text, 'gpt-4o-mini') as sentiment
FROM customer_complaints
LIMIT 1000
```

The SQL rewriter (`rewriter.go`) detects `ai_generate(prompt_expr, model_name)` calls and rewrites them before execution.

Two paths:

**Path A — DuckDB LLM extension (preferred for small result sets):**
Rewrite to use DuckDB's community `llm` extension:
```sql
-- After rewrite:
SELECT 
  order_id,
  llm_complete(complaint_text, 'Classify as positive/negative/neutral: ', 'gpt-4o-mini') as sentiment
FROM customer_complaints
LIMIT 1000
```
The DuckDB worker must have the `llm` extension loaded and configured with the customer's OpenAI/Anthropic API key (stored encrypted in your DB, injected at runtime).

**Path B — Batch API call for large sets:**
If the query would call ai_generate() on more than 500 rows (detected by running a COUNT first), rewrite the execution flow:
1. Run the query without the ai_generate() call to get the base result set
2. Extract the prompt column values
3. Batch-call the LLM API (OpenAI batch API or Anthropic batch)
4. Join results back to the result set
5. Return the combined result to the client

Supported model names (map these in `rewriter.go`):
- `'gpt-4o-mini'` → OpenAI GPT-4o Mini
- `'gpt-4o'` → OpenAI GPT-4o
- `'claude-3-haiku'` → Anthropic Claude 3 Haiku
- `'claude-3-5-sonnet'` → Anthropic Claude 3.5 Sonnet
- `'gemini-flash'` → Google Gemini 1.5 Flash

The syntax is identical to BigQuery's `AI.GENERATE_TEXT()` but works across all warehouses.
/ai
rewriter.go        # Detect and rewrite ai_generate() calls
llm_client.go      # Unified LLM client (OpenAI + Anthropic + Google)
batch.go           # Batch execution for large row sets
cost_tracker.go    # Track LLM token costs per customer

### 8. Control Plane API

A REST API backend in Go (use `gin` or `chi` router).

Endpoints:
POST   /api/v1/auth/register          # Create account
POST   /api/v1/auth/login             # Get JWT
GET    /api/v1/me                     # Current user + org info
POST   /api/v1/connections            # Add warehouse connection
GET    /api/v1/connections            # List connections
DELETE /api/v1/connections/:id        # Remove connection
POST   /api/v1/connections/:id/test   # Test connection credentials
GET    /api/v1/connections/:id/tables # List available tables
POST   /api/v1/sync/tables            # Add tables to sync
GET    /api/v1/sync/tables            # List synced tables + status
DELETE /api/v1/sync/tables/:id        # Remove table from sync
GET    /api/v1/sync/tables/:id/status # Sync status + last snapshot time
GET    /api/v1/queries                # Query history (last 1000)
GET    /api/v1/queries/:id            # Single query detail
GET    /api/v1/analytics/savings      # Cost savings summary
GET    /api/v1/analytics/routing      # % routed to DuckDB vs warehouse
POST   /api/v1/apikeys                # Generate new API key
GET    /api/v1/apikeys                # List API keys
DELETE /api/v1/apikeys/:id            # Revoke API key
GET    /api/v1/connection-string      # Get the Postgres connection string for this account

Database: PostgreSQL (for control plane data — ironic but correct).
Schema tables: `customers`, `connections`, `synced_tables`, `query_log`, `api_keys`, `sync_jobs`.
/api
main.go            # Router setup, middleware
auth.go            # JWT middleware, API key middleware
connections.go     # Warehouse connection CRUD
sync.go            # Table sync management
queries.go         # Query log
analytics.go       # Savings + routing analytics
apikeys.go         # API key management

### 9. Query Logging

Every query that passes through the proxy must be logged asynchronously (non-blocking):
```go
type QueryLog struct {
  ID            uuid.UUID
  CustomerID    uuid.UUID
  ConnectionID  uuid.UUID
  SQL           string
  NormalizedSQL string  // stripped of literals for grouping
  RoutedTo      string  // "duckdb" | "snowflake" | "bigquery"
  DurationMs    int64
  RowsReturned  int64
  EstimatedCost float64 // in USD, based on bytes scanned * warehouse rate
  ActualCost    float64 // from warehouse query history (backfilled async)
  SavedCost     float64 // EstimatedCost - ActualCost
  Error         string
  CreatedAt     time.Time
}
```
Write to Postgres async via a buffered channel. Batch insert every 5 seconds.

---

## TECH STACK
Language:         Go 1.22+
Proxy server:     Pure Go (no framework)
API server:       Go + chi router
Database:         PostgreSQL 16 (control plane)
Cache/pubsub:     Redis 7
Object storage:   AWS S3 (managed mode) + GCS support (BYOS)
DuckDB:           go-duckdb v1.x (CGo bindings)
SQL parser:       pganalyze/pg_query_go
Iceberg:          Custom Go Iceberg writer (write Parquet + metadata JSON)
Reference: Apache Iceberg spec v2
Parquet writer:   parquet-go (xitongsys/parquet-go or apache/arrow/go)
Snowflake SDK:    snowflakedb/gosnowflake
BigQuery SDK:     cloud.google.com/go/bigquery
Frontend:         React + TypeScript + Tailwind CSS + shadcn/ui
Deployment:       Docker Compose (dev), single docker-compose.yml

---

## DIRECTORY STRUCTURE
/prism
/cmd
/proxy          # main.go for proxy server (port 5432)
/api            # main.go for REST API server (port 8080)
/sync           # main.go for sync worker
/internal
/proxy          # Postgres wire protocol implementation
/router         # Query routing logic
/workers        # DuckDB worker pool
/connectors     # Snowflake, BigQuery connectors
/sync           # CDC sync workers
/iceberg        # Iceberg reader/writer
/ai             # ai_generate() rewriter + LLM client
/api            # REST API handlers
/store          # Database models + queries (use sqlc)
/config         # Config loading (env vars + YAML)
/logger         # Structured logging (zerolog)
/migrations       # SQL migration files (use golang-migrate)
/frontend         # React app
/docker-compose.yml
/Makefile

---

## ENVIRONMENT VARIABLES

```bash
# Core
DATABASE_URL=postgres://prism:prism@localhost:5432/prism
REDIS_URL=redis://localhost:6379
PORT_PROXY=5432
PORT_API=8080
JWT_SECRET=changeme

# AWS (managed storage mode)
AWS_REGION=us-east-1
AWS_ACCESS_KEY_ID=...
AWS_SECRET_ACCESS_KEY=...
S3_BUCKET=prism-data

# LLM (for ai_generate)
OPENAI_API_KEY=...
ANTHROPIC_API_KEY=...

# Encryption (for warehouse credentials at rest)
ENCRYPTION_KEY=32-byte-hex-key
```

---

## DOCKER COMPOSE (dev environment)

Include a `docker-compose.yml` that starts:
1. `postgres` — PostgreSQL 16 for control plane
2. `redis` — Redis 7 for pubsub/cache
3. `prism-proxy` — the proxy server (built from /cmd/proxy)
4. `prism-api` — the REST API (built from /cmd/api)
5. `prism-sync` — the sync worker (built from /cmd/sync)
6. `prism-frontend` — the React UI (built from /frontend)
7. `localstack` — LocalStack to emulate S3 for local dev

---

## WHAT TO BUILD FIRST (ordered)

Build in this exact order so you have something runnable after each step:

1. **Postgres wire protocol server** — accept connections, parse startup message, respond to simple queries with hardcoded data. Goal: `psql -h localhost -U testkey -d acme_snowflake -c "SELECT 1"` works.

2. **Control plane DB schema + migrations** — customers, connections, api_keys, synced_tables, query_log tables. Use golang-migrate.

3. **Auth middleware** — API key lookup from "user" field in PG startup message. Reject unknown keys.

4. **Snowflake connector** — forward all queries to Snowflake, stream results back. No routing yet. Goal: Looker can connect and run reports.

5. **SQL router (DDL/write detection)** — classify queries as DDL/write/read. Log the classification. Still forward everything to warehouse.

6. **Iceberg writer** — write Parquet files + Iceberg v2 metadata to S3 (LocalStack in dev). Test with a small Snowflake table.

7. **Snowflake CDC sync** — poll Snowflake STREAM, apply changes to Iceberg. Test with a table that has active inserts.

8. **DuckDB worker pool** — spin up workers, attach Iceberg tables, execute read queries. Compare results against Snowflake direct for correctness.

9. **Full routing** — read queries on synced+fresh tables go to DuckDB, everything else goes to Snowflake. Test end-to-end with Looker.

10. **BigQuery connector** — same as step 4 but for BigQuery.

11. **BigQuery CDC sync** — same as step 7 but for BigQuery.

12. **ai_generate() rewriter** — detect, rewrite, execute via DuckDB LLM extension.

13. **REST API + frontend** — onboarding UI, table sync management, query analytics.

---

## KEY CONSTRAINTS AND RULES

- Never store warehouse credentials in plaintext. Encrypt at rest using AES-256-GCM before writing to DB.
- Never log SQL containing potential PII in plaintext. Log only normalized SQL (literals stripped) in production.
- The proxy must never crash on malformed SQL. Always catch panics, return a proper Postgres error response.
- DuckDB workers must never block the proxy's main goroutine. All DuckDB execution must run in worker goroutines with a context deadline.
- If the DuckDB worker returns an error or takes >30s, automatically fall back to the warehouse. Never return an error to the client that originated from DuckDB routing logic.
- All warehouse credentials must be tested (via /api/v1/connections/:id/test) before the first sync job starts.
- Iceberg table names in DuckDB must be namespaced per customer to avoid collisions: `{customer_id}_{schema}_{table}`.

---

## PHASE 1 DONE = SUCCESS CRITERIA

The MVP is complete when:
1. A Snowflake customer can point Looker at the proxy connection string and all their reports load correctly
2. At least 80% of their read queries are routed to DuckDB (visible in the analytics dashboard)
3. A BigQuery customer can do the same
4. `SELECT ai_generate('Classify: ' || text_col, 'gpt-4o-mini') FROM my_table LIMIT 100` works correctly
5. The sync lag is under 2 minutes for tables with active writes
6. The proxy adds less than 100ms overhead vs direct warehouse connection for DuckDB-routed queries

Start building now. Ask clarifying questions only if a technical decision blocks you from writing code. Prefer making a reasonable implementation choice and noting it as a TODO over asking.

That's the full prompt. A few notes on using it effectively with Claude Code:
When you paste it, Claude Code will likely start with the Postgres wire protocol server first (step 1 in the ordered build list). Let it run through several steps before interrupting — the ordering is deliberate so you get a working proxy quickly before adding complexity. If it gets stuck on the CGo DuckDB bindings (common on Mac M-series), tell it to use marcboeker/go-duckdb and ensure CGo is enabled in the build. For local dev, tell it to use LocalStack for S3 so you don't need real AWS credentials early on.
