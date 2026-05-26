// Package api — email subscriptions to dashboards (bu-2).
//
// CRUD over delivery_subscription with target_type='dashboard'. The cron driver
// is out of scope here; the /test-send endpoint dispatches one snapshot through
// notifications.Router so operators can verify wiring without waiting on cron.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/notifications"
)

type dashSubReq struct {
	CustomerID   uuid.UUID `json:"customer_id"`
	UserEmail    string    `json:"user_email"`
	ScheduleCron string    `json:"schedule_cron"`
	Channel      string    `json:"channel"` // optional; defaults to "email"
}

type dashSubRow struct {
	ID           uuid.UUID `json:"id"`
	CustomerID   uuid.UUID `json:"customer_id"`
	TargetID     uuid.UUID `json:"target_id"`
	ScheduleCron string    `json:"schedule_cron"`
	Channel      string    `json:"channel"`
	Address      string    `json:"address"`
	Enabled      bool      `json:"enabled"`
}

// createDashboardSubscription POSTs to /api/v1/dashboards/{id}/subscriptions.
func (s *Server) createDashboardSubscription(w http.ResponseWriter, r *http.Request) {
	dashID, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body dashSubReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.CustomerID == uuid.Nil {
		if cid, cerr := s.activeCustomer(r); cerr == nil {
			body.CustomerID = cid
		} else {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("customer_id required"))
			return
		}
	}
	if body.UserEmail == "" || body.ScheduleCron == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("user_email + schedule_cron required"))
		return
	}
	channel := body.Channel
	if channel == "" {
		channel = "email"
	}
	var id uuid.UUID
	err = s.db.Pool().QueryRow(r.Context(),
		`INSERT INTO delivery_subscription
		   (customer_id, target_type, target_id, schedule_cron, channel, address, enabled)
		 VALUES ($1, 'dashboard', $2, $3, $4, $5, true)
		 RETURNING id`,
		body.CustomerID, dashID, body.ScheduleCron, channel, body.UserEmail,
	).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]uuid.UUID{"id": id})
}

// listDashboardSubscriptions GETs /api/v1/dashboards/{id}/subscriptions.
func (s *Server) listDashboardSubscriptions(w http.ResponseWriter, r *http.Request) {
	dashID, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rows, err := s.db.Pool().Query(r.Context(),
		`SELECT id, customer_id, target_id, schedule_cron, channel, address, enabled
		 FROM delivery_subscription
		 WHERE target_type='dashboard' AND target_id = $1
		 ORDER BY created_at DESC`, dashID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := []dashSubRow{}
	for rows.Next() {
		var d dashSubRow
		if err := rows.Scan(&d.ID, &d.CustomerID, &d.TargetID, &d.ScheduleCron, &d.Channel, &d.Address, &d.Enabled); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, out)
}

// deleteSubscription DELETEs /api/v1/subscriptions/{id}.
func (s *Server) deleteSubscription(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if _, err := s.db.Pool().Exec(r.Context(),
		`DELETE FROM delivery_subscription WHERE id = $1`, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// testSendSubscription POSTs /api/v1/subscriptions/{id}/test-send — sends a
// canned snapshot payload through notifications.Router so the address can be
// verified end-to-end without waiting on cron.
func (s *Server) testSendSubscription(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var (
		customerID uuid.UUID
		targetID   uuid.UUID
		channel    string
		address    string
	)
	err = s.db.Pool().QueryRow(r.Context(),
		`SELECT customer_id, target_id, channel, address
		 FROM delivery_subscription WHERE id = $1`, id).
		Scan(&customerID, &targetID, &channel, &address)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	// Build a one-off router constrained to just this subscription's channel.
	smtp, _ := notifications.NewSMTPSenderFromEnv() // nil if SMTP_HOST unset
	router := notifications.NewRouter(s.db.Pool(), smtp, nil)
	perr := router.Send(r.Context(), customerID, notifications.KindScheduledRun, notifications.Payload{
		Title:    fmt.Sprintf("Dashboard subscription test (%s)", channel),
		Body:     fmt.Sprintf("Test snapshot for dashboard %s", targetID),
		Link:     fmt.Sprintf("/dashboards/%s", targetID),
		Severity: "info",
	})
	resp := map[string]any{"sent": perr == nil}
	if perr != nil {
		resp["error"] = perr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}
