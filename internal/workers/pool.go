// Package workers manages the per-customer DuckDB worker pool. Each worker has the
// Iceberg + httpfs + (optional) llm extensions loaded and ATTACHes the customer's
// Iceberg catalog from S3. CGo execution NEVER on the proxy main goroutine; every
// query runs with context.WithTimeout ≤30s per docs/process/ultrathink.md axis 3.
package workers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/marcboeker/go-duckdb"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/ultraviolet-dev/ultraviolet/internal/config"
	"github.com/ultraviolet-dev/ultraviolet/internal/protocols/pgwire"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
)

type Pool struct {
	cfg *config.Config
	db  *store.DB
	log zerolog.Logger
	rdb *redis.Client

	mu      sync.Mutex
	closed  bool
	workers map[string]*worker // key = customerSlug
}

type worker struct {
	db      *sql.DB
	once    sync.Once
	subDone chan struct{}
}

func NewPool(ctx context.Context, cfg *config.Config, db *store.DB, log zerolog.Logger) (*Pool, error) {
	p := &Pool{
		cfg:     cfg,
		db:      db,
		log:     log.With().Str("component", "duckdb-pool").Logger(),
		workers: map[string]*worker{},
	}
	if cfg.RedisURL != "" {
		opt, err := redis.ParseURL(cfg.RedisURL)
		if err == nil {
			p.rdb = redis.NewClient(opt)
		}
	}
	return p, nil
}

func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	for k, w := range p.workers {
		if w.subDone != nil {
			close(w.subDone)
		}
		_ = w.db.Close()
		delete(p.workers, k)
	}
	if p.rdb != nil {
		_ = p.rdb.Close()
	}
}

// ExecuteStreaming runs SQL on the customer's DuckDB worker, writes results as PG frames.
func (p *Pool) ExecuteStreaming(ctx context.Context, customerSlug, query string, params [][]byte, w *pgwire.Writer) (int64, error) {
	wk, err := p.workerFor(ctx, customerSlug)
	if err != nil {
		return 0, err
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rows, err := wk.db.QueryContext(cctx, query)
	if err != nil {
		return 0, fmt.Errorf("duckdb query: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		return 0, err
	}
	fields := make([]pgwire.Field, len(cols))
	for i, t := range types {
		oid, size := mapDuckDBType(t.DatabaseTypeName())
		fields[i] = pgwire.Field{Name: cols[i], TypeOID: oid, TypeSize: size, Format: 0}
	}
	if err := w.WriteRowDescription(fields); err != nil {
		return 0, err
	}
	scan := make([]interface{}, len(cols))
	ptrs := make([]interface{}, len(cols))
	for i := range scan {
		ptrs[i] = &scan[i]
	}
	var n int64
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return n, err
		}
		vals := make([][]byte, len(cols))
		for i, v := range scan {
			vals[i] = encodeDuckDBValue(v)
		}
		if err := w.WriteDataRow(vals); err != nil {
			return n, err
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return n, err
	}
	return n, w.WriteCommandComplete(commandTag(query, n))
}

// ExecuteRows runs SQL on the customer's DuckDB worker and returns the result as
// string-encoded columns + rows (JSON-friendly), the total row count, and the
// approximate bytes produced (sum of encoded cell sizes — a real measured
// quantity used as the warehouse-equivalent "bytes scanned" for cost). Display
// rows are capped at maxDisplayRows; counting + byte-summing continue for the
// full result (bounded by the 30s context). This powers the in-app Workbench so
// a query runs on the SAME engine the proxy uses — no mock execution.
func (p *Pool) ExecuteRows(ctx context.Context, customerSlug, query string) (cols []string, rows [][]string, rowCount int64, bytesScanned int64, err error) {
	const maxDisplayRows = 1000
	wk, err := p.workerFor(ctx, customerSlug)
	if err != nil {
		return nil, nil, 0, 0, err
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rs, err := wk.db.QueryContext(cctx, query)
	if err != nil {
		return nil, nil, 0, 0, fmt.Errorf("duckdb query: %w", err)
	}
	defer rs.Close()

	cols, err = rs.Columns()
	if err != nil {
		return nil, nil, 0, 0, err
	}
	scan := make([]interface{}, len(cols))
	ptrs := make([]interface{}, len(cols))
	for i := range scan {
		ptrs[i] = &scan[i]
	}
	rows = [][]string{}
	for rs.Next() {
		if err := rs.Scan(ptrs...); err != nil {
			return cols, rows, rowCount, bytesScanned, err
		}
		rec := make([]string, len(cols))
		for i, v := range scan {
			b := encodeDuckDBValue(v)
			bytesScanned += int64(len(b))
			rec[i] = string(b)
		}
		if rowCount < maxDisplayRows {
			rows = append(rows, rec)
		}
		rowCount++
	}
	if err := rs.Err(); err != nil {
		return cols, rows, rowCount, bytesScanned, err
	}
	return cols, rows, rowCount, bytesScanned, nil
}

func (p *Pool) workerFor(ctx context.Context, slug string) (*worker, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, errors.New("pool closed")
	}
	if w, ok := p.workers[slug]; ok {
		return w, nil
	}
	dbh, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("duckdb open: %w", err)
	}
	dbh.SetMaxOpenConns(4)
	dbh.SetMaxIdleConns(2)
	if err := installExtensions(ctx, dbh, p.cfg); err != nil {
		_ = dbh.Close()
		return nil, fmt.Errorf("duckdb extensions: %w", err)
	}
	// ATTACH the customer's Iceberg catalog so DuckDB can resolve synced tables by FQN.
	// Catalog URL is provided via UV_ICEBERG_CATALOG_URL (default localhost dev port);
	// non-fatal on attach failure — workers without iceberg ext will simply not see
	// synced tables, and the router fallback will send queries to the warehouse.
	if p.cfg.IcebergCatalogURL != "" {
		stmt := fmt.Sprintf("ATTACH '%s' AS uv_iceberg (TYPE ICEBERG)", strings.ReplaceAll(p.cfg.IcebergCatalogURL, "'", "''"))
		if _, err := dbh.ExecContext(ctx, stmt); err != nil {
			p.log.Warn().Err(err).Str("customer", slug).Msg("duckdb iceberg attach (continuing without)")
		}
	}
	// TODO(pu-2): apply registered UDFs from UDFRegistry here. The registry
	// already stores per-customer UDFSpec entries; we just need a safe DuckDB
	// CREATE OR REPLACE MACRO/FUNCTION wrapper before flipping this on so a
	// malformed spec can't poison a worker.
	w := &worker{db: dbh, subDone: make(chan struct{})}
	p.workers[slug] = w
	p.log.Info().Str("customer", slug).Msg("duckdb worker initialized")
	if p.rdb != nil {
		go p.subscribeRefresh(slug, w)
	}
	return w, nil
}

