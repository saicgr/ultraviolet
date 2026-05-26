// Phase 6 ergonomics (pu-7): dbt-style macro expansion for workbench SQL.
//
// POST /api/v1/workbench/expand body {customer_id, sql} returns
// {expanded, resolved_refs}. Substring-replaces `{{ ref('NAME') }}` with the
// table's FQN. NAME is resolved against synced_tables.table_name (joined
// through connections to scope to the customer) and table_metadata — the
// existing schema has no alias_name column, so we match on the last segment
// of `fqn` or on the `tags` array as a secondary signal. No migration added
// per task constraints; unresolved refs return 400 with the missing names.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/macros"
)

type expandRequest struct {
	CustomerID uuid.UUID `json:"customer_id"`
	SQL        string    `json:"sql"`
}

type expandResponse struct {
	Expanded     string   `json:"expanded"`
	ResolvedRefs []string `json:"resolved_refs"`
}

func (s *Server) workbenchExpand(w http.ResponseWriter, r *http.Request) {
	var body expandRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.CustomerID == uuid.Nil || strings.TrimSpace(body.SQL) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("customer_id and sql required"))
		return
	}

	lookup := s.buildRefLookup(r.Context(), body.CustomerID)
	expanded, refs, err := macros.Resolve(body.SQL, body.CustomerID, lookup)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, expandResponse{Expanded: expanded, ResolvedRefs: refs})
}

// buildRefLookup returns a closure that resolves a bare table name to a
// fully-qualified `schema.table` string. synced_tables wins over
// table_metadata (it's the source of truth for what the proxy actually
// serves); ties within a source are broken by the most recently updated row.
func (s *Server) buildRefLookup(ctx context.Context, customerID uuid.UUID) func(string) (string, bool) {
	return func(name string) (string, bool) {
		name = strings.TrimSpace(name)
		if name == "" {
			return "", false
		}

		// Primary: synced_tables joined via connections (scope to customer).
		var schema, table string
		err := s.db.Pool().QueryRow(ctx, `
			SELECT t.schema_name, t.table_name
			FROM synced_tables t
			JOIN connections c ON c.id = t.connection_id
			WHERE c.customer_id = $1 AND t.table_name = $2
			ORDER BY t.updated_at DESC NULLS LAST
			LIMIT 1`, customerID, name).Scan(&schema, &table)
		if err == nil {
			return fmt.Sprintf("%s.%s", schema, table), true
		}

		// Fallback: table_metadata.fqn ending in `.NAME`, or `NAME` in tags.
		var fqn string
		err = s.db.Pool().QueryRow(ctx, `
			SELECT fqn FROM table_metadata
			WHERE customer_id = $1
			  AND (fqn = $2 OR fqn LIKE '%.' || $2 OR $2 = ANY(tags))
			ORDER BY updated_at DESC
			LIMIT 1`, customerID, name).Scan(&fqn)
		if err == nil && fqn != "" {
			return fqn, true
		}
		return "", false
	}
}
