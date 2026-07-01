package githubapp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/rs/zerolog"
)

// maxWebhookBody bounds the body we read so a hostile sender can't OOM us.
const maxWebhookBody = 2 << 20 // 2 MiB

// Handler is the single GitHub webhook endpoint. It verifies the signature
// (mandatory — the bot refuses to boot without a secret), de-dupes redeliveries
// by X-GitHub-Delivery, resolves the tenant fail-closed, and dispatches by
// X-GitHub-Event. cmd wiring registers ServeWebhook and nothing else.
type Handler struct {
	store    *Store
	client   *Client
	analyzer *Analyzer
	secret   []byte
	log      zerolog.Logger

	// OnPush, if set, handles `push` events (dashboards-as-code). Kept as a hook
	// so that concern stays in its own file without bloating this package.
	OnPush func(ctx context.Context, body []byte)
}

func NewHandler(store *Store, client *Client, analyzer *Analyzer, secret []byte, log zerolog.Logger) *Handler {
	return &Handler{store: store, client: client, analyzer: analyzer, secret: secret, log: log}
}

// VerifySignature checks an X-Hub-Signature-256 HMAC. There is no empty-secret
// bypass: an unsigned/invalid request is rejected. (Boot-time config refuses to
// start without a secret, so this is never accidentally permissive.)
func VerifySignature(secret, body []byte, sig string) bool {
	const prefix = "sha256="
	if len(secret) == 0 || !strings.HasPrefix(sig, prefix) {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(strings.TrimPrefix(sig, prefix)))
}

func (h *Handler) ServeWebhook(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if !VerifySignature(h.secret, body, r.Header.Get("X-Hub-Signature-256")) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	event := r.Header.Get("X-GitHub-Event")
	delivery := r.Header.Get("X-GitHub-Delivery")
	first, err := h.store.WebhookSeen(r.Context(), delivery, event)
	if err != nil {
		// DB error — return 5xx so GitHub retries rather than silently dropping.
		http.Error(w, "delivery dedupe failed", http.StatusInternalServerError)
		return
	}
	if !first {
		w.WriteHeader(http.StatusOK) // replay: already processed
		return
	}
	if err := h.dispatch(r.Context(), event, body); err != nil {
		h.log.Warn().Err(err).Str("event", event).Msg("github webhook")
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) dispatch(ctx context.Context, event string, body []byte) error {
	switch event {
	case "pull_request":
		return h.onPullRequest(ctx, body)
	case "issue_comment":
		return h.onIssueComment(ctx, body)
	case "installation":
		return h.onInstallation(ctx, body)
	case "installation_repositories":
		return h.onInstallationRepos(ctx, body)
	case "repository":
		return h.onRepository(ctx, body)
	case "push":
		if h.OnPush != nil {
			h.OnPush(ctx, body)
		}
		return nil
	default:
		return nil
	}
}

func (h *Handler) onPullRequest(ctx context.Context, body []byte) error {
	var ev struct {
		Action      string `json:"action"`
		PullRequest struct {
			Number  int    `json:"number"`
			HTMLURL string `json:"html_url"`
			Head    struct {
				SHA string `json:"sha"`
			} `json:"head"`
		} `json:"pull_request"`
		Repository struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
		} `json:"repository"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return err
	}
	switch ev.Action {
	case "opened", "synchronize", "reopened", "edited":
	default:
		return nil
	}
	cid, err := h.store.ResolveCustomer(ctx, ev.Installation.ID, ev.Repository.ID)
	if errors.Is(err, ErrNoCustomer) {
		h.log.Info().Str("repo", ev.Repository.FullName).Msg("github: no tenant mapping, skipping")
		return nil
	}
	if err != nil {
		return err
	}
	if h.store.IsSuppressed(ctx, cid, ev.Repository.FullName, ev.PullRequest.Number) {
		return nil
	}
	report, err := h.analyzer.AnalyzePR(ctx, cid, ev.Installation.ID, ev.Repository.FullName,
		ev.PullRequest.Number, ev.PullRequest.Head.SHA, ev.PullRequest.HTMLURL)
	if err != nil {
		return err
	}
	comment := RenderComment(report.Changes, report.Hits, report.DQRefs, ev.PullRequest.Head.SHA)
	if err := h.client.UpsertComment(ctx, ev.Installation.ID, ev.Repository.FullName, ev.PullRequest.Number, CommentMarker, comment); err != nil {
		h.log.Warn().Err(err).Msg("github: post comment")
	}
	payload := CheckRunPayload(report.Changes, report.Hits, report.DQRefs, ev.PullRequest.Head.SHA)
	if err := h.client.CreateCheckRun(ctx, ev.Installation.ID, ev.Repository.FullName, ev.PullRequest.Head.SHA, payload); err != nil {
		h.log.Warn().Err(err).Msg("github: check-run")
	}
	return nil
}

func (h *Handler) onIssueComment(ctx context.Context, body []byte) error {
	var ev struct {
		Action string `json:"action"`
		Issue  struct {
			Number      int              `json:"number"`
			PullRequest *json.RawMessage `json:"pull_request"`
		} `json:"issue"`
		Comment struct {
			Body string `json:"body"`
			User struct {
				Login string `json:"login"`
			} `json:"user"`
		} `json:"comment"`
		Repository struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
		} `json:"repository"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return err
	}
	if ev.Action != "created" || ev.Issue.PullRequest == nil || !hasSuppressDirective(ev.Comment.Body) {
		return nil
	}
	cid, err := h.store.ResolveCustomer(ctx, ev.Installation.ID, ev.Repository.ID)
	if errors.Is(err, ErrNoCustomer) {
		return nil
	}
	if err != nil {
		return err
	}
	return h.store.RecordSuppression(ctx, cid, ev.Repository.FullName, ev.Repository.ID, ev.Issue.Number, "", ev.Comment.User.Login)
}

