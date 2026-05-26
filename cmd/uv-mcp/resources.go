// MCP resources surface — exposes Ultraviolet catalog/lineage/dashboards as
// addressable MCP resources (uv://table/..., uv://dashboard/..., uv://schema/...).
// All resource bodies are fetched by passthrough() to the REST API so that
// auth, RBAC, and audit logging are inherited from the control plane.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// customerFromArgs extracts the customer ID from MCP resources/list arguments
// or, if absent, from the UV_CUSTOMER_ID env fallback so single-tenant
// installs (the common dev case) don't have to pass it every call.
func customerFromArgs(args map[string]any) string {
	if args != nil {
		if v, ok := args["customer_id"]; ok && v != nil {
			return fmt.Sprintf("%v", v)
		}
	}
	return os.Getenv("UV_CUSTOMER_ID")
}

// fetchJSON does a GET against the REST API and returns the decoded body as a
// generic value. Errors are returned as Go errors (caller wraps in rpcError).
func fetchJSON(method, path string, body []byte) (any, int, error) {
	var rdr io.Reader
	if body != nil {
		rdr = strings.NewReader(string(body))
	}
	req, err := http.NewRequest(method, apiBase+path, rdr)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if t := os.Getenv("UV_TOKEN"); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	bs, _ := io.ReadAll(resp.Body)
	var out any
	if len(bs) > 0 {
		_ = json.Unmarshal(bs, &out)
	}
	return out, resp.StatusCode, nil
}

// listResources enumerates MCP resources for the given customer. Three classes:
// tables (one per synced_table), dashboards, schemas (distinct from catalog).
func listResources(id json.RawMessage, args map[string]any) rpcResponse {
	customer := customerFromArgs(args)
	if customer == "" {
		return rpcResponse{ID: id, Error: &rpcError{Code: -32602, Message: "customer_id required (pass as arg or set UV_CUSTOMER_ID)"}}
	}
	resources := []map[string]any{}

	// Tables — GET /api/v1/connections/{customer_id}/synced-tables
	// TODO(mcp-resources): synced-tables endpoint returns connection-scoped
	// rows; once the API exposes a customer-wide aggregator we can drop the
	// per-connection fanout below.
	if body, status, err := fetchJSON("GET", fmt.Sprintf("/api/v1/connections/%s/synced-tables", customer), nil); err == nil && status < 400 {
		for _, row := range coerceRows(body) {
			schema := stringField(row, "schema")
			name := stringField(row, "name", "table_name")
			if name == "" {
				continue
			}
			resources = append(resources, map[string]any{
				"uri":         fmt.Sprintf("uv://table/%s/%s/%s", customer, schema, name),
				"name":        fmt.Sprintf("%s.%s", schema, name),
				"description": fmt.Sprintf("Synced table %s.%s for customer %s", schema, name, customer),
				"mimeType":    "application/json",
			})
		}
	}

	// Dashboards — GET /api/v1/customers/{customer_id}/dashboards
	if body, status, err := fetchJSON("GET", fmt.Sprintf("/api/v1/customers/%s/dashboards", customer), nil); err == nil && status < 400 {
		for _, row := range coerceRows(body) {
			dashID := stringField(row, "id", "dashboard_id")
			title := stringField(row, "name", "title")
			if dashID == "" {
				continue
			}
			resources = append(resources, map[string]any{
				"uri":         fmt.Sprintf("uv://dashboard/%s/%s", customer, dashID),
				"name":        title,
				"description": fmt.Sprintf("Dashboard %s", title),
				"mimeType":    "application/json",
			})
		}
	}

	// Schemas — distinct schema names from catalog search.
	// TODO(mcp-resources): the catalog-search endpoint doesn't expose a
	// schema-distinct endpoint yet; we derive it from the synced-tables list
	// above to avoid an extra round-trip. Replace with a dedicated
	// /api/v1/customers/{id}/schemas endpoint when it lands.
	seen := map[string]bool{}
	for _, r := range resources {
		uri, _ := r["uri"].(string)
		if !strings.HasPrefix(uri, "uv://table/") {
			continue
		}
		parts := strings.SplitN(strings.TrimPrefix(uri, "uv://table/"), "/", 3)
		if len(parts) < 2 {
			continue
		}
		schema := parts[1]
		if schema == "" || seen[schema] {
			continue
		}
		seen[schema] = true
		resources = append(resources, map[string]any{
			"uri":         fmt.Sprintf("uv://schema/%s/%s", customer, schema),
			"name":        schema,
			"description": fmt.Sprintf("Schema %s for customer %s", schema, customer),
			"mimeType":    "application/json",
		})
	}

	return rpcResponse{ID: id, Result: map[string]any{"resources": resources}}
}

