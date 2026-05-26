// Package api — inbox wiring (co-1).
//
// Lightweight wrappers around internal/notifications that accept the explicit
// REST shape requested in co-1: `?customer_id=...&unread_only=true`, plus
// POST /inbox/{id}/read and POST /inbox/read-all. Falls back to activeCustomer
// when customer_id is omitted so the UI badge keeps working.
package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/notifications"
)

// resolveInboxCustomer prefers an explicit customer_id query param, then
// JWT-derived activeCustomer. Returns 400 if neither is present.
func (s *Server) resolveInboxCustomer(r *http.Request) (uuid.UUID, error) {
	if v := r.URL.Query().Get("customer_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return uuid.Nil, fmt.Errorf("invalid customer_id: %w", err)
		}
		return id, nil
	}
	return s.activeCustomer(r)
}

// inboxList implements GET /api/v1/inbox?customer_id=...&unread_only=true.
func (s *Server) inboxList(w http.ResponseWriter, r *http.Request) {
	cid, err := s.resolveInboxCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// Accept either unread_only (co-1 spec) or unread (existing).
	q := r.URL.Query()
	unread := q.Get("unread_only") == "true" || q.Get("unread") == "true"
	limit, _ := strconv.Atoi(q.Get("limit"))
	out, err := notifications.New(s.db.Pool()).List(r.Context(), cid, nil, unread, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// inboxMarkRead implements POST /api/v1/inbox/{id}/read.
func (s *Server) inboxMarkRead(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid id: %w", err))
		return
	}
	if err := notifications.New(s.db.Pool()).MarkRead(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// inboxMarkAllRead implements POST /api/v1/inbox/read-all.
func (s *Server) inboxMarkAllRead(w http.ResponseWriter, r *http.Request) {
	cid, err := s.resolveInboxCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := notifications.New(s.db.Pool()).MarkAllRead(r.Context(), cid, nil); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// inboxBadgeCount implements GET /api/v1/inbox/unread-count.
func (s *Server) inboxBadgeCount(w http.ResponseWriter, r *http.Request) {
	cid, err := s.resolveInboxCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	n, err := notifications.New(s.db.Pool()).UnreadCount(r.Context(), cid, nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"unread": n})
}
