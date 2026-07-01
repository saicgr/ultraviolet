package lineage

import (
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// maxColDepth guards the recursive AST walk + scope resolution against
// pathological nesting (deep subqueries / expression trees). Beyond it we stop
// descending — a bounded under-count, never a crash.
const maxColDepth = 64

const (
	confExact     = "exact"
	confAmbiguous = "ambiguous"
)

// colRef is a resolved upstream column ("schema.table.col") with the confidence
// of that binding. Ambiguous = the column name matched >1 candidate relation
// and we over-approximated rather than guess one (wrong lineage is worse than a
// flagged-ambiguous edge for a governance buyer).
type colRef struct {
	fqn  string
	conf string
}

// projMap maps a (lowercased) output column name to the upstream base-table
// columns that feed it. It is the "shape" a SELECT/CTE/subselect exposes to an
// enclosing scope, enabling transitive resolution through derived tables.
type projMap map[string][]colRef

// resolveColumns produces column-level edges (upstream col → downstream col) for
// a SELECT body writing into `downstream` (an INSERT/CTAS/VIEW target). It is
// best-effort: a SELECT * or an unresolvable reference yields no column edge
// (the table-level edge still covers it); an ambiguous reference is emitted to
// every candidate with confidence="ambiguous".
func resolveColumns(query *pg_query.Node, downstream, hash string) []Edge {
	sel := query.GetSelectStmt()
	if sel == nil {
		return nil
	}
	proj := projection(sel, map[string]projMap{}, 0)
	var edges []Edge
	for outCol, refs := range proj {
		down := downstream + "." + outCol
		for _, r := range refs {
			if r.fqn == "" {
				continue
			}
			edges = append(edges, Edge{
				UpstreamFQN:   r.fqn,
				DownstreamFQN: down,
				EdgeType:      "column",
				Confidence:    r.conf,
				QueryHash:     hash,
			})
		}
	}
	return edges
}

// projection resolves a SELECT to its output-column → upstream-base-column map.
// outerCTEs carries WITH definitions visible from an enclosing scope.
func projection(sel *pg_query.SelectStmt, outerCTEs map[string]projMap, depth int) projMap {
	if sel == nil || depth > maxColDepth {
		return projMap{}
	}
	// Set operation (UNION/INTERSECT/EXCEPT): merge both arms by output name.
	if sel.Larg != nil || sel.Rarg != nil {
		left := projection(sel.Larg, outerCTEs, depth+1)
		for k, v := range projection(sel.Rarg, outerCTEs, depth+1) {
			left[k] = append(left[k], v...)
		}
		return left
	}
	sc := buildScope(sel, outerCTEs, depth)
	proj := projMap{}
	for _, t := range sel.TargetList {
		rt := t.GetResTarget()
		if rt == nil {
			continue
		}
		name, ok := targetName(rt)
		if !ok {
			continue // unnamed expression — no stable downstream column name
		}
		var refs []colRef
		for _, cr := range columnRefsIn(rt.Val, 0) {
			refs = append(refs, sc.resolveColumn(cr)...)
		}
		key := lower(name)
		proj[key] = append(proj[key], refs...)
	}
	return proj
}

// scope is one SELECT level's symbol table: base-table aliases plus derived
// (subselect/CTE) sources whose columns resolve transitively to base columns.
type scope struct {
	tables  map[string]string  // alias → "schema.table"
	derived map[string]projMap // alias → output-column map
	bases   []string           // all base table FQNs (for unqualified ambiguity)
}

func buildScope(sel *pg_query.SelectStmt, outerCTEs map[string]projMap, depth int) *scope {
	sc := &scope{tables: map[string]string{}, derived: map[string]projMap{}}
	// CTEs visible here: the inherited set plus this level's WITH. Each CTE sees
	// only those defined before it (non-recursive visibility); a recursive
	// self-reference therefore resolves to nothing for the recursive arm
	// (anchor-arm lineage only) rather than looping forever.
	ctes := map[string]projMap{}
	for k, v := range outerCTEs {
		ctes[k] = v
	}
	if sel.WithClause != nil {
		for _, cnode := range sel.WithClause.Ctes {
			cte := cnode.GetCommonTableExpr()
			if cte == nil || cte.Ctequery == nil {
				continue
			}
			if cq := cte.Ctequery.GetSelectStmt(); cq != nil {
				ctes[lower(cte.Ctename)] = projection(cq, ctes, depth+1)
			}
		}
	}
	for _, f := range sel.FromClause {
		sc.addFrom(f, ctes, depth)
	}
	return sc
}

func (sc *scope) addFrom(n *pg_query.Node, ctes map[string]projMap, depth int) {
	if n == nil {
		return
	}
	if rv := n.GetRangeVar(); rv != nil {
		alias := rv.Relname
		if rv.Alias != nil && rv.Alias.Aliasname != "" {
			alias = rv.Alias.Aliasname
		}
		// A FROM entry naming a CTE binds to that CTE's projection, not a table.
		if cproj, ok := ctes[lower(rv.Relname)]; ok && rv.Schemaname == "" {
			sc.derived[lower(alias)] = cproj
			return
		}
		fqn := rv.Relname
		if rv.Schemaname != "" {
			fqn = rv.Schemaname + "." + rv.Relname
		}
		sc.tables[lower(alias)] = fqn
		sc.bases = append(sc.bases, fqn)
		return
	}
	if rs := n.GetRangeSubselect(); rs != nil {
		if rs.Alias != nil && rs.Alias.Aliasname != "" {
			if sub := rs.Subquery.GetSelectStmt(); sub != nil {
				sc.derived[lower(rs.Alias.Aliasname)] = projection(sub, ctes, depth+1)
			}
		}
		return
	}
	if je := n.GetJoinExpr(); je != nil {
		sc.addFrom(je.Larg, ctes, depth)
		sc.addFrom(je.Rarg, ctes, depth)
	}
}

// resolveColumn maps one ColumnRef to its upstream base column(s).
func (sc *scope) resolveColumn(cr *pg_query.ColumnRef) []colRef {
	parts, star := fieldNames(cr.Fields)
	if star || len(parts) == 0 {
		return nil // SELECT * / a.* — covered by the table edge, not column edges
	}
	col := parts[len(parts)-1]
	if len(parts) >= 2 { // qualified: alias.col
		qual := lower(parts[len(parts)-2])
		if fqn, ok := sc.tables[qual]; ok {
			return []colRef{{fqn: fqn + "." + col, conf: confExact}}
		}
		if dp, ok := sc.derived[qual]; ok {
			return dp[lower(col)] // transitive through the derived source
		}
		return nil // unknown qualifier
	}
	// Unqualified column.
	if len(sc.bases) == 1 && len(sc.derived) == 0 {
		return []colRef{{fqn: sc.bases[0] + "." + col, conf: confExact}}
	}
	if len(sc.bases) == 0 && len(sc.derived) == 1 {
		for _, dp := range sc.derived {
			return dp[lower(col)]
		}
	}
	// Ambiguous across ≥2 sources: over-approximate to all, mark ambiguous.
	var out []colRef
	for _, b := range sc.bases {
		out = append(out, colRef{fqn: b + "." + col, conf: confAmbiguous})
	}
	for _, dp := range sc.derived {
		for _, r := range dp[lower(col)] {
			out = append(out, colRef{fqn: r.fqn, conf: confAmbiguous})
		}
	}
	return out
}

// columnRefsIn walks an expression node collecting every ColumnRef leaf, so a
// derived column over N inputs (a+b, COALESCE(x,y), CASE…) yields N edges.
func columnRefsIn(n *pg_query.Node, depth int) []*pg_query.ColumnRef {
	if n == nil || depth > maxColDepth {
		return nil
	}
	if cr := n.GetColumnRef(); cr != nil {
		return []*pg_query.ColumnRef{cr}
	}
	var out []*pg_query.ColumnRef
	add := func(nodes ...*pg_query.Node) {
		for _, x := range nodes {
			out = append(out, columnRefsIn(x, depth+1)...)
		}
	}
	switch {
	case n.GetAExpr() != nil:
		add(n.GetAExpr().Lexpr, n.GetAExpr().Rexpr)
	case n.GetFuncCall() != nil:
		add(n.GetFuncCall().Args...)
	case n.GetCoalesceExpr() != nil:
		add(n.GetCoalesceExpr().Args...)
	case n.GetBoolExpr() != nil:
		add(n.GetBoolExpr().Args...)
	case n.GetCaseExpr() != nil:
		c := n.GetCaseExpr()
		add(c.Arg, c.Defresult)
		for _, w := range c.Args {
			if cw := w.GetCaseWhen(); cw != nil {
				add(cw.Expr, cw.Result)
			}
		}
	case n.GetTypeCast() != nil:
		add(n.GetTypeCast().Arg)
	case n.GetList() != nil:
		add(n.GetList().Items...)
	case n.GetSubLink() != nil:
		// Scalar/correlated subquery used as a value: descend its projected
		// expressions for column refs (best-effort, depth-bounded).
		if s := n.GetSubLink().Subselect.GetSelectStmt(); s != nil {
			for _, t := range s.TargetList {
				if rt := t.GetResTarget(); rt != nil {
					add(rt.Val)
				}
			}
		}
	}
	return out
}

// targetName returns the output column name for a ResTarget: an explicit alias,
// else the bare column name of a plain ColumnRef. Unnamed expressions return
// ok=false (no stable downstream name to attach lineage to).
func targetName(rt *pg_query.ResTarget) (string, bool) {
	if rt.Name != "" {
		return rt.Name, true
	}
	if cr := rt.Val.GetColumnRef(); cr != nil {
		if parts, star := fieldNames(cr.Fields); !star && len(parts) > 0 {
			return parts[len(parts)-1], true
		}
	}
	return "", false
}

// fieldNames extracts the dotted identifier parts of a ColumnRef, reporting
// whether the reference is a star (`*` or `alias.*`).
func fieldNames(fields []*pg_query.Node) ([]string, bool) {
	var parts []string
	for _, f := range fields {
		if f.GetAStar() != nil {
			return parts, true
		}
		if s := f.GetString_(); s != nil {
			parts = append(parts, s.Sval)
		}
	}
	return parts, false
}

func lower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
