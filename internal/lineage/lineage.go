// Package lineage implements W1 of the BI-platform expansion: runtime,
// column-level lineage observed from every executed query. Static parsers
// (dbt-docs, Atlan) miss views/CTEs/subqueries — we don't, because we see the
// real SQL with bind parameters resolved.
package lineage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	pg_query "github.com/pganalyze/pg_query_go/v6"

	"github.com/ultraviolet-dev/ultraviolet/internal/router"
)

// Edge is a single (upstream → downstream) reference at table or column granularity.
type Edge struct {
	UpstreamFQN   string
	DownstreamFQN string
	EdgeType      string // "table" | "column"
	QueryHash     string
}

// Extractor turns a SQL string into a slice of edges. INSERT/UPDATE/CREATE TABLE AS
// produce a downstream node (the target); pure SELECTs produce no edges (the result
// set isn't a persistent node) but their FROM list still feeds anomaly + impact APIs.
type Extractor struct{}

func NewExtractor() *Extractor { return &Extractor{} }

func (e *Extractor) Extract(sql string) []Edge {
	hash := QueryHash(sql)
	tree, err := pg_query.Parse(sql)
	if err != nil {
		return nil
	}
	var edges []Edge
	for _, stmt := range tree.Stmts {
		edges = append(edges, edgesFromStmt(stmt.Stmt, hash)...)
	}
	return dedupeEdges(edges)
}

func edgesFromStmt(node *pg_query.Node, hash string) []Edge {
	if node == nil {
		return nil
	}
	if ins := node.GetInsertStmt(); ins != nil {
		downstream := fqn(ins.Relation.Schemaname, ins.Relation.Relname)
		return edgesFromSelect(ins.SelectStmt, downstream, hash)
	}
	if upd := node.GetUpdateStmt(); upd != nil {
		downstream := fqn(upd.Relation.Schemaname, upd.Relation.Relname)
		var out []Edge
		for _, n := range upd.FromClause {
			for _, t := range router.ExtractTables(stringifyNode(n)) {
				out = append(out, Edge{UpstreamFQN: fqn(t.Schema, t.Table), DownstreamFQN: downstream, EdgeType: "table", QueryHash: hash})
			}
		}
		return out
	}
	if cts := node.GetCreateTableAsStmt(); cts != nil && cts.Into != nil && cts.Into.Rel != nil {
		downstream := fqn(cts.Into.Rel.Schemaname, cts.Into.Rel.Relname)
		return edgesFromSelect(cts.Query, downstream, hash)
	}
	return nil
}

func edgesFromSelect(node *pg_query.Node, downstream, hash string) []Edge {
	tables := router.ExtractTables(stringifyNode(node))
	out := make([]Edge, 0, len(tables))
	for _, t := range tables {
		out = append(out, Edge{
			UpstreamFQN:   fqn(t.Schema, t.Table),
			DownstreamFQN: downstream,
			EdgeType:      "table",
			QueryHash:     hash,
		})
	}
	return out
}

// stringifyNode is a helper because router.ExtractTables wants a SQL string.
// We deparse via pg_query when possible; fall back to "" so the caller bails.
func stringifyNode(n *pg_query.Node) string {
	if n == nil {
		return ""
	}
	if s, err := pg_query.Deparse(&pg_query.ParseResult{
		Stmts: []*pg_query.RawStmt{{Stmt: n}},
	}); err == nil {
		return s
	}
	return ""
}

func fqn(schema, table string) string {
	schema = strings.TrimSpace(schema)
	table = strings.TrimSpace(table)
	if schema == "" {
		return table
	}
	return schema + "." + table
}

// QueryHash is a stable identifier for "the same query shape" — sha256 over the
// trimmed, lower-cased SQL. Distinct from the literal-stripped logger.NormalizeSQL
// hash; we use this purely for lineage cross-reference.
func QueryHash(sql string) string {
	h := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(sql))))
	return hex.EncodeToString(h[:8])
}

func dedupeEdges(in []Edge) []Edge {
	seen := map[Edge]struct{}{}
	out := in[:0]
	for _, e := range in {
		if e.UpstreamFQN == "" || e.DownstreamFQN == "" {
			continue
		}
		if _, ok := seen[e]; ok {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	return out
}

// Writer persists edges to lineage_edge with upsert semantics.
type Writer struct {
	pool *pgxpool.Pool
}

func NewWriter(pool *pgxpool.Pool) *Writer { return &Writer{pool: pool} }

func (w *Writer) Write(ctx context.Context, customerID uuid.UUID, edges []Edge) error {
	if len(edges) == 0 {
		return nil
	}
	now := time.Now()
	for _, e := range edges {
		_, err := w.pool.Exec(ctx, `
			INSERT INTO lineage_edge (customer_id, upstream_fqn, downstream_fqn, edge_type, query_hash, last_seen_at, seen_count)
			VALUES ($1,$2,$3,$4,$5,$6,1)
			ON CONFLICT (customer_id, upstream_fqn, downstream_fqn, edge_type)
			DO UPDATE SET last_seen_at = EXCLUDED.last_seen_at, seen_count = lineage_edge.seen_count + 1`,
			customerID, e.UpstreamFQN, e.DownstreamFQN, e.EdgeType, e.QueryHash, now)
		if err != nil {
			return err
		}
	}
	return nil
}

// Lookup returns upstream edges for an FQN (what does X depend on?).
func (w *Writer) Upstream(ctx context.Context, customerID uuid.UUID, fqn string) ([]Edge, error) {
	rows, err := w.pool.Query(ctx,
		`SELECT upstream_fqn, downstream_fqn, edge_type, query_hash
		 FROM lineage_edge WHERE customer_id = $1 AND downstream_fqn = $2`, customerID, fqn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Edge
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.UpstreamFQN, &e.DownstreamFQN, &e.EdgeType, &e.QueryHash); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (w *Writer) Downstream(ctx context.Context, customerID uuid.UUID, fqn string) ([]Edge, error) {
	rows, err := w.pool.Query(ctx,
		`SELECT upstream_fqn, downstream_fqn, edge_type, query_hash
		 FROM lineage_edge WHERE customer_id = $1 AND upstream_fqn = $2`, customerID, fqn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Edge
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.UpstreamFQN, &e.DownstreamFQN, &e.EdgeType, &e.QueryHash); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
