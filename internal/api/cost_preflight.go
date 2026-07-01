// Pre-flight cost check (Phase 6 Wave 1 "an-1"):
//   POST /api/v1/cost/preflight  body {customer_id, sql}
//
// Returns the estimated bytes-scanned + USD cost for `sql` against the
// customer's primary warehouse, plus a flag indicating whether running it
// would push the workspace past its hard cost-budget cap. The cost model lives
// in internal/costest (shared with the backfiller) so pre-flight and
// attribution never diverge.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/budgets"
	"github.com/ultraviolet-dev/ultraviolet/internal/costest"
)

type preflightRequest struct {
	CustomerID string `json:"customer_id"`
	SQL        string `json:"sql"`
}

type preflightResponse struct {
	EstimatedBytesScanned  int64    `json:"estimated_bytes_scanned"`
	EstimatedCostUSD       float64  `json:"estimated_cost_usd"`
	Warehouse              string   `json:"warehouse"`
	WouldBlockIfOverBudget bool     `json:"would_block_if_over_budget"`
	BudgetReason           string   `json:"budget_reason,omitempty"`
	UnknownTables          []string `json:"unknown_tables,omitempty"`
}

// preflightCheck handles POST /api/v1/cost/preflight.
func (s *Server) preflightCheck(w http.ResponseWriter, r *http.Request) {
	var req preflightRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.SQL == "" {
		writeErr(w, http.StatusBadRequest, errors.New("sql required"))
		return
	}
	cid, err := s.resolvePreflightCustomer(r, req.CustomerID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	warehouse, err := s.primaryWarehouseFor(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	est := costest.New(s.db, s.cfg.BigQueryUSDPerTiB, s.cfg.SnowflakeUSDPerTiB)
	bytes, cost, unknown, err := est.EstimateSQL(r.Context(), cid, warehouse, req.SQL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	allow, st, _ := budgets.New(s.db.Pool()).PreflightAllow(r.Context(), cid, cost)
	resp := preflightResponse{
		EstimatedBytesScanned:  bytes,
		EstimatedCostUSD:       cost,
		Warehouse:              warehouse,
		WouldBlockIfOverBudget: !allow,
		UnknownTables:          unknown,
	}
	if !allow {
		resp.BudgetReason = fmt.Sprintf("would exceed hard cap: spend $%.2f + est $%.2f > cap $%.2f",
			st.SpendMTDUSD, cost, st.Budget.MonthlyCapUSD)
	}
	writeJSON(w, http.StatusOK, resp)
}

// resolvePreflightCustomer prefers an explicit body value, falling back to the
// session's active customer. We never default silently to a random customer.
func (s *Server) resolvePreflightCustomer(r *http.Request, raw string) (uuid.UUID, error) {
	if raw != "" {
		cid, err := uuid.Parse(raw)
		if err != nil {
			return uuid.Nil, fmt.Errorf("invalid customer_id: %w", err)
		}
		return cid, nil
	}
	return s.activeCustomer(r)
}

// primaryWarehouseFor returns the most-recent connection's warehouse_type for a
// customer. Defaults to "bigquery" when the customer has no connections (e.g.,
// during onboarding).
func (s *Server) primaryWarehouseFor(ctx context.Context, customerID uuid.UUID) (string, error) {
	conns, err := s.db.ListConnections(ctx, customerID)
	if err != nil {
		return "", err
	}
	if len(conns) == 0 {
		return "bigquery", nil
	}
	return conns[0].WarehouseType, nil
}
