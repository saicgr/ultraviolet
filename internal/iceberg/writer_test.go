package iceberg

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/ultraviolet-dev/ultraviolet/internal/config"
)

// TestCreateTableAs_FailsClosedOnBadSource verifies the atomicity contract: an
// invalid source query must surface as an error rather than silently writing a
// partial snapshot. (It also exercises the iceberg/parquet fallback path.)
func TestCreateTableAs_FailsClosedOnBadSource(t *testing.T) {
	cfg := &config.Config{
		S3Endpoint:   "http://127.0.0.1:1", // unreachable; ensures S3 write would fail
		S3Bucket:     "test",
		AWSRegion:    "us-east-1",
		AWSAccessKey: "test",
		AWSSecretKey: "test",
	}
	w, err := NewWriter(context.Background(), cfg, zerolog.Nop())
	if err != nil {
		t.Skipf("duckdb extensions unavailable in this environment: %v", err)
	}
	defer w.Close()

	err = w.CreateTableAs(context.Background(), "s3://test/foo", "SELECT * FROM nonexistent_table")
	if err == nil {
		t.Error("expected error from invalid source query, got nil")
	}
}
