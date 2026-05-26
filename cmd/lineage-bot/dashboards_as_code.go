// pu-4 — dashboards-as-code via GitHub App.
//
// On a `push` event the bot scans commits[].added + commits[].modified for
// files under `dashboards/*.yaml`. For each match it fetches the file blob
// from the GitHub raw API, parses it as YAML into a dashboards.Dashboard,
// validates required fields, and upserts via dashboards.Store.
//
// One repo == one customer for now (mirrors the PR-comment customer routing
// in main.go); a real GitHub App installation will plumb workspace mapping.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"

	"github.com/ultraviolet-dev/ultraviolet/internal/dashboards"
)

// pushEvent is the slice of the GitHub push webhook payload we care about.
// Only the file lists per commit + the head ref are needed.
type pushEvent struct {
	Ref     string `json:"ref"`
	After   string `json:"after"`
	Commits []struct {
		ID       string   `json:"id"`
		Added    []string `json:"added"`
		Modified []string `json:"modified"`
		Removed  []string `json:"removed"`
	} `json:"commits"`
	Repository struct {
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	} `json:"repository"`
}

// dashboardYAML is the YAML shape we accept on disk. Mirrors
// dashboards.Dashboard but uses YAML tags so contributors can keep the source
// in their dbt/analytics repo as a human-editable file.
type dashboardYAML struct {
	Name     string             `yaml:"name"`
	IsPublic bool               `yaml:"is_public"`
	Tiles    []dashboards.Tile  `yaml:"tiles"`
}

// handleDashboardsAsCodePush is the entry point invoked from main.go's
// /webhook/push handler. It collects unique dashboards/*.yaml paths across
// commits, fetches each, and applies it to the dashboards store.
func handleDashboardsAsCodePush(ctx context.Context, log zerolog.Logger, pool *pgxpool.Pool, ev *pushEvent) {
	paths := collectDashboardPaths(ev)
	if len(paths) == 0 {
		return
	}
	customerID := pushEventCustomerID(ev.Repository.FullName)
	store := dashboards.NewStore(pool)
	for _, p := range paths {
		raw, err := fetchRepoFile(ctx, ev.Repository.FullName, ev.After, p)
		if err != nil {
			log.Warn().Err(err).Str("path", p).Msg("dashboards-as-code: fetch")
			continue
		}
		spec, err := parseDashboardYAML(raw)
		if err != nil {
			log.Warn().Err(err).Str("path", p).Msg("dashboards-as-code: parse")
			continue
		}
		d := &dashboards.Dashboard{
			CustomerID: customerID,
			Name:       spec.Name,
			Tiles:      spec.Tiles,
			IsPublic:   spec.IsPublic,
		}
		id, err := store.Create(ctx, d)
		if err != nil {
			log.Warn().Err(err).Str("path", p).Msg("dashboards-as-code: upsert")
			continue
		}
		log.Info().Str("path", p).Str("dashboard_id", id.String()).Msg("dashboards-as-code applied")
	}
}

// collectDashboardPaths returns the unique set of `dashboards/*.yaml` paths
// across commits[].added + .modified.
func collectDashboardPaths(ev *pushEvent) []string {
	seen := map[string]struct{}{}
	var out []string
	consider := func(p string) {
		if !strings.HasPrefix(p, "dashboards/") {
			return
		}
		if !(strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml")) {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	for _, c := range ev.Commits {
		for _, p := range c.Added {
			consider(p)
		}
		for _, p := range c.Modified {
			consider(p)
		}
	}
	return out
}

// parseDashboardYAML decodes + validates the YAML body. Returns a usable spec
// or an error — never a half-populated zero value.
func parseDashboardYAML(raw []byte) (*dashboardYAML, error) {
	var spec dashboardYAML
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	if strings.TrimSpace(spec.Name) == "" {
		return nil, fmt.Errorf("dashboard.name required")
	}
	for i, t := range spec.Tiles {
		if t.ChartType == "" {
			return nil, fmt.Errorf("tile[%d].chart_type required", i)
		}
	}
	return &spec, nil
}

// fetchRepoFile pulls a single file at `ref` from the GitHub raw API.
func fetchRepoFile(ctx context.Context, repo, ref, path string) ([]byte, error) {
	tok := os.Getenv("UV_GITHUB_TOKEN")
	url := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s?ref=%s", repo, path, ref)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	// Ask for raw bytes directly — avoids the base64 wrapping of the default
	// JSON response.
	req.Header.Set("Accept", "application/vnd.github.raw")
	c := &http.Client{Timeout: 15 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("github contents http %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// pushEventCustomerID is the same fixed-customer placeholder as the PR-comment
// path; a real GitHub App installation will plumb workspace mapping.
func pushEventCustomerID(_ string) uuid.UUID {
	return uuid.MustParse("00000000-0000-0000-0000-000000000000")
}
