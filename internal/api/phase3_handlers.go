package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/audit"
	"github.com/ultraviolet-dev/ultraviolet/internal/dashboards"
	"github.com/ultraviolet-dev/ultraviolet/internal/impact"
	"github.com/ultraviolet-dev/ultraviolet/internal/lineage"
	"github.com/ultraviolet-dev/ultraviolet/internal/reverseetl"
	"github.com/ultraviolet-dev/ultraviolet/internal/semantic"
)

// activeCustomer is a Phase-1 simplification — JWT subject = customer id.
// Phase-2 wires real users + workspaces via internal/rbac.
func (s *Server) activeCustomer(r *http.Request) (uuid.UUID, error) {
	if uid := UserIDFromContext(r.Context()); uid != uuid.Nil {
		return uid, nil
	}
	cs, err := s.db.ListCustomers(r.Context())
	if err != nil || len(cs) == 0 {
		return uuid.Nil, fmt.Errorf("no active customer")
	}
	return cs[0].ID, nil
}

func (s *Server) lineageUpstream(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	fqn := r.URL.Query().Get("fqn")
	wr := lineage.NewWriter(s.db.Pool())
	edges, err := wr.Upstream(r.Context(), cid, fqn)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if edges == nil {
		edges = []lineage.Edge{}
	}
	writeJSON(w, http.StatusOK, edges)
}

func (s *Server) lineageDownstream(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	fqn := r.URL.Query().Get("fqn")
	wr := lineage.NewWriter(s.db.Pool())
	edges, err := wr.Downstream(r.Context(), cid, fqn)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if edges == nil {
		edges = []lineage.Edge{}
	}
	writeJSON(w, http.StatusOK, edges)
}

func (s *Server) catalogSearch(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	q := r.URL.Query().Get("q")
	rows, err := s.db.Pool().Query(r.Context(), `
		SELECT fqn, description, owner, sla_minutes, source FROM table_metadata
		WHERE customer_id = $1
		  AND ($2 = '' OR to_tsvector('english', coalesce(description, '') || ' ' || fqn) @@ plainto_tsquery('english', $2))
		ORDER BY updated_at DESC
		LIMIT 100`, cid, q)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	type row struct {
		FQN         string  `json:"fqn"`
		Description *string `json:"description"`
		Owner       *string `json:"owner"`
		SLAMinutes  *int    `json:"sla_minutes"`
		Source      string  `json:"source"`
	}
	out := []row{}
	for rows.Next() {
		var rr row
		if err := rows.Scan(&rr.FQN, &rr.Description, &rr.Owner, &rr.SLAMinutes, &rr.Source); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, rr)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listDashboards(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	store := dashboards.NewStore(s.db.Pool())
	out, err := store.List(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createDashboard(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var d dashboards.Dashboard
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	d.CustomerID = id
	store := dashboards.NewStore(s.db.Pool())
	newID, err := store.Create(r.Context(), &d)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	_ = audit.Record(r.Context(), s.db.Pool(), id, "dashboard.create",
		"dashboard:"+newID.String(),
		map[string]any{"name": d.Name})
	writeJSON(w, http.StatusCreated, map[string]uuid.UUID{"id": newID})
}

func (s *Server) workbenchRun(w http.ResponseWriter, r *http.Request) {
	// Phase-1 stub: the workbench page demonstrates the route. Real impl forwards
	// the SQL through the proxy via psql-DSN-on-loopback so the same router applies.
	var body struct {
		SQL string `json:"sql"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	writeJSON(w, http.StatusOK, map[string]any{
		"sql":  body.SQL,
		"rows": []any{},
		"hint": "connect via psql -h localhost -p 5000 -U <slug> -d <slug>_bigquery",
	})
}

func (s *Server) impactPreview(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		Kind string `json:"kind"`
		FQN  string `json:"fqn"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	an := impact.New(lineage.NewWriter(s.db.Pool()))
	hits, err := an.Preview(r.Context(), cid, impact.Change{Kind: body.Kind, FQN: body.FQN}, 5)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if hits == nil {
		hits = []impact.Hit{}
	}
	writeJSON(w, http.StatusOK, hits)
}

func (s *Server) upsertSemantic(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		YAML string `json:"yaml"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	model, err := semantic.Parse(body.YAML)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	store := semantic.NewStore(s.db.Pool())
	if err := store.Upsert(r.Context(), cid, model.Name, body.YAML, ""); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": model.Name})
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rows, err := s.db.Pool().Query(r.Context(),
		`SELECT id, action, target_type, target_id, created_at FROM audit_log
		 WHERE customer_id = $1 ORDER BY created_at DESC LIMIT 200`, cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	type row struct {
		ID         int64  `json:"id"`
		Action     string `json:"action"`
		TargetType string `json:"target_type"`
		TargetID   string `json:"target_id"`
		CreatedAt  string `json:"created_at"`
	}
	out := []row{}
	for rows.Next() {
		var rr row
		var tt, ti *string
		var ts string
		if err := rows.Scan(&rr.ID, &rr.Action, &tt, &ti, &ts); err == nil {
			if tt != nil {
				rr.TargetType = *tt
			}
			if ti != nil {
				rr.TargetID = *ti
			}
			rr.CreatedAt = ts
			out = append(out, rr)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listUsage(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rows, err := s.db.Pool().Query(r.Context(),
		`SELECT event_type, SUM(quantity) FROM usage_event
		 WHERE customer_id = $1 AND created_at > now() - interval '30 days'
		 GROUP BY 1`, cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := map[string]float64{}
	for rows.Next() {
		var k string
		var v float64
		if err := rows.Scan(&k, &v); err == nil {
			out[k] = v
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listDestinations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, reverseetl.Registry())
}
