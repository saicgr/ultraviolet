// Package quotas implements per-user $ monthly quotas with optional hard-block.
// Companion to internal/budgets (workspace-wide); this is per-user-slug scoped.
//
// Schema: migrations/0007_quotas_and_cost_index.up.sql.
package quotas

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Quota is the per-user spend cap row.
type Quota struct {
	CustomerID    uuid.UUID `json:"customer_id"`
	UserSlug      string    `json:"user_slug"`
	MonthlyCapUSD float64   `json:"monthly_cap_usd"`
	HardBlock     bool      `json:"hard_block"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Quotas is the per-user quota service.
type Quotas struct{ pool *pgxpool.Pool }

// New constructs a Quotas service bound to the given pgx pool.
func New(pool *pgxpool.Pool) *Quotas { return &Quotas{pool: pool} }

// ErrNotFound indicates no quota row exists for the (customer, user_slug) pair.
var ErrNotFound = errors.New("quota not found")

// Upsert inserts or updates a quota row.
func (q *Quotas) Upsert(ctx context.Context, in Quota) error {
	if in.CustomerID == uuid.Nil || in.UserSlug == "" {
		return errors.New("customer_id and user_slug required")
	}
	if in.MonthlyCapUSD < 0 {
		return errors.New("monthly_cap_usd must be >= 0")
	}
	_, err := q.pool.Exec(ctx, `
		INSERT INTO user_quota (customer_id, user_slug, monthly_cap_usd, hard_block)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (customer_id, user_slug) DO UPDATE SET
			monthly_cap_usd = EXCLUDED.monthly_cap_usd,
			hard_block      = EXCLUDED.hard_block,
			updated_at      = now()`,
		in.CustomerID, in.UserSlug, in.MonthlyCapUSD, in.HardBlock)
	return err
}

// List returns every quota row for a customer.
func (q *Quotas) List(ctx context.Context, customerID uuid.UUID) ([]Quota, error) {
	rows, err := q.pool.Query(ctx, `
		SELECT customer_id, user_slug, monthly_cap_usd, hard_block, created_at, updated_at
		FROM user_quota WHERE customer_id = $1 ORDER BY user_slug`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Quota{}
	for rows.Next() {
		var qu Quota
		if err := rows.Scan(&qu.CustomerID, &qu.UserSlug, &qu.MonthlyCapUSD, &qu.HardBlock, &qu.CreatedAt, &qu.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, qu)
	}
	return out, rows.Err()
}

// Get loads one (customer_id, user_slug) row. Returns ErrNotFound if absent.
func (q *Quotas) Get(ctx context.Context, customerID uuid.UUID, userSlug string) (Quota, error) {
	var qu Quota
	row := q.pool.QueryRow(ctx, `
		SELECT customer_id, user_slug, monthly_cap_usd, hard_block, created_at, updated_at
		FROM user_quota WHERE customer_id = $1 AND user_slug = $2`, customerID, userSlug)
	err := row.Scan(&qu.CustomerID, &qu.UserSlug, &qu.MonthlyCapUSD, &qu.HardBlock, &qu.CreatedAt, &qu.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Quota{}, ErrNotFound
	}
	return qu, err
}

// Delete removes a quota row. No-op if absent.
func (q *Quotas) Delete(ctx context.Context, customerID uuid.UUID, userSlug string) error {
	_, err := q.pool.Exec(ctx, `DELETE FROM user_quota WHERE customer_id=$1 AND user_slug=$2`, customerID, userSlug)
	return err
}

// MonthSpend returns the user's MTD actual spend in USD. Until query_log carries
// user_slug attribution (Phase-7), this falls back to the customer-wide MTD
// spend so the gate is never silently bypassed — we over-account, never under.
// TODO(phase-7): add query_log.user_slug column + filter here.
func (q *Quotas) MonthSpend(ctx context.Context, customerID uuid.UUID, userSlug string) (float64, error) {
	monthStart := monthStartUTC(time.Now())
	var spend float64
	err := q.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(actual_cost_usd), 0)
		FROM query_log
		WHERE customer_id = $1 AND started_at >= $2
		  AND actual_cost_usd IS NOT NULL`, customerID, monthStart).Scan(&spend)
	return spend, err
}

// Check returns (allowed, reason, err) for a candidate query of `estimatedCostUSD`.
// Semantics:
//   - No quota row → allowed (no cap configured).
//   - hard_block=false → always allowed, reason notes soft state.
//   - hard_block=true and current+estimated > cap → not allowed.
func (q *Quotas) Check(ctx context.Context, customerID uuid.UUID, userSlug string, estimatedCostUSD float64) (bool, string, error) {
	qu, err := q.Get(ctx, customerID, userSlug)
	if errors.Is(err, ErrNotFound) {
		return true, "no quota configured", nil
	}
	if err != nil {
		return false, "", err
	}
	spend, err := q.MonthSpend(ctx, customerID, userSlug)
	if err != nil {
		return false, "", err
	}
	projected := spend + estimatedCostUSD
	if qu.MonthlyCapUSD > 0 && projected > qu.MonthlyCapUSD {
		if qu.HardBlock {
			return false, fmt.Sprintf("hard quota exceeded: projected $%.2f > cap $%.2f", projected, qu.MonthlyCapUSD), nil
		}
		return true, fmt.Sprintf("soft quota exceeded (warn only): projected $%.2f > cap $%.2f", projected, qu.MonthlyCapUSD), nil
	}
	return true, "ok", nil
}

// CapFor returns the configured monthly_cap_usd for (customer, user_slug), or 0 if absent.
func (q *Quotas) CapFor(ctx context.Context, customerID uuid.UUID, userSlug string) (float64, bool, error) {
	qu, err := q.Get(ctx, customerID, userSlug)
	if errors.Is(err, ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return qu.MonthlyCapUSD, true, nil
}

func monthStartUTC(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
}
