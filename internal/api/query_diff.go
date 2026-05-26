package api

// Query history diff (an-6). Given two query_hash values for the same customer,
// fetch the two query_log entries and return a unified line-by-line diff of the
// normalized SQL plus per-query metadata. The query_log schema has no user_id
// column, so "ran_by" is reported as the api_key_id that submitted the query.

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type queryDiffMetadata struct {
	QueryHash     string    `json:"query_hash"`
	NormalizedSQL string    `json:"normalized_sql"`
	DurationMS    int       `json:"duration_ms"`
	BytesScanned  *int64    `json:"bytes_scanned,omitempty"`
	RouteDecision string    `json:"route_decision"`
	StartedAt     time.Time `json:"started_at"`
	RanBy         *string   `json:"ran_by,omitempty"` // api_key_id (no user_id column on query_log)
}

type queryDiffResponse struct {
	A    queryDiffMetadata `json:"a"`
	B    queryDiffMetadata `json:"b"`
	Diff string            `json:"diff"`
}

func (s *Server) queryHistoryDiff(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cidStr := q.Get("customer_id")
	hashA := q.Get("hash_a")
	hashB := q.Get("hash_b")
	if cidStr == "" || hashA == "" || hashB == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("customer_id, hash_a, hash_b required"))
		return
	}
	cid, err := uuid.Parse(cidStr)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid customer_id: %w", err))
		return
	}

	a, err := fetchQueryByHash(r, s, cid, hashA)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeErr(w, status, fmt.Errorf("hash_a: %w", err))
		return
	}
	b, err := fetchQueryByHash(r, s, cid, hashB)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		writeErr(w, status, fmt.Errorf("hash_b: %w", err))
		return
	}

	writeJSON(w, http.StatusOK, queryDiffResponse{
		A:    a,
		B:    b,
		Diff: unifiedDiff(a.NormalizedSQL, b.NormalizedSQL),
	})
}

func fetchQueryByHash(r *http.Request, s *Server, customerID uuid.UUID, hash string) (queryDiffMetadata, error) {
	var (
		m        queryDiffMetadata
		bytes    *int64
		apiKeyID *uuid.UUID
	)
	row := s.db.Pool().QueryRow(r.Context(),
		`SELECT query_hash, normalized_sql, duration_ms, bytes_scanned,
		        route_decision, started_at, api_key_id
		 FROM query_log
		 WHERE customer_id = $1 AND query_hash = $2
		 ORDER BY started_at DESC
		 LIMIT 1`, customerID, hash)
	if err := row.Scan(&m.QueryHash, &m.NormalizedSQL, &m.DurationMS, &bytes,
		&m.RouteDecision, &m.StartedAt, &apiKeyID); err != nil {
		return queryDiffMetadata{}, err
	}
	m.BytesScanned = bytes
	if apiKeyID != nil {
		s := apiKeyID.String()
		m.RanBy = &s
	}
	return m, nil
}

// unifiedDiff is a tiny line-by-line differ: lines unique to `a` are emitted with
// a leading "-", lines unique to `b` with "+", and shared lines (in order) with
// " ". Whole-line equality is sufficient for normalized SQL.
func unifiedDiff(a, b string) string {
	la := strings.Split(a, "\n")
	lb := strings.Split(b, "\n")
	var out strings.Builder
	i, j := 0, 0
	for i < len(la) && j < len(lb) {
		if la[i] == lb[j] {
			out.WriteString(" " + la[i] + "\n")
			i++
			j++
			continue
		}
		// Look ahead: does la[i] appear later in lb? then b inserted lines.
		if idx := indexOfFrom(lb, la[i], j+1); idx >= 0 {
			for ; j < idx; j++ {
				out.WriteString("+" + lb[j] + "\n")
			}
			continue
		}
		out.WriteString("-" + la[i] + "\n")
		i++
	}
	for ; i < len(la); i++ {
		out.WriteString("-" + la[i] + "\n")
	}
	for ; j < len(lb); j++ {
		out.WriteString("+" + lb[j] + "\n")
	}
	return out.String()
}

func indexOfFrom(lines []string, target string, start int) int {
	for k := start; k < len(lines); k++ {
		if lines[k] == target {
			return k
		}
	}
	return -1
}
