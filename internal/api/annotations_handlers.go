// Annotations REST surface (an-10). Row-level / tile / lineage-node annotations.
//
// Endpoints:
//
//	POST   /api/v1/annotations                       create
//	GET    /api/v1/annotations?target_kind=&target_id= list
//	DELETE /api/v1/annotations/{id}                  revoke (author or admin)
//
// `target_kind` ∈ {query_result, dashboard_tile, lineage_node}. The optional
// `row_key` pins an annotation to a specific row inside a result set (e.g. a
// hash of the PK columns of a `query_result` row).
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ultraviolet-dev/ultraviolet/internal/annotations"
)

// allowedAnnotationTargets is the closed set accepted on POST. Stored in the
// `annotation.target_type` column; the DB CHECK constraint enforces the same
// set defensively (see migration 0018).
var allowedAnnotationTargets = map[string]bool{
	"query_result":   true,
	"dashboard_tile": true,
	"lineage_node":   true,
}

func (s *Server) createAnnotation(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		CustomerID *uuid.UUID `json:"customer_id"`
		TargetKind string     `json:"target_kind"`
		TargetID   string     `json:"target_id"`
		RowKey     *string    `json:"row_key"`
		Body       string     `json:"body"`
		Author     *uuid.UUID `json:"author"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if !allowedAnnotationTargets[body.TargetKind] {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("target_kind must be one of query_result|dashboard_tile|lineage_node"))
		return
	}
	if body.TargetID == "" || body.Body == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("target_id and body are required"))
		return
	}
	if body.CustomerID != nil {
		cid = *body.CustomerID
	}
	a := annotations.Annotation{
		CustomerID: cid,
		UserID:     body.Author,
		TargetType: body.TargetKind,
		TargetID:   body.TargetID,
		RowKey:     body.RowKey,
		Body:       body.Body,
	}
	store := annotations.New(s.db.Pool())
	id, err := store.Create(r.Context(), a)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]uuid.UUID{"id": id})
}

func (s *Server) listAnnotations(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	kind := r.URL.Query().Get("target_kind")
	tid := r.URL.Query().Get("target_id")
	if kind == "" || tid == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("target_kind + target_id query params required"))
		return
	}
	out, err := annotations.New(s.db.Pool()).ForTarget(r.Context(), cid, kind, tid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if out == nil {
		out = []annotations.Annotation{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteAnnotation(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid id: %w", err))
		return
	}
	store := annotations.New(s.db.Pool())
	author, _, err := store.GetAuthor(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, fmt.Errorf("not found"))
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Authorization: requesting principal must be the author, or admin.
	// Admin check is the dev-bypass header pending real RBAC (Phase 2); it is
	// ignored in production (UV_PROD=true), same as api.authMiddleware.
	caller := UserIDFromContext(r.Context())
	isAdmin := !s.cfg.Prod && r.Header.Get("X-UV-Dev-Bypass") == "1"
	if !isAdmin && (author == nil || *author != caller) {
		writeErr(w, http.StatusForbidden, fmt.Errorf("only the author or an admin may revoke this annotation"))
		return
	}
	if err := store.Delete(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
