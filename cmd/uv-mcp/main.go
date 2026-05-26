// Command uv-mcp is an MCP (Model Context Protocol) server that exposes the
// Ultraviolet catalog + lineage + dashboards to Claude/Cursor agents.
//
// Phase-1 surface: stdio transport, tools list_tables, get_lineage, run_query,
// list_dashboards. The real protocol body is JSON-RPC over stdio per
// https://modelcontextprotocol.io/specification.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// Transports: stdio (default — matches Claude Desktop / Cursor's MCP client),
// or HTTP+SSE (UV_MCP_HTTP=:9090). The SSE variant lets remote agents connect
// without a stdio bridge — useful for Vercel-hosted Claude API agents.

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

var apiBase string

func main() {
	apiBase = os.Getenv("UV_API_URL")
	if apiBase == "" {
		apiBase = "http://localhost:8080"
	}
	if addr := os.Getenv("UV_MCP_HTTP"); addr != "" {
		serveHTTP(addr)
		return
	}
	rd := bufio.NewReader(os.Stdin)
	for {
		line, err := rd.ReadString('\n')
		if err == io.EOF {
			return
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "read:", err)
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			respond(rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: err.Error()}})
			continue
		}
		respond(handle(req))
	}
}

func respond(r rpcResponse) {
	r.JSONRPC = "2.0"
	out, _ := json.Marshal(r)
	os.Stdout.Write(out)
	os.Stdout.Write([]byte("\n"))
}

func handle(req rpcRequest) rpcResponse {
	switch req.Method {
	case "initialize":
		return rpcResponse{ID: req.ID, Result: map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo":      map[string]string{"name": "uv-mcp", "version": "0.1.0"},
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"resources": map[string]any{"subscribe": false, "listChanged": false},
				"sampling":  map[string]any{},
			},
		}}
	case "tools/list":
		return rpcResponse{ID: req.ID, Result: map[string]any{"tools": toolCatalog()}}
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(req.Params, &p)
		return callTool(req.ID, p.Name, p.Arguments)
	case "resources/list":
		var p struct {
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(req.Params, &p)
		return listResources(req.ID, p.Arguments)
	case "resources/read":
		var p struct {
			URI string `json:"uri"`
		}
		_ = json.Unmarshal(req.Params, &p)
		return readResource(req.ID, p.URI)
	case "sampling/createMessage":
		return createMessage(req.ID, req.Params)
	}
	return rpcResponse{ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found"}}
}

func callTool(id json.RawMessage, name string, args map[string]any) rpcResponse {
	switch name {
	case "list_tables":
		return passthrough(id, "GET", fmt.Sprintf("/api/v1/connections/%v/synced-tables", args["customer_id"]))
	case "get_lineage_upstream":
		return passthrough(id, "GET", fmt.Sprintf("/api/v1/lineage/upstream?fqn=%v", args["fqn"]))
	case "get_lineage_downstream":
		return passthrough(id, "GET", fmt.Sprintf("/api/v1/lineage/downstream?fqn=%v", args["fqn"]))
	case "catalog_search":
		return passthrough(id, "GET", fmt.Sprintf("/api/v1/catalog/search?q=%v", args["q"]))
	case "list_dashboards":
		return passthrough(id, "GET", fmt.Sprintf("/api/v1/customers/%v/dashboards", args["customer_id"]))
	case "preview_impact":
		body, _ := json.Marshal(args)
		return passthroughBody(id, "POST", "/api/v1/impact/preview", body)
	case "freshness_check":
		return passthrough(id, "GET", fmt.Sprintf("/api/v1/customers/%v/freshness", args["customer_id"]))
	case "cost_estimate":
		body, _ := json.Marshal(args)
		return passthroughBody(id, "POST", "/api/v1/cost/estimate", body)
	case "text_to_sql":
		body, _ := json.Marshal(args)
		return passthroughBody(id, "POST", "/api/v1/ai/text2sql", body)
	case "run_query":
		return rpcResponse{ID: id, Result: map[string]string{"hint": "connect via psql -h localhost -p 5000 to execute; this MCP server intentionally does NOT proxy SQL — agents should declare their queries to the catalog tools and let humans run them in workbench"}}
	case "explain_query":
		return passthrough(id, "GET", fmt.Sprintf("/api/v1/queries/%v/explain", args["query_hash"]))
	case "list_savings":
		return passthrough(id, "GET", fmt.Sprintf("/api/v1/customers/%v/savings", args["customer_id"]))
	case "agent_run":
		body, _ := json.Marshal(args)
		return passthroughBody(id, "POST", fmt.Sprintf("/api/v1/agents/%v/run", args["agent"]), body)
	}
	return rpcResponse{ID: id, Error: &rpcError{Code: -32602, Message: "unknown tool"}}
}

