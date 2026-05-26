// Workspace branding REST surface (bu-11).
//
//	GET /api/v1/workspaces/{id}/branding   — current branding (defaults if unset)
//	PUT /api/v1/workspaces/{id}/branding   — upsert branding for the workspace
package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/branding"
)

func (s *Server) getWorkspaceBranding(w http.ResponseWriter, r *http.Request) {
	wsID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid workspace id: %w", err))
		return
	}
	b, err := branding.LoadForWorkspace(r.Context(), s.db.Pool(), wsID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) putWorkspaceBranding(w http.ResponseWriter, r *http.Request) {
	wsID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid workspace id: %w", err))
		return
	}
	var body struct {
		Name       string `json:"name"`
		Tagline    string `json:"tagline"`
		LogoURL    string `json:"logo_url"`
		PrimaryHex string `json:"primary_hex"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// Confirm the workspace actually exists before writing a branding row —
	// FK ON DELETE CASCADE protects us against orphans but a 404 here gives
	// callers a clearer error than a constraint violation.
	var exists bool
	if err := s.db.Pool().QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM workspace WHERE id = $1)`, wsID).Scan(&exists); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if !exists {
		writeErr(w, http.StatusNotFound, fmt.Errorf("workspace not found"))
		return
	}
	err = branding.UpsertForWorkspace(r.Context(), s.db.Pool(), branding.WorkspaceBranding{
		WorkspaceID: wsID,
		Name:        body.Name,
		Tagline:     body.Tagline,
		LogoURL:     body.LogoURL,
		PrimaryHex:  body.PrimaryHex,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out, err := branding.LoadForWorkspace(r.Context(), s.db.Pool(), wsID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
