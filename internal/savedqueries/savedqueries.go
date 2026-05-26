// Package savedqueries CRUDs the saved_query table — analyst-authored named
// queries with parameter substitution, favorites, and last-run tracking.
package savedqueries

import (
	"context"
	"encoding/json"
	"regexp"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// paramRE matches ${name} placeholders with [A-Za-z0-9_] names. Anything not
// matched (e.g. `${weird-name}`, `${1}`) is left untouched.
var paramRE = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// SubstituteParams replaces every `${name}` token in sql with params["name"].
// Unmatched placeholders are left as-is (callers can decide whether to reject).
// This is purely textual — callers must still send the resulting SQL through
// the normal validation pipeline before execution.
func SubstituteParams(sql string, params map[string]string) string {
	return paramRE.ReplaceAllStringFunc(sql, func(match string) string {
		name := match[2 : len(match)-1]
		if v, ok := params[name]; ok {
			return v
		}
		return match
	})
}

type SavedQuery struct {
	ID          uuid.UUID         `json:"id"`
	CustomerID  uuid.UUID         `json:"customer_id"`
	UserID      *uuid.UUID        `json:"user_id,omitempty"`
	Name        string            `json:"name"`
	Description *string           `json:"description,omitempty"`
	SQL         string            `json:"sql"`
	Params      map[string]any    `json:"params"`
	Favorite    bool              `json:"favorite"`
	LastRunAt   *string           `json:"last_run_at,omitempty"`
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Upsert(ctx context.Context, q *SavedQuery) (uuid.UUID, error) {
	params, _ := json.Marshal(q.Params)
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO saved_query (customer_id, user_id, name, description, sql, params, favorite)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (customer_id, user_id, name) DO UPDATE SET
			description = EXCLUDED.description,
			sql = EXCLUDED.sql,
			params = EXCLUDED.params,
			favorite = EXCLUDED.favorite,
			updated_at = now()
		RETURNING id`,
		q.CustomerID, q.UserID, q.Name, q.Description, q.SQL, params, q.Favorite).Scan(&id)
	return id, err
}

func (s *Store) List(ctx context.Context, customerID uuid.UUID, favoritesOnly bool) ([]SavedQuery, error) {
	q := `SELECT id, customer_id, user_id, name, description, sql, params, favorite, last_run_at::text
	      FROM saved_query WHERE customer_id = $1`
	if favoritesOnly {
		q += ` AND favorite = true`
	}
	q += ` ORDER BY favorite DESC, updated_at DESC LIMIT 200`
	rows, err := s.pool.Query(ctx, q, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SavedQuery
	for rows.Next() {
		var sq SavedQuery
		var paramsJSON []byte
		if err := rows.Scan(&sq.ID, &sq.CustomerID, &sq.UserID, &sq.Name, &sq.Description, &sq.SQL, &paramsJSON, &sq.Favorite, &sq.LastRunAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(paramsJSON, &sq.Params)
		out = append(out, sq)
	}
	return out, rows.Err()
}

func (s *Store) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM saved_query WHERE id = $1`, id)
	return err
}

func (s *Store) MarkRun(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `UPDATE saved_query SET last_run_at = now() WHERE id = $1`, id)
	return err
}
