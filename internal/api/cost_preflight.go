// Pre-flight cost check (Phase 6 Wave 1 "an-1"):
//   POST /api/v1/cost/preflight  body {customer_id, sql}
//
// Returns the estimated bytes-scanned + USD cost for `sql` against the
// customer's primary warehouse, plus a flag indicating whether running it
// would push the workspace past its hard cost-budget cap.
//
// Cost model (per docs/architecture/cost-attribution.md):
//   - BigQuery on-demand: $6.25 / TB scanned (UV_BQ_USD_PER_TIB future override).
//   - Snowflake: warehouse-credit proxy via UV_SF_USD_PER_TIB.
// Table-size lookup uses synced_tables.row_count × 256 B/row heuristic when
// present; missing tables fall back to a TODO-marked BigQuery
// INFORMATION_SCHEMA path (never mock data — we return a real "unknown" error).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ultraviolet-dev/ultraviolet/internal/budgets"
	"github.com/ultraviolet-dev/ultraviolet/internal/router"
)

const (
	bqUSDPerTiBDefault = 6.25 // on-demand list price ceiling per docs/architecture/cost-attribution.md
	bytesPerRowGuess   = 256  // heuristic until INFORMATION_SCHEMA path lands
	tib                = 1 << 40
)

type preflightRequest struct {
	CustomerID string `json:"customer_id"`
	SQL        string `json:"sql"`
}

type preflightResponse struct {
	EstimatedBytesScanned   int64   `json:"estimated_bytes_scanned"`
	EstimatedCostUSD        float64 `json:"estimated_cost_usd"`
	Warehouse               string  `json:"warehouse"`
	WouldBlockIfOverBudget  bool    `json:"would_block_if_over_budget"`
	BudgetReason            string  `json:"budget_reason,omitempty"`
	UnknownTables           []string `json:"unknown_tables,omitempty"`
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

	bytes, warehouse, unknown, err := s.estimateScannedBytes(r.Context(), cid, req.SQL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	cost := costForBytes(warehouse, bytes)

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

// estimateScannedBytes walks ExtractTables → synced_tables row counts. Tables
// not present are returned as `unknown`; we still produce a best-effort number
// (sum of known) and let the caller decide. Returns the customer's primary
// warehouse type so cost-per-TB is applied correctly.
func (s *Server) estimateScannedBytes(ctx context.Context, customerID uuid.UUID, sql string) (int64, string, []string, error) {
	refs := router.ExtractTables(sql)
	warehouse, err := s.primaryWarehouseFor(ctx, customerID)
	if err != nil {
		return 0, "", nil, err
	}
	if len(refs) == 0 {
		return 0, warehouse, nil, nil
	}
	var total int64
	var unknown []string
	for _, t := range refs {
		rowCount, found, err := s.lookupRowCount(ctx, customerID, t.Schema, t.Table)
		if err != nil {
			return 0, "", nil, err
		}
		if !found {
			// TODO(phase-2): fall back to BigQuery INFORMATION_SCHEMA
			// (`SELECT total_logical_bytes FROM region-XX.INFORMATION_SCHEMA.TABLE_STORAGE`)
			// for tables that aren't yet synced. Mark unknown for now — never mock.
			label := t.Table
			if t.Schema != "" {
				label = t.Schema + "." + t.Table
			}
			unknown = append(unknown, label)
			continue
		}
		total += rowCount * bytesPerRowGuess
	}
	return total, warehouse, unknown, nil
}

// lookupRowCount inspects synced_tables for a customer-scoped (schema, table)
// match. Returns (rows, found, err). Uses connections.customer_id as the
// authorization boundary.
func (s *Server) lookupRowCount(ctx context.Context, customerID uuid.UUID, schema, table string) (int64, bool, error) {
	row := s.db.Pool().QueryRow(ctx, `
		SELECT t.row_count
		FROM synced_tables t
		JOIN connections c ON c.id = t.connection_id
		WHERE c.customer_id = $1
		  AND t.table_name  = $2
		  AND ($3 = '' OR t.schema_name = $3)
		ORDER BY t.last_sync_at DESC NULLS LAST
		LIMIT 1`, customerID, table, schema)
	var n int64
	if err := row.Scan(&n); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, err
	}
	return n, true, nil
}

// primaryWarehouseFor returns the most-recent connection's warehouse_type
// for a customer. Defaults to "bigquery" when the customer has no
// connections (e.g., during onboarding).
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

// costForBytes converts an estimated byte-scan count into a USD figure using
// the warehouse-specific rate.
func costForBytes(warehouse string, bytes int64) float64 {
	if bytes <= 0 {
		return 0
	}
	rate := bqUSDPerTiBDefault
	switch strings.ToLower(warehouse) {
	case "snowflake":
		if v, err := strconv.ParseFloat(os.Getenv("UV_SF_USD_PER_TIB"), 64); err == nil && v > 0 {
			rate = v
		} else {
			rate = 5.0
		}
	case "bigquery":
		if v, err := strconv.ParseFloat(os.Getenv("UV_BQ_USD_PER_TIB"), 64); err == nil && v > 0 {
			rate = v
		}
	}
	return float64(bytes) / float64(tib) * rate
}
