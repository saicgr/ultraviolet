package githubapp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNoCustomer means no installation/repo mapping resolved to a tenant. The
// caller MUST treat this as "skip, do not process" — never as a default tenant.
var ErrNoCustomer = errors.New("no customer mapping for installation/repo")

// Store is the github_* + pr_* persistence layer.
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// ResolveCustomer maps a webhook event to a tenant, fail-closed. Precedence:
// a connected per-repo override, then the installation default. A genuine DB
// error is returned (so the caller can 5xx + let GitHub retry); a clean "no
// mapping" returns ErrNoCustomer (so the caller no-ops with 200).
func (s *Store) ResolveCustomer(ctx context.Context, installationID, repoID int64) (uuid.UUID, error) {
	var cid *uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT customer_id FROM github_repo WHERE repo_id = $1 AND connected = true`, repoID).Scan(&cid)
	if err == nil && cid != nil {
		return *cid, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}
	err = s.pool.QueryRow(ctx,
		`SELECT customer_id FROM github_installation WHERE installation_id = $1`, installationID).Scan(&cid)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNoCustomer
	}
	if err != nil {
		return uuid.Nil, err
	}
	if cid == nil {
		return uuid.Nil, ErrNoCustomer
	}
	return *cid, nil
}

// WebhookSeen records a delivery id and reports whether it is the FIRST time we
// have seen it. A replay (GitHub's at-least-once redelivery) returns false so
// the caller can no-op instead of re-running analysis.
func (s *Store) WebhookSeen(ctx context.Context, deliveryID, event string) (firstTime bool, err error) {
	if deliveryID == "" {
		return true, nil // no id to dedupe on; process
	}
	ct, err := s.pool.Exec(ctx,
		`INSERT INTO github_webhook_delivery (delivery_id, event) VALUES ($1,$2)
		 ON CONFLICT (delivery_id) DO NOTHING`, deliveryID, event)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() == 1, nil
}

func (s *Store) UpsertInstallation(ctx context.Context, installationID int64, login, accountType string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO github_installation (installation_id, account_login, account_type)
		 VALUES ($1,$2,$3)
		 ON CONFLICT (installation_id)
		 DO UPDATE SET account_login = EXCLUDED.account_login, suspended_at = NULL, updated_at = now()`,
		installationID, login, accountType)
	return err
}

func (s *Store) DeleteInstallation(ctx context.Context, installationID int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
	return err
}

func (s *Store) SuspendInstallation(ctx context.Context, installationID int64, suspended bool) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE github_installation SET suspended_at = CASE WHEN $2 THEN now() ELSE NULL END, updated_at = now()
		 WHERE installation_id = $1`, installationID, suspended)
	return err
}

func (s *Store) UpsertRepo(ctx context.Context, repoID, installationID int64, fullName string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO github_repo (repo_id, installation_id, repo_full_name)
		 VALUES ($1,$2,$3)
		 ON CONFLICT (repo_id)
		 DO UPDATE SET repo_full_name = EXCLUDED.repo_full_name, installation_id = EXCLUDED.installation_id`,
		repoID, installationID, fullName)
	return err
}

func (s *Store) RemoveRepo(ctx context.Context, repoID int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM github_repo WHERE repo_id = $1`, repoID)
	return err
}

// PRAnalysisRow is one persisted analysis (JSONB payloads pre-marshaled here).
type PRAnalysisRow struct {
	CustomerID     uuid.UUID
	InstallationID int64
	RepoFullName   string
	PRNumber       int
	HeadSHA        string
	PullRequestURL string
	Changes        any // []Change
	Hits           any // []impact.Hit
	DQRefs         any // []DQRef
	Conclusion     string
}

// UpsertPRAnalysis writes/refreshes the analysis for a head SHA (idempotent on
// re-push of the same commit).
func (s *Store) UpsertPRAnalysis(ctx context.Context, row PRAnalysisRow) error {
	changes, _ := json.Marshal(row.Changes)
	hits, _ := json.Marshal(row.Hits)
	dq, _ := json.Marshal(row.DQRefs)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO pr_analysis
		   (customer_id, installation_id, repo_full_name, pr_number, head_sha, pull_request_url, changes, hits, dq_refs, conclusion)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 ON CONFLICT (repo_full_name, pr_number, head_sha)
		 DO UPDATE SET changes = EXCLUDED.changes, hits = EXCLUDED.hits, dq_refs = EXCLUDED.dq_refs,
		               conclusion = EXCLUDED.conclusion, customer_id = EXCLUDED.customer_id`,
		row.CustomerID, row.InstallationID, row.RepoFullName, row.PRNumber, row.HeadSHA, row.PullRequestURL,
		changes, hits, dq, row.Conclusion)
	return err
}

// IsSuppressed reports whether the whole PR (finding_fp=”) is suppressed for
// this tenant. Fail-open on DB error: surfacing info beats silently hiding it.
func (s *Store) IsSuppressed(ctx context.Context, customerID uuid.UUID, repo string, pr int) bool {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pr_suppression
		 WHERE customer_id = $1 AND repo = $2 AND pr_number = $3 AND finding_fp = '')`,
		customerID, repo, pr).Scan(&exists)
	if err != nil {
		return false
	}
	return exists
}

// RecordSuppression records a tenant-scoped suppression. fp="" suppresses the
// whole PR; a specific fingerprint suppresses just that finding.
func (s *Store) RecordSuppression(ctx context.Context, customerID uuid.UUID, repo string, repoID int64, pr int, fp, by string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO pr_suppression (customer_id, repo, repo_id, pr_number, finding_fp, suppressed_by)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (customer_id, repo, pr_number, finding_fp) DO NOTHING`,
		customerID, repo, repoID, pr, fp, by)
	return err
}
