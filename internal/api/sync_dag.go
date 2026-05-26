// Phase 6 Wave 2 (de-1): sync DAG visualization endpoint.
//
// Builds a graph from a customer's synced_tables (nodes) and the subset of
// lineage_edge rows whose upstream + downstream are both synced. Two
// sequential queries on the shared pool — nodes first (joined to connections
// for customer scope), then edges filtered in-Postgres via a CTE that
// re-derives the same node set, so we don't ship N FQNs back as a parameter.
package api

import (
	"net/http"
	"time"
)

type dagNode struct {
	FQN             string     `json:"fqn"`
	State           string     `json:"state"`
	LastSyncAt      *time.Time `json:"last_sync_at,omitempty"`
	LagSeconds      *int       `json:"lag_seconds,omitempty"`
	SyncMode        string     `json:"sync_mode"`
	DownstreamCount int        `json:"downstream_count"`
}

type dagEdge struct {
	Upstream   string `json:"upstream"`
	Downstream string `json:"downstream"`
	EdgeType   string `json:"edge_type"`
}

type dagResponse struct {
	Nodes []dagNode `json:"nodes"`
	Edges []dagEdge `json:"edges"`
}

// syncDAG: GET /api/v1/customers/{id}/sync/dag.
func (s *Server) syncDAG(w http.ResponseWriter, r *http.Request) {
	customerID, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx := r.Context()

	// Node list. fqn convention matches internal/lineage/lineage.go::fqn().
	nodeRows, err := s.db.Pool().Query(ctx, `
		SELECT t.schema_name, t.table_name, t.state, t.sync_mode,
		       t.last_sync_at, t.sync_lag_seconds
		FROM synced_tables t
		JOIN connections c ON c.id = t.connection_id
		WHERE c.customer_id = $1
		ORDER BY t.schema_name, t.table_name`, customerID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer nodeRows.Close()

	nodes := []dagNode{}
	idx := map[string]int{} // fqn -> position in nodes
	for nodeRows.Next() {
		var schema, table, state, mode string
		var lastSync *time.Time
		var lag *int
		if err := nodeRows.Scan(&schema, &table, &state, &mode, &lastSync, &lag); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		fqn := schema + "." + table
		if schema == "" {
			fqn = table
		}
		idx[fqn] = len(nodes)
		nodes = append(nodes, dagNode{
			FQN: fqn, State: state, SyncMode: mode,
			LastSyncAt: lastSync, LagSeconds: lag,
		})
	}
	if err := nodeRows.Err(); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	// Edges restricted to lineage rows whose both endpoints are synced for
	// this customer. The CTE re-derives the node set in-Postgres.
	edgeRows, err := s.db.Pool().Query(ctx, `
		WITH synced AS (
		    SELECT CASE WHEN t.schema_name = '' THEN t.table_name
		                ELSE t.schema_name || '.' || t.table_name END AS fqn
		    FROM synced_tables t
		    JOIN connections c ON c.id = t.connection_id
		    WHERE c.customer_id = $1
		)
		SELECT le.upstream_fqn, le.downstream_fqn, le.edge_type
		FROM lineage_edge le
		WHERE le.customer_id = $1
		  AND le.upstream_fqn   IN (SELECT fqn FROM synced)
		  AND le.downstream_fqn IN (SELECT fqn FROM synced)`, customerID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer edgeRows.Close()

	edges := []dagEdge{}
	for edgeRows.Next() {
		var e dagEdge
		if err := edgeRows.Scan(&e.Upstream, &e.Downstream, &e.EdgeType); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		edges = append(edges, e)
		if i, ok := idx[e.Upstream]; ok {
			nodes[i].DownstreamCount++
		}
	}
	if err := edgeRows.Err(); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, dagResponse{Nodes: nodes, Edges: edges})
}
