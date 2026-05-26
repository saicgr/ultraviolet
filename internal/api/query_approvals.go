// Phase 6 Wave 3 go-7: query approvals on PII tables.
//
// Endpoints:
//   POST   /api/v1/queries/approvals                 — submit
//   GET    /api/v1/queries/approvals?status=pending  — list
//   POST   /api/v1/queries/approvals/{id}/decide     — approve|deny
//
// Submitting a query for approval persists the SQL + the list of PII columns
// it touches (computed elsewhere — typically via /workbench/privacy-preview).
// An approver reviews and decides. Status transitions are append-only via
// `decide`: only pending → approved | denied is allowed.

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type queryApproval struct {
	ID          uuid.UUID  `json:"id"`
	CustomerID  uuid.UUID  `json:"customer_id"`
	Requester   string     `json:"requester"`
	SQL         string     `json:"sql"`
	PIIColumns  []string   `json:"pii_columns"`
	Status      string     `json:"status"`
	Approver    *string    `json:"approver,omitempty"`
	DecidedAt   *time.Time `json:"decided_at,omitempty"`
	RequestedAt time.Time  `json:"requested_at"`
}

type submitApprovalRequest struct {
	Requester  string   `json:"requester"`
	SQL        string   `json:"sql"`
	PIIColumns []string `json:"pii_columns"`
}

func (s *Server) submitQueryApproval(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body submitApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.Requester == "" || body.SQL == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("requester + sql required"))
		return
	}
	if body.PIIColumns == nil {
		body.PIIColumns = []string{}
	}
	var id uuid.UUID
	var requestedAt time.Time
	err = s.db.Pool().QueryRow(r.Context(),
		`INSERT INTO query_approval (customer_id, requester, sql, pii_columns)
		 VALUES ($1,$2,$3,$4) RETURNING id, requested_at`,
		cid, body.Requester, body.SQL, body.PIIColumns,
	).Scan(&id, &requestedAt)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":           id,
		"status":       "pending",
		"requested_at": requestedAt,
	})
}

func (s *Server) listQueryApprovals(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	status := r.URL.Query().Get("status")
	query := `SELECT id, customer_id, requester, sql, pii_columns, status, approver, decided_at, requested_at
	          FROM query_approval WHERE customer_id = $1`
	args := []any{cid}
	if status != "" {
		query += ` AND status = $2`
		args = append(args, status)
	}
	query += ` ORDER BY requested_at DESC LIMIT 200`

	rows, err := s.db.Pool().Query(r.Context(), query, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := []queryApproval{}
	for rows.Next() {
		var qa queryApproval
		if err := rows.Scan(&qa.ID, &qa.CustomerID, &qa.Requester, &qa.SQL,
			&qa.PIIColumns, &qa.Status, &qa.Approver, &qa.DecidedAt, &qa.RequestedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, qa)
	}
	writeJSON(w, http.StatusOK, out)
}

type decideApprovalRequest struct {
	Decision string `json:"decision"` // "approve" | "deny"
	Approver string `json:"approver"`
}

func (s *Server) decideQueryApproval(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body decideApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var newStatus string
	switch body.Decision {
	case "approve":
		newStatus = "approved"
	case "deny":
		newStatus = "denied"
	default:
		writeErr(w, http.StatusBadRequest, fmt.Errorf("decision must be approve|deny"))
		return
	}
	if body.Approver == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("approver required"))
		return
	}

	ct, err := s.db.Pool().Exec(r.Context(),
		`UPDATE query_approval
		   SET status = $1, approver = $2, decided_at = now()
		 WHERE id = $3 AND status = 'pending'`,
		newStatus, body.Approver, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, fmt.Errorf("approval not found"))
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if ct.RowsAffected() == 0 {
		writeErr(w, http.StatusConflict, fmt.Errorf("approval not pending or not found"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": newStatus})
}
