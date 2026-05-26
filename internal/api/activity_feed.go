package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// GET /api/v1/customers/{id}/activity?limit=50&since=<RFC3339>
// Returns rows from activity_event ordered by created_at desc.
func (s *Server) listActivity(w http.ResponseWriter, r *http.Request) {
	cid, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	limit := 50
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, perr := strconv.Atoi(q); perr == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	var since *time.Time
	if q := r.URL.Query().Get("since"); q != "" {
		t, perr := time.Parse(time.RFC3339, q)
		if perr != nil {
			writeErr(w, http.StatusBadRequest, perr)
			return
		}
		since = &t
	}

	rows, err := s.db.Pool().Query(r.Context(), `
		SELECT id, user_id, kind, target_type, target_id, summary, payload, created_at
		  FROM activity_event
		 WHERE customer_id = $1
		   AND ($2::timestamptz IS NULL OR created_at > $2)
		 ORDER BY created_at DESC
		 LIMIT $3`, cid, since, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	type event struct {
		ID         int64           `json:"id"`
		UserID     *string         `json:"user_id,omitempty"`
		Kind       string          `json:"kind"`
		TargetType *string         `json:"target_type,omitempty"`
		TargetID   *string         `json:"target_id,omitempty"`
		Summary    string          `json:"summary"`
		Payload    json.RawMessage `json:"payload,omitempty"`
		CreatedAt  time.Time       `json:"created_at"`
	}
	out := []event{}
	for rows.Next() {
		var (
			e          event
			userID     *string
			targetType *string
			targetID   *string
			payload    []byte
		)
		if scanErr := rows.Scan(&e.ID, &userID, &e.Kind, &targetType, &targetID, &e.Summary, &payload, &e.CreatedAt); scanErr != nil {
			writeErr(w, http.StatusInternalServerError, scanErr)
			return
		}
		e.UserID = userID
		e.TargetType = targetType
		e.TargetID = targetID
		if len(payload) > 0 {
			e.Payload = json.RawMessage(payload)
		}
		out = append(out, e)
	}
	if rows.Err() != nil {
		writeErr(w, http.StatusInternalServerError, rows.Err())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
