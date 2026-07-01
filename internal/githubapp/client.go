package githubapp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// repoNameRe constrains "owner/name" so a hostile repository.full_name can't be
// interpolated into a GitHub API path to reach another host/route (SSRF). The
// API host itself is a fixed constant, never derived from the payload.
var repoNameRe = regexp.MustCompile(`^[\w.-]+/[\w.-]+$`)

// ValidRepo reports whether a repo full name is shaped "owner/name".
func ValidRepo(full string) bool { return repoNameRe.MatchString(full) }

// Client is a thin authenticated GitHub REST client. Each call resolves a
// per-installation token via the App.
type Client struct {
	app    *App
	http   *http.Client
	apiURL string
}

func NewClient(app *App) *Client {
	return &Client{app: app, http: &http.Client{Timeout: 20 * time.Second}, apiURL: "https://api.github.com"}
}

func (c *Client) do(ctx context.Context, installationID int64, method, path, accept string, body io.Reader) (*http.Response, error) {
	if c.app == nil {
		return nil, fmt.Errorf("github app not configured")
	}
	tok, err := c.app.InstallationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.apiURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if accept == "" {
		accept = "application/vnd.github+json"
	}
	req.Header.Set("Accept", accept)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

// PRDiff fetches the unified diff for a pull request.
func (c *Client) PRDiff(ctx context.Context, installationID int64, repo string, pr int) (string, error) {
	if !ValidRepo(repo) {
		return "", fmt.Errorf("invalid repo %q", repo)
	}
	resp, err := c.do(ctx, installationID, http.MethodGet,
		fmt.Sprintf("/repos/%s/pulls/%d", repo, pr), "application/vnd.github.v3.diff", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("pr diff http %d", resp.StatusCode)
	}
	// Cap the diff we read so a huge PR can't exhaust memory.
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2 MiB
	return string(b), err
}

// FileAtRef fetches a file's raw contents at a commit. Returns ("", nil) when
// the file is absent (404) so a newly-added or deleted path is not an error.
func (c *Client) FileAtRef(ctx context.Context, installationID int64, repo, path, ref string) (string, error) {
	if !ValidRepo(repo) {
		return "", fmt.Errorf("invalid repo %q", repo)
	}
	resp, err := c.do(ctx, installationID, http.MethodGet,
		fmt.Sprintf("/repos/%s/contents/%s?ref=%s", repo, path, ref), "application/vnd.github.raw", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("contents http %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return string(b), err
}

type issueComment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

// FindComment returns the id of the first issue comment containing marker, or 0.
func (c *Client) FindComment(ctx context.Context, installationID int64, repo string, pr int, marker string) (int64, error) {
	if !ValidRepo(repo) {
		return 0, fmt.Errorf("invalid repo %q", repo)
	}
	resp, err := c.do(ctx, installationID, http.MethodGet,
		fmt.Sprintf("/repos/%s/issues/%d/comments?per_page=100", repo, pr), "", nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("list comments http %d", resp.StatusCode)
	}
	var comments []issueComment
	if err := json.NewDecoder(resp.Body).Decode(&comments); err != nil {
		return 0, err
	}
	for _, cm := range comments {
		if strings.Contains(cm.Body, marker) {
			return cm.ID, nil
		}
	}
	return 0, nil
}

// UpsertComment edits the marked comment if present, else creates a new one.
func (c *Client) UpsertComment(ctx context.Context, installationID int64, repo string, pr int, marker, body string) error {
	id, err := c.FindComment(ctx, installationID, repo, pr, marker)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"body": body})
	var method, path string
	if id != 0 {
		method, path = http.MethodPatch, fmt.Sprintf("/repos/%s/issues/comments/%d", repo, id)
	} else {
		method, path = http.MethodPost, fmt.Sprintf("/repos/%s/issues/%d/comments", repo, pr)
	}
	resp, err := c.do(ctx, installationID, method, path, "", strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("upsert comment http %d", resp.StatusCode)
	}
	return nil
}

// CreateCheckRun posts a completed Check Run on a head SHA.
func (c *Client) CreateCheckRun(ctx context.Context, installationID int64, repo, sha string, payload []byte) error {
	if !ValidRepo(repo) {
		return fmt.Errorf("invalid repo %q", repo)
	}
	resp, err := c.do(ctx, installationID, http.MethodPost,
		fmt.Sprintf("/repos/%s/check-runs", repo), "", strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("check-run http %d", resp.StatusCode)
	}
	return nil
}

// Repo is a repository visible to an installation.
type Repo struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
}

// ListInstallationRepos lists repositories an installation can access.
func (c *Client) ListInstallationRepos(ctx context.Context, installationID int64) ([]Repo, error) {
	resp, err := c.do(ctx, installationID, http.MethodGet, "/installation/repositories?per_page=100", "", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("list repos http %d", resp.StatusCode)
	}
	var out struct {
		Repositories []Repo `json:"repositories"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Repositories, nil
}
