package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ultraviolet-dev/ultraviolet/internal/agents"
	"github.com/ultraviolet-dev/ultraviolet/internal/audit"
	"github.com/ultraviolet-dev/ultraviolet/internal/billing"
	"github.com/ultraviolet-dev/ultraviolet/internal/metadata"
)

// agent registry — name → constructor. Adding a new agent is one line here.
func (s *Server) agentByName(name string) (agents.Agent, bool) {
	switch name {
	case "cost_optimizer":
		return agents.NewCostOptimizer(s.db.Pool()), true
	case "schema_safeguard":
		return agents.NewSchemaSafeguard(s.db.Pool()), true
	case "sync_autopilot":
		return agents.NewSyncAutopilot(s.db.Pool()), true
	case "query_regressor":
		return agents.NewQueryRegressor(s.db.Pool()), true
	case "anomaly_investigator":
		return agents.NewAnomalyInvestigator(s.db.Pool()), true
	case "auto_documenter":
		return agents.NewAutoDocumenter(s.db.Pool(), nil, metadata.NewEnricher(s.db.Pool())), true
	case "oncall_summarizer":
		return agents.NewOnCallSummarizer(s.db.Pool()), true
	case "onboarding_planner":
		return agents.NewOnboardingPlanner(s.db.Pool()), true
	case "self_healing_sync":
		return agents.NewSelfHealingSync(s.db.Pool()), true
	case "metric_explainer":
		return agents.NewMetricExplainer(s.db.Pool()), true
	case "query_optimizer":
		return agents.NewQueryOptimizer(s.db.Pool()), true
	case "pii_tagger":
		return agents.NewPIITagger(s.db.Pool()), true
	}
	return nil, false
}

func (s *Server) listAgents(w http.ResponseWriter, _ *http.Request) {
	out := []map[string]string{}
	for _, n := range []string{
		"cost_optimizer", "schema_safeguard", "sync_autopilot", "query_regressor",
		"anomaly_investigator", "auto_documenter", "oncall_summarizer",
		"onboarding_planner", "self_healing_sync",
		"metric_explainer", "query_optimizer", "pii_tagger",
	} {
		if a, ok := s.agentByName(n); ok {
			out = append(out, map[string]string{"name": a.Name(), "kind": a.Kind()})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) runAgent(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ag, ok := s.agentByName(name)
	if !ok {
		writeErr(w, http.StatusNotFound, fmt.Errorf("unknown agent %q", name))
		return
	}
	var body struct {
		DryRun bool           `json:"dry_run"`
		Args   map[string]any `json:"args"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Args == nil {
		body.Args = map[string]any{}
	}
	runner := agents.NewRunner(audit.New(s.db.Pool()), billing.New(s.db.Pool()), s.log)
	out, err := runner.Invoke(r.Context(), ag, agents.Input{
		CustomerID: cid,
		DryRun:     body.DryRun,
		Args:       body.Args,
		UserSlug:   "",
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