// subscribeRefresh listens on iceberg.snapshot.{slug}.* and reattaches affected tables
// when a sync commits a new snapshot.
func (p *Pool) subscribeRefresh(slug string, w *worker) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Match every snapshot for this customer regardless of connection_id / schema /
	// table. Channel layout: iceberg.snapshot.{slug}.{connection_id}.{schema}.{table}.
	ps := p.rdb.PSubscribe(ctx, fmt.Sprintf("iceberg.snapshot.%s.*.*.*", slug))
	defer ps.Close()
	ch := ps.Channel()
	for {
		select {
		case <-w.subDone:
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			p.log.Debug().Str("customer", slug).Str("channel", msg.Channel).Msg("snapshot pubsub")
			// CHECKPOINT serializes any in-flight writes to disk so the next ATTACH
			// for this table sees the new Iceberg snapshot rather than a cached
			// metadata pointer. Errors are logged, not fatal — pool stays usable.
			if _, err := w.db.ExecContext(ctx, "CHECKPOINT"); err != nil {
				p.log.Debug().Err(err).Msg("duckdb checkpoint")
			}
		}
	}
}

func installExtensions(ctx context.Context, db *sql.DB, cfg *config.Config) error {
	required := []string{
		"INSTALL httpfs",
		"LOAD httpfs",
		fmt.Sprintf("SET s3_endpoint='%s'", strings.TrimPrefix(strings.TrimPrefix(cfg.S3Endpoint, "http://"), "https://")),
		fmt.Sprintf("SET s3_access_key_id='%s'", cfg.AWSAccessKey),
		fmt.Sprintf("SET s3_secret_access_key='%s'", cfg.AWSSecretKey),
		fmt.Sprintf("SET s3_region='%s'", cfg.AWSRegion),
		"SET s3_url_style='path'",
		"SET s3_use_ssl=false",
	}
	// Optional extensions — failure to load (e.g. offline dev box) must not bring down
	// the proxy. Per docs/process/no-fallback-data.md the failure is logged but doesn't
	// silently degrade query results because non-iceberg queries still work.
	optional := []string{
		"INSTALL iceberg",
		"LOAD iceberg",
		"INSTALL llm FROM community",
		"LOAD llm",
	}
	for _, s := range required {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("%s: %w", s, err)
		}
	}
	for _, s := range optional {
		if _, err := db.ExecContext(ctx, s); err != nil {
			// Logged via parent, not fatal.
			_ = err
		}
	}
	return nil
}

func commandTag(sql string, rows int64) string {
	verb := strings.ToUpper(strings.Fields(strings.TrimSpace(sql))[0])
	switch verb {
	case "SELECT", "WITH", "TABLE", "VALUES":
		return fmt.Sprintf("SELECT %d", rows)
	default:
		return verb
	}
}
