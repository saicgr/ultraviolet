// Package api — dashboard_ai_edit.go: AI-assisted dashboard editing (ai-u4).
//
// POST /api/v1/dashboards/{id}/ai-edit body {instruction}. Loads the current
// dashboard JSON, asks the LLM to apply the edit and return strict JSON, then
// validates against dashboards.Dashboard shape, writes a new dashboard_version
// row, mirrors the new tiles/name onto the live dashboard row, and returns
// the updated payload. Malformed LLM JSON → 502 (no fake fallback).
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/ai"
	"github.com/ultraviolet-dev/ultraviolet/internal/dashboards"
)

type dashboardAIEditRequest struct {
	Instruction string `json:"instruction"`
}

func (s *Server) dashboardAIEdit(w http.ResponseWriter, r *http.Request) {
	if s.rewriter == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("ai-edit: ai provider not configured"))
		return
	}
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body dashboardAIEditRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(body.Instruction) == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("instruction required"))
		return
	}

	store := dashboards.NewStore(s.db.Pool())
	current, err := store.Get(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, fmt.Errorf("dashboard not found: %w", err))
		return
	}

	currentJSON, err := json.Marshal(current)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	prompt := buildDashboardEditPrompt(body.Instruction, currentJSON)
	out, err := s.rewriter.CompleteOne(r.Context(), s.cfg.AIDefaultModel, prompt, current.CustomerID.String())
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, ai.ErrNoProvider) {
			status = http.StatusServiceUnavailable
		}
		writeErr(w, status, fmt.Errorf("ai-edit: %w", err))
		return
	}

	updated, perr := parseDashboardEdit(out, id, current.CustomerID)
	if perr != nil {
		writeErr(w, http.StatusBadGateway, fmt.Errorf("ai-edit: malformed LLM output: %w", perr))
		return
	}

	// Persist: append a new version row + mirror to live dashboard.
	tilesJSON, _ := json.Marshal(updated.Tiles)
	payload := map[string]any{"name": updated.Name, "tiles": updated.Tiles}
	payloadBytes, _ := json.Marshal(payload)
	author := "ai-edit"
	if uid := UserIDFromContext(r.Context()); uid != uuid.Nil {
		author = uid.String()
	}
	newVersion, err := insertDashboardVersion(r.Context(), s.db.Pool(), id, payloadBytes, author, body.Instruction)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("insert version: %w", err))
		return
	}
	if _, err := s.db.Pool().Exec(r.Context(),
		`UPDATE dashboard SET name = $2, tiles = $3::jsonb, updated_at = now() WHERE id = $1`,
		id, updated.Name, tilesJSON); err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("mirror: %w", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"dashboard":   updated,
		"new_version": newVersion,
	})
}

func buildDashboardEditPrompt(instruction string, currentJSON []byte) string {
	var sb strings.Builder
	sb.WriteString("Apply this edit to the dashboard JSON, return only the modified JSON.\n")
	sb.WriteString("The dashboard schema is {id, customer_id, name, tiles:[{id,title,chart_type,sql,x,y,w,h,config?}], is_public}.\n")
	sb.WriteString("Preserve id and customer_id verbatim. Output JSON only — no commentary, no markdown fences.\n\n")
	sb.WriteString("Instruction: ")
	sb.WriteString(instruction)
	sb.WriteString("\n\nCurrent dashboard:\n")
	sb.Write(currentJSON)
	return sb.String()
}

// parseDashboardEdit accepts the LLM output, strips optional fences, unmarshals
// into a Dashboard, and forces id+customer_id back to the authoritative values.
func parseDashboardEdit(out string, id, customerID uuid.UUID) (*dashboards.Dashboard, error) {
	s := strings.TrimSpace(out)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") {
		return nil, fmt.Errorf("expected JSON object")
	}
	var d dashboards.Dashboard
	if err := json.Unmarshal([]byte(s), &d); err != nil {
		return nil, err
	}
	if strings.TrimSpace(d.Name) == "" {
		return nil, fmt.Errorf("missing name")
	}
	if len(d.Tiles) == 0 {
		return nil, fmt.Errorf("missing tiles")
	}
	for i, t := range d.Tiles {
		if strings.TrimSpace(t.Title) == "" || strings.TrimSpace(t.ChartType) == "" {
			return nil, fmt.Errorf("tile %d missing title or chart_type", i)
		}
	}
	d.ID = id
	d.CustomerID = customerID
	return &d, nil
}
