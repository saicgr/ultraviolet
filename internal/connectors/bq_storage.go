package connectors

import (
	"context"
	"fmt"
)

// useBQStorageRead is a build-time toggle for the Storage Read API path. The full
// wiring needs `cloud.google.com/go/bigquery/storage/apiv1` (heavy gRPC tree).
// We expose the entry point so the syncer + connector can opt in once the dep
// lands; today it returns ErrNotImplemented and the caller falls back to the
// REST iterator. Tracked under AUDIT.md §connectors gap.
var useBQStorageRead = false

func bqStorageRead(ctx context.Context, project, dataset, table string) error {
	if !useBQStorageRead {
		return fmt.Errorf("BQ Storage Read API: %w", ErrNotImplemented)
	}
	return nil
}
