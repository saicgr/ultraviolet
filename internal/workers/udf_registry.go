// Package workers — UDF registry (pu-2).
//
// Phase-1 backend stub for per-workspace custom DuckDB UDFs. The DuckDB Go
// binding (marcboeker/go-duckdb) does not expose easy runtime UDF registration
// without CGo gymnastics, so this file ONLY records the desired UDFs in
// process memory; actual installation into a DuckDB worker is a Phase-2 task.
//
// PERSISTENCE-TODO: spec entries are kept in a sync.Map keyed by customer slug.
// We deliberately avoid adding a `customer_udf` table here because that would
// require a migration (out of scope for this round). On process restart all
// registered UDFs are lost. Once we add the migration this file should swap
// the sync.Map for pgxpool queries — the UDFSpec shape already matches the
// intended table layout (customer_id, name, kind, body, created_at).
//
// INTEGRATION-TODO: at the ATTACH path in pool.go::workerFor we should iterate
// the registry for `slug` and `CREATE [TEMP] MACRO / FUNCTION` each entry into
// the new worker. That wiring is intentionally NOT done here — see the TODO
// comment in pool.go — because touching the worker boot path is risky and the
// real path needs DuckDB scalar/aggregate function registration support.
package workers

import (
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// UDFKind enumerates the UDF flavours the registry accepts. The body string is
// raw DuckDB SQL (e.g. `CREATE MACRO add_one(x) AS x + 1`) — the registry does
// NOT parse or validate beyond a non-empty check, mirroring how Snowflake +
// BigQuery store UDF bodies as opaque text.
type UDFKind string

const (
	UDFKindMacro  UDFKind = "macro"
	UDFKindScalar UDFKind = "scalar"
)

// UDFSpec is the persisted shape. Matches the (eventual) customer_udf row.
type UDFSpec struct {
	Name      string    `json:"name"`
	Kind      UDFKind   `json:"kind"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// UDFRegistry stores per-customer UDF specs. Safe for concurrent use.
type UDFRegistry struct {
	pool *pgxpool.Pool // reserved for the Phase-2 persistent table
	mu   sync.Mutex
	// entries map keyed by customer slug → list of UDFSpec.
	entries map[string][]UDFSpec
}

// NewUDFRegistry constructs an empty registry. `pool` may be nil in tests; it
// is currently unused but kept on the struct for the Phase-2 migration.
func NewUDFRegistry(pool *pgxpool.Pool) *UDFRegistry {
	return &UDFRegistry{pool: pool, entries: map[string][]UDFSpec{}}
}

// Register adds a new spec for `customerSlug`. Returns an error if a UDF with
// the same name already exists for that customer — names must be unique within
// a workspace so the apply step can issue an idempotent CREATE OR REPLACE.
func (r *UDFRegistry) Register(customerSlug string, spec UDFSpec) error {
	if customerSlug == "" {
		return errors.New("customer slug required")
	}
	if spec.Name == "" || spec.Body == "" {
		return errors.New("udf name + body required")
	}
	if spec.Kind == "" {
		spec.Kind = UDFKindMacro
	}
	if spec.CreatedAt.IsZero() {
		spec.CreatedAt = time.Now().UTC()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries[customerSlug] {
		if e.Name == spec.Name {
			return errors.New("udf name already registered")
		}
	}
	r.entries[customerSlug] = append(r.entries[customerSlug], spec)
	return nil
}

// List returns a copy of the registered specs for `customerSlug`.
func (r *UDFRegistry) List(customerSlug string) []UDFSpec {
	r.mu.Lock()
	defer r.mu.Unlock()
	src := r.entries[customerSlug]
	out := make([]UDFSpec, len(src))
	copy(out, src)
	return out
}

// Delete removes a UDF by name across all customers; returns true if anything
// was removed. The API surface scopes by name only (no customer in the path),
// matching the Phase-1 REST shape.
func (r *UDFRegistry) Delete(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := false
	for slug, specs := range r.entries {
		kept := specs[:0]
		for _, s := range specs {
			if s.Name == name {
				removed = true
				continue
			}
			kept = append(kept, s)
		}
		r.entries[slug] = kept
	}
	return removed
}
