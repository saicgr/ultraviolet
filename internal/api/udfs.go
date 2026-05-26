// pu-2 — custom DuckDB UDF registry endpoints.
//
// Phase-1 stub: handlers persist into the in-memory workers.UDFRegistry held
// by the Server. See internal/workers/udf_registry.go for the persistence-TODO
// (no migration this round).
package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/workers"
)

// SetUDFRegistry wires the registry the handlers operate on. Safe to call
// before ListenAndServe; nil leaves the endpoints returning 503.
func (s *Server) SetUDFRegistry(r *workers.UDFRegistry) { s.udfs = r }

// listUDFs returns all UDFs registered for the customer (by slug lookup).
func (s *Server) listUDFs(w http.ResponseWriter, r *http.Request) {
	if s.udfs == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("udf registry not configured"))
		return
	}
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// Customer-by-id → slug; UDFRegistry keys by slug to match the DuckDB pool.
	cust, err := s.customerByID(r, id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, s.udfs.List(cust.Slug))
}

// createUDF registers a new UDF for the customer.
func (s *Server) createUDF(w http.ResponseWriter, r *http.Request) {
	if s.udfs == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("udf registry not configured"))
		return
	}
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	cust, err := s.customerByID(r, id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	var body struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	spec := workers.UDFSpec{
		Name: body.Name,
		Kind: workers.UDFKind(body.Kind),
		Body: body.Body,
	}
	if err := s.udfs.Register(cust.Slug, spec); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, spec)
}

// deleteUDF removes a UDF by name across all customers (Phase-1 simplification).
func (s *Server) deleteUDF(w http.ResponseWriter, r *http.Request) {
	if s.udfs == nil {
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("udf registry not configured"))
		return
	}
	name := chi.URLParam(r, "name")
	if name == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("name required"))
		return
	}
	if !s.udfs.Delete(name) {
		writeErr(w, http.StatusNotFound, fmt.Errorf("udf %q not found", name))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// customerByID is a small helper that pulls a customer row by UUID. The store
// package only exposes lookup-by-slug today, so we issue the SELECT inline.
func (s *Server) customerByID(r *http.Request, id uuid.UUID) (*customerRow, error) {
	row := s.db.Pool().QueryRow(r.Context(),
		`SELECT id, slug, display_name FROM customers WHERE id = $1`, id)
	c := &customerRow{}
	if err := row.Scan(&c.ID, &c.Slug, &c.DisplayName); err != nil {
		return nil, err
	}
	return c, nil
}

type customerRow struct {
	ID          uuid.UUID `json:"id"`
	Slug        string    `json:"slug"`
	DisplayName string    `json:"display_name"`
}
