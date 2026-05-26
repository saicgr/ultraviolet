package api

// go-4 Data-dictionary CSV export.
//
// GET /api/v1/customers/{id}/dictionary.csv
//
// Streams a CSV with one row per (table, column) pairing — joining
// `table_metadata` (table-level description / owner / SLA) with `pii_tag`
// (column-level PII detections). Tables with no detected PII columns still
// emit a row with empty column/pii_kind fields so the export is a complete
// inventory of catalogued tables.

import (
	"encoding/csv"
	"net/http"
)

func (s *Server) dictionaryCSV(w http.ResponseWriter, r *http.Request) {
	cid, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	// LEFT JOIN so tables without any column-level PII tag still appear as a
	// single row (column / pii_kind blank). Coalesce on the table-level
	// metadata side too, since multiple `table_metadata` rows can exist per
	// fqn (one per source) — we pick a deterministic latest row per fqn via
	// DISTINCT ON updated_at DESC.
	const q = `
		WITH meta AS (
		  SELECT DISTINCT ON (fqn) fqn, description, owner, sla_minutes
		  FROM table_metadata
		  WHERE customer_id = $1
		  ORDER BY fqn, updated_at DESC
		)
		SELECT m.fqn,
		       COALESCE(p.column_name, ''),
		       '',
		       COALESCE(m.description, ''),
		       COALESCE(m.owner, ''),
		       COALESCE(m.sla_minutes, 0),
		       COALESCE(p.kind, '')
		FROM meta m
		LEFT JOIN pii_tag p
		       ON p.customer_id = $1
		      AND p.fqn = m.fqn
		ORDER BY m.fqn, p.column_name NULLS FIRST`

	rows, err := s.db.Pool().Query(r.Context(), q, cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="dictionary.csv"`)
	w.WriteHeader(http.StatusOK)

	cw := csv.NewWriter(w)
	defer cw.Flush()

	// Header row — caller treats this as a stable contract.
	if err := cw.Write([]string{"fqn", "column", "type", "description", "owner", "sla_minutes", "pii_kind"}); err != nil {
		return
	}

	var (
		fqn, col, typ, desc, owner, kind string
		sla                              int
	)
	for rows.Next() {
		if err := rows.Scan(&fqn, &col, &typ, &desc, &owner, &sla, &kind); err != nil {
			// Mid-stream error: we've already written the header + possibly
			// rows, so we can't change the status code. Log via logErrors
			// middleware is bypassed since status was 200; flush + abort.
			s.log.Warn().Err(err).Msg("dictionary csv scan failed mid-stream")
			return
		}
		slaStr := ""
		if sla > 0 {
			slaStr = itoa(sla)
		}
		_ = cw.Write([]string{fqn, col, typ, desc, owner, slaStr, kind})
	}
}

// itoa is a tiny local helper to avoid pulling strconv just for one int.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
