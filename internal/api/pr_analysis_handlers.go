package api

// W2 PR analysis history for the webapp:
//   GET /api/v1/pr-analysis           → list (customer-scoped)
//   GET /api/v1/pr-analysis/{id}      → detail (changes + hits + dq_refs)

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

type prAnalysisRow struct {
	ID             int64           `json:"id"`
	Repo           string          `json:"repo"`
	PRNumber       int             `json:"pr_number"`
	HeadSHA        string          `json:"head_sha"`
	PullRequestURL string          `json:"pull_request_url"`
	Conclusion     string          `json:"conclusion"`
	ImpactedCount  int             `json:"impacted_count"`
	DQImpact       string          `json:"dq_impact"` // none|touched|failing
	CreatedAt      time.Time       `json:"created_at"`
	Changes        json.RawMessage `json:"changes,omitempty"`
	Hits           json.RawMessage `json:"hits,omitempty"`
	DQRefs         json.RawMessage `json:"dq_refs,omitempty"`
}

func dqImpact(dqRaw []byte) string {
	var dqRefs []map[string]any
	_ = json.Unmarshal(dqRaw, &dqRefs)
	touched := false
	for _, d := range dqRefs {
		touched = true
		if st, _ := d["status"].(string); st == "fail" || st == "error" {
			return "failing"
		}
	}
	if touched {
		return "touched"
	}
	return "none"
}

func (s *Server) listPRAnalysis(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rows, err := s.db.Pool().Query(r.Context(),
		`SELECT id, repo_full_name, pr_number, head_sha, pull_request_url, conclusion,
		        jsonb_array_length(hits), dq_refs, created_at
		 FROM pr_analysis WHERE customer_id = $1 ORDER BY created_at DESC, pr_number DESC, id DESC LIMIT 200`, cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	out := []prAnalysisRow{}
	for rows.Next() {
		var x prAnalysisRow
		var dqRaw []byte
		if err := rows.Scan(&x.ID, &x.Repo, &x.PRNumber, &x.HeadSHA, &x.PullRequestURL, &x.Conclusion,
			&x.ImpactedCount, &dqRaw, &x.CreatedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		x.DQImpact = dqImpact(dqRaw)
		out = append(out, x)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getPRAnalysis(w http.ResponseWriter, r *http.Request) {
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var x prAnalysisRow
	var dqRaw []byte
	row := s.db.Pool().QueryRow(r.Context(),
		`SELECT id, repo_full_name, pr_number, head_sha, pull_request_url, conclusion,
		        jsonb_array_length(hits), changes, hits, dq_refs, created_at
		 FROM pr_analysis WHERE id = $1 AND customer_id = $2`, id, cid)
	if err := row.Scan(&x.ID, &x.Repo, &x.PRNumber, &x.HeadSHA, &x.PullRequestURL, &x.Conclusion,
		&x.ImpactedCount, &x.Changes, &x.Hits, &dqRaw, &x.CreatedAt); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	x.DQRefs = dqRaw
	x.DQImpact = dqImpact(dqRaw)
	writeJSON(w, http.StatusOK, x)
}
