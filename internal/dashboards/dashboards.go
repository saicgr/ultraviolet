// Package dashboards CRUDs the dashboard table + serves tiles. Tiles are JSON
// blobs (see Tile struct) so the frontend renders without server-side rendering
// in Phase 1.
package dashboards

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Tile struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	ChartType   string         `json:"chart_type"` // line|bar|pie|area|table|scorecard|map|heatmap|funnel|treemap
	SemanticRef string         `json:"semantic_ref,omitempty"`
	SQL         string         `json:"sql,omitempty"`
	X           int            `json:"x"`
	Y           int            `json:"y"`
	W           int            `json:"w"`
	H           int            `json:"h"`
	Config      map[string]any `json:"config,omitempty"`
}

type Dashboard struct {
	ID         uuid.UUID `json:"id"`
	CustomerID uuid.UUID `json:"customer_id"`
	Name       string    `json:"name"`
	Tiles      []Tile    `json:"tiles"`
	IsPublic   bool      `json:"is_public"`
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Create(ctx context.Context, d *Dashboard) (uuid.UUID, error) {
	tiles, _ := json.Marshal(d.Tiles)
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO dashboard (customer_id, name, tiles, is_public)
		VALUES ($1,$2,$3,$4) RETURNING id`,
		d.CustomerID, d.Name, tiles, d.IsPublic).Scan(&id)
	return id, err
}

func (s *Store) Get(ctx context.Context, id uuid.UUID) (*Dashboard, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, customer_id, name, tiles, is_public FROM dashboard WHERE id = $1`, id)
	var d Dashboard
	var tilesJSON []byte
	if err := row.Scan(&d.ID, &d.CustomerID, &d.Name, &tilesJSON, &d.IsPublic); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(tilesJSON, &d.Tiles)
	return &d, nil
}

func (s *Store) List(ctx context.Context, customerID uuid.UUID) ([]Dashboard, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, customer_id, name, tiles, is_public FROM dashboard
		WHERE customer_id = $1 ORDER BY updated_at DESC`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Dashboard
	for rows.Next() {
		var d Dashboard
		var tilesJSON []byte
		if err := rows.Scan(&d.ID, &d.CustomerID, &d.Name, &tilesJSON, &d.IsPublic); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(tilesJSON, &d.Tiles)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM dashboard WHERE id = $1`, id)
	return err
}
