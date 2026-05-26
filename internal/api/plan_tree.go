// Phase 6 ergonomics (de-8): query plan visualizer.
//
// POST /api/v1/queries/{hash}/plan-tree body {customer_id, sql?}.
//
// When `sql` is provided we would normally dispatch `EXPLAIN (FORMAT JSON)` to
// the customer's connector and normalize the resulting tree. Today the
// Connector interface only exposes ExecuteStreaming which writes pgwire frames
// directly to a wire writer — we have no in-process JSON-capture path. Rather
// than fake a plan (project rule: no mock/fallback data) we return 501 with an
// explicit message until a connector grows an ExecuteRaw / ExecuteJSON method.
//
// The query_log.plan_json cache lookup is also TODO — column does not exist in
// the current schema. Documented as IMPROVEMENTS.md follow-up; no silent
// fallback.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// PlanNode is the normalized cross-warehouse plan node. Postgres-style trees
// map cleanly; BigQuery + Snowflake plans differ in shape so we tag them with
// `warehouse` and pass through the raw JSON for the UI to render as needed.
type PlanNode struct {
	Op        string          `json:"op"`
	RowsEst   float64         `json:"rows_est"`
	CostEst   float64         `json:"cost_est"`
	Warehouse string          `json:"warehouse,omitempty"`
	Raw       json.RawMessage `json:"raw,omitempty"`
	Children  []PlanNode      `json:"children,omitempty"`
}

type planTreeRequest struct {
	CustomerID string `json:"customer_id"`
	SQL        string `json:"sql,omitempty"`
}

func (s *Server) queryPlanTree(w http.ResponseWriter, r *http.Request) {
	hash := chi.URLParam(r, "hash")
	if hash == "" {
		writeErr(w, http.StatusBadRequest, errors.New("hash required"))
		return
	}
	var body planTreeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.CustomerID == "" {
		writeErr(w, http.StatusBadRequest, errors.New("customer_id required"))
		return
	}
	if _, err := uuid.Parse(body.CustomerID); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("customer_id: %w", err))
		return
	}

	sql := strings.TrimSpace(body.SQL)
	if sql == "" {
		// query_log.plan_json column does not exist today. Per project rule
		// we do not silently degrade — return 501 with an explicit message.
		writeErr(w, http.StatusNotImplemented,
			errors.New("plan-tree: cached plan_json not stored yet; pass `sql` to trigger EXPLAIN"))
		return
	}

	// No connector currently exposes EXPLAIN (FORMAT JSON) with structured
	// in-process capture — ExecuteStreaming writes pgwire frames straight to
	// the client. Returning a faked plan would violate the no-mock-data rule.
	writeErr(w, http.StatusNotImplemented,
		fmt.Errorf("plan-tree: no connector supports structured EXPLAIN capture today (hash=%s)", hash))
}

// normalizePostgresPlan walks a Postgres `EXPLAIN (FORMAT JSON)` payload into a
// PlanNode. Exposed for future use when a connector grows EXPLAIN support.
func normalizePostgresPlan(raw json.RawMessage) (PlanNode, error) {
	// Postgres returns `[{"Plan": {...}}]`.
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return PlanNode{}, err
	}
	if len(arr) == 0 {
		return PlanNode{}, errors.New("empty plan array")
	}
	planRaw, ok := arr[0]["Plan"]
	if !ok {
		return PlanNode{}, errors.New("no Plan key")
	}
	return walkPGPlan(planRaw)
}

func walkPGPlan(raw json.RawMessage) (PlanNode, error) {
	var m struct {
		NodeType     string            `json:"Node Type"`
		PlanRows     float64           `json:"Plan Rows"`
		TotalCost    float64           `json:"Total Cost"`
		Plans        []json.RawMessage `json:"Plans"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return PlanNode{}, err
	}
	out := PlanNode{
		Op:      m.NodeType,
		RowsEst: m.PlanRows,
		CostEst: m.TotalCost,
		Raw:     raw,
	}
	for _, c := range m.Plans {
		child, err := walkPGPlan(c)
		if err != nil {
			return PlanNode{}, err
		}
		out.Children = append(out.Children, child)
	}
	return out, nil
}
