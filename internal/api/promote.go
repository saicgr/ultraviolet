// Phase-6 Wave-1 de-2: promote a workbench query to a managed synced_table.
// We wrap the SQL in a CTAS-style entry and persist `source_query` so the
// sync runner refreshes by re-executing the query (vs base-table scan).
// Initial state is 'pending' — the sync worker picks it up on its next tick.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

type promoteRequest struct {
	CustomerID   uuid.UUID `json:"customer_id"`
	ConnectionID uuid.UUID `json:"connection_id"`
	SQL          string    `json:"sql"`
	TargetSchema string    `json:"target_schema"`
	TargetTable  string    `json:"target_table"`
}

type promoteResponse struct {
	SyncedTableID uuid.UUID `json:"synced_table_id"`
	CTAS          string    `json:"ctas"`
	State         string    `json:"state"`
}

func (s *Server) workbenchPromote(w http.ResponseWriter, r *http.Request) {
	var body promoteRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sql := strings.TrimSpace(body.SQL)
	body.TargetSchema = strings.TrimSpace(body.TargetSchema)
	body.TargetTable = strings.TrimSpace(body.TargetTable)
	if sql == "" || body.TargetSchema == "" || body.TargetTable == "" ||
		body.CustomerID == uuid.Nil || body.ConnectionID == uuid.Nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("customer_id, connection_id, sql, target_schema, target_table required"))
		return
	}

	// Verify the connection belongs to the asserted customer. Avoids
	// cross-tenant promotion via stolen connection IDs.
	var owner uuid.UUID
	if err := s.db.Pool().QueryRow(r.Context(),
		`SELECT customer_id FROM connections WHERE id = $1`, body.ConnectionID).
		Scan(&owner); err != nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("connection not found: %w", err))
		return
	}
	if owner != body.CustomerID {
		writeErr(w, http.StatusForbidden, fmt.Errorf("connection does not belong to customer"))
		return
	}

	// Strip trailing semicolons so the CTAS we hand the sync worker parses
	// cleanly when the worker wraps it.
	cleanSQL := strings.TrimRight(sql, "; \t\r\n")
	ctas := fmt.Sprintf(`CREATE TABLE %q.%q AS %s`, body.TargetSchema, body.TargetTable, cleanSQL)

	var id uuid.UUID
	err := s.db.Pool().QueryRow(r.Context(), `
		INSERT INTO synced_tables
			(connection_id, schema_name, table_name, sync_mode, source_query, state)
		VALUES ($1, $2, $3, 'watermark', $4, 'pending')
		ON CONFLICT (connection_id, schema_name, table_name) DO UPDATE
			SET source_query = EXCLUDED.source_query,
			    sync_mode    = 'watermark',
			    state        = 'pending',
			    updated_at   = now()
		RETURNING id`,
		body.ConnectionID, body.TargetSchema, body.TargetTable, cleanSQL).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusCreated, promoteResponse{
		SyncedTableID: id,
		CTAS:          ctas,
		State:         "pending",
	})
}
