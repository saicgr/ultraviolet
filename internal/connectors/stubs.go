package connectors

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/ultraviolet-dev/ultraviolet/internal/protocols/pgwire"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
)

// stub embeds the bookkeeping every stub connector needs so the per-warehouse
// file is a 4-line constructor. Each stub returns ErrNotImplemented from
// ExecuteStreaming so the proxy sees a real PG ErrorResponse, not a silent
// pass-through. Wiring a real connector means replacing the call to
// newStub() with a real client + Execute path; everything else stays.
type stub struct {
	connID    uuid.UUID
	warehouse string
	log       zerolog.Logger
}

func newStub(conn *store.Connection, warehouse string, log zerolog.Logger) *stub {
	return &stub{connID: conn.ID, warehouse: warehouse, log: log}
}

func (s *stub) ConnectionID() uuid.UUID { return s.connID }
func (s *stub) WarehouseType() string   { return s.warehouse }
func (s *stub) Close() error            { return nil }

func (s *stub) ExecuteStreaming(ctx context.Context, sql string, params [][]byte, w *pgwire.Writer) (int64, error) {
	return 0, WrapWarehouseError(fmt.Errorf("%s: %w", s.warehouse, ErrNotImplemented))
}

// ---- Per-warehouse constructors. Each is a one-liner over `newStub` until the
// real SDK wiring lands. Order matches the W4 deliverable list in AUDIT.md. ----

func NewDatabricks(ctx context.Context, conn *store.Connection, creds []byte, log zerolog.Logger) (Connector, error) {
	return newStub(conn, "databricks", log), nil
}
func NewRedshift(ctx context.Context, conn *store.Connection, creds []byte, log zerolog.Logger) (Connector, error) {
	return newStub(conn, "redshift", log), nil
}
func NewClickHouse(ctx context.Context, conn *store.Connection, creds []byte, log zerolog.Logger) (Connector, error) {
	return newStub(conn, "clickhouse", log), nil
}
func NewMotherDuck(ctx context.Context, conn *store.Connection, creds []byte, log zerolog.Logger) (Connector, error) {
	return newStub(conn, "motherduck", log), nil
}
func NewFabric(ctx context.Context, conn *store.Connection, creds []byte, log zerolog.Logger) (Connector, error) {
	return newStub(conn, "fabric", log), nil
}
func NewPGSource(ctx context.Context, conn *store.Connection, creds []byte, log zerolog.Logger) (Connector, error) {
	return newStub(conn, "pg_source", log), nil
}
func NewMySQL(ctx context.Context, conn *store.Connection, creds []byte, log zerolog.Logger) (Connector, error) {
	return newStub(conn, "mysql", log), nil
}
func NewTrino(ctx context.Context, conn *store.Connection, creds []byte, log zerolog.Logger) (Connector, error) {
	return newStub(conn, "trino", log), nil
}
func NewMongo(ctx context.Context, conn *store.Connection, creds []byte, log zerolog.Logger) (Connector, error) {
	return newStub(conn, "mongo", log), nil
}
func NewDynamo(ctx context.Context, conn *store.Connection, creds []byte, log zerolog.Logger) (Connector, error) {
	return newStub(conn, "dynamo", log), nil
}
func NewLocalDuckDB(ctx context.Context, conn *store.Connection, creds []byte, log zerolog.Logger) (Connector, error) {
	return newStub(conn, "local_duckdb", log), nil
}
