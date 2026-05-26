// Phase-6 Wave-1 co-6: per-user watch lists on lineage nodes. CRUD only;
// notification dispatch lives in Phase-6 W2 (it will fan out to the same
// notification table the inbox handlers already read).
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/alerts"
)

type lineageWatch struct {
	ID         uuid.UUID `json:"id"`
	CustomerID uuid.UUID `json:"customer_id"`
	UserSlug   string    `json:"user_slug"`
	FQN        string    `json:"fqn"`
	NotifyOn   string    `json:"notify_on"`
	CreatedAt  time.Time `json:"created_at"`
}

// Allowed notify_on values mirror the CHECK constraint in migration 0007.
var allowedNotifyOn = map[string]struct{}{
	"change": {}, "freshness": {}, "cost": {}, "any": {},
}

func (s *Server) createLineageWatch(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		UserSlug string `json:"user_slug"`
		FQN      string `json:"fqn"`
		NotifyOn string `json:"notify_on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	body.UserSlug = strings.TrimSpace(body.UserSlug)
	body.FQN = strings.TrimSpace(body.FQN)
	if body.UserSlug == "" || body.FQN == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("user_slug + fqn required"))
		return
	}
	if body.NotifyOn == "" {
		body.NotifyOn = "change"
	}
	if _, ok := allowedNotifyOn[body.NotifyOn]; !ok {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("notify_on must be one of change|freshness|cost|any"))
		return
	}

	// Upsert so a re-POST is idempotent — frontend "watch this" toggles can
	// click twice without 409s.
	var out lineageWatch
	err = s.db.Pool().QueryRow(r.Context(), `
		INSERT INTO lineage_watch (customer_id, user_slug, fqn, notify_on)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (customer_id, user_slug, fqn) DO UPDATE
			SET notify_on = EXCLUDED.notify_on
		RETURNING id, customer_id, user_slug, fqn, notify_on, created_at`,
		cid, body.UserSlug, body.FQN, body.NotifyOn).
		Scan(&out.ID, &out.CustomerID, &out.UserSlug, &out.FQN, &out.NotifyOn, &out.CreatedAt)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) listLineageWatches(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	q := `
		SELECT id, customer_id, user_slug, fqn, notify_on, created_at
		FROM lineage_watch
		WHERE customer_id = $1`
	args := []any{cid}
	if userSlug := strings.TrimSpace(r.URL.Query().Get("user_slug")); userSlug != "" {
		q += ` AND user_slug = $2`
		args = append(args, userSlug)
	}
	q += ` ORDER BY created_at DESC LIMIT 500`
	rows, err := s.db.Pool().Query(r.Context(), q, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := []lineageWatch{}
	for rows.Next() {
		var lw lineageWatch
		if err := rows.Scan(&lw.ID, &lw.CustomerID, &lw.UserSlug, &lw.FQN, &lw.NotifyOn, &lw.CreatedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, lw)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteLineageWatch(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid id: %w", err))
		return
	}
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	tag, err := s.db.Pool().Exec(r.Context(),
		`DELETE FROM lineage_watch WHERE id = $1 AND customer_id = $2`, id, cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, fmt.Errorf("watch not found"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// costRegressionAlerts surfaces fo-4 dashboard cost regressions for a customer.
// Placed alongside lineage watches because both are Phase-6 Wave-1 endpoints
// that read JSON dashboards + query_log.
func (s *Server) costRegressionAlerts(w http.ResponseWriter, r *http.Request) {
	cid, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	out, err := alerts.Detect(r.Context(), s.db.Pool(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
