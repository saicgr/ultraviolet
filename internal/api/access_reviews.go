package api

// go-6 Access reviews — quarterly RBAC review flow.
//
//   POST /api/v1/access-reviews                          open a review
//   GET  /api/v1/customers/{id}/access-reviews           list reviews for customer
//   POST /api/v1/access-reviews/{id}/decisions           record a keep/revoke
//   POST /api/v1/access-reviews/{id}/close               finalise the review
//
// All handlers require role admin or superadmin. Per-decision rows are
// append-only; closing the review is irreversible.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type accessReview struct {
	ID         uuid.UUID  `json:"id"`
	CustomerID uuid.UUID  `json:"customer_id"`
	Period     string     `json:"period"`
	Status     string     `json:"status"`
	OpenedAt   time.Time  `json:"opened_at"`
	ClosedAt   *time.Time `json:"closed_at,omitempty"`
}

type accessReviewDecision struct {
	ID         uuid.UUID `json:"id"`
	ReviewID   uuid.UUID `json:"review_id"`
	UserID     uuid.UUID `json:"user_id"`
	Role       string    `json:"role"`
	Action     string    `json:"action"`
	Reviewer   string    `json:"reviewer"`
	DecidedAt  time.Time `json:"decided_at"`
}

func (s *Server) openAccessReview(w http.ResponseWriter, r *http.Request) {
	if err := requireRole(r, "admin", "superadmin"); err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	var body struct {
		CustomerID uuid.UUID `json:"customer_id"`
		Period     string    `json:"period"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.CustomerID == uuid.Nil || body.Period == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("customer_id + period required"))
		return
	}
	var rev accessReview
	err := s.db.Pool().QueryRow(r.Context(), `
		INSERT INTO access_review (customer_id, period)
		VALUES ($1, $2)
		RETURNING id, customer_id, period, status, opened_at, closed_at`,
		body.CustomerID, body.Period,
	).Scan(&rev.ID, &rev.CustomerID, &rev.Period, &rev.Status, &rev.OpenedAt, &rev.ClosedAt)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, rev)
}

func (s *Server) listAccessReviews(w http.ResponseWriter, r *http.Request) {
	cid, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rows, err := s.db.Pool().Query(r.Context(), `
		SELECT id, customer_id, period, status, opened_at, closed_at
		FROM access_review
		WHERE customer_id = $1
		ORDER BY opened_at DESC`, cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := []accessReview{}
	for rows.Next() {
		var rec accessReview
		if err := rows.Scan(&rec.ID, &rec.CustomerID, &rec.Period, &rec.Status, &rec.OpenedAt, &rec.ClosedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, rec)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) recordAccessReviewDecision(w http.ResponseWriter, r *http.Request) {
	if err := requireRole(r, "admin", "superadmin"); err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	reviewID, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		UserID   uuid.UUID `json:"user_id"`
		Role     string    `json:"role"`
		Action   string    `json:"action"`
		Reviewer string    `json:"reviewer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.UserID == uuid.Nil || body.Role == "" || body.Reviewer == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("user_id, role, reviewer required"))
		return
	}
	if body.Action != "keep" && body.Action != "revoke" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("action must be keep or revoke"))
		return
	}

	// Reject decisions against closed reviews so a finalised audit trail
	// can't be retroactively re-litigated.
	var status string
	if err := s.db.Pool().QueryRow(r.Context(),
		`SELECT status FROM access_review WHERE id = $1`, reviewID).Scan(&status); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if status != "open" {
		writeErr(w, http.StatusConflict, fmt.Errorf("review is %s, cannot record decision", status))
		return
	}

	var dec accessReviewDecision
	err = s.db.Pool().QueryRow(r.Context(), `
		INSERT INTO access_review_decision (review_id, user_id, role, action, reviewer)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING id, review_id, user_id, role, action, reviewer, decided_at`,
		reviewID, body.UserID, body.Role, body.Action, body.Reviewer,
	).Scan(&dec.ID, &dec.ReviewID, &dec.UserID, &dec.Role, &dec.Action, &dec.Reviewer, &dec.DecidedAt)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, dec)
}

func (s *Server) closeAccessReview(w http.ResponseWriter, r *http.Request) {
	if err := requireRole(r, "admin", "superadmin"); err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	reviewID, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ct, err := s.db.Pool().Exec(r.Context(), `
		UPDATE access_review
		SET status = 'completed', closed_at = now()
		WHERE id = $1 AND status = 'open'`, reviewID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if ct.RowsAffected() == 0 {
		writeErr(w, http.StatusConflict, fmt.Errorf("review not found or already closed"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": reviewID, "status": "completed"})
}
