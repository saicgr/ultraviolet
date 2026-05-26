// Package api — copilot.go: embedded copilot chat endpoint (ai-7).
//
// POST /api/v1/copilot/chat. Builds a context-aware prompt from the request
// (optional dashboard, optional current SQL, top-5 synced tables), calls the
// LLM via Rewriter.CompleteOne, parses an optional trailing ```json``` block
// containing suggested_actions, and returns {reply, suggested_actions}.
//
// No silent fallback: if the rewriter is unconfigured we return 503 so the UI
// can show "AI not configured" rather than a fake answer.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/ai"
)

type copilotChatRequest struct {
	CustomerID string `json:"customer_id"`
	SessionID  string `json:"session_id"`
	Message    string `json:"message"`
	Context    struct {
		DashboardID *string `json:"dashboard_id,omitempty"`
		SQL         *string `json:"sql,omitempty"`
	} `json:"context"`
}

type copilotSuggestedAction struct {
	Kind    string          `json:"kind"`
	Label   string          `json:"label"`
	Payload json.RawMessage `json:"payload"`
}

type copilotChatResponse struct {
	Reply            string                   `json:"reply"`
	SuggestedActions []copilotSuggestedAction `json:"suggested_actions"`
}

func (s *Server) copilotChat(w http.ResponseWriter, r *http.Request) {
	if s.rewriter == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("copilot: ai provider not configured"))
		return
	}
	var body copilotChatRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.CustomerID == "" || body.Message == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("customer_id and message required"))
		return
	}
	cid, err := uuid.Parse(body.CustomerID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("customer_id: %w", err))
		return
	}

	prompt := s.buildCopilotPrompt(r.Context(), cid, body)
	out, err := s.rewriter.CompleteOne(r.Context(), s.cfg.AIDefaultModel, prompt, body.CustomerID)
	if err != nil {
		// Per project rule (no fallback) — surface upstream error directly.
		status := http.StatusBadGateway
		if errors.Is(err, ai.ErrNoProvider) {
			status = http.StatusServiceUnavailable
		}
		writeErr(w, status, fmt.Errorf("copilot: %w", err))
		return
	}
	reply, actions := splitCopilotReply(out)
	writeJSON(w, http.StatusOK, copilotChatResponse{Reply: reply, SuggestedActions: actions})
}

// buildCopilotPrompt assembles a system + user prompt with dashboard, SQL, and
// schema context. Best-effort: any DB lookup failure is logged and skipped so
// the copilot still works without the optional context.
func (s *Server) buildCopilotPrompt(ctx context.Context, customerID uuid.UUID, body copilotChatRequest) string {
	var sb strings.Builder
	sb.WriteString("You are Ultraviolet's embedded analytics copilot. Help the user navigate dashboards, SQL, and lineage. ")
	sb.WriteString("Be concise (≤4 sentences) and end with a fenced ```json``` block containing an array named \"actions\". ")
	sb.WriteString("Each action has fields {kind, label, payload}. Valid kinds: ")
	sb.WriteString("\"open_workbench_with_sql\" (payload {sql}), \"view_dashboard\" (payload {dashboard_id}), \"run_text2sql\" (payload {question}). ")
	sb.WriteString("If no action applies, return an empty array.\n\n")

	if body.Context.DashboardID != nil && *body.Context.DashboardID != "" {
		if did, err := uuid.Parse(*body.Context.DashboardID); err == nil {
			var name string
			var tileCount int
			err := s.db.Pool().QueryRow(ctx,
				`SELECT name, COALESCE(jsonb_array_length(tiles), 0)
				 FROM dashboard WHERE customer_id = $1 AND id = $2`,
				customerID, did).Scan(&name, &tileCount)
			if err == nil {
				fmt.Fprintf(&sb, "Active dashboard: %q (id=%s, tiles=%d).\n", name, did, tileCount)
			} else {
				s.log.Debug().Err(err).Str("dashboard_id", did.String()).Msg("copilot: dashboard context lookup failed")
			}
		}
	}
	if body.Context.SQL != nil && strings.TrimSpace(*body.Context.SQL) != "" {
		sb.WriteString("Current SQL in workbench:\n")
		sb.WriteString("```sql\n")
		sb.WriteString(strings.TrimSpace(*body.Context.SQL))
		sb.WriteString("\n```\n")
	}

	// Top-5 tables across all customer connections — surfaces schema vocabulary
	// without leaking row data.
	rows, err := s.db.Pool().Query(ctx,
		`SELECT t.schema_name, t.table_name
		 FROM synced_tables t
		 JOIN connections c ON c.id = t.connection_id
		 WHERE c.customer_id = $1
		 ORDER BY t.schema_name, t.table_name
		 LIMIT 5`, customerID)
	if err == nil {
		defer rows.Close()
		var tables []string
		for rows.Next() {
			var sc, tn string
			if err := rows.Scan(&sc, &tn); err == nil {
				tables = append(tables, sc+"."+tn)
			}
		}
		if len(tables) > 0 {
			sb.WriteString("Customer tables (top 5): ")
			sb.WriteString(strings.Join(tables, ", "))
			sb.WriteString("\n")
		}
	} else {
		s.log.Debug().Err(err).Msg("copilot: synced_tables lookup failed")
	}

	if body.SessionID != "" {
		fmt.Fprintf(&sb, "Session: %s\n", body.SessionID)
	}
	sb.WriteString("\nUser: ")
	sb.WriteString(body.Message)
	return sb.String()
}

// copilotJSONBlockRe captures a fenced ```json ... ``` block, anchored to the
// last such block in the response so prose can mention "json" earlier.
var copilotJSONBlockRe = regexp.MustCompile("(?s)```json\\s*(.*?)```")

// splitCopilotReply pulls the trailing ```json``` action block off the LLM
// reply. When the block is missing or malformed we return the full text with
// an empty actions slice — handlers must not fail on bad LLM JSON.
func splitCopilotReply(raw string) (string, []copilotSuggestedAction) {
	matches := copilotJSONBlockRe.FindAllStringSubmatchIndex(raw, -1)
	if len(matches) == 0 {
		return strings.TrimSpace(raw), []copilotSuggestedAction{}
	}
	last := matches[len(matches)-1]
	block := raw[last[2]:last[3]]
	reply := strings.TrimSpace(raw[:last[0]])

	var parsed struct {
		Actions []copilotSuggestedAction `json:"actions"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(block)), &parsed); err != nil {
		return strings.TrimSpace(raw), []copilotSuggestedAction{}
	}
	if parsed.Actions == nil {
		parsed.Actions = []copilotSuggestedAction{}
	}
	// Filter to the known-kind whitelist so the frontend can switch on `kind`
	// without defensive coding.
	out := make([]copilotSuggestedAction, 0, len(parsed.Actions))
	for _, a := range parsed.Actions {
		switch a.Kind {
		case "open_workbench_with_sql", "view_dashboard", "run_text2sql":
			out = append(out, a)
		}
	}
	return reply, out
}
