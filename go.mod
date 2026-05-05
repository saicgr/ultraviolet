module github.com/ultraviolet-dev/ultraviolet

go 1.22

// Dependencies will be added by `go get` as Phase 1 packages are implemented.
// Anchor packages anticipated:
//   github.com/marcboeker/go-duckdb              // CGo bindings for DuckDB workers
//   github.com/pganalyze/pg_query_go/v5          // PG SQL parser
//   github.com/jackc/pgx/v5                      // control-plane Postgres driver
//   github.com/snowflakedb/gosnowflake           // Snowflake connector
//   cloud.google.com/go/bigquery                 // BigQuery connector
//   github.com/aws/aws-sdk-go-v2                 // S3 client
//   github.com/redis/go-redis/v9                 // pubsub + freshness cache
//   github.com/rs/zerolog                        // structured logging
//   github.com/go-chi/chi/v5                     // REST API router
//   github.com/golang-jwt/jwt/v5                 // JWT auth
//   github.com/google/uuid                       // UUIDv7
//   github.com/sqlc-dev/sqlc                     // SQL → Go (build-time)
//   golang.org/x/sync/errgroup                   // goroutine coordination
//   github.com/stretchr/testify                  // test assertions
//   go.uber.org/goleak                           // goroutine-leak tests
//   github.com/testcontainers/testcontainers-go  // integration test infra
