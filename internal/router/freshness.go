package router

import "time"

// FreshnessThreshold is the max staleness for which DuckDB-from-Iceberg can serve a query.
// Default 5 min; tunable per-customer via UV_FRESHNESS_SECONDS in a future iteration.
const FreshnessThreshold = 5 * time.Minute

// IsFresh returns true when the synced table's last_sync_at is within the threshold of now.
func IsFresh(lastSyncAt *time.Time) bool {
	if lastSyncAt == nil {
		return false
	}
	return time.Since(*lastSyncAt) <= FreshnessThreshold
}
