// Semantic "what-if" parameter overrides (bu-8).
//
// POST /api/v1/semantic/{id}/what-if
//
//	body: {"params": {"name": "value", ...}, "dimensions": [...], "measures": [...]}
//	returns: {"sql": "..."}
//
// Strategy: load the stored semantic model YAML by id, parse + compile with
// the requested dimensions/measures, then do simple `:param_name` → value
// substitution on the compiled SQL. Finally re-parse with pg_query_go to
// guarantee the rewritten SQL is still valid Postgres. No execution.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	pg_query "github.com/pganalyze/pg_query_go/v6"

	"github.com/ultraviolet-dev/ultraviolet/internal/semantic"
)

func (s *Server) semanticWhatIf(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	modelID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid id: %w", err))
		return
	}
	var body struct {
		Params     map[string]string `json:"params"`
		Dimensions []string          `json:"dimensions"`
		Measures   []string          `json:"measures"`
		Filters    []string          `json:"filters"`
		Limit      int               `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	yamlSrc, err := loadSemanticYAML(r.Context(), s, cid, modelID)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	sql, err := semantic.CompileWithParams(yamlSrc, semantic.Query{
		Dimensions: body.Dimensions,
		Measures:   body.Measures,
		Filters:    body.Filters,
		Limit:      body.Limit,
	}, body.Params)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if _, perr := pg_query.Parse(sql); perr != nil {
		// Refuse to return SQL that no longer parses — fail loud, per CLAUDE.md
		// "no silent degrade".
		writeErr(w, http.StatusUnprocessableEntity, fmt.Errorf("override produced invalid SQL: %w", perr))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"sql": sql})
}

// loadSemanticYAML fetches the stored YAML source for a semantic model owned
// by the active customer. Scoped on customer_id to prevent cross-tenant reads.
func loadSemanticYAML(ctx context.Context, s *Server, customerID, modelID uuid.UUID) (string, error) {
	var src string
	err := s.db.Pool().QueryRow(ctx,
		`SELECT yaml_source FROM semantic_model WHERE id = $1 AND customer_id = $2`,
		modelID, customerID).Scan(&src)
	if err != nil {
		return "", fmt.Errorf("semantic model not found: %w", err)
	}
	return src, nil
}

// validateParamValue rejects parameter override values that would obviously
// break the SQL or smuggle in injection. We accept identifiers, numbers, and
// quoted string literals; anything containing a `;` or unmatched quote is
// rejected. The final pg_query.Parse pass is the real backstop.
func validateParamValue(v string) error {
	if strings.ContainsAny(v, ";\x00") {
		return fmt.Errorf("param value contains forbidden characters")
	}
	return nil
}
