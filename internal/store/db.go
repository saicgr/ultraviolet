package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB is a thin wrapper over pgxpool with the queries the proxy + API actually use.
// We deliberately avoid sqlc codegen at this stage to keep deps minimal; the queries
// here are small + stable. If/when query surface explodes, switch to sqlc.yaml-driven gen.
type DB struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, dsn string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MaxConnLifetime = 30 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &DB{pool: pool}, nil
}

func (db *DB) Close() {
	if db.pool != nil {
		db.pool.Close()
	}
}

func (db *DB) Pool() *pgxpool.Pool { return db.pool }

// ----- Domain types (mirror migrations/0001) -----

type Customer struct {
	ID          uuid.UUID `json:"id"`
	Slug        string    `json:"slug"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

type Connection struct {
	ID            uuid.UUID `json:"id"`
	CustomerID    uuid.UUID `json:"customer_id"`
	WarehouseType string    `json:"warehouse_type"`
	Name          string    `json:"name"`
	StorageMode   string    `json:"storage_mode"`
	S3Bucket      *string   `json:"s3_bucket,omitempty"`
	S3Prefix      *string   `json:"s3_prefix,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type APIKey struct {
	ID          uuid.UUID  `json:"id"`
	CustomerID  uuid.UUID  `json:"customer_id"`
	KeyPrefix   string     `json:"key_prefix"`
	Description *string    `json:"description,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}

type SyncedTable struct {
	ID              uuid.UUID  `json:"id"`
	ConnectionID    uuid.UUID  `json:"connection_id"`
	SchemaName      string     `json:"schema_name"`
	TableName       string     `json:"table_name"`
	SyncMode        string     `json:"sync_mode"`
	WatermarkColumn *string    `json:"watermark_column,omitempty"`
	LastSyncAt      *time.Time `json:"last_sync_at,omitempty"`
	LastSnapshotID  *int64     `json:"last_snapshot_id,omitempty"`
	LastWatermark   *string    `json:"last_watermark,omitempty"`
	IcebergPath     *string    `json:"iceberg_path,omitempty"`
	RowCount        int64      `json:"row_count"`
	State           string     `json:"state"`
	LastError       *string    `json:"last_error,omitempty"`
}

// ----- Customers -----

func (db *DB) CreateCustomer(ctx context.Context, slug, displayName string) (*Customer, error) {
	row := db.pool.QueryRow(ctx,
		`INSERT INTO customers (slug, display_name) VALUES ($1,$2)
		 RETURNING id, slug, display_name, created_at`, slug, displayName)
	c := &Customer{}
	if err := row.Scan(&c.ID, &c.Slug, &c.DisplayName, &c.CreatedAt); err != nil {
		return nil, err
	}
	return c, nil
}

func (db *DB) GetCustomerBySlug(ctx context.Context, slug string) (*Customer, error) {
	row := db.pool.QueryRow(ctx,
		`SELECT id, slug, display_name, created_at FROM customers WHERE slug = $1`, slug)
	c := &Customer{}
	if err := row.Scan(&c.ID, &c.Slug, &c.DisplayName, &c.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return c, nil
}

func (db *DB) ListCustomers(ctx context.Context) ([]Customer, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT id, slug, display_name, created_at FROM customers ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Customer
	for rows.Next() {
		var c Customer
		if err := rows.Scan(&c.ID, &c.Slug, &c.DisplayName, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ----- API keys -----

func (db *DB) CreateAPIKey(ctx context.Context, customerID uuid.UUID, apiKey, prefix string, description *string) (*APIKey, error) {
	hash := HashAPIKey(apiKey)
	row := db.pool.QueryRow(ctx,
		`INSERT INTO api_keys (customer_id, key_hash, key_prefix, description)
		 VALUES ($1,$2,$3,$4) RETURNING id, customer_id, key_prefix, description, created_at, last_used_at, revoked_at`,
		customerID, hash, prefix, description)
	k := &APIKey{}
	if err := row.Scan(&k.ID, &k.CustomerID, &k.KeyPrefix, &k.Description, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt); err != nil {
		return nil, err
	}
	return k, nil
}

// CustomerIDByAPIKeyHash resolves a sha256(api_key) hash to its customer id, or
// ErrNotFound when no live row matches. Used by /api/v1/auth/login.
func (db *DB) CustomerIDByAPIKeyHash(ctx context.Context, hash []byte) (uuid.UUID, error) {
	row := db.pool.QueryRow(ctx,
		`SELECT customer_id FROM api_keys WHERE key_hash = $1 AND revoked_at IS NULL`, hash)
	var id uuid.UUID
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, err
	}
	return id, nil
}

// LookupAPIKey returns (api_key_id, customer_id, customer_slug) for a plaintext key,
// or ErrNotFound. Bumps last_used_at on success.
func (db *DB) LookupAPIKey(ctx context.Context, apiKey string) (uuid.UUID, uuid.UUID, string, error) {
	hash := HashAPIKey(apiKey)
	row := db.pool.QueryRow(ctx,
		`SELECT k.id, k.customer_id, c.slug FROM api_keys k
		 JOIN customers c ON c.id = k.customer_id
		 WHERE k.key_hash = $1 AND k.revoked_at IS NULL`, hash)
	var keyID, customerID uuid.UUID
	var slug string
	if err := row.Scan(&keyID, &customerID, &slug); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, uuid.Nil, "", ErrNotFound
		}
		return uuid.Nil, uuid.Nil, "", err
	}
	_, _ = db.pool.Exec(ctx, `UPDATE api_keys SET last_used_at = now() WHERE id = $1`, keyID)
	return keyID, customerID, slug, nil
}

func (db *DB) ListAPIKeys(ctx context.Context, customerID uuid.UUID) ([]APIKey, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT id, customer_id, key_prefix, description, created_at, last_used_at, revoked_at
		 FROM api_keys WHERE customer_id = $1 ORDER BY created_at DESC`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKey
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.CustomerID, &k.KeyPrefix, &k.Description, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (db *DB) RevokeAPIKey(ctx context.Context, id uuid.UUID) error {
	_, err := db.pool.Exec(ctx, `UPDATE api_keys SET revoked_at = now() WHERE id = $1`, id)
	return err
}

// ----- Connections -----

func (db *DB) CreateConnection(ctx context.Context, customerID uuid.UUID, warehouseType, name string, ciphertext, nonce []byte, storageMode string, s3Bucket, s3Prefix *string) (*Connection, error) {
	row := db.pool.QueryRow(ctx,
		`INSERT INTO connections (customer_id, warehouse_type, name, credentials_ciphertext, credentials_nonce, storage_mode, s3_bucket, s3_prefix)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 RETURNING id, customer_id, warehouse_type, name, storage_mode, s3_bucket, s3_prefix, created_at`,
		customerID, warehouseType, name, ciphertext, nonce, storageMode, s3Bucket, s3Prefix)
	c := &Connection{}
	if err := row.Scan(&c.ID, &c.CustomerID, &c.WarehouseType, &c.Name, &c.StorageMode, &c.S3Bucket, &c.S3Prefix, &c.CreatedAt); err != nil {
		return nil, err
	}
	return c, nil
}

// GetConnectionForCustomer fetches the connection for (customer, warehouse_type) and
// returns the *decrypted* credentials JSON. Caller is responsible for not logging it.
func (db *DB) GetConnectionForCustomer(ctx context.Context, enc *Encryptor, customerID uuid.UUID, warehouseType string) (*Connection, []byte, error) {
	row := db.pool.QueryRow(ctx,
		`SELECT id, customer_id, warehouse_type, name, storage_mode, s3_bucket, s3_prefix, created_at,
		        credentials_ciphertext, credentials_nonce
		 FROM connections WHERE customer_id = $1 AND warehouse_type = $2
		 ORDER BY created_at DESC LIMIT 1`, customerID, warehouseType)
	c := &Connection{}
	var ct, nonce []byte
	if err := row.Scan(&c.ID, &c.CustomerID, &c.WarehouseType, &c.Name, &c.StorageMode, &c.S3Bucket, &c.S3Prefix, &c.CreatedAt, &ct, &nonce); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	pt, err := enc.Decrypt(ct, nonce)
	if err != nil {
		return nil, nil, err
	}
	return c, pt, nil
}

func (db *DB) ListConnections(ctx context.Context, customerID uuid.UUID) ([]Connection, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT id, customer_id, warehouse_type, name, storage_mode, s3_bucket, s3_prefix, created_at
		 FROM connections WHERE customer_id = $1 ORDER BY created_at DESC`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Connection
	for rows.Next() {
		var c Connection
		if err := rows.Scan(&c.ID, &c.CustomerID, &c.WarehouseType, &c.Name, &c.StorageMode, &c.S3Bucket, &c.S3Prefix, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ----- Synced tables -----

func (db *DB) UpsertSyncedTable(ctx context.Context, t SyncedTable) (uuid.UUID, error) {
	row := db.pool.QueryRow(ctx,
		`INSERT INTO synced_tables (connection_id, schema_name, table_name, sync_mode, watermark_column, iceberg_path, state)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (connection_id, schema_name, table_name)
		 DO UPDATE SET sync_mode = EXCLUDED.sync_mode, watermark_column = EXCLUDED.watermark_column, updated_at = now()
		 RETURNING id`,
		t.ConnectionID, t.SchemaName, t.TableName, t.SyncMode, t.WatermarkColumn, t.IcebergPath, nullDefault(t.State, "pending"))
	var id uuid.UUID
	if err := row.Scan(&id); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

func (db *DB) ListSyncedTables(ctx context.Context, connectionID uuid.UUID) ([]SyncedTable, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT id, connection_id, schema_name, table_name, sync_mode, watermark_column,
		        last_sync_at, last_snapshot_id, last_watermark, iceberg_path, row_count, state, last_error
		 FROM synced_tables WHERE connection_id = $1 ORDER BY schema_name, table_name`, connectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncedTable
	for rows.Next() {
		var t SyncedTable
		if err := rows.Scan(&t.ID, &t.ConnectionID, &t.SchemaName, &t.TableName, &t.SyncMode, &t.WatermarkColumn,
			&t.LastSyncAt, &t.LastSnapshotID, &t.LastWatermark, &t.IcebergPath, &t.RowCount, &t.State, &t.LastError); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (db *DB) MarkSyncedTableState(ctx context.Context, id uuid.UUID, state string, lastErr *string, snapshotID *int64, watermark *string, rowCount int64) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE synced_tables
		   SET state = $2, last_error = $3, last_snapshot_id = COALESCE($4, last_snapshot_id),
		       last_watermark = COALESCE($5, last_watermark), row_count = $6,
		       last_sync_at = now(), updated_at = now()
		 WHERE id = $1`,
		id, state, lastErr, snapshotID, watermark, rowCount)
	return err
}

// LookupSyncedTable returns the sync state for (customer, warehouse, schema, table).
// Used by router/freshness to decide DuckDB vs warehouse.
func (db *DB) LookupSyncedTable(ctx context.Context, customerID uuid.UUID, warehouseType, schema, table string) (*SyncedTable, error) {
	row := db.pool.QueryRow(ctx,
		`SELECT t.id, t.connection_id, t.schema_name, t.table_name, t.sync_mode, t.watermark_column,
		        t.last_sync_at, t.last_snapshot_id, t.last_watermark, t.iceberg_path, t.row_count, t.state, t.last_error
		 FROM synced_tables t JOIN connections c ON c.id = t.connection_id
		 WHERE c.customer_id = $1 AND c.warehouse_type = $2 AND t.schema_name = $3 AND t.table_name = $4`,
		customerID, warehouseType, schema, table)
	var t SyncedTable
	if err := row.Scan(&t.ID, &t.ConnectionID, &t.SchemaName, &t.TableName, &t.SyncMode, &t.WatermarkColumn,
		&t.LastSyncAt, &t.LastSnapshotID, &t.LastWatermark, &t.IcebergPath, &t.RowCount, &t.State, &t.LastError); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &t, nil
}

// ----- Query log -----

type QueryLogEntry struct {
	CustomerID       uuid.UUID
	ConnectionID     *uuid.UUID
	APIKeyID         *uuid.UUID
	QueryHash        string
	NormalizedSQL    string
	RouteDecision    string
	FallbackReason   *string
	DurationMS       int
	RowsReturned     int64
	BytesScanned     *int64
	EstimatedCostUSD *float64
	ErrorCode        *string
}

func (db *DB) InsertQueryLog(ctx context.Context, e QueryLogEntry) error {
	_, err := db.pool.Exec(ctx,
		`INSERT INTO query_log (customer_id, connection_id, api_key_id, query_hash, normalized_sql,
		                        route_decision, fallback_reason, duration_ms, rows_returned, bytes_scanned,
		                        estimated_cost_usd, error_code)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		e.CustomerID, e.ConnectionID, e.APIKeyID, e.QueryHash, e.NormalizedSQL,
		e.RouteDecision, e.FallbackReason, e.DurationMS, e.RowsReturned, e.BytesScanned,
		e.EstimatedCostUSD, e.ErrorCode)
	return err
}

// ----- Helpers -----

var ErrNotFound = errors.New("not found")

func nullDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
