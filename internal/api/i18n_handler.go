package api

// i18n bundle endpoint — GET /api/v1/i18n/{locale}/messages.json.
// Returns the backend system-string dictionary for a locale so the frontend's
// remote-dictionary fetch resolves to a 200 (it merges these over its bundled
// English fallback). Public — carries no tenant data.

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ultraviolet-dev/ultraviolet/internal/i18n"
)

func (s *Server) i18nMessages(w http.ResponseWriter, r *http.Request) {
	locale := chi.URLParam(r, "locale")
	writeJSON(w, http.StatusOK, i18n.Messages(locale))
}
