// Phase 6 Wave 1 — per-user quota CRUD + "don't run, schedule" suggester.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ultraviolet-dev/ultraviolet/internal/costest"
	"github.com/ultraviolet-dev/ultraviolet/internal/quotas"
)

// listQuotas: GET /api/v1/customers/{id}/quotas
func (s *Server) listQuotas(w http.ResponseWriter, r *http.Request) {
	cid, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	out, err := quotas.New(s.db.Pool()).List(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// upsertQuota: POST /api/v1/quotas
func (s *Server) upsertQuota(w http.ResponseWriter, r *http.Request) {
	var q quotas.Quota
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := quotas.New(s.db.Pool()).Upsert(r.Context(), q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, q)
}

// scheduleSuggest: POST /api/v1/cost/schedule-suggest
//
// Combines quota check (fo-7) + preflight cost (an-1). Returns
// {suggest_schedule: true, reason: "..."} when:
//   - Quotas.Check returns false (hard-blocked), or
//   - Estimated cost > 50% of the user's monthly cap (heuristic threshold).
// Otherwise {suggest_schedule: false}.
func (s *Server) scheduleSuggest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CustomerID string `json:"customer_id"`
		SQL        string `json:"sql"`
		UserSlug   string `json:"user_slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.SQL == "" || body.UserSlug == "" {
		writeErr(w, http.StatusBadRequest, errors.New("sql + user_slug required"))
		return
	}
	cid, err := s.resolvePreflightCustomer(r, body.CustomerID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	warehouse, err := s.primaryWarehouseFor(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	bytes, cost, unknown, err := costest.New(s.db, s.cfg.BigQueryUSDPerTiB, s.cfg.SnowflakeUSDPerTiB).
		EstimateSQL(r.Context(), cid, warehouse, body.SQL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	q := quotas.New(s.db.Pool())
	allowed, reason, err := q.Check(r.Context(), cid, body.UserSlug, cost)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	resp := map[string]any{
		"suggest_schedule":      false,
		"estimated_cost_usd":    cost,
		"estimated_bytes":       bytes,
		"warehouse":             warehouse,
		"unknown_tables":        unknown,
		"quota_allowed":         allowed,
		"quota_reason":          reason,
	}
	if !allowed {
		resp["suggest_schedule"] = true
		resp["reason"] = "quota would be exceeded; defer to off-peak schedule: " + reason
		writeJSON(w, http.StatusOK, resp)
		return
	}
	cap, hasCap, err := q.CapFor(r.Context(), cid, body.UserSlug)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if hasCap && cap > 0 && cost > cap*0.5 {
		resp["suggest_schedule"] = true
		resp["reason"] = fmt.Sprintf("estimated $%.2f > 50%% of monthly cap $%.2f — recommend scheduling off-peak", cost, cap)
	}
	writeJSON(w, http.StatusOK, resp)
}
