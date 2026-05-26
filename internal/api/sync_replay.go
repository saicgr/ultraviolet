// Phase 6 ergonomics (de-5): replay a sync from a chosen watermark.
//
// POST /api/v1/synced-tables/{id}/replay body {from_watermark}. Rewinds
// synced_tables.last_watermark to the supplied value and marks state='pending'
// so the next sync tick picks the table back up. Validates the watermark
// parses as the table's watermark type (timestamp/date — string for everything
// else).
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ultraviolet-dev/ultraviolet/internal/audit"
)

type replayRequest struct {
	FromWatermark string `json:"from_watermark"`
}

func (s *Server) syncReplay(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body replayRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	wm := strings.TrimSpace(body.FromWatermark)
	if wm == "" {
		writeErr(w, http.StatusBadRequest, errors.New("from_watermark required"))
		return
	}

	// Load the table so we can validate the watermark against its column type
	// and audit-log with the customer that owns it.
	var (
		watermarkCol *string
		connID       string
	)
	err = s.db.Pool().QueryRow(r.Context(),
		`SELECT watermark_column, connection_id::text FROM synced_tables WHERE id = $1`, id).
		Scan(&watermarkCol, &connID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, errors.New("synced table not found"))
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if watermarkCol == nil || *watermarkCol == "" {
		writeErr(w, http.StatusBadRequest, errors.New("table has no watermark column; replay not applicable"))
		return
	}

	// Best-effort type validation: if the column name hints at a temporal type
	// (date/timestamp/ts/_at) require RFC3339 or YYYY-MM-DD parsing. Otherwise
	// accept any non-empty string.
	if hintsTemporal(*watermarkCol) {
		if _, err := time.Parse(time.RFC3339, wm); err != nil {
			if _, err2 := time.Parse("2006-01-02", wm); err2 != nil {
				writeErr(w, http.StatusBadRequest,
					fmt.Errorf("from_watermark must be RFC3339 or YYYY-MM-DD for column %q", *watermarkCol))
				return
			}
		}
	}

	if _, err := s.db.Pool().Exec(r.Context(),
		`UPDATE synced_tables SET last_watermark = $2, state = 'pending', last_error = NULL, updated_at = now()
		 WHERE id = $1`, id, wm); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	// Look up customer_id via the connection for the audit row.
	var customerID *string
	_ = s.db.Pool().QueryRow(r.Context(),
		`SELECT c.customer_id::text FROM connections c WHERE c.id::text = $1`, connID).Scan(&customerID)
	idStr := id.String()
	ev := audit.Event{
		Action:     "sync.replay.requested",
		TargetType: "synced_table",
		TargetID:   idStr,
		Payload:    map[string]string{"from_watermark": wm},
		RemoteIP:   r.RemoteAddr,
		UserAgent:  r.UserAgent(),
	}
	if customerID != nil {
		if parsed, perr := uuid.Parse(*customerID); perr == nil {
			ev.CustomerID = &parsed
		}
	}
	_ = audit.New(s.db.Pool()).Log(r.Context(), ev)

	writeJSON(w, http.StatusOK, map[string]any{
		"id":             idStr,
		"from_watermark": wm,
		"state":          "pending",
	})
}

func hintsTemporal(col string) bool {
	c := strings.ToLower(col)
	return strings.Contains(c, "date") || strings.Contains(c, "time") ||
		strings.HasSuffix(c, "_at") || strings.HasSuffix(c, "_ts") || c == "ts"
}
