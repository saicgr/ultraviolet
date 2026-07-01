// Package costest is the single source of truth for Ultraviolet's
// warehouse-equivalent cost estimate: given a SQL string and a customer's
// warehouse, it derives the bytes that would be scanned and converts that to
// USD. It is used by the pre-flight API (synchronous, on demand) and the cost
// backfiller (async, to populate query_log.estimated_cost_usd) so both compute
// the same number — never two divergent cost models.
//
// Cost model (per docs/architecture/cost-attribution.md):
//   - BigQuery on-demand: USD/TiB scanned (default $6.25, UV_BQ_USD_PER_TIB).
//   - Snowflake: warehouse-credit proxy via UV_SF_USD_PER_TIB (default $5).
//
// Table size uses synced_tables.row_count × 256 B/row until an
// INFORMATION_SCHEMA path lands. Tables not present are returned as `unknown`
// (never mocked) so the caller can surface a best-effort + honest gap.
package costest

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ultraviolet-dev/ultraviolet/internal/router"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
)

const (
	bytesPerRowGuess = 256 // heuristic until INFORMATION_SCHEMA path lands
	tib              = 1 << 40
)

// Estimator computes warehouse-equivalent query cost from referenced tables.
type Estimator struct {
	db          *store.DB
	bqUSDPerTiB float64
	sfUSDPerTiB float64
}

// New builds an Estimator. Rates come from config (BigQueryUSDPerTiB,
// SnowflakeUSDPerTiB); pass non-positive to accept the model defaults.
func New(db *store.DB, bqUSDPerTiB, sfUSDPerTiB float64) *Estimator {
	if bqUSDPerTiB <= 0 {
		bqUSDPerTiB = 6.25
	}
	if sfUSDPerTiB <= 0 {
		sfUSDPerTiB = 5.0
	}
	return &Estimator{db: db, bqUSDPerTiB: bqUSDPerTiB, sfUSDPerTiB: sfUSDPerTiB}
}

// EstimateSQL parses sql, sums known table sizes, and converts to USD for the
// given warehouse. Returns (bytes, usd, unknownTables, err). A SQL with no
// resolvable table refs yields (0, 0, nil, nil) — a real zero, not an error.
func (e *Estimator) EstimateSQL(ctx context.Context, customerID uuid.UUID, warehouse, sql string) (int64, float64, []string, error) {
	refs := router.ExtractTables(sql)
	if len(refs) == 0 {
		return 0, 0, nil, nil
	}
	var total int64
	var unknown []string
	for _, t := range refs {
		rowCount, found, err := e.lookupRowCount(ctx, customerID, t.Schema, t.Table)
		if err != nil {
			return 0, 0, nil, err
		}
		if !found {
			label := t.Table
			if t.Schema != "" {
				label = t.Schema + "." + t.Table
			}
			unknown = append(unknown, label)
			continue
		}
		total += rowCount * bytesPerRowGuess
	}
	return total, e.CostForBytes(warehouse, total), unknown, nil
}

// CostForBytes converts a byte-scan count into USD using the warehouse rate.
func (e *Estimator) CostForBytes(warehouse string, bytes int64) float64 {
	if bytes <= 0 {
		return 0
	}
	rate := e.bqUSDPerTiB
	if strings.EqualFold(warehouse, "snowflake") {
		rate = e.sfUSDPerTiB
	}
	return float64(bytes) / float64(tib) * rate
}

// lookupRowCount inspects synced_tables for a customer-scoped (schema, table)
// match, using connections.customer_id as the authorization boundary.
func (e *Estimator) lookupRowCount(ctx context.Context, customerID uuid.UUID, schema, table string) (int64, bool, error) {
	row := e.db.Pool().QueryRow(ctx, `
		SELECT t.row_count
		FROM synced_tables t
		JOIN connections c ON c.id = t.connection_id
		WHERE c.customer_id = $1
		  AND t.table_name  = $2
		  AND ($3 = '' OR t.schema_name = $3)
		ORDER BY t.last_sync_at DESC NULLS LAST
		LIMIT 1`, customerID, table, schema)
	var n int64
	if err := row.Scan(&n); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return n, true, nil
}
