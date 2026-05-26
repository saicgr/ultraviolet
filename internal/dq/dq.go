// Package dq runs Great-Expectations / dbt-test-class checks and persists
// pass/fail to dq_result. Failed tests block snapshot publish (the syncer reads
// the latest result row before committing).
package dq

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Result struct {
	CustomerID uuid.UUID
	TableFQN   string
	TestName   string
	Status     string // pass|fail|skip|error
	Details    map[string]any
}

type Recorder struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Recorder { return &Recorder{pool: pool} }

func (r *Recorder) Record(ctx context.Context, res Result) error {
	var details []byte
	if res.Details != nil {
		details, _ = json.Marshal(res.Details)
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO dq_result (customer_id, table_fqn, test_name, status, details)
		VALUES ($1,$2,$3,$4,$5)`,
		res.CustomerID, res.TableFQN, res.TestName, res.Status, details)
	return err
}

// LatestStatus returns the most-recent status for (customer, table, test). The
// syncer calls this before committing a new snapshot; "fail" blocks publish.
func (r *Recorder) LatestStatus(ctx context.Context, customerID uuid.UUID, tableFQN string) (string, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT status FROM dq_result WHERE customer_id = $1 AND table_fqn = $2
		 ORDER BY created_at DESC LIMIT 1`, customerID, tableFQN)
	var s string
	if err := row.Scan(&s); err != nil {
		return "pass", nil // no test = no block
	}
	return s, nil
}
