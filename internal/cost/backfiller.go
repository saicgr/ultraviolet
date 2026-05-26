// Package cost backfills `query_log.actual_cost_usd` from per-warehouse cost APIs and
// rolls them up into the `cost_attribution` table. Runs on a nightly cron (cmd/sync)
// and on demand from the API.
//
// See docs/architecture/cost-attribution.md for the per-warehouse mapping.
package cost

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/ultraviolet-dev/ultraviolet/internal/config"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
)

// Backfiller orchestrates per-warehouse cost lookups.
type Backfiller struct {
	cfg *config.Config
	db  *store.DB
	enc *store.Encryptor
	log zerolog.Logger

	bq *BigQueryCost
	sf *SnowflakeCost
}

func New(cfg *config.Config, db *store.DB, enc *store.Encryptor, log zerolog.Logger) *Backfiller {
	return &Backfiller{
		cfg: cfg, db: db, enc: enc,
		log: log.With().Str("component", "cost-backfiller").Logger(),
		bq:  &BigQueryCost{db: db, enc: enc},
		sf:  &SnowflakeCost{db: db, enc: enc, USDPerTiB: cfg.SnowflakeUSDPerTiB},
	}
}

// Run loops every interval. Process one customer at a time (cost APIs are heavy;
// no parallelism on Phase 1).
func (b *Backfiller) Run(ctx context.Context, interval time.Duration) error {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	if err := b.tick(ctx); err != nil {
		b.log.Warn().Err(err).Msg("first tick")
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			if err := b.tick(ctx); err != nil {
				b.log.Warn().Err(err).Msg("tick")
			}
		}
	}
}

func (b *Backfiller) tick(ctx context.Context) error {
	customers, err := b.db.ListCustomers(ctx)
	if err != nil {
		return err
	}
	for _, c := range customers {
		if err := b.backfillCustomer(ctx, c.ID); err != nil {
			b.log.Warn().Err(err).Str("customer", c.Slug).Msg("backfill customer")
		}
	}
	return nil
}

func (b *Backfiller) backfillCustomer(ctx context.Context, customerID uuid.UUID) error {
	// Process unattributed query_log rows from the last 24h.
	rows, err := b.db.Pool().Query(ctx,
		`SELECT id, connection_id, query_hash, started_at
		 FROM query_log
		 WHERE customer_id = $1 AND actual_cost_usd IS NULL
		   AND started_at > now() - interval '24 hours'
		   AND route_decision IN ('warehouse','fallback')
		 ORDER BY started_at DESC LIMIT 1000`, customerID)
	if err != nil {
		return err
	}
	type pending struct {
		id           uuid.UUID
		connectionID *uuid.UUID
		queryHash    string
		startedAt    time.Time
	}
	var todo []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.connectionID, &p.queryHash, &p.startedAt); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, p)
	}
	rows.Close()

	for _, p := range todo {
		if p.connectionID == nil {
			continue
		}
		conns, err := b.db.ListConnections(ctx, customerID)
		if err != nil {
			return err
		}
		var conn *store.Connection
		for i := range conns {
			if conns[i].ID == *p.connectionID {
				conn = &conns[i]
				break
			}
		}
		if conn == nil {
			continue
		}
		var costUSD float64
		switch conn.WarehouseType {
		case "bigquery":
			costUSD, err = b.bq.LookupCost(ctx, customerID, p.queryHash, p.startedAt)
		case "snowflake":
			costUSD, err = b.sf.LookupCost(ctx, customerID, p.queryHash, p.startedAt)
		default:
			err = ErrUnsupportedWarehouse
		}
		if err != nil {
			b.log.Debug().Err(err).Str("hash", p.queryHash).Msg("cost lookup")
			continue
		}
		_, _ = b.db.Pool().Exec(ctx,
			`UPDATE query_log SET actual_cost_usd = $1 WHERE id = $2`, costUSD, p.id)
	}

	return b.rollup(ctx, customerID)
}

// rollup writes today's cost_attribution row for the customer.
func (b *Backfiller) rollup(ctx context.Context, customerID uuid.UUID) error {
	conns, err := b.db.ListConnections(ctx, customerID)
	if err != nil {
		return err
	}
	for _, c := range conns {
		row := b.db.Pool().QueryRow(ctx,
			`SELECT
			   COUNT(*),
			   COUNT(*) FILTER (WHERE route_decision = 'duckdb'),
			   COALESCE(SUM(actual_cost_usd) FILTER (WHERE route_decision IN ('warehouse','fallback')), 0),
			   COALESCE(SUM(estimated_cost_usd) FILTER (WHERE route_decision = 'duckdb'), 0),
			   COALESCE(SUM(estimated_cost_usd) FILTER (WHERE route_decision = 'duckdb'), 0)
			 FROM query_log
			 WHERE customer_id = $1 AND connection_id = $2
			   AND started_at >= date_trunc('day', now())`, customerID, c.ID)
		var qTotal, qDuck int64
		var whCost, ddCost, savings float64
		if err := row.Scan(&qTotal, &qDuck, &whCost, &ddCost, &savings); err != nil {
			return err
		}
		periodStart := time.Now().UTC().Truncate(24 * time.Hour)
		periodEnd := periodStart.Add(24 * time.Hour)
		if _, err := b.db.Pool().Exec(ctx,
			`INSERT INTO cost_attribution
			   (customer_id, connection_id, period_start, period_end,
			    warehouse_cost_usd, duckdb_cost_usd, estimated_savings_usd,
			    queries_total, queries_duckdb)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			 ON CONFLICT (customer_id, connection_id, period_start)
			 DO UPDATE SET warehouse_cost_usd = EXCLUDED.warehouse_cost_usd,
			               duckdb_cost_usd = EXCLUDED.duckdb_cost_usd,
			               estimated_savings_usd = EXCLUDED.estimated_savings_usd,
			               queries_total = EXCLUDED.queries_total,
			               queries_duckdb = EXCLUDED.queries_duckdb`,
			customerID, c.ID, periodStart, periodEnd, whCost, ddCost, savings, qTotal, qDuck); err != nil {
			return fmt.Errorf("rollup upsert: %w", err)
		}
	}
	return nil
}

// ErrUnsupportedWarehouse mirrors connectors.ErrUnsupportedWarehouse but kept local
// to avoid a circular import at package load.
var ErrUnsupportedWarehouse = errors.New("unsupported warehouse for cost backfill")
