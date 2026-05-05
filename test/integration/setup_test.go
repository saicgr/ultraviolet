// Package integration runs end-to-end tests against real warehouses
// (bigquery-public-data.*) and LocalStack S3, never against mocks.
//
// Required env (per .env.test.example):
//   - GOOGLE_APPLICATION_CREDENTIALS — service account with BQ Job User + Data Viewer
//   - DATABASE_URL — control-plane Postgres
//   - REDIS_URL — pubsub + freshness cache
//   - S3_ENDPOINT — LocalStack URL
//   - UV_PROXY_PORT — defaults to 5000 in dev/test (avoids local-Postgres clash on 5432)
//
// Tests that require a missing env var must call t.Skip with explicit reason
// (NEVER silently pass — see docs/process/no-fallback-data.md).
//
// Run: `make test-integration`

//go:build integration

package integration

import (
	"os"
	"testing"
)

// TestMain boots LocalStack + Postgres + Redis (via docker-compose),
// applies migrations, seeds a fake test customer + BQ connection,
// then runs tests. Tear-down on exit.
func TestMain(m *testing.M) {
	// TODO Phase 1 step 2: implement once internal/store + migrations exist
	// 1. testcontainers-go to start LocalStack + Postgres + Redis (or rely on `make dev`)
	// 2. golang-migrate up against $DATABASE_URL
	// 3. seed customer "test_uv" + BQ connection from $GOOGLE_APPLICATION_CREDENTIALS
	// 4. m.Run()
	// 5. tear down

	os.Exit(m.Run())
}

// requireBQ skips a test when the GCP service-account env is unset.
// Never silently passes; explicit skip with reason.
func requireBQ(t *testing.T) {
	t.Helper()
	if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") == "" {
		t.Skip("SKIP: GOOGLE_APPLICATION_CREDENTIALS unset — see .env.test.example")
	}
}

// requireLocalStack skips when LocalStack S3 is unreachable.
func requireLocalStack(t *testing.T) {
	t.Helper()
	if os.Getenv("S3_ENDPOINT") == "" {
		t.Skip("SKIP: S3_ENDPOINT unset — start LocalStack via `make dev`")
	}
}

// proxyPort returns the dev/test proxy port (5000 by default).
// Never use the prod default 5432 in tests — it collides with a local Postgres.
func proxyPort() string {
	if p := os.Getenv("UV_PROXY_PORT"); p != "" {
		return p
	}
	return "5000"
}
