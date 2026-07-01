package api

// Workbench query execution.
//
//	POST /api/v1/workbench/run  body {sql}
//
// When a QueryRunner is wired (the demo wires a *workers.Pool), this runs the
// SQL on the SAME DuckDB engine the proxy uses, logs the run to query_log with a
// warehouse-equivalent cost estimate, and rolls today's cost_attribution so the
// savings dashboard reflects real activity. Without a runner it returns the
// psql connect hint (the plain control plane delegates execution to the proxy).
//
// The cost model is deliberately simple and honest: estimated_cost_usd =
// bytes_produced / TiB × the configured warehouse $/TiB rate. That figure is the
// warehouse-equivalent cost the DuckDB route avoided — i.e. the saving — so a
// query that scans more shows a larger (real) saving. No invented numbers.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/cost"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
)

const bytesPerTiB = 1 << 40

func (s *Server) workbenchRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SQL string `json:"sql"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SQL == "" {
		writeErr(w, http.StatusBadRequest, errors.New("sql required"))
		return
	}

	// No runner wired → plain control plane delegates to the proxy.
	if s.queryRunner == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"sql":   body.SQL,
			"rows":  []any{},
			"route": "proxy",
			"hint":  "connect via psql -h localhost -p 5000 -U <slug> -d <slug>_bigquery",
		})
		return
	}

	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	slug, connID := s.customerSlugAndConn(r.Context(), cid)

	t0 := time.Now()
	cols, rows, rowCount, bytesScanned, runErr := s.queryRunner.ExecuteRows(r.Context(), slug, body.SQL)
	durMS := int(time.Since(t0).Milliseconds())
	if runErr != nil {
		// Surface the real DuckDB error — never a mock success.
		writeErr(w, http.StatusBadRequest, runErr)
		return
	}

	// Warehouse-equivalent cost of this query (the saving from serving it on DuckDB).
	est := float64(bytesScanned) / float64(bytesPerTiB) * s.cfg.BigQueryUSDPerTiB
	sum := sha256.Sum256([]byte(body.SQL))
	logErr := s.db.InsertQueryLog(r.Context(), store.QueryLogEntry{
		CustomerID:       cid,
		ConnectionID:     connID,
		QueryHash:        hex.EncodeToString(sum[:8]),
		NormalizedSQL:    body.SQL,
		RouteDecision:    "duckdb",
		DurationMS:       durMS,
		RowsReturned:     rowCount,
		BytesScanned:     &bytesScanned,
		EstimatedCostUSD: &est,
	})
	if logErr != nil {
		s.log.Warn().Err(logErr).Msg("workbench query_log insert")
	} else if connID != nil {
		// Roll today's activity into cost_attribution so the savings dashboard
		// updates immediately — the same pipeline the nightly cron runs.
		if rerr := cost.New(s.cfg, s.db, s.enc, s.log).RollupCustomer(r.Context(), cid); rerr != nil {
			s.log.Warn().Err(rerr).Msg("workbench cost rollup")
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"columns":            cols,
		"rows":               rows,
		"row_count":          rowCount,
		"truncated":          rowCount > int64(len(rows)),
		"route":              "duckdb",
		"duration_ms":        durMS,
		"bytes_scanned":      bytesScanned,
		"estimated_cost_usd": est,
	})
}

// customerSlugAndConn resolves the customer's slug (for the DuckDB worker key)
// and primary connection id (to attribute the query_log row). Best-effort: a
// missing connection just means no rollup (the query still runs + logs).
func (s *Server) customerSlugAndConn(ctx context.Context, cid uuid.UUID) (string, *uuid.UUID) {
	slug := cid.String()
	if cs, err := s.db.ListCustomers(ctx); err == nil {
		for i := range cs {
			if cs[i].ID == cid {
				slug = cs[i].Slug
				break
			}
		}
	}
	var connID *uuid.UUID
	if conns, err := s.db.ListConnections(ctx, cid); err == nil && len(conns) > 0 {
		connID = &conns[0].ID
	}
	return slug, connID
}
