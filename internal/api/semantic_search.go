// Package api — semantic_search.go: catalog semantic search + reindex (ai-u5).
//
// POST /api/v1/embeddings/reindex?customer_id=... — embeds + persists all
//   table_metadata rows for the given customer.
// GET  /api/v1/catalog/semantic-search?q=...&k=10 — embeds the query, returns
//   top-k hits by cosine similarity over catalog_embedding.
//
// Both routes return 503 when OPENAI_API_KEY is empty — no fake embeddings.
package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/ai"
)

func (s *Server) embeddingsReindex(w http.ResponseWriter, r *http.Request) {
	if s.cfg.OpenAIKey == "" {
		writeErr(w, http.StatusServiceUnavailable, errors.New("embeddings: OPENAI_API_KEY not configured"))
		return
	}
	cidStr := r.URL.Query().Get("customer_id")
	cid, err := uuid.Parse(cidStr)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("customer_id: %w", err))
		return
	}
	emb := ai.NewEmbedder(s.cfg.OpenAIKey, "", s.db.Pool())
	n, err := emb.IndexCatalog(r.Context(), cid)
	if err != nil {
		if errors.Is(err, ai.ErrNoProvider) {
			writeErr(w, http.StatusServiceUnavailable, err)
			return
		}
		writeErr(w, http.StatusBadGateway, fmt.Errorf("reindex: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"customer_id": cid,
		"indexed":     n,
		"model":       ai.DefaultEmbeddingModel,
	})
}

func (s *Server) catalogSemanticSearch(w http.ResponseWriter, r *http.Request) {
	if s.cfg.OpenAIKey == "" {
		writeErr(w, http.StatusServiceUnavailable, errors.New("embeddings: OPENAI_API_KEY not configured"))
		return
	}
	cid, err := s.activeCustomer(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("q required"))
		return
	}
	k := 10
	if v := r.URL.Query().Get("k"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			k = n
		}
	}
	emb := ai.NewEmbedder(s.cfg.OpenAIKey, "", s.db.Pool())
	hits, err := emb.Search(r.Context(), cid, q, k)
	if err != nil {
		if errors.Is(err, ai.ErrNoProvider) {
			writeErr(w, http.StatusServiceUnavailable, err)
			return
		}
		writeErr(w, http.StatusBadGateway, fmt.Errorf("search: %w", err))
		return
	}
	if hits == nil {
		hits = []ai.SearchHit{}
	}
	writeJSON(w, http.StatusOK, hits)
}
