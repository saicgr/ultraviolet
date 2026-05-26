// Phase 6 Wave 2 (de-3): schema-snapshot capture + diff viewer.
// POST /api/v1/connections/{id}/schema/capture uses connectors.Introspecter
// (optional) to list tables and writes a JSONB row to schema_snapshot; 501
// if the connector doesn't support introspection.
// GET /api/v1/connections/{id}/schema/diff?snapshot_a=&snapshot_b= loads two
// payloads and diffs in Go: added/dropped tables, added/dropped columns,
// type_changes.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ultraviolet-dev/ultraviolet/internal/connectors"
)

// snapshotTable mirrors connectors.Table for stable on-disk JSON encoding.
type snapshotTable struct {
	Schema   string           `json:"schema"`
	Name     string           `json:"name"`
	RowCount int64            `json:"row_count"`
	Columns  []snapshotColumn `json:"columns"`
}

type snapshotColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Null bool   `json:"null"`
}

type schemaDiff struct {
	AddedTables    []string     `json:"added_tables"`
	DroppedTables  []string     `json:"dropped_tables"`
	AddedColumns   []colRef     `json:"added_columns"`
	DroppedColumns []colRef     `json:"dropped_columns"`
	TypeChanges    []typeChange `json:"type_changes"`
}

type colRef struct {
	Table  string `json:"table"`
	Column string `json:"column"`
	Type   string `json:"type,omitempty"`
}

type typeChange struct {
	Table   string `json:"table"`
	Column  string `json:"column"`
	OldType string `json:"old_type"`
	NewType string `json:"new_type"`
}

// captureSchema: POST /api/v1/connections/{id}/schema/capture.
func (s *Server) captureSchema(w http.ResponseWriter, r *http.Request) {
	connID, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx := r.Context()
	var customerID uuid.UUID
	var warehouseType string
	if err := s.db.Pool().QueryRow(ctx,
		`SELECT customer_id, warehouse_type FROM connections WHERE id = $1`, connID,
	).Scan(&customerID, &warehouseType); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, fmt.Errorf("connection not found"))
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	factory, err := connectors.NewFactory(s.cfg, s.db, s.enc, s.log)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	conn, err := factory.For(ctx, customerID, warehouseType)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer conn.Close()
	intro, ok := conn.(connectors.Introspecter)
	if !ok {
		writeErr(w, http.StatusNotImplemented, fmt.Errorf("connector %q does not support introspection", warehouseType))
		return
	}
	icx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tables, err := intro.Introspect(icx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Errorf("introspect: %w", err))
		return
	}
	payload := make([]snapshotTable, 0, len(tables))
	for _, t := range tables {
		cols := make([]snapshotColumn, 0, len(t.Columns))
		for _, c := range t.Columns {
			cols = append(cols, snapshotColumn{Name: c.Name, Type: c.Type, Null: c.Null})
		}
		payload = append(payload, snapshotTable{Schema: t.Schema, Name: t.Name, RowCount: t.RowCount, Columns: cols})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var id uuid.UUID
	var capturedAt time.Time
	if err := s.db.Pool().QueryRow(ctx,
		`INSERT INTO schema_snapshot (customer_id, connection_id, payload)
		 VALUES ($1, $2, $3) RETURNING id, captured_at`,
		customerID, connID, encoded,
	).Scan(&id, &capturedAt); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "captured_at": capturedAt, "table_count": len(payload)})
}

// diffSchema: GET /api/v1/connections/{id}/schema/diff?snapshot_a=&snapshot_b=.
func (s *Server) diffSchema(w http.ResponseWriter, r *http.Request) {
	connID, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	aID, err := uuid.Parse(r.URL.Query().Get("snapshot_a"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("snapshot_a uuid: %w", err))
		return
	}
	bID, err := uuid.Parse(r.URL.Query().Get("snapshot_b"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("snapshot_b uuid: %w", err))
		return
	}
	a, status, err := s.loadSnapshot(r.Context(), connID, aID)
	if err != nil {
		writeErr(w, status, err)
		return
	}
	b, status, err := s.loadSnapshot(r.Context(), connID, bID)
	if err != nil {
		writeErr(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, diffSnapshots(a, b))
}

func (s *Server) loadSnapshot(ctx context.Context, connID, snapID uuid.UUID) ([]snapshotTable, int, error) {
	var raw []byte
	err := s.db.Pool().QueryRow(ctx,
		`SELECT payload FROM schema_snapshot WHERE id = $1 AND connection_id = $2`,
		snapID, connID,
	).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, http.StatusNotFound, fmt.Errorf("snapshot %s not found", snapID)
	}
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	var out []snapshotTable
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("decode snapshot %s: %w", snapID, err)
	}
	return out, http.StatusOK, nil
}

// diffSnapshots is pure; tested separately. fqn = "schema.name" (matches the
// lineage convention so a diff entry can be x-referenced with lineage_edge).
func diffSnapshots(a, b []snapshotTable) schemaDiff {
	d := schemaDiff{
		AddedTables: []string{}, DroppedTables: []string{},
		AddedColumns: []colRef{}, DroppedColumns: []colRef{},
		TypeChanges: []typeChange{},
	}
	ai := indexSnapshot(a)
	bi := indexSnapshot(b)

	for fqn := range bi {
		if _, ok := ai[fqn]; !ok {
			d.AddedTables = append(d.AddedTables, fqn)
		}
	}
	for fqn := range ai {
		if _, ok := bi[fqn]; !ok {
			d.DroppedTables = append(d.DroppedTables, fqn)
		}
	}

	for fqn, at := range ai {
		bt, ok := bi[fqn]
		if !ok {
			continue
		}
		aCols := colMap(at.Columns)
		bCols := colMap(bt.Columns)
		for name, c := range bCols {
			if _, ok := aCols[name]; !ok {
				d.AddedColumns = append(d.AddedColumns, colRef{Table: fqn, Column: name, Type: c.Type})
			}
		}
		for name, c := range aCols {
			if _, ok := bCols[name]; !ok {
				d.DroppedColumns = append(d.DroppedColumns, colRef{Table: fqn, Column: name, Type: c.Type})
			}
		}
		for name, ac := range aCols {
			if bc, ok := bCols[name]; ok && ac.Type != bc.Type {
				d.TypeChanges = append(d.TypeChanges, typeChange{
					Table: fqn, Column: name, OldType: ac.Type, NewType: bc.Type,
				})
			}
		}
	}
	return d
}

func indexSnapshot(s []snapshotTable) map[string]snapshotTable {
	out := make(map[string]snapshotTable, len(s))
	for _, t := range s {
		fqn := t.Name
		if t.Schema != "" {
			fqn = t.Schema + "." + t.Name
		}
		out[fqn] = t
	}
	return out
}

func colMap(cols []snapshotColumn) map[string]snapshotColumn {
	out := make(map[string]snapshotColumn, len(cols))
	for _, c := range cols {
		out[c.Name] = c
	}
	return out
}
