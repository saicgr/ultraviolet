// Phase 6 ergonomics (an-3): SQL formatter + linter REST surface.
// Thin wrapper around internal/sqlfmt — Format normalizes whitespace +
// keywords via pg_query.Deparse, Lint applies a small rule-set.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ultraviolet-dev/ultraviolet/internal/sqlfmt"
)

type sqlBody struct {
	SQL string `json:"sql"`
}

func (s *Server) sqlFormat(w http.ResponseWriter, r *http.Request) {
	var body sqlBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(body.SQL) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("sql required"))
		return
	}
	out, err := sqlfmt.Format(body.SQL)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"formatted": out})
}

func (s *Server) sqlLint(w http.ResponseWriter, r *http.Request) {
	var body sqlBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(body.SQL) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("sql required"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": sqlfmt.Lint(body.SQL)})
}