// readResource resolves a uv:// URI by dispatching to the corresponding REST
// endpoint and returning the body as an MCP resource content object.
func readResource(id json.RawMessage, uri string) rpcResponse {
	if !strings.HasPrefix(uri, "uv://") {
		return rpcResponse{ID: id, Error: &rpcError{Code: -32602, Message: "uri must start with uv://"}}
	}
	rest := strings.TrimPrefix(uri, "uv://")
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) < 2 {
		return rpcResponse{ID: id, Error: &rpcError{Code: -32602, Message: "malformed uri"}}
	}
	kind := parts[0]
	switch kind {
	case "table":
		// uv://table/{customer_id}/{schema}/{name}
		if len(parts) < 4 {
			return rpcResponse{ID: id, Error: &rpcError{Code: -32602, Message: "table uri must be uv://table/{customer}/{schema}/{name}"}}
		}
		customer, schema, name := parts[1], parts[2], parts[3]
		// TODO(mcp-resources): no per-table read endpoint yet; we return the
		// filtered slice from the synced-tables list.
		body, status, err := fetchJSON("GET", fmt.Sprintf("/api/v1/connections/%s/synced-tables", customer), nil)
		if err != nil {
			return rpcResponse{ID: id, Error: &rpcError{Code: -32000, Message: err.Error()}}
		}
		var match any
		for _, row := range coerceRows(body) {
			if stringField(row, "schema") == schema && (stringField(row, "name", "table_name") == name) {
				match = row
				break
			}
		}
		return wrapResource(id, uri, "application/json", map[string]any{"http_status": status, "table": match})
	case "dashboard":
		// uv://dashboard/{customer_id}/{dashboard_id}
		if len(parts) < 3 {
			return rpcResponse{ID: id, Error: &rpcError{Code: -32602, Message: "dashboard uri must be uv://dashboard/{customer}/{id}"}}
		}
		customer, dashID := parts[1], parts[2]
		body, status, err := fetchJSON("GET", fmt.Sprintf("/api/v1/customers/%s/dashboards/%s", customer, dashID), nil)
		if err != nil {
			return rpcResponse{ID: id, Error: &rpcError{Code: -32000, Message: err.Error()}}
		}
		return wrapResource(id, uri, "application/json", map[string]any{"http_status": status, "dashboard": body})
	case "schema":
		// uv://schema/{customer_id}/{schema}
		if len(parts) < 3 {
			return rpcResponse{ID: id, Error: &rpcError{Code: -32602, Message: "schema uri must be uv://schema/{customer}/{schema}"}}
		}
		_, schema := parts[1], parts[2]
		// TODO(mcp-resources): catalog/search endpoint is the closest we
		// have for an enumerator; replace with a dedicated schema-detail
		// endpoint when available.
		body, status, err := fetchJSON("GET", fmt.Sprintf("/api/v1/catalog/search?q=%s", schema), nil)
		if err != nil {
			return rpcResponse{ID: id, Error: &rpcError{Code: -32000, Message: err.Error()}}
		}
		return wrapResource(id, uri, "application/json", map[string]any{"http_status": status, "schema": schema, "matches": body})
	}
	return rpcResponse{ID: id, Error: &rpcError{Code: -32602, Message: "unknown resource kind: " + kind}}
}

// wrapResource shapes a single resource read result per the MCP spec's
// contents[] array, with a JSON-encoded text payload.
func wrapResource(id json.RawMessage, uri, mime string, payload any) rpcResponse {
	bs, _ := json.Marshal(payload)
	return rpcResponse{ID: id, Result: map[string]any{
		"contents": []map[string]any{{
			"uri":      uri,
			"mimeType": mime,
			"text":     string(bs),
		}},
	}}
}

// coerceRows pulls a slice-of-maps out of either a top-level array or a
// {"items": [...]} / {"rows": [...]} envelope — both shapes appear in the
// Ultraviolet REST API depending on the endpoint vintage.
func coerceRows(v any) []map[string]any {
	if v == nil {
		return nil
	}
	if arr, ok := v.([]any); ok {
		return rowsFromArr(arr)
	}
	if m, ok := v.(map[string]any); ok {
		for _, k := range []string{"items", "rows", "data", "tables", "dashboards"} {
			if arr, ok := m[k].([]any); ok {
				return rowsFromArr(arr)
			}
		}
	}
	return nil
}

func rowsFromArr(arr []any) []map[string]any {
	out := make([]map[string]any, 0, len(arr))
	for _, x := range arr {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func stringField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}
