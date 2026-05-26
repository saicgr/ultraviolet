// Package ai — embeddings.go: semantic catalog search (ai-u5).
//
// Embedder.Embed calls OpenAI /v1/embeddings (text-embedding-3-small, 1536-dim
// default). IndexCatalog enumerates table_metadata rows and persists vectors
// as little-endian []float32 packed into bytea. Search embeds the query and
// scores all customer vectors in Go via cosine similarity. No pgvector dep.
//
// Project rule: no fake embeddings — when OPENAI_API_KEY is empty, Embed
// returns ErrNoProvider and handlers map that to 503.
package ai

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	DefaultEmbeddingModel = "text-embedding-3-small"
	DefaultEmbeddingDim   = 1536
)

// SearchHit is one result of Embedder.Search.
type SearchHit struct {
	FQN   string  `json:"fqn"`
	Kind  string  `json:"kind"`
	Text  string  `json:"text"`
	Score float64 `json:"score"`
}

type Embedder struct {
	apiKey string
	model  string
	pool   *pgxpool.Pool
	client *http.Client
}

func NewEmbedder(apiKey, model string, pool *pgxpool.Pool) *Embedder {
	if model == "" {
		model = DefaultEmbeddingModel
	}
	return &Embedder{apiKey: apiKey, model: model, pool: pool, client: defaultHTTP()}
}

// Embed calls the OpenAI embeddings endpoint. Returns ErrNoProvider if no key.
func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if e.apiKey == "" {
		return nil, ErrNoProvider
	}
	body, _ := json.Marshal(map[string]any{"model": e.model, "input": text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.openai.com/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("openai embeddings %d: %s", res.StatusCode, string(raw))
	}
	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return nil, errors.New("openai embeddings: empty result")
	}
	return parsed.Data[0].Embedding, nil
}

// IndexCatalog enumerates table_metadata for the customer and writes one
// embedding row per table (kind='table'). Upserts on (customer_id, fqn, kind, model).
func (e *Embedder) IndexCatalog(ctx context.Context, customerID uuid.UUID) (int, error) {
	if e.apiKey == "" {
		return 0, ErrNoProvider
	}
	if e.pool == nil {
		return 0, errors.New("embedder: nil pool")
	}
	rows, err := e.pool.Query(ctx, `
		SELECT fqn, COALESCE(description,'')
		FROM table_metadata WHERE customer_id = $1`, customerID)
	if err != nil {
		return 0, err
	}
	type entry struct{ fqn, desc string }
	var entries []entry
	for rows.Next() {
		var fqn, desc string
		if err := rows.Scan(&fqn, &desc); err != nil {
			rows.Close()
			return 0, err
		}
		entries = append(entries, entry{fqn, desc})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	count := 0
	for _, ent := range entries {
		text := ent.fqn
		if ent.desc != "" {
			text = ent.fqn + " — " + ent.desc
		}
		vec, err := e.Embed(ctx, text)
		if err != nil {
			return count, fmt.Errorf("embed %q: %w", ent.fqn, err)
		}
		blob := PackFloat32(vec)
		if _, err := e.pool.Exec(ctx, `
			INSERT INTO catalog_embedding (customer_id, fqn, kind, text, embedding, model)
			VALUES ($1,$2,'table',$3,$4,$5)
			ON CONFLICT (customer_id, fqn, kind, model) DO UPDATE
			  SET text = EXCLUDED.text, embedding = EXCLUDED.embedding, created_at = now()`,
			customerID, ent.fqn, text, blob, e.model); err != nil {
			return count, fmt.Errorf("persist %q: %w", ent.fqn, err)
		}
		count++
	}
	return count, nil
}

// Search embeds the query, scans all customer embeddings, returns top-k by cosine.
func (e *Embedder) Search(ctx context.Context, customerID uuid.UUID, query string, k int) ([]SearchHit, error) {
	if k <= 0 {
		k = 10
	}
	qvec, err := e.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	rows, err := e.pool.Query(ctx, `
		SELECT fqn, kind, text, embedding FROM catalog_embedding
		WHERE customer_id = $1 AND model = $2`, customerID, e.model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []SearchHit
	for rows.Next() {
		var fqn, kind, text string
		var blob []byte
		if err := rows.Scan(&fqn, &kind, &text, &blob); err != nil {
			return nil, err
		}
		vec, err := UnpackFloat32(blob)
		if err != nil {
			continue
		}
		hits = append(hits, SearchHit{FQN: fqn, Kind: kind, Text: text, Score: cosine(qvec, vec)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits, nil
}

// PackFloat32 encodes a []float32 as little-endian bytes (4 bytes per float).
func PackFloat32(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// UnpackFloat32 decodes the inverse of PackFloat32.
func UnpackFloat32(b []byte) ([]float32, error) {
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("embedding blob length %d not multiple of 4", len(b))
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out, nil
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
