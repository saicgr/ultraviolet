package api

// W1 lineage graph: GET /api/v1/lineage/graph?fqn=&depth=&direction=&granularity=
//
// Returns a multi-hop {nodes, edges} graph rooted at fqn for the active
// customer, in one round trip (recursive CTE) instead of N×1-hop fetches.
// Always returns the root node even with zero edges so the UI renders an honest
// "no lineage observed yet" state rather than a blank canvas.

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/ultraviolet-dev/ultraviolet/internal/lineage"
)

func (s *Server) lineageGraph(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	q := r.URL.Query()
	fqn := q.Get("fqn")
	if fqn == "" {
		writeErr(w, http.StatusBadRequest, errors.New("fqn required"))
		return
	}
	depth, _ := strconv.Atoi(q.Get("depth"))

	wr := lineage.NewWriter(s.db.Pool())
	graph, err := wr.Graph(r.Context(), cid, fqn, q.Get("direction"), q.Get("granularity"), depth)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, graph)
}