// toolCatalog returns the full MCP tool list. Each tool corresponds to a REST
// endpoint Ultraviolet already serves, so adding a new agent or analytics call
// is purely additive — register it in callTool + here, no separate wire format.
func toolCatalog() []map[string]any {
	str := func() map[string]string { return map[string]string{"type": "string"} }
	obj := func(props map[string]any, required ...string) map[string]any {
		return map[string]any{"type": "object", "properties": props, "required": required}
	}
	return []map[string]any{
		{"name": "list_tables", "description": "List synced tables for a customer.", "inputSchema": obj(map[string]any{"customer_id": str()}, "customer_id")},
		{"name": "get_lineage_upstream", "description": "Return upstream lineage edges for an FQN — what does this table depend on?", "inputSchema": obj(map[string]any{"fqn": str()}, "fqn")},
		{"name": "get_lineage_downstream", "description": "Return downstream lineage edges for an FQN — what depends on this table?", "inputSchema": obj(map[string]any{"fqn": str()}, "fqn")},
		{"name": "catalog_search", "description": "Full-text search over the enriched catalog (descriptions, owners, tags, FQNs).", "inputSchema": obj(map[string]any{"q": str()}, "q")},
		{"name": "list_dashboards", "description": "List dashboards for a customer.", "inputSchema": obj(map[string]any{"customer_id": str()}, "customer_id")},
		{"name": "preview_impact", "description": "Preview blast-radius for a schema change (drop_column / drop_table / rename_column / type_change).", "inputSchema": obj(map[string]any{"kind": str(), "fqn": str()}, "kind", "fqn")},
		{"name": "freshness_check", "description": "Return current per-table sync lag vs SLA.", "inputSchema": obj(map[string]any{"customer_id": str()}, "customer_id")},
		{"name": "cost_estimate", "description": "Estimate cost for a SQL query before running it (BQ on-demand model + Snowflake credits).", "inputSchema": obj(map[string]any{"sql": str()}, "sql")},
		{"name": "text_to_sql", "description": "Convert a natural-language question into SQL grounded by the customer's catalog + lineage.", "inputSchema": obj(map[string]any{"question": str(), "allowed_tables": map[string]any{"type": "array", "items": str()}}, "question")},
		{"name": "explain_query", "description": "Return the warehouse query plan + per-stage stats for a query_hash.", "inputSchema": obj(map[string]any{"query_hash": str()}, "query_hash")},
		{"name": "list_savings", "description": "Return the per-period savings report for a customer.", "inputSchema": obj(map[string]any{"customer_id": str()}, "customer_id")},
		{"name": "agent_run", "description": "Invoke a named Ultraviolet agent (cost_optimizer | schema_safeguard | sync_autopilot | query_regressor | anomaly_investigator | auto_documenter | oncall_summarizer | onboarding_planner | self_healing_sync). Returns Suggestions.", "inputSchema": obj(map[string]any{"agent": str(), "customer_id": str(), "dry_run": map[string]string{"type": "boolean"}}, "agent")},
		{"name": "run_query", "description": "Hint how to run SQL via psql against the proxy. The MCP server intentionally does NOT proxy SQL execution — agents declare intent; humans + connectors execute.", "inputSchema": obj(map[string]any{"sql": str()}, "sql")},
	}
}

func passthroughBody(id json.RawMessage, method, path string, body []byte) rpcResponse {
	req, _ := http.NewRequest(method, apiBase+path, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	if t := os.Getenv("UV_TOKEN"); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return rpcResponse{ID: id, Error: &rpcError{Code: -32000, Message: err.Error()}}
	}
	defer resp.Body.Close()
	bs, _ := io.ReadAll(resp.Body)
	return rpcResponse{ID: id, Result: map[string]any{"http_status": resp.StatusCode, "body": json.RawMessage(bs)}}
}

func passthrough(id json.RawMessage, method, path string) rpcResponse {
	req, _ := http.NewRequest(method, apiBase+path, nil)
	if t := os.Getenv("UV_TOKEN"); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return rpcResponse{ID: id, Error: &rpcError{Code: -32000, Message: err.Error()}}
	}
	defer resp.Body.Close()
	bs, _ := io.ReadAll(resp.Body)
	return rpcResponse{ID: id, Result: map[string]any{"http_status": resp.StatusCode, "body": json.RawMessage(bs)}}
}

// serveHTTP mounts:
//
//	POST /rpc       — single JSON-RPC request, single response (stateless).
//	GET  /sse       — SSE stream variant for streaming agent calls.
//	GET  /healthz   — liveness.
//
// Auth via UV_TOKEN bearer; propagated through passthrough() so the underlying
// REST calls inherit it.
func serveHTTP(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := handle(req)
		resp.JSONRPC = "2.0"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		body := r.URL.Query().Get("body")
		if body != "" {
			var req rpcRequest
			if err := json.Unmarshal([]byte(body), &req); err == nil {
				resp := handle(req)
				resp.JSONRPC = "2.0"
				bs, _ := json.Marshal(resp)
				fmt.Fprintf(w, "event: response\ndata: %s\n\n", bs)
				f.Flush()
			}
		}
		<-r.Context().Done()
	})
	srv := &http.Server{Addr: addr, Handler: mux}
	fmt.Fprintln(os.Stderr, "uv-mcp http listening on", addr)
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "uv-mcp http:", err)
	}
}
