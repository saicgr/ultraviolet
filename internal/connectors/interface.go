// Package connectors adapts warehouse SDKs to the proxy's wire layer.
// Each connector streams result rows back as Postgres DataRow frames.
package connectors

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/ultraviolet-dev/ultraviolet/internal/config"
	"github.com/ultraviolet-dev/ultraviolet/internal/protocols/pgwire"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
)

// Connector is implemented per warehouse type.
// ExecuteStreaming runs the SQL on the warehouse, writes RowDescription + DataRow frames
// to w, and finishes with a CommandComplete tag. Returns the row count.
type Connector interface {
	ExecuteStreaming(ctx context.Context, sql string, params [][]byte, w *pgwire.Writer) (int64, error)
	ConnectionID() uuid.UUID
	WarehouseType() string
	Close() error
}

// Cancel is implemented by connectors that support server-side cancel of an
// in-flight query (BQ Jobs.cancel, Snowflake SYSTEM$CANCEL_QUERY, etc.). pgwire
// CancelRequest forwarding looks for this interface; absence falls back to ctx
// cancel only.
type Cancellable interface {
	Cancel(ctx context.Context, queryID string) error
}

// Introspecter exposes table-list discovery so the catalog UI doesn't need to
// run ad-hoc SQL through the proxy. Optional per-connector.
type Introspecter interface {
	Introspect(ctx context.Context) ([]Table, error)
}

// Table is the result of introspection.
type Table struct {
	Schema  string
	Name    string
	RowCount int64
	Columns []Column
}

type Column struct {
	Name string
	Type string
	Null bool
}

// ErrNotImplemented is returned by stub connectors that ship in the W4 matrix
// before their full SDK wiring lands. They satisfy the interface so the
// frontend connection picker can list them.
var ErrNotImplemented = errors.New("connector: not yet implemented")

// ErrUnsupportedWarehouse is returned when the factory has no connector for the warehouse type.
var ErrUnsupportedWarehouse = errors.New("unsupported warehouse type")

// Factory builds connectors lazily, decrypts credentials per-customer, and caches by (customer, warehouse).
type Factory struct {
	cfg *config.Config
	db  *store.DB
	enc *store.Encryptor
	log zerolog.Logger
}

func NewFactory(cfg *config.Config, db *store.DB, enc *store.Encryptor, log zerolog.Logger) (*Factory, error) {
	return &Factory{cfg: cfg, db: db, enc: enc, log: log}, nil
}

// For returns a connector for (customer, warehouseType). Caller must Close when done
// (typically held for the lifetime of the proxy.Handler).
func (f *Factory) For(ctx context.Context, customerID uuid.UUID, warehouseType string) (Connector, error) {
	conn, creds, err := f.db.GetConnectionForCustomer(ctx, f.enc, customerID, warehouseType)
	if err != nil {
		return nil, fmt.Errorf("load connection: %w", err)
	}
	subLog := f.log.With().Str("warehouse", warehouseType).Str("customer", customerID.String()).Logger()
	switch warehouseType {
	case "bigquery":
		return NewBigQuery(ctx, conn, creds, subLog)
	case "snowflake":
		return NewSnowflake(ctx, conn, creds, subLog)
	case "databricks":
		return NewDatabricks(ctx, conn, creds, subLog)
	case "redshift":
		return NewRedshift(ctx, conn, creds, subLog)
	case "clickhouse":
		return NewClickHouse(ctx, conn, creds, subLog)
	case "motherduck":
		return NewMotherDuck(ctx, conn, creds, subLog)
	case "fabric":
		return NewFabric(ctx, conn, creds, subLog)
	case "pg_source":
		return NewPGSource(ctx, conn, creds, subLog)
	case "mysql":
		return NewMySQL(ctx, conn, creds, subLog)
	case "trino":
		return NewTrino(ctx, conn, creds, subLog)
	case "mongo":
		return NewMongo(ctx, conn, creds, subLog)
	case "dynamo":
		return NewDynamo(ctx, conn, creds, subLog)
	case "local_duckdb":
		return NewLocalDuckDB(ctx, conn, creds, subLog)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedWarehouse, warehouseType)
	}
}
