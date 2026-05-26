package router

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/ultraviolet-dev/ultraviolet/internal/metrics"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
)

// Target identifies which backend serves a query.
type Target string

const (
	TargetDuckDB    Target = "duckdb"
	TargetWarehouse Target = "warehouse"
	TargetUnknown   Target = "unknown"
)

// Decision is what Router.Decide returns.
type Decision struct {
	Target Target
	Class  QueryClass
	Tables []TableRef
	Reason string
}

// Router is stateless except for the DB handle.
type Router struct {
	db  *store.DB
	log zerolog.Logger
}

func New(db *store.DB, log zerolog.Logger) *Router {
	return &Router{db: db, log: log.With().Str("component", "router").Logger()}
}

// Decide picks DuckDB vs warehouse for a SQL query against the given customer's warehouse.
func (r *Router) Decide(ctx context.Context, customerID uuid.UUID, warehouseType, sql string) Decision {
	class := Classify(sql)
	d := Decision{Class: class}

	// Non-SELECT goes to warehouse: writes, DDL, utility statements.
	if class != ClassSelect {
		d.Target = TargetWarehouse
		d.Reason = fmt.Sprintf("class=%s", class)
		metrics.Inc("uv_route_decision_total", map[string]string{"target": string(d.Target)})
		return d
	}

	tables := ExtractTables(sql)
	d.Tables = tables
	if len(tables) == 0 {
		d.Target = TargetWarehouse
		d.Reason = "no tables extracted"
		return d
	}

	// All referenced tables must be synced + fresh for DuckDB to serve.
	for _, t := range tables {
		st, err := r.db.LookupSyncedTable(ctx, customerID, warehouseType, t.Schema, t.Table)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				d.Target = TargetWarehouse
				d.Reason = fmt.Sprintf("not synced: %s.%s", t.Schema, t.Table)
				return d
			}
			d.Target = TargetWarehouse
			d.Reason = "lookup error: " + err.Error()
			return d
		}
		if st.State != "live" {
			d.Target = TargetWarehouse
			d.Reason = fmt.Sprintf("table state=%s", st.State)
			return d
		}
		if !IsFresh(st.LastSyncAt) {
			d.Target = TargetWarehouse
			d.Reason = fmt.Sprintf("stale: %s.%s", t.Schema, t.Table)
			return d
		}
	}
	d.Target = TargetDuckDB
	d.Reason = "all tables synced+fresh"
	metrics.Inc("uv_route_decision_total", map[string]string{"target": string(d.Target)})
	return d
}
