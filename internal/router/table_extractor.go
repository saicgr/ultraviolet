package router

import (
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// TableRef = (schema, table). Schema may be "" if unspecified.
type TableRef struct {
	Schema string
	Table  string
}

// ExtractTables returns every relation referenced in the SQL. Phase 1 uses regex
// scraping for portability across BQ / Snowflake dialects (pg_query rejects many
// warehouse-specific constructs). The AST helper exists for Phase 2 once a
// dialect-aware parser is wired.
func ExtractTables(sql string) []TableRef {
	if refs := extractViaScrape(sql); len(refs) > 0 {
		return refs
	}
	if refs, err := extractViaAST(sql); err == nil {
		return refs
	}
	return nil
}

// extractViaAST is the strict pg_query_go fallback used when scrape returns no
// matches. It uses Parse + protojson round-trip to reach RangeVar nodes regardless
// of where they appear in the tree (top-level FROM, CTE bodies, subqueries, set
// ops). This replaces the earlier interface-cast trick which never matched the
// real generated types.
func extractViaAST(sql string) ([]TableRef, error) {
	tree, err := pg_query.Parse(sql)
	if err != nil {
		return nil, err
	}
	var out []TableRef
	for _, stmt := range tree.Stmts {
		walkProto(stmt.Stmt, &out)
	}
	return dedupeTables(out), nil
}

// walkProto descends a pg_query_go protobuf node, picking up every RangeVar.
// Uses the protoreflect-free oneof accessors directly to avoid an import.
func walkProto(node *pg_query.Node, out *[]TableRef) {
	if node == nil {
		return
	}
	if rv := node.GetRangeVar(); rv != nil && rv.Relname != "" {
		*out = append(*out, TableRef{Schema: rv.Schemaname, Table: rv.Relname})
	}
	// SelectStmt covers FROM, target lists, set ops, and CTE bodies.
	if sel := node.GetSelectStmt(); sel != nil {
		for _, n := range sel.FromClause {
			walkProto(n, out)
		}
		if sel.WhereClause != nil {
			walkProto(sel.WhereClause, out)
		}
		if sel.WithClause != nil {
			for _, cte := range sel.WithClause.Ctes {
				walkProto(cte, out)
			}
		}
		if sel.Larg != nil {
			walkProto(&pg_query.Node{Node: &pg_query.Node_SelectStmt{SelectStmt: sel.Larg}}, out)
		}
		if sel.Rarg != nil {
			walkProto(&pg_query.Node{Node: &pg_query.Node_SelectStmt{SelectStmt: sel.Rarg}}, out)
		}
	}
	if jx := node.GetJoinExpr(); jx != nil {
		walkProto(jx.Larg, out)
		walkProto(jx.Rarg, out)
	}
	if sub := node.GetRangeSubselect(); sub != nil {
		walkProto(sub.Subquery, out)
	}
	if cte := node.GetCommonTableExpr(); cte != nil {
		walkProto(cte.Ctequery, out)
	}
}

// extractViaScrape catches `FROM x.y`, `JOIN x.y`, `UPDATE x.y`, `INTO x.y` patterns.
// maxScrapeIterations is a defense-in-depth cap so adversarial SQL with millions of
// `FROM` tokens cannot pin a goroutine.
const maxScrapeIterations = 4096

func extractViaScrape(sql string) []TableRef {
	upper := strings.ToUpper(sql)
	keywords := []string{" FROM ", " JOIN ", " INTO ", " UPDATE ", " TABLE "}
	var out []TableRef
	iter := 0
	for _, kw := range keywords {
		idx := 0
		for {
			if iter >= maxScrapeIterations {
				return dedupeTables(out)
			}
			iter++
			i := strings.Index(upper[idx:], kw)
			if i < 0 {
				break
			}
			pos := idx + i + len(kw)
			rest := strings.TrimLeft(sql[pos:], " \t\n\r(")
			end := indexAnyDelim(rest)
			ident := rest[:end]
			ident = strings.Trim(ident, "\"`'")
			if ident == "" {
				idx = pos
				continue
			}
			parts := strings.Split(ident, ".")
			switch len(parts) {
			case 2:
				out = append(out, TableRef{Schema: parts[0], Table: parts[1]})
			case 3:
				out = append(out, TableRef{Schema: parts[1], Table: parts[2]}) // project.schema.table → drop project
			default:
				out = append(out, TableRef{Table: ident})
			}
			idx = pos + len(ident)
		}
	}
	return dedupeTables(out)
}

func indexAnyDelim(s string) int {
	for i, c := range s {
		switch c {
		case ' ', '\t', '\n', '\r', ',', ')', ';', '(':
			return i
		}
	}
	return len(s)
}

func dedupeTables(in []TableRef) []TableRef {
	seen := map[TableRef]struct{}{}
	out := in[:0]
	for _, t := range in {
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}
