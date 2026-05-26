// Phase 6 Wave 3 go-3: privacy preview.
//
// POST /api/v1/workbench/privacy-preview  body {customer_id, sql}
//
// Parses the SQL with pg_query_go, walks every RangeVar (table) + ColumnRef
// (column), and intersects the referenced (table_fqn, column_name) pairs
// against pii_tag + column_mask to surface:
//
//   pii_columns_touched: which columns the query is about to read are tagged PII.
//   would_be_masked:    which columns will be rewritten by a masking rule.
//
// The frontend renders a yellow banner before the workbench actually issues
// the query so the analyst gets a chance to bail.

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

type privacyPreviewRequest struct {
	CustomerID string `json:"customer_id"`
	SQL        string `json:"sql"`
}

type piiColumnTouched struct {
	FQN    string `json:"fqn"`
	Column string `json:"column"`
	Kind   string `json:"kind"`
}

type maskedColumnPreview struct {
	Column   string `json:"column"`
	Strategy string `json:"strategy"`
}

func (s *Server) privacyPreview(w http.ResponseWriter, r *http.Request) {
	var body privacyPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.CustomerID == "" || body.SQL == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("customer_id and sql required"))
		return
	}
	cid, err := uuid.Parse(body.CustomerID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid customer_id: %w", err))
		return
	}

	tables, columns, err := extractRefs(body.SQL)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("parse sql: %w", err))
		return
	}

	ctx := r.Context()
	touched := []piiColumnTouched{}
	if len(tables) > 0 {
		rows, err := s.db.Pool().Query(ctx,
			`SELECT fqn, column_name, kind FROM pii_tag
			 WHERE customer_id = $1 AND fqn = ANY($2)`,
			cid, tables)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		colSet := setOf(columns)
		for rows.Next() {
			var t piiColumnTouched
			if err := rows.Scan(&t.FQN, &t.Column, &t.Kind); err != nil {
				rows.Close()
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			// Restrict to columns actually referenced (or *).
			if len(colSet) == 0 || colSet["*"] || colSet[strings.ToLower(t.Column)] {
				touched = append(touched, t)
			}
		}
		rows.Close()
	}

	masked := []maskedColumnPreview{}
	if len(columns) > 0 {
		rows, err := s.db.Pool().Query(ctx,
			`SELECT column_name, strategy FROM column_mask
			 WHERE customer_id = $1 AND column_name = ANY($2)`,
			cid, columns)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		seen := map[string]bool{}
		for rows.Next() {
			var m maskedColumnPreview
			if err := rows.Scan(&m.Column, &m.Strategy); err != nil {
				rows.Close()
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			key := m.Column + "|" + m.Strategy
			if seen[key] {
				continue
			}
			seen[key] = true
			masked = append(masked, m)
		}
		rows.Close()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"pii_columns_touched": touched,
		"would_be_masked":     masked,
	})
}

// extractRefs returns (tableFQNs, columnNames) referenced by sql. ColumnNames
// are lowercased and de-duplicated. A "*" sentinel is included if SELECT * is
// present, so the caller can decide to match all PII tags on the table.
func extractRefs(sql string) ([]string, []string, error) {
	tree, err := pg_query.Parse(sql)
	if err != nil {
		return nil, nil, err
	}
	tableSet := map[string]struct{}{}
	colSet := map[string]struct{}{}
	for _, raw := range tree.Stmts {
		walk(raw.Stmt, tableSet, colSet)
	}
	return keys(tableSet), keys(colSet), nil
}

func walk(n *pg_query.Node, tables, cols map[string]struct{}) {
	if n == nil {
		return
	}
	if rv := n.GetRangeVar(); rv != nil {
		fqn := rv.Relname
		if rv.Schemaname != "" {
			fqn = rv.Schemaname + "." + rv.Relname
		}
		tables[fqn] = struct{}{}
	}
	if cr := n.GetColumnRef(); cr != nil {
		for _, f := range cr.Fields {
			if s := f.GetString_(); s != nil {
				cols[strings.ToLower(s.Sval)] = struct{}{}
			}
			if f.GetAStar() != nil {
				cols["*"] = struct{}{}
			}
		}
	}
	if sel := n.GetSelectStmt(); sel != nil {
		for _, t := range sel.FromClause {
			walk(t, tables, cols)
		}
		for _, t := range sel.TargetList {
			if rt := t.GetResTarget(); rt != nil {
				walk(rt.Val, tables, cols)
			}
		}
		walk(sel.WhereClause, tables, cols)
	}
	if je := n.GetJoinExpr(); je != nil {
		walk(je.Larg, tables, cols)
		walk(je.Rarg, tables, cols)
	}
}

func setOf(xs []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[strings.ToLower(x)] = true
	}
	return m
}

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
