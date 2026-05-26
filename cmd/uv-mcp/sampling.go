// MCP sampling — lets agents request LLM completions back through Ultraviolet
// rather than calling the model provider directly. Routing all sampling through
// our control plane gives us (a) audit trail, (b) per-customer budget caps,
// (c) the ability to inject policy / PII redaction before the prompt leaves.
// Spec: https://modelcontextprotocol.io/specification/sampling.
package main

import (
	"encoding/json"
	"fmt"
)

// samplingMessage matches the MCP spec's message envelope. Content can be
// either a string or a {type:"text",text:""} object depending on the client;
// we accept the raw value and forward it verbatim.
type samplingMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type modelPreferences struct {
	Hints                []map[string]any `json:"hints,omitempty"`
	CostPriority         *float64         `json:"costPriority,omitempty"`
	SpeedPriority        *float64         `json:"speedPriority,omitempty"`
	IntelligencePriority *float64         `json:"intelligencePriority,omitempty"`
}

type createMessageParams struct {
	Messages         []samplingMessage `json:"messages"`
	ModelPreferences *modelPreferences `json:"modelPreferences,omitempty"`
	SystemPrompt     string            `json:"systemPrompt,omitempty"`
	MaxTokens        int               `json:"maxTokens,omitempty"`
	Temperature      *float64          `json:"temperature,omitempty"`
	StopSequences    []string          `json:"stopSequences,omitempty"`
	Metadata         map[string]any    `json:"metadata,omitempty"`
}

// createMessage handles sampling/createMessage by passing through to the
// control-plane completion endpoint. The MCP response shape is stable even
// while the REST endpoint evolves.
//
// TODO(mcp-sampling): /api/v1/ai/complete may not be implemented yet — the
// dispatch is wired so the MCP surface is forward-compatible. Once the REST
// endpoint lands it should accept the JSON body below and return
// {text, model, stopReason}.
func createMessage(id json.RawMessage, raw json.RawMessage) rpcResponse {
	var p createMessageParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return rpcResponse{ID: id, Error: &rpcError{Code: -32602, Message: err.Error()}}
	}
	if len(p.Messages) == 0 {
		return rpcResponse{ID: id, Error: &rpcError{Code: -32602, Message: "messages required"}}
	}
	body, err := json.Marshal(map[string]any{
		"messages":          p.Messages,
		"model_preferences": p.ModelPreferences,
		"system_prompt":     p.SystemPrompt,
		"max_tokens":        p.MaxTokens,
		"temperature":       p.Temperature,
		"stop_sequences":    p.StopSequences,
		"metadata":          p.Metadata,
	})
	if err != nil {
		return rpcResponse{ID: id, Error: &rpcError{Code: -32000, Message: err.Error()}}
	}
	resp, status, err := fetchJSON("POST", "/api/v1/ai/complete", body)
	if err != nil {
		return rpcResponse{ID: id, Error: &rpcError{Code: -32000, Message: err.Error()}}
	}
	if status >= 400 {
		return rpcResponse{ID: id, Error: &rpcError{Code: -32000, Message: fmt.Sprintf("ai/complete returned %d", status)}}
	}
	// Coerce REST response into MCP sampling shape. Accept common field names
	// (text/completion/content; model; stop_reason/stopReason) so the REST
	// endpoint author has flexibility.
	text, model, stop := "", "", "endTurn"
	if m, ok := resp.(map[string]any); ok {
		text = firstString(m, "text", "completion", "content")
		if mm := firstString(m, "model"); mm != "" {
			model = mm
		}
		if sr := firstString(m, "stop_reason", "stopReason"); sr != "" {
			stop = sr
		}
	}
	return rpcResponse{ID: id, Result: map[string]any{
		"role":       "assistant",
		"content":    map[string]any{"type": "text", "text": text},
		"model":      model,
		"stopReason": stop,
	}}
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}
