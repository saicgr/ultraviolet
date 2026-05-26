package api

// bu-3 Public dashboard signed share token.
//
//   POST   /api/v1/dashboards/{id}/share
//   GET    /api/v1/public/dashboards/{token}     (no auth — token IS the auth)
//   DELETE /api/v1/share-tokens/{token}
//
// Persists to the `share_token` table (migration 0005). For the public GET to
// truly bypass auth we extend authMiddleware to whitelist the `/api/v1/public/`
// prefix in addition to /healthz; see auth.go.

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ultraviolet-dev/ultraviolet/internal/dashboards"
	"github.com/ultraviolet-dev/ultraviolet/internal/sharing"
)

func (s *Server) createDashboardShare(w http.ResponseWriter, r *http.Request) {
	did, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// Resolve owning customer from the dashboard row.
	d, err := dashboards.NewStore(s.db.Pool()).Get(r.Context(), did)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	var body struct {
		ExpiresInSeconds    int64 `json:"expires_in_seconds"`
		AllowFilterOverride bool  `json:"allow_filter_override"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	ttl := time.Duration(body.ExpiresInSeconds) * time.Second
	tok, err := sharing.New(s.db.Pool()).Mint(r.Context(), sharing.Token{
		CustomerID: d.CustomerID,
		TargetType: "dashboard",
		TargetID:   did,
	}, ttl)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	resp := map[string]any{
		"token":                 tok,
		"dashboard_id":          did,
		"allow_filter_override": body.AllowFilterOverride,
	}
	if ttl > 0 {
		resp["expires_at"] = time.Now().Add(ttl).Format(time.RFC3339)
	}
	writeJSON(w, http.StatusCreated, resp)
}

// publicDashboard resolves a share token without auth and returns the
// dashboard payload. Registered OUTSIDE the auth chain via the public-prefix
// whitelist in authMiddleware.
func (s *Server) publicDashboard(w http.ResponseWriter, r *http.Request) {
	tok := chi.URLParam(r, "token")
	if tok == "" {
		writeErr(w, http.StatusBadRequest, errors.New("token required"))
		return
	}
	t, err := sharing.New(s.db.Pool()).Resolve(r.Context(), tok)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if t.TargetType != "dashboard" {
		writeErr(w, http.StatusBadRequest, errors.New("token is not for a dashboard"))
		return
	}
	d, err := dashboards.NewStore(s.db.Pool()).Get(r.Context(), t.TargetID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":      tok,
		"dashboard":  d,
		"expires_at": t.ExpiresAt,
	})
}

func (s *Server) revokeDashboardShare(w http.ResponseWriter, r *http.Request) {
	tok := chi.URLParam(r, "token")
	if tok == "" {
		writeErr(w, http.StatusBadRequest, errors.New("token required"))
		return
	}
	if err := sharing.New(s.db.Pool()).Revoke(r.Context(), tok); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
