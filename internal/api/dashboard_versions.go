// Package api — ux-7 dashboard versioning (git-style history + restore).
//
// Every dashboard mutation should write a row to dashboard_version so the UI
// can list prior revisions and restore any of them. List + get are cheap reads
// against the unique (dashboard_id, version) index; restore inserts a new row
// with the next version number whose payload is copied from the named prior
// version and then mirrors that payload back onto the live dashboard row so
// subsequent reads see the restored state.
//
// TODO(ux-7-history-insert): the existing dashboard surface in phase3_handlers.go
// only exposes list + create — there is no PATCH/update handler today. When one
// is added (likely `func (s *Server) updateDashboard`), it MUST call
// insertDashboardVersion below inside the same transaction as the UPDATE so
// history never diverges from the live row. Until then, dashboard_version is
// populated only by createDashboard (initial v1) and restore (new tip).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type dashboardVersionRow struct {
	Version   int             `json:"version"`
	Author    *string         `json:"author,omitempty"`
	Message   *string         `json:"message,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// insertDashboardVersion appends a new (dashboard_id, version) row. It computes
// the next version as MAX(version)+1 for the dashboard so callers don't race
// on a separately-held counter. Safe to call from createDashboard (first PATCH
// becomes v1), the future PATCH handler, and the restore path.
func insertDashboardVersion(ctx context.Context, pool *pgxpool.Pool, dashboardID uuid.UUID, payload []byte, author, message string) (int, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // best-effort on early return

	var next int
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(version), 0) + 1 FROM dashboard_version WHERE dashboard_id = $1`,
		dashboardID).Scan(&next); err != nil {
		return 0, fmt.Errorf("compute next version: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO dashboard_version (dashboard_id, version, payload, author, message)
		 VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''))`,
		dashboardID, next, payload, author, message); err != nil {
		return 0, fmt.Errorf("insert version: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return next, nil
}

func (s *Server) listDashboardVersions(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rows, err := s.db.Pool().Query(r.Context(),
		`SELECT version, author, message, created_at
		 FROM dashboard_version WHERE dashboard_id = $1 ORDER BY version DESC`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := []dashboardVersionRow{}
	for rows.Next() {
		var v dashboardVersionRow
		if err := rows.Scan(&v.Version, &v.Author, &v.Message, &v.CreatedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getDashboardVersion(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	version, err := strconv.Atoi(chi.URLParam(r, "version"))
	if err != nil || version <= 0 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid version"))
		return
	}
	var row dashboardVersionRow
	row.Version = version
	err = s.db.Pool().QueryRow(r.Context(),
		`SELECT author, message, created_at, payload
		 FROM dashboard_version WHERE dashboard_id = $1 AND version = $2`,
		id, version).Scan(&row.Author, &row.Message, &row.CreatedAt, &row.Payload)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, fmt.Errorf("version not found"))
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// restoreDashboardVersion makes the named prior version the new tip: it inserts
// a new dashboard_version row (with bumped version) carrying the prior payload,
// then mirrors that payload onto the live dashboard row so subsequent reads
// observe the restore. Author/message default to "restore" markers when the
// request body omits them.
func (s *Server) restoreDashboardVersion(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	version, err := strconv.Atoi(chi.URLParam(r, "version"))
	if err != nil || version <= 0 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid version"))
		return
	}
	var body struct {
		Author  string `json:"author"`
		Message string `json:"message"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Message == "" {
		body.Message = fmt.Sprintf("restore v%d", version)
	}

	var payload []byte
	err = s.db.Pool().QueryRow(r.Context(),
		`SELECT payload FROM dashboard_version WHERE dashboard_id = $1 AND version = $2`,
		id, version).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, fmt.Errorf("version not found"))
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	newVersion, err := insertDashboardVersion(r.Context(), s.db.Pool(), id, payload, body.Author, body.Message)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	// Mirror payload onto the live dashboard row. Payload is expected to carry
	// at least {"tiles": [...], "name": "..."} — we forward whichever fields the
	// version carries and leave the rest untouched. Errors here are fatal: a
	// successful version insert without a matching live update would silently
	// diverge history from the rendered state.
	var parsed struct {
		Name  *string         `json:"name"`
		Tiles json.RawMessage `json:"tiles"`
	}
	_ = json.Unmarshal(payload, &parsed)
	if _, err := s.db.Pool().Exec(r.Context(),
		`UPDATE dashboard SET
		    name = COALESCE($2, name),
		    tiles = COALESCE($3::jsonb, tiles),
		    updated_at = now()
		 WHERE id = $1`,
		id, parsed.Name, []byte(parsed.Tiles)); err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("mirror restore to dashboard: %w", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"dashboard_id":  id,
		"restored_from": version,
		"new_version":   newVersion,
		"message":       body.Message,
	})
}
