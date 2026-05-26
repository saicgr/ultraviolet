package api

// go-5 Audit-log SIEM export.
//
// GET /api/v1/customers/{id}/audit-log.ndjson?since=...&until=...
//
// Streams NDJSON (one JSON object per line). Customer-scoped. Rows stream
// directly from the pgx cursor to the response writer so memory stays bounded
// for arbitrarily wide date windows.

import (
	"encoding/json"
	"net/http"
	"time"
)

func (s *Server) auditLogNDJSON(w http.ResponseWriter, r *http.Request) {
	cid, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	since, until := parseSinceUntil(r)

	const q = `
		SELECT id, customer_id::text, user_id::text, action,
		       COALESCE(target_type,''), COALESCE(target_id,''),
		       COALESCE(payload, '{}'::jsonb),
		       COALESCE(host(remote_ip),''), COALESCE(user_agent,''),
		       created_at
		FROM audit_log
		WHERE customer_id = $1
		  AND created_at >= $2
		  AND created_at <  $3
		ORDER BY created_at ASC, id ASC`

	rows, err := s.db.Pool().Query(r.Context(), q, cid, since, until)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", `attachment; filename="audit-log.ndjson"`)
	w.WriteHeader(http.StatusOK)

	enc := json.NewEncoder(w)
	for rows.Next() {
		var (
			id                                                          int64
			customerID, userID, action, targetType, targetID            string
			payload                                                     []byte
			remoteIP, userAgent                                         string
			createdAt                                                   time.Time
		)
		if err := rows.Scan(&id, &customerID, &userID, &action, &targetType, &targetID, &payload, &remoteIP, &userAgent, &createdAt); err != nil {
			s.log.Warn().Err(err).Msg("audit ndjson scan failed mid-stream")
			return
		}
		_ = enc.Encode(map[string]any{
			"id":          id,
			"customer_id": customerID,
			"user_id":     userID,
			"action":      action,
			"target_type": targetType,
			"target_id":   targetID,
			"payload":     json.RawMessage(payload),
			"remote_ip":   remoteIP,
			"user_agent":  userAgent,
			"created_at":  createdAt,
		})
	}
}

// parseSinceUntil reads RFC-3339 ?since= and ?until= query params, falling
// back to a 30-day window ending at now() when missing or unparsable.
func parseSinceUntil(r *http.Request) (time.Time, time.Time) {
	now := time.Now().UTC()
	since := now.AddDate(0, 0, -30)
	until := now
	if v := r.URL.Query().Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			since = t
		}
	}
	if v := r.URL.Query().Get("until"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			until = t
		}
	}
	return since, until
}
