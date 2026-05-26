package connectors

import (
	"context"
	"database/sql"
	"errors"

	sf "github.com/snowflakedb/gosnowflake"
)

// snowflakeArrowExec runs a query in Arrow-batch mode (gosnowflake's
// `WithArrowBatches` ctx flag). Returns the underlying ArrowBatch slice
// for caller-side encoding to PG DataRow frames.
//
// Phase-1: the helper is callable but not yet wired into the streaming path —
// the existing rows.Scan loop in connectors/snowflake.go works for
// hundreds-of-thousands-row results. Switch to Arrow when a customer query
// crosses that threshold; the dispatch lives one level up.
func snowflakeArrowExec(ctx context.Context, dbh *sql.DB, query string) (any, error) {
	actx := sf.WithArrowBatches(ctx)
	rows, err := dbh.QueryContext(actx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Pull the underlying *snowflake.Rows to get .GetArrowBatches(); not exposed
	// by the std database/sql interface so we walk through Driver().Conn() in
	// production. Stubbed return so the helper doesn't pin a heavyweight Arrow
	// dependency on every dev box.
	return nil, errors.New("snowflake Arrow batches: caller-side decode pending")
}
