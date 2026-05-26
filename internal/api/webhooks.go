// pu-6 — webhook_endpoint CRUD. Dispatch lives in internal/webhooks; this
// surface is just the registry. Secrets are encrypted at rest with the
// control-plane Encryptor (AES-256-GCM); nonce is prepended to the ciphertext
// so the dispatcher can recover both from the single bytea column.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

type webhookReq struct {
	CustomerID uuid.UUID `json:"customer_id"`
	URL        string    `json:"url"`
	EventKinds []string  `json:"event_kinds"`
	Secret     string    `json:"secret"`
}

type webhookRow struct {
	ID         uuid.UUID `json:"id"`
	CustomerID uuid.UUID `json:"customer_id"`
	URL        string    `json:"url"`
	EventKinds []string  `json:"event_kinds"`
	Active     bool      `json:"active"`
}

func (s *Server) createWebhook(w http.ResponseWriter, r *http.Request) {
	var body webhookReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.CustomerID == uuid.Nil || body.URL == "" || body.Secret == "" || len(body.EventKinds) == 0 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("customer_id, url, secret, event_kinds required"))
		return
	}
	ct, nonce, err := s.enc.Encrypt([]byte(body.Secret))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Store as nonce||ciphertext so a single bytea round-trips cleanly.
	blob := append(append([]byte{}, nonce...), ct...)
	var id uuid.UUID
	err = s.db.Pool().QueryRow(r.Context(),
		`INSERT INTO webhook_endpoint (customer_id, url, event_kinds, secret_encrypted)
		 VALUES ($1,$2,$3,$4) RETURNING id`,
		body.CustomerID, body.URL, body.EventKinds, blob).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]uuid.UUID{"id": id})
}

func (s *Server) listWebhooks(w http.ResponseWriter, r *http.Request) {
	cid, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rows, err := s.db.Pool().Query(r.Context(),
		`SELECT id, customer_id, url, event_kinds, active
		   FROM webhook_endpoint WHERE customer_id = $1 ORDER BY created_at DESC LIMIT 200`, cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := []webhookRow{}
	for rows.Next() {
		var rec webhookRow
		if err := rows.Scan(&rec.ID, &rec.CustomerID, &rec.URL, &rec.EventKinds, &rec.Active); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, rec)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if _, err := s.db.Pool().Exec(r.Context(), `DELETE FROM webhook_endpoint WHERE id = $1`, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
