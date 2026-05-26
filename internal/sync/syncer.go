// Package sync runs CDC pollers per warehouse, writes Iceberg snapshots, publishes
// refresh events on Redis pubsub. See docs/architecture/cdc-sync.md.
package sync

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/ultraviolet-dev/ultraviolet/internal/config"
	"github.com/ultraviolet-dev/ultraviolet/internal/connectors"
	"github.com/ultraviolet-dev/ultraviolet/internal/iceberg"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
)

// Syncer is the per-customer CDC orchestrator.
type Syncer struct {
	cfg      *config.Config
	db       *store.DB
	enc      *store.Encryptor
	connFact *connectors.Factory
	writer   *iceberg.Writer
	rdb      *redis.Client
	log      zerolog.Logger
	interval time.Duration
}

func New(ctx context.Context, cfg *config.Config, db *store.DB, enc *store.Encryptor, log zerolog.Logger) (*Syncer, error) {
	cf, err := connectors.NewFactory(cfg, db, enc, log)
	if err != nil {
		return nil, err
	}
	wr, err := iceberg.NewWriter(ctx, cfg, log)
	if err != nil {
		return nil, err
	}
	opt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	rdb := redis.NewClient(opt)
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Warn().Err(err).Msg("redis ping failed — continuing without pubsub")
	}
	return &Syncer{
		cfg:      cfg,
		db:       db,
		enc:      enc,
		connFact: cf,
		writer:   wr,
		rdb:      rdb,
		log:      log.With().Str("component", "syncer").Logger(),
		interval: 60 * time.Second,
	}, nil
}

func (s *Syncer) Close() error {
	if s.rdb != nil {
		_ = s.rdb.Close()
	}
	if s.writer != nil {
		return s.writer.Close()
	}
	return nil
}

// Run loops every interval and processes all synced_tables in state pending|live.
func (s *Syncer) Run(ctx context.Context) error {
	tick := time.NewTicker(s.interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			if err := s.tick(ctx); err != nil {
				s.log.Error().Err(err).Msg("sync tick error")
			}
		}
	}
}

func (s *Syncer) tick(ctx context.Context) error {
	// Fan out per customer, but keep concurrency bounded.
	customers, err := s.db.ListCustomers(ctx)
	if err != nil {
		return err
	}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(4)
	for _, c := range customers {
		c := c
		g.Go(func() error { return s.syncCustomer(gctx, c.ID) })
	}
	return g.Wait()
}

func (s *Syncer) syncCustomer(ctx context.Context, customerID uuid.UUID) error {
	conns, err := s.db.ListConnections(ctx, customerID)
	if err != nil {
		return err
	}
	for _, conn := range conns {
		tables, err := s.db.ListSyncedTables(ctx, conn.ID)
		if err != nil {
			return err
		}
		for _, t := range tables {
			if err := s.syncTable(ctx, customerID, conn, t); err != nil {
				s.log.Warn().Err(err).Str("table", t.SchemaName+"."+t.TableName).Msg("sync table")
				_ = s.markError(ctx, t.ID, err)
			}
		}
	}
	return nil
}

func (s *Syncer) syncTable(ctx context.Context, customerID uuid.UUID, conn store.Connection, t store.SyncedTable) error {
	switch conn.WarehouseType {
	case "bigquery":
		return s.syncBigQueryTable(ctx, customerID, conn, t)
	case "snowflake":
		return s.syncSnowflakeTable(ctx, customerID, conn, t)
	default:
		return fmt.Errorf("unsupported warehouse for sync: %s", conn.WarehouseType)
	}
}

func (s *Syncer) markError(ctx context.Context, id uuid.UUID, err error) error {
	msg := err.Error()
	return s.db.MarkSyncedTableState(ctx, id, "error", &msg, nil, nil, 0)
}

// PublishSnapshot notifies the worker pool a table has a new snapshot.
// Channel format: iceberg.snapshot.{slug}.{connection_id}.{schema}.{table}.
// The connection_id segment prevents two BQ projects with the same {schema.table}
// from clobbering each other's pubsub stream (AUDIT.md §sync gap).
func (s *Syncer) PublishSnapshot(ctx context.Context, customerSlug string, connID uuid.UUID, schema, table string, snapshotID int64) {
	if s.rdb == nil {
		return
	}
	channel := fmt.Sprintf("iceberg.snapshot.%s.%s.%s.%s", customerSlug, connID, schema, table)
	if err := s.rdb.Publish(ctx, channel, fmt.Sprintf("%d", snapshotID)).Err(); err != nil {
		s.log.Warn().Err(err).Str("channel", channel).Msg("publish")
	}
}

// ErrSkipped is a sentinel for "nothing changed since last sync".
var ErrSkipped = errors.New("no changes since last watermark")
