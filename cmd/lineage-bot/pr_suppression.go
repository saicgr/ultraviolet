package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// isSuppressed returns true if `/uv suppress` was recorded for this PR. The
// caller uses this to short-circuit comment posting/editing so the bot stays
// silent for the lifetime of the PR.
func isSuppressed(ctx context.Context, pool *pgxpool.Pool, repo string, prNumber int) bool {
	if pool == nil {
		return false
	}
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pr_suppression WHERE repo = $1 AND pr_number = $2)`,
		repo, prNumber,
	).Scan(&exists)
	if err != nil {
		// Fail open: don't suppress on DB error — surface info is more useful
		// than silent drop, and the operator will see the warn log upstream.
		return false
	}
	return exists
}

// recordSuppression upserts a suppression row. Idempotent: re-issuing
// `/uv suppress` is a no-op on the suppression set.
func recordSuppression(ctx context.Context, pool *pgxpool.Pool, repo string, prNumber int, by string) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO pr_suppression (repo, pr_number, suppressed_by)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (repo, pr_number) DO NOTHING`,
		repo, prNumber, by,
	)
	return err
}
