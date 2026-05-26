// Package notifications is the in-app inbox: alerts, @mentions, sync failures,
// agent suggestions, dashboard shares. Read by the Inbox UI badge + page.
package notifications

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Kind string

const (
	KindMention       Kind = "mention"
	KindAlert         Kind = "alert"
	KindAgent         Kind = "agent"
	KindSyncFail      Kind = "sync_fail"
	KindShare         Kind = "dashboard_share"
	KindBudgetWarn    Kind = "budget_warn"
	KindAnomaly       Kind = "anomaly"
	KindScheduledRun  Kind = "scheduled_run"
)

type Notification struct {
	ID         int64      `json:"id"`
	CustomerID uuid.UUID  `json:"customer_id"`
	UserID     *uuid.UUID `json:"user_id,omitempty"`
	Kind       Kind       `json:"kind"`
	Title      string     `json:"title"`
	Body       string     `json:"body"`
	Link       string     `json:"link"`
	ReadAt     *time.Time `json:"read_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type Inbox struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Inbox { return &Inbox{pool: pool} }

func (i *Inbox) Push(ctx context.Context, n Notification) error {
	_, err := i.pool.Exec(ctx, `
		INSERT INTO notification (customer_id, user_id, kind, title, body, link)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		n.CustomerID, n.UserID, n.Kind, n.Title, n.Body, n.Link)
	return err
}

func (i *Inbox) List(ctx context.Context, customerID uuid.UUID, userID *uuid.UUID, unreadOnly bool, limit int) ([]Notification, error) {
	if limit <= 0 {
		limit = 50
	}
	q := `SELECT id, customer_id, user_id, kind, title, COALESCE(body,''), COALESCE(link,''), read_at, created_at
	      FROM notification WHERE customer_id = $1 AND (user_id IS NULL OR user_id = $2)`
	if unreadOnly {
		q += ` AND read_at IS NULL`
	}
	q += ` ORDER BY created_at DESC LIMIT $3`
	rows, err := i.pool.Query(ctx, q, customerID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.CustomerID, &n.UserID, &n.Kind, &n.Title, &n.Body, &n.Link, &n.ReadAt, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (i *Inbox) MarkRead(ctx context.Context, id int64) error {
	_, err := i.pool.Exec(ctx, `UPDATE notification SET read_at = now() WHERE id = $1`, id)
	return err
}

func (i *Inbox) MarkAllRead(ctx context.Context, customerID uuid.UUID, userID *uuid.UUID) error {
	_, err := i.pool.Exec(ctx,
		`UPDATE notification SET read_at = now()
		 WHERE customer_id = $1 AND (user_id IS NULL OR user_id = $2) AND read_at IS NULL`,
		customerID, userID)
	return err
}

func (i *Inbox) UnreadCount(ctx context.Context, customerID uuid.UUID, userID *uuid.UUID) (int64, error) {
	var n int64
	err := i.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM notification
		 WHERE customer_id = $1 AND (user_id IS NULL OR user_id = $2) AND read_at IS NULL`,
		customerID, userID).Scan(&n)
	return n, err
}
