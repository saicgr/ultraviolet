package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/budgets"
	"github.com/ultraviolet-dev/ultraviolet/internal/dashboards"
	"github.com/ultraviolet-dev/ultraviolet/internal/exports"
	"github.com/ultraviolet-dev/ultraviolet/internal/notifications"
	"github.com/ultraviolet-dev/ultraviolet/internal/savedqueries"
	"github.com/ultraviolet-dev/ultraviolet/internal/sharing"
)

// ---- Saved queries ----

func (s *Server) listSavedQueries(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	favOnly := r.URL.Query().Get("favorite") == "true"
	store := savedqueries.NewStore(s.db.Pool())
	out, err := store.List(r.Context(), cid, favOnly)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) upsertSavedQuery(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var q savedqueries.SavedQuery
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	q.CustomerID = cid
	store := savedqueries.NewStore(s.db.Pool())
	id, err := store.Upsert(r.Context(), &q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]uuid.UUID{"id": id})
}

func (s *Server) deleteSavedQuery(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := savedqueries.NewStore(s.db.Pool()).Delete(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Notifications inbox ----

func (s *Server) listInbox(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	unread := r.URL.Query().Get("unread") == "true"
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := notifications.New(s.db.Pool()).List(r.Context(), cid, nil, unread, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) inboxBadge(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
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

func (s *Server) markInboxRead(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := notifications.New(s.db.Pool()).MarkRead(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) markInboxAllRead(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := notifications.New(s.db.Pool()).MarkAllRead(r.Context(), cid, nil); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Annotations: now live in annotations_handlers.go (an-10) ----

// ---- Budget ----

func (s *Server) getBudgetState(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	st, err := budgets.New(s.db.Pool()).Status(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) upsertBudget(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var b budgets.Budget
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	b.CustomerID = cid
	if err := budgets.New(s.db.Pool()).Upsert(r.Context(), b); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Pre-flight cost check (block / warn before running expensive query) ----

func (s *Server) preflightCost(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	allow, st, _ := budgets.New(s.db.Pool()).PreflightAllow(r.Context(), cid, body.EstimatedCostUSD)
	writeJSON(w, http.StatusOK, map[string]any{
		"allow":           allow,
		"reason":          ifElse(allow, "ok", "would exceed monthly hard cap"),
		"budget_state":    st,
	})
}

func ifElse(b bool, a, c string) string {
	if b {
		return a
	}
	return c
}

// ---- Share tokens ----

func (s *Server) mintShareToken(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		TargetType string `json:"target_type"`
		TargetID   string `json:"target_id"`
		TTLDays    int    `json:"ttl_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	tid, err := uuid.Parse(body.TargetID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ttl := time.Duration(body.TTLDays) * 24 * time.Hour
	tok, err := sharing.New(s.db.Pool()).Mint(r.Context(), sharing.Token{
		CustomerID: cid, TargetType: body.TargetType, TargetID: tid,
	}, ttl)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"token": tok})
}

func (s *Server) resolveShareToken(w http.ResponseWriter, r *http.Request) {
	tok := chi.URLParam(r, "token")
	t, err := sharing.New(s.db.Pool()).Resolve(r.Context(), tok)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if t.TargetType == "dashboard" {
		d, err := dashboards.NewStore(s.db.Pool()).Get(r.Context(), t.TargetID)
		if err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, http.StatusOK, d)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) revokeShareToken(w http.ResponseWriter, r *http.Request) {
	tok := chi.URLParam(r, "token")
	if err := sharing.New(s.db.Pool()).Revoke(r.Context(), tok); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Export results ----

func (s *Server) exportResult(w http.ResponseWriter, r *http.Request) {
	format := exports.Format(r.URL.Query().Get("format"))
	if format == "" {
		format = exports.CSV
	}
	// Phase-1: read result-rows from the body so workbench can POST what's
	// already on screen. Phase-2 wires query_result_cache for re-export of
	// queries that have run.
	var body struct {
		Columns []string `json:"columns"`
		Rows    [][]any  `json:"rows"`
		Filename string  `json:"filename"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Filename == "" {
		body.Filename = "result." + string(format)
	}
	w.Header().Set("Content-Type", exports.MIMEType(format))
	w.Header().Set("Content-Disposition", `attachment; filename="`+body.Filename+`"`)
	if err := exports.Write(w, exports.Result{Columns: body.Columns, Rows: body.Rows}, format); err != nil {
		s.log.Warn().Err(err).Msg("export")
	}
}
