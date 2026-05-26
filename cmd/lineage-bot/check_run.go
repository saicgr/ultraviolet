package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ultraviolet-dev/ultraviolet/internal/impact"
)

// conclusionFromHits derives a GitHub check-run conclusion from impact hits.
// Mapping (per Phase-5b ci-checks spec):
//   0 hits           → "success"      (safe to merge)
//   1-2 hits         → "neutral"      (FYI, not blocking)
//   ≥3 hits OR any   → "action_required"
//   severity hint
//
// Severity is not yet a first-class field on impact.Hit; we fall back to a
// substring scan over `Why` so callers can mark e.g. "critical: drops PK" as
// action_required even with a single hit.
func conclusionFromHits(hits []impact.Hit) string {
	for _, h := range hits {
		w := strings.ToLower(h.Why)
		if strings.Contains(w, "critical") || strings.Contains(w, "warn") {
			return "action_required"
		}
	}
	switch {
	case len(hits) == 0:
		return "success"
	case len(hits) <= 2:
		return "neutral"
	default:
		return "action_required"
	}
}

// postCheckRun posts a completed Check Run on the PR's head SHA so impact
// analysis shows up natively in the PR "Checks" tab (not just as a comment).
// The body reuses renderComment so PR and Check stay in sync.
func postCheckRun(ctx context.Context, repo, sha string, hits []impact.Hit) error {
	tok := os.Getenv("UV_GITHUB_TOKEN")
	if tok == "" {
		return fmt.Errorf("UV_GITHUB_TOKEN unset")
	}
	if sha == "" {
		return fmt.Errorf("empty head sha")
	}

	body := renderComment(hits, sha)
	summary := fmt.Sprintf("%d downstream node(s) impacted.", len(hits))
	if len(hits) == 0 {
		summary = "No downstream consumers detected."
	}

	payload, _ := json.Marshal(map[string]any{
		"name":        "Ultraviolet impact analysis",
		"head_sha":    sha,
		"status":      "completed",
		"conclusion":  conclusionFromHits(hits),
		"completed_at": time.Now().UTC().Format(time.RFC3339),
		"output": map[string]string{
			"title":   "Ultraviolet impact analysis",
			"summary": summary,
			"text":    body,
		},
	})

	url := fmt.Sprintf("https://api.github.com/repos/%s/check-runs", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	c := &http.Client{Timeout: 15 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("check-run http %d", resp.StatusCode)
	}
	return nil
}