func (h *Handler) onInstallation(ctx context.Context, body []byte) error {
	var ev struct {
		Action       string `json:"action"`
		Installation struct {
			ID      int64 `json:"id"`
			Account struct {
				Login string `json:"login"`
				Type  string `json:"type"`
			} `json:"account"`
		} `json:"installation"`
		Repositories []struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
		} `json:"repositories"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return err
	}
	switch ev.Action {
	case "created":
		if err := h.store.UpsertInstallation(ctx, ev.Installation.ID, ev.Installation.Account.Login, ev.Installation.Account.Type); err != nil {
			return err
		}
		for _, r := range ev.Repositories {
			if err := h.store.UpsertRepo(ctx, r.ID, ev.Installation.ID, r.FullName); err != nil {
				return err
			}
		}
	case "deleted":
		return h.store.DeleteInstallation(ctx, ev.Installation.ID)
	case "suspend":
		return h.store.SuspendInstallation(ctx, ev.Installation.ID, true)
	case "unsuspend":
		return h.store.SuspendInstallation(ctx, ev.Installation.ID, false)
	}
	return nil
}

func (h *Handler) onInstallationRepos(ctx context.Context, body []byte) error {
	var ev struct {
		Action       string `json:"action"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
		RepositoriesAdded []struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
		} `json:"repositories_added"`
		RepositoriesRemoved []struct {
			ID int64 `json:"id"`
		} `json:"repositories_removed"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return err
	}
	for _, r := range ev.RepositoriesAdded {
		if err := h.store.UpsertRepo(ctx, r.ID, ev.Installation.ID, r.FullName); err != nil {
			return err
		}
	}
	for _, r := range ev.RepositoriesRemoved {
		if err := h.store.RemoveRepo(ctx, r.ID); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) onRepository(ctx context.Context, body []byte) error {
	var ev struct {
		Action     string `json:"action"`
		Repository struct {
			ID       int64  `json:"id"`
			FullName string `json:"full_name"`
		} `json:"repository"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return err
	}
	if ev.Action == "renamed" || ev.Action == "transferred" {
		// Keyed on immutable repo_id, so this just refreshes the current name.
		return h.store.UpsertRepo(ctx, ev.Repository.ID, ev.Installation.ID, ev.Repository.FullName)
	}
	return nil
}

// hasSuppressDirective scans a PR comment for a line beginning `/uv suppress`.
func hasSuppressDirective(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "/uv suppress") {
			return true
		}
	}
	return false
}
