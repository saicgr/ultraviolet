// Command uv is the developer CLI: login, query, manage connections, push
// semantic models, deploy dashboards from YAML.
//
// Usage:
//
//	uv login --api-key <key>
//	uv query "SELECT 1"
//	uv connections list
//	uv semantic push model.yaml
//	uv dashboard deploy dashboard.yaml
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "login":
		runLogin(os.Args[2:])
	case "query":
		runQuery(os.Args[2:])
	case "connections":
		runConnections(os.Args[2:])
	case "semantic":
		runSemantic(os.Args[2:])
	case "dashboard":
		runDashboard(os.Args[2:])
	case "version":
		fmt.Println("uv 0.1.0")
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `uv — Ultraviolet CLI
  uv login --api-key <key>
  uv query "<sql>"
  uv connections list
  uv semantic push <file.yaml>
  uv dashboard deploy <file.yaml>
  uv version`)
	os.Exit(2)
}

func base() string {
	if v := os.Getenv("UV_API_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://localhost:8080"
}

func tokenPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".uv-token")
}

func runLogin(args []string) {
	var key string
	for i := 0; i < len(args); i++ {
		if args[i] == "--api-key" && i+1 < len(args) {
			key = args[i+1]
		}
	}
	if key == "" {
		fmt.Fprintln(os.Stderr, "--api-key required")
		os.Exit(2)
	}
	body, _ := json.Marshal(map[string]string{"api_key": key})
	resp, err := http.Post(base()+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	must(err)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		bs, _ := io.ReadAll(resp.Body)
		fmt.Fprintln(os.Stderr, "login failed:", string(bs))
		os.Exit(1)
	}
	var t struct{ AccessToken string `json:"access_token"` }
	must(json.NewDecoder(resp.Body).Decode(&t))
	must(os.WriteFile(tokenPath(), []byte(t.AccessToken), 0o600))
	fmt.Println("logged in. token saved to", tokenPath())
}

func loadToken() string {
	b, err := os.ReadFile(tokenPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, "not logged in. run: uv login --api-key <key>")
		os.Exit(2)
	}
	return strings.TrimSpace(string(b))
}

func runQuery(args []string) {
	if len(args) < 1 {
		usage()
	}
	// Phase-1: the proxy speaks PG-wire. CLI defers to psql when present so we
	// don't reinvent the wheel here. If you'd rather hit the REST analytics API,
	// use `curl` directly.
	fmt.Fprintln(os.Stderr, "uv query proxies to PG-wire on :5000 — use `psql -h localhost -p 5000 -U <slug> -d <slug>_bigquery`")
	_ = args
}

func runConnections(args []string) {
	if len(args) == 0 || args[0] != "list" {
		usage()
	}
	tok := loadToken()
	req, _ := http.NewRequest("GET", base()+"/api/v1/customers", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	must(err)
	defer resp.Body.Close()
	bs, _ := io.ReadAll(resp.Body)
	fmt.Println(string(bs))
}

func runSemantic(args []string) {
	if len(args) < 2 || args[0] != "push" {
		usage()
	}
	src, err := os.ReadFile(args[1])
	must(err)
	tok := loadToken()
	body, _ := json.Marshal(map[string]string{"yaml": string(src)})
	req, _ := http.NewRequest("POST", base()+"/api/v1/semantic", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	must(err)
	defer resp.Body.Close()
	bs, _ := io.ReadAll(resp.Body)
	fmt.Println(string(bs))
}

func runDashboard(args []string) {
	if len(args) < 2 || args[0] != "deploy" {
		usage()
	}
	src, err := os.ReadFile(args[1])
	must(err)
	tok := loadToken()
	req, _ := http.NewRequest("POST", base()+"/api/v1/dashboards", bytes.NewReader(src))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/yaml")
	resp, err := http.DefaultClient.Do(req)
	must(err)
	defer resp.Body.Close()
	bs, _ := io.ReadAll(resp.Body)
	fmt.Println(string(bs))
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
