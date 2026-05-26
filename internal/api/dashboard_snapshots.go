// Package api — bu-9 shareable point-in-time dashboard snapshots.
//
// A snapshot freezes a dashboard view against a specific Iceberg snapshot id
// so the shared link is reproducible: DuckDB can re-execute tile SQL with
// `FOR VERSION AS OF <iceberg_snapshot_id>` and see exactly the same rows.
// The share_token is the public, unguessable handle (32 hex chars).
package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ultraviolet-dev/ultraviolet/internal/dashboards"
)

func newShareToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (s *Server) createDashboardSnapshot(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		AtIcebergSnapshotID string `json:"at_iceberg_snapshot_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.AtIcebergSnapshotID == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("at_iceberg_snapshot_id required"))
		return
	}

	// Verify dashboard exists before issuing a share token — avoids leaking
	// orphan tokens against a deleted dashboard.
	if _, err := dashboards.NewStore(s.db.Pool()).Get(r.Context(), id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, fmt.Errorf("dashboard not found"))
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	token, err := newShareToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var snapshotID uuid.UUID
	if err := s.db.Pool().QueryRow(r.Context(),
		`INSERT INTO dashboard_snapshot (dashboard_id, iceberg_snapshot_id, share_token)
		 VALUES ($1, $2, $3) RETURNING id`,
		id, body.AtIcebergSnapshotID, token).Scan(&snapshotID); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":                  snapshotID,
		"dashboard_id":        id,
		"iceberg_snapshot_id": body.AtIcebergSnapshotID,
		"share_token":         token,
	})
}

// getDashboardSnapshot resolves a share token to the full frozen view. The
// returned `iceberg_snapshot_id` is what DuckDB-side workers feed into
// `FOR VERSION AS OF` when re-executing the dashboard's tile SQL, so the link
// renders identical data forever (until the Iceberg snapshot is GC'd).
func (s *Server) getDashboardSnapshot(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("token required"))
		return
	}
	var (
		dashboardID uuid.UUID
		icebergID   string
	)
	err := s.db.Pool().QueryRow(r.Context(),
		`SELECT dashboard_id, iceberg_snapshot_id FROM dashboard_snapshot WHERE share_token = $1`,
		token).Scan(&dashboardID, &icebergID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, fmt.Errorf("snapshot not found"))
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	d, err := dashboards.NewStore(s.db.Pool()).Get(r.Context(), dashboardID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"dashboard":           d,
		"iceberg_snapshot_id": icebergID,
		"share_token":         token,
	})
}
