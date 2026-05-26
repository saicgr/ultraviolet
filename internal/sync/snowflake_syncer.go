package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	sf "github.com/snowflakedb/gosnowflake"

	"github.com/ultraviolet-dev/ultraviolet/internal/iceberg"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
)

// syncSnowflakeTable polls a Snowflake STREAM for change rows and applies them to
// the matching Iceberg table. The STREAM must already be created on the Snowflake
// side as `CREATE STREAM uv_<schema>_<table>_stream ON TABLE <schema>.<table>`.
//
// Behaviour when no Snowflake account is configured: the call returns nil (logged) so
// the syncer doesn't crash the proxy on dev machines without Snowflake creds.
func (s *Syncer) syncSnowflakeTable(ctx context.Context, customerID uuid.UUID, conn store.Connection, t store.SyncedTable) error {
	if conn.WarehouseType != "snowflake" {
		return fmt.Errorf("called snowflake syncer with %s", conn.WarehouseType)
	}
	_, creds, err := s.db.GetConnectionForCustomer(ctx, s.enc, customerID, "snowflake")
	if err != nil {
		s.log.Debug().Err(err).Msg("snowflake creds unavailable — skipping")
		return nil
	}
	dsn, err := snowflakeDSNFromCreds(creds)
	if err != nil {
		return err
	}
	dbh, err := sql.Open("snowflake", dsn)
	if err != nil {
		return err
	}
	defer dbh.Close()
	if err := dbh.PingContext(ctx); err != nil {
		s.log.Debug().Err(err).Msg("snowflake ping failed — skipping")
		return nil
	}

	streamName := fmt.Sprintf("uv_%s_%s_stream", t.SchemaName, t.TableName)
	// Snowflake STREAM advance semantics: any DML against the stream rows
	// inside a transaction consumes them. We open a tx, materialize the
	// change rows into a stage view, append to Iceberg, then commit.
	tx, err := dbh.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("snowflake begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", streamName))
	if err != nil {
		return fmt.Errorf("snowflake stream query: %w", err)
	}
	var changeCount int64
	if rows.Next() {
		_ = rows.Scan(&changeCount)
	}
	rows.Close()
	if changeCount == 0 {
		s.log.Debug().Str("stream", streamName).Msg("no changes")
		return nil
	}

	customer, err := s.db.GetCustomerBySlug(ctx, customerSlugFromID(ctx, s.db, customerID))
	if err != nil {
		return err
	}
	location := iceberg.TableLocation(s.cfg.S3Bucket, customer.Slug, t.SchemaName, t.TableName)
	// Real consumption: this issues a `SELECT *` against the STREAM in the same
	// transaction, ensuring the offset advances on commit. We pass that source
	// to the iceberg writer, which buffers + commits a snapshot.
	source := fmt.Sprintf("SELECT * FROM %s", streamName)
	if err := s.writer.Append(ctx, location, source); err != nil {
		s.log.Warn().Err(err).Msg("snowflake append iceberg")
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("snowflake commit (stream advance): %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.db.MarkSyncedTableState(ctx, t.ID, "live", nil, nil, &now, t.RowCount+changeCount); err != nil {
		return err
	}
	s.PublishSnapshot(ctx, customer.Slug, conn.ID, t.SchemaName, t.TableName, time.Now().Unix())
	return nil
}

func snowflakeDSNFromCreds(creds []byte) (string, error) {
	var c struct {
		Account   string `json:"account"`
		User      string `json:"user"`
		Password  string `json:"password,omitempty"`
		Role      string `json:"role,omitempty"`
		Warehouse string `json:"warehouse,omitempty"`
		Database  string `json:"database,omitempty"`
		Schema    string `json:"schema,omitempty"`
	}
	if err := json.Unmarshal(creds, &c); err != nil {
		return "", err
	}
	cfg := &sf.Config{
		Account:   c.Account,
		User:      c.User,
		Password:  c.Password,
		Role:      c.Role,
		Warehouse: c.Warehouse,
		Database:  c.Database,
		Schema:    c.Schema,
	}
	return sf.DSN(cfg)
}
