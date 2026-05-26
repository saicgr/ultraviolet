// pu-1 — scheduled_query CRUD. The runner that actually executes due rows
// lives in internal/scheduler; these handlers are pure registry.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/savedqueries"
)

// an-7 / an-8 — render a saved query with ${param} substitution applied. We
// do NOT execute the SQL here; the handler returns the rendered text so the
// workbench can preview before running. Wire connector dispatch when the
// api↔connectors import cycle is resolved.
func (s *Server) runSavedQuery(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		Params map[string]string `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var sql string
	if err := s.db.Pool().QueryRow(r.Context(),
		`SELECT sql FROM saved_query WHERE id = $1`, id).Scan(&sql); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	rendered := savedqueries.SubstituteParams(sql, body.Params)
	writeJSON(w, http.StatusOK, map[string]string{"sql": rendered})
}

type scheduledQueryReq struct {
	CustomerID    uuid.UUID `json:"customer_id"`
	Name          string    `json:"name"`
	SQL           string    `json:"sql"`
	ScheduleCron  string    `json:"schedule_cron"`
	TargetTable   *string   `json:"target_table,omitempty"`
}

type scheduledQueryRow struct {
	ID            uuid.UUID `json:"id"`
	CustomerID    uuid.UUID `json:"customer_id"`
	Name          string    `json:"name"`
	SQL           string    `json:"sql"`
	ScheduleCron  string    `json:"schedule_cron"`
	TargetTable   *string   `json:"target_table,omitempty"`
	Enabled       bool      `json:"enabled"`
	LastRunStatus *string   `json:"last_run_status,omitempty"`
}

func (s *Server) createScheduledQuery(w http.ResponseWriter, r *http.Request) {
	var body scheduledQueryReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.CustomerID == uuid.Nil || body.Name == "" || body.SQL == "" || body.ScheduleCron == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("customer_id, name, sql, schedule_cron required"))
		return
	}
	var id uuid.UUID
	err := s.db.Pool().QueryRow(r.Context(),
		`INSERT INTO scheduled_query (customer_id, name, sql, schedule_cron, target_table)
		 VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		body.CustomerID, body.Name, body.SQL, body.ScheduleCron, body.TargetTable).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]uuid.UUID{"id": id})
}

func (s *Server) listScheduledQueries(w http.ResponseWriter, r *http.Request) {
	cid, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rows, err := s.db.Pool().Query(r.Context(),
		`SELECT id, customer_id, name, sql, schedule_cron, target_table, enabled, last_run_status
		   FROM scheduled_query WHERE customer_id = $1 ORDER BY created_at DESC LIMIT 200`, cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := []scheduledQueryRow{}
	for rows.Next() {
		var rec scheduledQueryRow
		if err := rows.Scan(&rec.ID, &rec.CustomerID, &rec.Name, &rec.SQL, &rec.ScheduleCron, &rec.TargetTable, &rec.Enabled, &rec.LastRunStatus); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, rec)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteScheduledQuery(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if _, err := s.db.Pool().Exec(r.Context(), `DELETE FROM scheduled_query WHERE id = $1`, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
