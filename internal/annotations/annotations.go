// Package annotations is the comments + chart-overlay layer. Annotations
// can target dashboards, individual tiles, lineage nodes, queries, tables,
// or named metrics. `pinned_at_event` lets a chart annotation overlay a
// vertical line at a point in time ("deploy on Tue 14:00").
package annotations

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Annotation struct {
	ID            uuid.UUID  `json:"id"`
	CustomerID    uuid.UUID  `json:"customer_id"`
	UserID        *uuid.UUID `json:"user_id,omitempty"`
	TargetType    string     `json:"target_type"`
	TargetID      string     `json:"target_id"`
	RowKey        *string    `json:"row_key,omitempty"`
	Body          string     `json:"body"`
	PinnedAtEvent *time.Time `json:"pinned_at_event,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Create(ctx context.Context, a Annotation) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO annotation (customer_id, user_id, target_type, target_id, row_key, body, pinned_at_event)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		a.CustomerID, a.UserID, a.TargetType, a.TargetID, a.RowKey, a.Body, a.PinnedAtEvent).Scan(&id)
	return id, err
}

func (s *Store) ForTarget(ctx context.Context, customerID uuid.UUID, targetType, targetID string) ([]Annotation, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, customer_id, user_id, target_type, target_id, row_key, body, pinned_at_event, created_at
		 FROM annotation WHERE customer_id = $1 AND target_type = $2 AND target_id = $3
		 ORDER BY COALESCE(pinned_at_event, created_at) ASC`,
		customerID, targetType, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Annotation
	for rows.Next() {
		var a Annotation
		if err := rows.Scan(&a.ID, &a.CustomerID, &a.UserID, &a.TargetType, &a.TargetID, &a.RowKey, &a.Body, &a.PinnedAtEvent, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAuthor returns the user_id (annotation author) for a single annotation.
// Used by the REST DELETE handler to authorize revocation: only the author
// or an admin may delete.
func (s *Store) GetAuthor(ctx context.Context, id uuid.UUID) (*uuid.UUID, uuid.UUID, error) {
	var author *uuid.UUID
	var customerID uuid.UUID
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, customer_id FROM annotation WHERE id = $1`, id).Scan(&author, &customerID)
	return author, customerID, err
}

func (s *Store) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM annotation WHERE id = $1`, id)
	return err
}
