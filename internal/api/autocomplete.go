package api

// Workbench autocomplete (an-2 / ai-u9 — satisfies both an-2 and ai-u9).
// Returns up to 10 normalized SQL snippets from the past 30 days whose
// normalized_sql starts with the user-supplied prefix and whose route_decision
// indicates a successful run (anything other than 'error'). Snippets are
// truncated to 200 chars so the dropdown remains usable.

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

const autocompleteMaxResults = 10
const autocompleteSnippetMax = 200

func (s *Server) workbenchAutocomplete(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cidStr := q.Get("customer_id")
	prefix := q.Get("prefix")
	if cidStr == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("customer_id required"))
		return
	}
	if prefix == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("prefix required"))
		return
	}
	cid, err := uuid.Parse(cidStr)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid customer_id: %w", err))
		return
	}

	// ILIKE pattern: trigram GIN supports anchored LIKE/ILIKE.
	pattern := strings.ReplaceAll(prefix, "%", `\%`)
	pattern = strings.ReplaceAll(pattern, "_", `\_`) + "%"

	rows, err := s.db.Pool().Query(r.Context(),
		`SELECT DISTINCT LEFT(normalized_sql, $3) AS snippet
		 FROM query_log
		 WHERE customer_id = $1
		   AND started_at > now() - interval '30 days'
		   AND route_decision <> 'error'
		   AND normalized_sql ILIKE $2
		 ORDER BY snippet
		 LIMIT $4`,
		cid, pattern, autocompleteSnippetMax, autocompleteMaxResults)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	out := make([]string, 0, autocompleteMaxResults)
	for rows.Next() {
		var snip string
		if err := rows.Scan(&snip); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, snip)
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": out})
}
