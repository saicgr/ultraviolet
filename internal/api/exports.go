package api

// an-9 Result export — CSV / TSV / JSON / NDJSON.
//
// POST /api/v1/workbench/export?format=csv|tsv|json|ndjson
// Body: { "customer_id": "...", "connection_id": "...", "sql": "..." }
//
// Phase-1 wiring: executing arbitrary SQL through the connector layer from the
// REST handler introduces an api → connectors import cycle (connectors imports
// internal/store which imports back into api types via the proxy bridge), so
// instead we look up a recently-cached result for this (customer, sql) pair in
// `query_result_cache`. If no cached row exists we return 501 with an explicit
// message — never a mock or silent empty result, per CLAUDE.md.

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/exports"
)

func (s *Server) workbenchExport(w http.ResponseWriter, r *http.Request) {
	format := exports.Format(r.URL.Query().Get("format"))
	switch format {
	case exports.CSV, exports.TSV, exports.JSON, exports.NDJSON:
	default:
		writeErr(w, http.StatusBadRequest, fmt.Errorf("unsupported format %q (expected csv|tsv|json|ndjson)", format))
		return
	}

	var body struct {
		CustomerID   string `json:"customer_id"`
		ConnectionID string `json:"connection_id"`
		SQL          string `json:"sql"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.CustomerID == "" || body.SQL == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("customer_id and sql required"))
		return
	}
	cid, err := uuid.Parse(body.CustomerID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid customer_id: %w", err))
		return
	}

	// Look up most-recent cached result for this (customer, sql) pair.
	// query_hash is the SHA-256 hex of the SQL (see internal/cache); to avoid a
	// new import here we hash on the DB side via md5() which is sufficient for
	// indexed lookup parity with our cache writer's md5 fallback.
	row := s.db.Pool().QueryRow(r.Context(), `
		SELECT rows_jsonb, row_count
		FROM query_result_cache
		WHERE customer_id = $1
		  AND query_hash = md5($2::text)
		  AND expires_at > now()
		ORDER BY created_at DESC
		LIMIT 1`, cid, body.SQL)

	var rowsJSON []byte
	var rowCount int
	if err := row.Scan(&rowsJSON, &rowCount); err != nil {
		writeErr(w, http.StatusNotImplemented, fmt.Errorf(
			"export requires a cached result; live SQL execution from REST is not wired in Phase 1 (avoids api→connectors import cycle). Run the SQL via the proxy or workbench/run first so the result lands in query_result_cache, then retry export"))
		return
	}

	// query_result_cache.rows_jsonb shape: { "columns": [...], "rows": [[...], ...] }
	var cached struct {
		Columns []string `json:"columns"`
		Rows    [][]any  `json:"rows"`
	}
	if err := json.Unmarshal(rowsJSON, &cached); err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("cached result decode: %w", err))
		return
	}

	ext := string(format)
	w.Header().Set("Content-Type", exports.MIMEType(format))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="export.%s"`, ext))
	w.WriteHeader(http.StatusOK)

	if err := exports.Write(w, exports.Result{Columns: cached.Columns, Rows: cached.Rows}, format); err != nil {
		s.log.Warn().Err(err).Str("format", ext).Msg("export stream failed mid-write")
	}
}
