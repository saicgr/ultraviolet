package lineage

import (
	"context"

	"github.com/google/uuid"
)

// maxRuntimeSQLBytes caps the SQL we attempt to parse for runtime lineage. A
// pathological 100k-literal IN-list or thousand-way UNION can pin the parser /
// AST walk; above this we skip extraction rather than risk an OOM or a long
// stall on the (already off-hot-path) consumer goroutine. Skips are logged by
// the caller as a deliberate, visible gap — never a silent drop of edges we
// could have produced.
const maxRuntimeSQLBytes = 256 * 1024

// RecordQuery extracts lineage from a single executed query and persists it,
// tagging the edges as runtime-observed. It is meant to be called OFF the proxy
// hot path (a background consumer) under a bounded context — never on the
// request goroutine. Returns (skipped, error): skipped=true means the SQL was
// too large to parse safely, which the caller should log.
func RecordQuery(ctx context.Context, ex *Extractor, w *Writer, customerID uuid.UUID, sql string) (skipped bool, err error) {
	if len(sql) > maxRuntimeSQLBytes {
		return true, nil
	}
	edges := ex.Extract(sql)
	if len(edges) == 0 {
		return false, nil
	}
	return false, w.Write(ctx, customerID, edges)
}
