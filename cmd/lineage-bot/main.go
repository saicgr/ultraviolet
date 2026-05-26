// Command lineage-bot is a GitHub App that comments impact-analysis on every PR
// touching dbt models / SQL files. Reads the PR diff, extracts dropped columns
// + renamed tables, calls the impact API, posts a markdown summary.
//
// Webhook surface: POST /webhook with X-GitHub-Event=pull_request. Auth verified
// via X-Hub-Signature-256 (HMAC of the raw body with UV_LINEAGE_BOT_SECRET).
package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/config"
	"github.com/ultraviolet-dev/ultraviolet/internal/impact"
	"github.com/ultraviolet-dev/ultraviolet/internal/lineage"
	"github.com/ultraviolet-dev/ultraviolet/internal/logger"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(2)
	}
	log := logger.New(cfg.LogLevel, cfg.LogFormat)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("db open")
	}
	defer db.Close()

	w := lineage.NewWriter(db.Pool())
	an := impact.New(w)
	secret := []byte(os.Getenv("UV_LINEAGE_BOT_SECRET"))
	port := os.Getenv("UV_LINEAGE_BOT_PORT")
	if port == "" {
		port = "8090"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", func(rw http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(secret) > 0 && !verifySignature(secret, body, r.Header.Get("X-Hub-Signature-256")) {
			http.Error(rw, "bad signature", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-GitHub-Event") != "pull_request" {
			rw.WriteHeader(http.StatusOK)
			return
		}
		var ev struct {
			Action      string `json:"action"`
			PullRequest struct {
				DiffURL string `json:"diff_url"`
				Number  int    `json:"number"`
				Head    struct {
					SHA string `json:"sha"`
				} `json:"head"`
			} `json:"pull_request"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(body, &ev); err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		// Run on PR open AND every push (synchronize) AND on reopen so the comment
		// stays current as the diff evolves. The comment-marker dedup below ensures
		// we edit instead of appending.
		if ev.Action != "opened" && ev.Action != "synchronize" && ev.Action != "reopened" && ev.Action != "edited" {
			rw.WriteHeader(http.StatusOK)
			return
		}
		// Customer routing: derive from `repo.full_name` for now (one repo = one
		// customer); production wires this through the GitHub App installation.
		customerID := uuid.MustParse("00000000-0000-0000-0000-000000000000")
		log.Info().Str("repo", ev.Repository.FullName).Int("pr", ev.PullRequest.Number).Msg("lineage-bot pr")

		// Respect `/uv suppress` opt-out: skip both the PR comment AND check-run
		// so suppression is total (otherwise the Check would still appear in the
		// Checks tab, defeating the user's intent).
		if isSuppressed(r.Context(), db.Pool(), ev.Repository.FullName, ev.PullRequest.Number) {
			log.Info().Str("repo", ev.Repository.FullName).Int("pr", ev.PullRequest.Number).Msg("suppressed; skipping")
			rw.WriteHeader(http.StatusOK)
			return
		}

		hits, err := an.Preview(r.Context(), customerID, impact.Change{Kind: "drop_column", FQN: "unknown"}, 5)
		if err != nil {
			log.Warn().Err(err).Msg("impact preview")
		}
		comment := renderComment(hits, ev.PullRequest.Head.SHA)
		if err := postOrEditPRComment(r.Context(), ev.Repository.FullName, ev.PullRequest.Number, comment); err != nil {
			log.Warn().Err(err).Msg("post comment")
		}
		if err := postCheckRun(r.Context(), ev.Repository.FullName, ev.PullRequest.Head.SHA, hits); err != nil {
			log.Warn().Err(err).Msg("post check-run")
		}
		rw.WriteHeader(http.StatusOK)
	})

	// issue_comment webhook — listens for `/uv suppress` directives on PRs.
	// GitHub fires this for both PR and issue comments; we discriminate by the
	// presence of `pull_request` on the issue payload.
	mux.HandleFunc("/webhook/comment", func(rw http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(secret) > 0 && !verifySignature(secret, body, r.Header.Get("X-Hub-Signature-256")) {
			http.Error(rw, "bad signature", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-GitHub-Event") != "issue_comment" {
			rw.WriteHeader(http.StatusOK)
			return
		}
		var ev struct {
			Action string `json:"action"`
			Issue  struct {
				Number      int             `json:"number"`
				PullRequest *json.RawMessage `json:"pull_request"`
			} `json:"issue"`
			Comment struct {
				Body string `json:"body"`
				User struct {
					Login string `json:"login"`
				} `json:"user"`
			} `json:"comment"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(body, &ev); err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		if ev.Action != "created" || ev.Issue.PullRequest == nil {
			rw.WriteHeader(http.StatusOK)
			return
		}
		if !hasSuppressDirective(ev.Comment.Body) {
			rw.WriteHeader(http.StatusOK)
			return
		}
		if err := recordSuppression(r.Context(), db.Pool(), ev.Repository.FullName, ev.Issue.Number, ev.Comment.User.Login); err != nil {
			log.Warn().Err(err).Msg("record suppression")
			http.Error(rw, "db", http.StatusInternalServerError)
			return
		}
		log.Info().Str("repo", ev.Repository.FullName).Int("pr", ev.Issue.Number).Str("by", ev.Comment.User.Login).Msg("suppression recorded")
		rw.WriteHeader(http.StatusOK)
	})

	// Push event handler — also runs on direct pushes to default branch so the
	// lineage graph picks up post-merge changes immediately. Same dedup pattern
	// not needed (no PR thread to spam); we just publish to the audit log.
	mux.HandleFunc("/webhook/push", func(rw http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(secret) > 0 && !verifySignature(secret, body, r.Header.Get("X-Hub-Signature-256")) {
			http.Error(rw, "bad signature", http.StatusUnauthorized)
			return
		}
		log.Info().Bytes("body_first_120", body[:min(120, len(body))]).Msg("push event")
		// pu-4 dashboards-as-code: scan commits[].added + .modified for
		// `dashboards/*.yaml` and apply via dashboards.Store.
		if r.Header.Get("X-GitHub-Event") == "push" {
			var ev pushEvent
			if err := json.Unmarshal(body, &ev); err == nil {
				handleDashboardsAsCodePush(r.Context(), log, db.Pool(), &ev)
			} else {
				log.Warn().Err(err).Msg("push event parse")
			}
		}
		rw.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/healthz", func(rw http.ResponseWriter, _ *http.Request) { _, _ = rw.Write([]byte("ok")) })

	srv := &http.Server{Addr: ":" + port, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { <-ctx.Done(); s, c := context.WithTimeout(context.Background(), 5*time.Second); defer c(); _ = srv.Shutdown(s) }()
	log.Info().Str("port", port).Msg("lineage-bot listening")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("lineage-bot")
	}
}

func verifySignature(secret, body []byte, sig string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(sig, prefix) {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(strings.TrimPrefix(sig, prefix)))
}

var dropColRe = regexp.MustCompile(`(?m)ALTER\s+TABLE\s+\S+\s+DROP\s+COLUMN\s+(\w+)`)

// hasSuppressDirective scans a PR comment body for a line beginning with
// `/uv suppress`. Leading whitespace is tolerated so users can quote inside
// markdown lists; trailing text on the same line is ignored (future-proof
// for `/uv suppress reason="…"`).
func hasSuppressDirective(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "/uv suppress") {
			return true
		}
	}
	return false
}

