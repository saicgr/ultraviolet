package api

// W2 GitHub App settings surface (customer-scoped):
//   GET  /api/v1/github/install-info        → { install_url, installed }
//   GET  /api/v1/github/setup?installation_id=  → bind installation to the
//        active customer, then 302 to the webapp (install callback target)
//   GET  /api/v1/github/installations       → installations for this customer
//   GET  /api/v1/github/repos               → repos under them + connection state
//   POST /api/v1/github/connect   { repo_full_name }
//   POST /api/v1/github/disconnect { repo_full_name }

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

type githubInstallInfo struct {
	InstallURL string `json:"install_url"`
	Installed  bool   `json:"installed"`
}

func (s *Server) githubInstallInfo(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var installed bool
	_ = s.db.Pool().QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM github_installation WHERE customer_id = $1)`, cid).Scan(&installed)
	url := ""
	if s.cfg.GitHubAppSlug != "" {
		url = "https://github.com/apps/" + s.cfg.GitHubAppSlug + "/installations/new"
	}
	writeJSON(w, http.StatusOK, githubInstallInfo{InstallURL: url, Installed: installed})
}

// githubSetupCallback is GitHub's post-install redirect target. It associates
// the new installation with the active customer, then redirects to the webapp.
func (s *Server) githubSetupCallback(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if idStr := r.URL.Query().Get("installation_id"); idStr != "" {
		if id, perr := strconv.ParseInt(idStr, 10, 64); perr == nil {
			// The installation webhook upserts the row; here we claim it for the
			// customer that initiated the install.
			_, _ = s.db.Pool().Exec(r.Context(),
				`INSERT INTO github_installation (installation_id, account_login, customer_id)
				 VALUES ($1, '', $2)
				 ON CONFLICT (installation_id) DO UPDATE SET customer_id = EXCLUDED.customer_id, updated_at = now()`,
				id, cid)
		}
	}
	redirect := s.cfg.GitHubSetupRedirectURL
	if redirect == "" {
		redirect = "/"
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

type githubInstallationRow struct {
	InstallationID int64  `json:"installation_id"`
	AccountLogin   string `json:"account_login"`
	Suspended      bool   `json:"suspended"`
}

func (s *Server) listGitHubInstallations(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rows, err := s.db.Pool().Query(r.Context(),
		`SELECT installation_id, account_login, suspended_at IS NOT NULL
		 FROM github_installation WHERE customer_id = $1 ORDER BY created_at DESC`, cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := []githubInstallationRow{}
	for rows.Next() {
		var x githubInstallationRow
		if err := rows.Scan(&x.InstallationID, &x.AccountLogin, &x.Suspended); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, x)
	}
	writeJSON(w, http.StatusOK, out)
}

type githubRepoRow struct {
	RepoID         int64  `json:"repo_id"`
	FullName       string `json:"full_name"`
	InstallationID int64  `json:"installation_id"`
	Connected      bool   `json:"connected"`
}

func (s *Server) listGitHubRepos(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rows, err := s.db.Pool().Query(r.Context(),
		`SELECT gr.repo_id, gr.repo_full_name, gr.installation_id, gr.connected
		 FROM github_repo gr
		 JOIN github_installation gi ON gi.installation_id = gr.installation_id
		 WHERE gi.customer_id = $1
		 ORDER BY gr.repo_full_name`, cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := []githubRepoRow{}
	for rows.Next() {
		var x githubRepoRow
		if err := rows.Scan(&x.RepoID, &x.FullName, &x.InstallationID, &x.Connected); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, x)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) setGitHubRepoConnected(w http.ResponseWriter, r *http.Request, connected bool) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		RepoFullName string `json:"repo_full_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RepoFullName == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("repo_full_name required"))
		return
	}
	// Scope the update to the customer's own installations (no cross-tenant writes).
	// Disconnect only flips the `connected` flag — it must NOT null customer_id,
	// which would unbind the repo's tenant (data loss). Reconnecting later would
	// otherwise route the bot to the zero-UUID placeholder again.
	ct, err := s.db.Pool().Exec(r.Context(),
		`UPDATE github_repo gr SET connected = $1, customer_id = $2
		 FROM github_installation gi
		 WHERE gr.installation_id = gi.installation_id
		   AND gi.customer_id = $3 AND gr.repo_full_name = $4`,
		connected, cid, cid, body.RepoFullName)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if ct.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, fmt.Errorf("repo not found under your installations"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connected": connected})
}

func (s *Server) connectGitHubRepo(w http.ResponseWriter, r *http.Request) {
	s.setGitHubRepoConnected(w, r, true)
}

func (s *Server) disconnectGitHubRepo(w http.ResponseWriter, r *http.Request) {
	s.setGitHubRepoConnected(w, r, false)
}
