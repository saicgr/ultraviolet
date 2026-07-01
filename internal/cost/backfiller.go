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
	"github.com/ultraviolet-dev/ultraviolet/internal/costest"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
)

// Backfiller orchestrates per-warehouse cost lookups.
type Backfiller struct {
	cfg *config.Config
	db  *store.DB
	enc *store.Encryptor
	log zerolog.Logger

	bq  *BigQueryCost
	sf  *SnowflakeCost
	est *costest.Estimator
}

func New(cfg *config.Config, db *store.DB, enc *store.Encryptor, log zerolog.Logger) *Backfiller {
	return &Backfiller{
		cfg: cfg, db: db, enc: enc,
		log: log.With().Str("component", "cost-backfiller").Logger(),
		bq:  &BigQueryCost{db: db, enc: enc},
		sf:  &SnowflakeCost{db: db, enc: enc, USDPerTiB: cfg.SnowflakeUSDPerTiB},
		est: costest.New(db, cfg.BigQueryUSDPerTiB, cfg.SnowflakeUSDPerTiB),
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

	// Fill estimated_cost_usd (warehouse-equivalent) for any row still missing
	// it BEFORE rolling up, so the savings = estimated − actual math is real.
	// This is the warehouse-equivalent estimate even for DuckDB-routed queries
	// (that estimate is precisely the saving).
	if err := b.backfillEstimatedCost(ctx, customerID); err != nil {
		b.log.Warn().Err(err).Msg("estimated cost backfill")
	}

	return b.rollup(ctx, customerID)
}

// backfillEstimatedCost populates query_log.estimated_cost_usd (the
// warehouse-equivalent cost) for rows that lack it, using the shared costest
// estimator over the stored normalized_sql. Runs off the hot path (the proxy
// must never touch Postgres for this — see internal/router/CLAUDE.md), here on
// the nightly/on-demand backfill tick. Without this, estimated_cost is NULL and
// the savings calculation has nothing to subtract from.
func (b *Backfiller) backfillEstimatedCost(ctx context.Context, customerID uuid.UUID) error {
	rows, err := b.db.Pool().Query(ctx,
		`SELECT q.id, q.normalized_sql, COALESCE(c.warehouse_type, '')
		 FROM query_log q
		 LEFT JOIN connections c ON c.id = q.connection_id
		 WHERE q.customer_id = $1 AND q.estimated_cost_usd IS NULL
		   AND q.route_decision <> 'error'
		   AND q.started_at > now() - interval '24 hours'
		 ORDER BY q.started_at DESC LIMIT 2000`, customerID)
	if err != nil {
		return err
	}
	type pendingEst struct {
		id        uuid.UUID
		sql       string
		warehouse string
	}
	var todo []pendingEst
	for rows.Next() {
		var p pendingEst
		if err := rows.Scan(&p.id, &p.sql, &p.warehouse); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, p)
	}
	rows.Close()

	for _, p := range todo {
		warehouse := p.warehouse
		if warehouse == "" {
			warehouse = "bigquery" // rate-only default; bytes drive the figure
		}
		_, cost, _, err := b.est.EstimateSQL(ctx, customerID, warehouse, p.sql)
		if err != nil {
			b.log.Debug().Err(err).Str("id", p.id.String()).Msg("estimate")
			continue
		}
		_, _ = b.db.Pool().Exec(ctx,
			`UPDATE query_log SET estimated_cost_usd = $1 WHERE id = $2`, cost, p.id)
	}
	return nil
}

// RollupCustomer fills any missing estimated costs then rolls today's
// query_log activity into cost_attribution for a single customer. Exposed for
// on-demand callers (the in-app Workbench rolls up immediately after a run so
// the savings dashboard reflects real activity without waiting for the cron).
func (b *Backfiller) RollupCustomer(ctx context.Context, customerID uuid.UUID) error {
	if err := b.backfillEstimatedCost(ctx, customerID); err != nil {
		b.log.Warn().Err(err).Msg("estimated cost backfill (on-demand)")
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
		// Cost model (docs/architecture/cost-attribution.md, saved = estimated −
		// actual):
		//   warehouse_cost = actual paid on the warehouse (warehouse+fallback rows)
		//   duckdb_cost    = actual cost of DuckDB-routed queries (≈0; we don't
		//                    bill DuckDB, so actual_cost stays NULL→0 for them)
		//   savings        = warehouse-EQUIVALENT estimate of the DuckDB-routed
		//                    queries minus what they actually cost. Previously
		//                    duckdb_cost and savings both read the SAME
		//                    estimated-cost sum, so savings was wrong and equal
		//                    to duckdb_cost — fixed here.
		row := b.db.Pool().QueryRow(ctx,
			`SELECT
			   COUNT(*),
			   COUNT(*) FILTER (WHERE route_decision = 'duckdb'),
			   COALESCE(SUM(actual_cost_usd) FILTER (WHERE route_decision IN ('warehouse','fallback')), 0),
			   COALESCE(SUM(actual_cost_usd) FILTER (WHERE route_decision = 'duckdb'), 0),
			   COALESCE(SUM(estimated_cost_usd) FILTER (WHERE route_decision = 'duckdb'), 0)
			     - COALESCE(SUM(actual_cost_usd) FILTER (WHERE route_decision = 'duckdb'), 0)
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