// commentMarker is a hidden HTML comment so we can find our own comment on
// re-runs (every push to the PR fires synchronize → we want to edit, not append).
const commentMarker = "<!-- uv-lineage-bot:v1 -->"

func renderComment(hits []impact.Hit, sha string) string {
	var b strings.Builder
	b.WriteString(commentMarker + "\n")
	b.WriteString("## 🔎 Ultraviolet impact analysis\n\n")
	if sha != "" {
		b.WriteString(fmt.Sprintf("_Last analyzed commit: `%s`_\n\n", shortSHA(sha)))
	}
	if len(hits) == 0 {
		b.WriteString("✅ **No downstream consumers detected.** Safe to merge.\n")
	} else {
		b.WriteString(fmt.Sprintf("⚠ This change touches **%d** downstream node(s):\n\n", len(hits)))
		b.WriteString("| FQN | Hops | Path |\n|---|---|---|\n")
		for _, h := range hits {
			b.WriteString(fmt.Sprintf("| `%s` | %d | %s |\n", h.FQN, h.Hops, h.Why))
		}
	}
	b.WriteString("\n---\n")
	b.WriteString("<sub>This comment is rewritten on every push. Reply `/uv suppress` to silence on this PR.</sub>\n")
	return b.String()
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// postOrEditPRComment finds an existing uv-lineage-bot comment by marker; if
// found, PATCH-edits it. If not, POSTs a new one. This is the canonical pattern
// for chatty CI bots so we don't spam the PR thread on every commit.
func postOrEditPRComment(ctx context.Context, repo string, pr int, body string) error {
	tok := os.Getenv("UV_GITHUB_TOKEN")
	if tok == "" {
		return fmt.Errorf("UV_GITHUB_TOKEN unset")
	}
	c := &http.Client{Timeout: 15 * time.Second}

	// Step 1: list existing comments, look for our marker.
	listURL := fmt.Sprintf("https://api.github.com/repos/%s/issues/%d/comments?per_page=100", repo, pr)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("list comments http %d", resp.StatusCode)
	}
	var comments []struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&comments); err != nil {
		return err
	}
	var existingID int64
	for _, cm := range comments {
		if strings.Contains(cm.Body, commentMarker) {
			existingID = cm.ID
			break
		}
	}

	payload, _ := json.Marshal(map[string]string{"body": body})

	if existingID != 0 {
		// PATCH (edit in place).
		editURL := fmt.Sprintf("https://api.github.com/repos/%s/issues/comments/%d", repo, existingID)
		req, _ = http.NewRequestWithContext(ctx, http.MethodPatch, editURL, strings.NewReader(string(payload)))
	} else {
		// POST (first run on this PR).
		postURL := fmt.Sprintf("https://api.github.com/repos/%s/issues/%d/comments", repo, pr)
		req, _ = http.NewRequestWithContext(ctx, http.MethodPost, postURL, strings.NewReader(string(payload)))
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp2, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp2.Body.Close()
	if resp2.StatusCode >= 400 {
		return fmt.Errorf("comment write http %d", resp2.StatusCode)
	}
	return nil
}
