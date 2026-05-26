// Package ai — cost_recommender.go: heuristic cost recommendations from query_log
// + synced_tables. No LLM call required; pure rule-based analysis.
//
// Heuristics:
//   - sync:        hit ≥100x in window AND not currently synced.
//                  savings ≈ avg(estimated_cost_usd) * hit_count * 0.7
//   - unsync:      synced but ≤5x hit count in window.
//                  savings ≈ sum(estimated_cost_usd) attributed to that table
//   - add_filter:  any query scanning >1TB whose normalized SQL has no WHERE.
//   - materialize: any query scanning >1TB whose normalized SQL contains WHERE
//                  (filter exists, scan is still huge → consider materialization).
package ai

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Recommendation kinds.
const (
	RecSync        = "sync"
	RecUnsync      = "unsync"
	RecAddFilter   = "add_filter"
	RecMaterialize = "materialize"
)

type Recommendation struct {
	Kind                string  `json:"kind"`
	Target              string  `json:"target"` // FQN or query_hash
	EstimatedSavingsUSD float64 `json:"estimated_savings_usd"`
	Rationale           string  `json:"rationale"`
}

type CostRecommender struct {
	pool *pgxpool.Pool
}

func NewCostRecommender(pool *pgxpool.Pool) *CostRecommender {
	return &CostRecommender{pool: pool}
}

// fqnRe matches dotted table identifiers like project.dataset.table or schema.table.
var fqnRe = regexp.MustCompile(`\b([a-zA-Z_][a-zA-Z0-9_-]*\.[a-zA-Z_][a-zA-Z0-9_-]*(?:\.[a-zA-Z_][a-zA-Z0-9_-]*)?)\b`)

// Recommendations computes all four heuristic categories in a single pass over query_log.
func (c *CostRecommender) Recommendations(ctx context.Context, customerID uuid.UUID, window time.Duration) ([]Recommendation, error) {
	if c.pool == nil {
		return nil, errors.New("cost_recommender: nil pool")
	}
	if window <= 0 {
		return nil, errors.New("cost_recommender: window must be positive")
	}
	since := time.Now().Add(-window)

	// Step 1: aggregate per-table hit counts + avg estimated cost over the window.
	type tableAgg struct {
		hits     int
		costSum  float64
		costMean float64
	}
	tableHits := map[string]*tableAgg{}

	rows, err := c.pool.Query(ctx, `
		SELECT normalized_sql, estimated_cost_usd, bytes_scanned, query_hash
		FROM query_log
		WHERE customer_id = $1 AND started_at >= $2`, customerID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var heavy []Recommendation
	seenHeavy := map[string]struct{}{}

	for rows.Next() {
		var sql, qhash string
		var estCost *float64
		var bytesScanned *int64
		if err := rows.Scan(&sql, &estCost, &bytesScanned, &qhash); err != nil {
			return nil, err
		}
		// Per-query heuristics: bytes_scanned > 1TB
		if bytesScanned != nil && *bytesScanned > 1<<40 {
			if _, dup := seenHeavy[qhash]; !dup {
				seenHeavy[qhash] = struct{}{}
				kind := RecMaterialize
				rationale := "Query scans >1TB despite filters — consider materializing or partitioning."
				if !hasWhereClause(sql) {
					kind = RecAddFilter
					rationale = "Query scans >1TB with no WHERE clause — add a filter to prune partitions."
				}
				var saving float64
				if estCost != nil {
					saving = *estCost * 0.5
				}
				heavy = append(heavy, Recommendation{
					Kind: kind, Target: qhash,
					EstimatedSavingsUSD: saving,
					Rationale:           rationale,
				})
			}
		}
		// Per-table aggregation
		for _, fqn := range extractFQNs(sql) {
			agg, ok := tableHits[fqn]
			if !ok {
				agg = &tableAgg{}
				tableHits[fqn] = agg
			}
			agg.hits++
			if estCost != nil {
				agg.costSum += *estCost
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, agg := range tableHits {
		if agg.hits > 0 {
			agg.costMean = agg.costSum / float64(agg.hits)
		}
	}

	// Step 2: pull synced_tables for this customer (via connections.customer_id).
	syncedSet, err := c.fetchSyncedFQNs(ctx, customerID)
	if err != nil {
		return nil, err
	}

	// Step 3: emit sync / unsync recommendations.
	var out []Recommendation
	for fqn, agg := range tableHits {
		_, isSynced := syncedSet[fqn]
		switch {
		case agg.hits >= 100 && !isSynced:
			out = append(out, Recommendation{
				Kind:                RecSync,
				Target:              fqn,
				EstimatedSavingsUSD: agg.costMean * float64(agg.hits) * 0.7,
				Rationale:           "Table hit ≥100x in window but not synced to Iceberg — sync would shift reads to DuckDB.",
			})
		case agg.hits <= 5 && isSynced:
			out = append(out, Recommendation{
				Kind:                RecUnsync,
				Target:              fqn,
				EstimatedSavingsUSD: agg.costSum,
				Rationale:           "Table synced but accessed ≤5x in window — unsync to reclaim Iceberg storage cost.",
			})
		}
	}
	out = append(out, heavy...)
	return out, nil
}

// fetchSyncedFQNs returns the set of `schema.table` strings currently synced for the customer.
func (c *CostRecommender) fetchSyncedFQNs(ctx context.Context, customerID uuid.UUID) (map[string]struct{}, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT st.schema_name, st.table_name
		FROM synced_tables st
		JOIN connections c ON c.id = st.connection_id
		WHERE c.customer_id = $1 AND st.state IN ('live','initial_load')`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var schema, table string
		if err := rows.Scan(&schema, &table); err != nil {
			return nil, err
		}
		out[schema+"."+table] = struct{}{}
		out[table] = struct{}{}
	}
	return out, rows.Err()
}

// extractFQNs returns dotted identifiers appearing in the SQL, lowercased.
func extractFQNs(sql string) []string {
	matches := fqnRe.FindAllStringSubmatch(sql, -1)
	seen := map[string]struct{}{}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		fqn := strings.ToLower(m[1])
		// Skip obvious SQL keywords producing dotted forms like a.b in joins; we
		// can't fully parse, but reserved single-letter aliases are rare in
		// normalized SQL stored by the proxy.
		if _, dup := seen[fqn]; dup {
			continue
		}
		seen[fqn] = struct{}{}
		out = append(out, fqn)
	}
	return out
}

func hasWhereClause(sql string) bool {
	return strings.Contains(strings.ToLower(sql), " where ")
}
