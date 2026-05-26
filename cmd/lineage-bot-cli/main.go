// Command lineage-bot-cli is the GitHub Actions counterpart to cmd/lineage-bot.
// It is invoked from the workflow on every pull_request event:
// opened|synchronize|reopened|edited. It:
//
//  1. Reads PR_DIFF (computed by the workflow `git diff` step).
//  2. Calls the customer's Ultraviolet API:
//        POST /api/v1/agents/schema_safeguard/run with {args: {diff: ...}}
//        POST /api/v1/agents/cost_optimizer/run    with {dry_run: true}
//  3. Renders a markdown comment containing the suggestions.
//  4. Finds an existing comment by marker, PATCHes it; else POSTs a new one.
//
// Authenticates via UV_TOKEN (Bearer JWT, minted via /api/v1/auth/login from
// a workspace API key stored in the repo's secrets as UV_API_TOKEN).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const commentMarker = "<!-- uv-lineage-bot:v1 -->"

type suggestion struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Rationale string `json:"rationale"`
	Severity  string `json:"severity"`
}
type runOut struct {
	Summary     string       `json:"summary"`
	Suggestions []suggestion `json:"suggestions"`
	Confidence  float64      `json:"confidence"`
}

func main() {
	if len(os.Args) < 2 || os.Args[1] != "analyze-pr" {
		fmt.Fprintln(os.Stderr, "usage: lineage-bot-cli analyze-pr")
		os.Exit(2)
	}
	apiBase := envOr("UV_API_URL", "https://api.ultraviolet.dev")
	apiToken := os.Getenv("UV_TOKEN")
	ghToken := os.Getenv("GITHUB_TOKEN")
	repo := os.Getenv("PR_REPO")
	prNumber := os.Getenv("PR_NUMBER")
	headSHA := os.Getenv("PR_HEAD_SHA")
	diff := os.Getenv("PR_DIFF")

	if repo == "" || prNumber == "" || ghToken == "" {
		fmt.Fprintln(os.Stderr, "missing PR_REPO / PR_NUMBER / GITHUB_TOKEN — abort")
		os.Exit(2)
	}

	schemaOut := callAgent(apiBase, apiToken, "schema_safeguard", map[string]any{
		"dry_run": true,
		"args":    map[string]any{"diff": diff},
	})
	costOut := callAgent(apiBase, apiToken, "cost_optimizer", map[string]any{"dry_run": true})

	body := renderComment(headSHA, schemaOut, costOut)
	if err := postOrEdit(repo, prNumber, ghToken, body); err != nil {
		fmt.Fprintln(os.Stderr, "comment write:", err)
		os.Exit(1)
	}
	fmt.Println("ok")
}

func callAgent(base, token, name string, body map[string]any) *runOut {
	if token == "" {
		return &runOut{Summary: "(skipped — no UV_TOKEN)"}
	}
	bs, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", base+"/api/v1/agents/"+name+"/run", bytes.NewReader(bs))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return &runOut{Summary: fmt.Sprintf("(error: %v)", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		bs, _ := io.ReadAll(resp.Body)
		return &runOut{Summary: fmt.Sprintf("(http %d: %s)", resp.StatusCode, string(bs))}
	}
	var out runOut
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return &out
}

func renderComment(sha string, schema, cost *runOut) string {
	var b strings.Builder
	b.WriteString(commentMarker + "\n")
	b.WriteString("## 🔎 Ultraviolet impact analysis\n\n")
	if sha != "" {
		s := sha
		if len(s) > 7 {
			s = s[:7]
		}
		b.WriteString(fmt.Sprintf("_Last analyzed commit: `%s` · re-runs on every push, this single comment edits in place._\n\n", s))
	}
	b.WriteString("### Schema-change blast radius\n")
	b.WriteString(schema.Summary + "\n\n")
	if len(schema.Suggestions) > 0 {
		b.WriteString("| Severity | Change | Rationale |\n|---|---|---|\n")
		for _, s := range schema.Suggestions {
			b.WriteString(fmt.Sprintf("| %s | %s | %s |\n", emoji(s.Severity), escape(s.Title), escape(s.Rationale)))
		}
		b.WriteString("\n")
	}
	b.WriteString("### Cost-optimizer review\n")
	b.WriteString(cost.Summary + "\n\n")
	if len(cost.Suggestions) > 0 {
		b.WriteString("| Severity | Suggestion |\n|---|---|\n")
		for _, s := range cost.Suggestions {
			b.WriteString(fmt.Sprintf("| %s | %s |\n", emoji(s.Severity), escape(s.Title)))
		}
		b.WriteString("\n")
	}
	b.WriteString("---\n<sub>Powered by Ultraviolet agents — `schema_safeguard` + `cost_optimizer`. Re-runs on every push to this PR. Reply `/uv suppress` to silence.</sub>\n")
	return b.String()
}

func emoji(sev string) string {
	switch sev {
	case "critical":
		return "🚨"
	case "warn":
		return "⚠️"
	default:
		return "ℹ️"
	}
}

func escape(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// postOrEdit finds the existing uv-lineage-bot comment by marker on the PR; if
// found, PATCHes it. Otherwise POSTs a new one. Mirrors cmd/lineage-bot's
// in-process equivalent for CI use.
func postOrEdit(repo, prNumber, token, body string) error {
	c := &http.Client{Timeout: 15 * time.Second}
	listURL := fmt.Sprintf("https://api.github.com/repos/%s/issues/%s/comments?per_page=100", repo, prNumber)
	req, _ := http.NewRequest("GET", listURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("list http %d", resp.StatusCode)
	}
	var existing []struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&existing)
	var existingID int64
	for _, cm := range existing {
		if strings.Contains(cm.Body, commentMarker) {
			existingID = cm.ID
			break
		}
	}
	payload, _ := json.Marshal(map[string]string{"body": body})
	var url, method string
	if existingID != 0 {
		url = fmt.Sprintf("https://api.github.com/repos/%s/issues/comments/%d", repo, existingID)
		method = "PATCH"
	} else {
		url = fmt.Sprintf("https://api.github.com/repos/%s/issues/%s/comments", repo, prNumber)
		method = "POST"
	}
	req2, _ := http.NewRequest(method, url, bytes.NewReader(payload))
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Accept", "application/vnd.github+json")
	resp2, err := c.Do(req2)
	if err != nil {
		return err
	}
	defer resp2.Body.Close()
	if resp2.StatusCode >= 400 {
		bs, _ := io.ReadAll(resp2.Body)
		return fmt.Errorf("write http %d: %s", resp2.StatusCode, string(bs))
	}
	return nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
