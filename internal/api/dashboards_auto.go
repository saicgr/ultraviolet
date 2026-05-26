// Package api — dashboards_auto.go: AI-generated dashboard from a table FQN (ai-u7).
//
// POST /api/v1/customers/{id}/dashboards/auto?fqn=<fqn>
// Calls internal/ai.AutoDash.Generate to produce a 4-tile DashboardSpec,
// persists it via internal/dashboards, and returns the new dashboard id.
// Surfaces LLM errors verbatim — no fake fallback.
package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ultraviolet-dev/ultraviolet/internal/ai"
	"github.com/ultraviolet-dev/ultraviolet/internal/dashboards"
)

func (s *Server) dashboardsAuto(w http.ResponseWriter, r *http.Request) {
	if s.rewriter == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("autodash: ai provider not configured"))
		return
	}
	cid, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	fqn := r.URL.Query().Get("fqn")
	if strings.TrimSpace(fqn) == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("fqn required"))
		return
	}

	auto := ai.NewAutoDash(s.rewriter, s.db.Pool())
	spec, err := auto.Generate(r.Context(), cid, fqn)
	if err != nil {
		status := http.StatusBadGateway
		switch {
		case errors.Is(err, ai.ErrNoProvider):
			status = http.StatusServiceUnavailable
		case errors.Is(err, ai.ErrMalformedLLMOutput):
			status = http.StatusBadGateway
		}
		writeErr(w, status, fmt.Errorf("autodash: %w", err))
		return
	}

	tiles := make([]dashboards.Tile, 0, len(spec.Tiles))
	for i, t := range spec.Tiles {
		tiles = append(tiles, dashboards.Tile{
			ID:        fmt.Sprintf("tile-%d", i+1),
			Title:     t.Title,
			ChartType: t.ChartType,
			SQL:       t.SQL,
			X:         (i % 2) * 6,
			Y:         (i / 2) * 4,
			W:         6,
			H:         4,
		})
	}

	d := &dashboards.Dashboard{
		CustomerID: cid,
		Name:       spec.Title,
		Tiles:      tiles,
	}
	store := dashboards.NewStore(s.db.Pool())
	newID, err := store.Create(r.Context(), d)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":    newID,
		"name":  spec.Title,
		"tiles": len(tiles),
		"fqn":   fqn,
	})
}
