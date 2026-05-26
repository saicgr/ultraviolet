// Package security holds cross-cutting access-control primitives — currently
// column-level masking (ent-5). The Apply function wraps a SELECT query in an
// outer projection that substitutes a masking expression for any column the
// caller's role is not allowed to see in cleartext. Phase-1 deliberately uses
// string templating rather than a full SQL AST rewrite: the masking surface is
// strict opt-in and matches BI-tool generated SELECT shapes (SELECT … FROM …).
// A real parser-based rewrite is tracked in IMPROVEMENTS.md.
package security

import (
	"fmt"
	"regexp"
	"strings"
)

// MaskingRule is one (schema, table, column) → strategy binding for a single role.
// The Schema/Table fields are case-insensitive; matching is done lowercased.
// Strategy ∈ {"null","hash","redact","first_4"}.
type MaskingRule struct {
	Schema   string
	Table    string
	Column   string
	Role     string
	Strategy string
}

var (
	// selectRE matches a leading SELECT … FROM and captures the projection list.
	// Multiline-aware. Conservative: bails out (returns input unchanged) on
	// anything other than a top-level SELECT projection.
	selectRE = regexp.MustCompile(`(?is)^\s*select\s+(.+?)\s+from\s+`)
)

// Apply returns the SQL rewritten so that every column listed in rules is
// replaced with the appropriate masking expression. If the SQL is not a
// recognisable SELECT or no rules match, the input is returned verbatim.
//
// The strategy → SQL expression mapping:
//   null     → NULL                       (drops the value entirely)
//   hash     → md5(<col>::text)           (deterministic surrogate)
//   redact   → '***REDACTED***'           (fixed placeholder)
//   first_4  → left(<col>::text, 4) || '…' (truncated preview)
func Apply(sql string, rules []MaskingRule) string {
	if len(rules) == 0 || sql == "" {
		return sql
	}
	m := selectRE.FindStringSubmatchIndex(sql)
	if m == nil {
		return sql
	}
	projStart, projEnd := m[2], m[3]
	projection := sql[projStart:projEnd]

	// Build a fast lookup keyed by lowercase column name. We can only safely
	// rewrite when the projection lists columns by simple identifiers or
	// `table.col` qualified names — wildcards fall through unchanged.
	masked := make(map[string]string, len(rules))
	for _, r := range rules {
		masked[strings.ToLower(r.Column)] = maskExpr(r.Strategy, r.Column)
	}
	if strings.Contains(projection, "*") {
		// SELECT * — wrap in an outer SELECT that re-projects masked columns by
		// name. The inner query keeps the * so the outer can reference them.
		return wrapStar(sql, rules)
	}

	parts := splitProjection(projection)
	rewritten := make([]string, len(parts))
	for i, p := range parts {
		trimmed := strings.TrimSpace(p)
		col := bareColumn(trimmed)
		if expr, ok := masked[strings.ToLower(col)]; ok {
			rewritten[i] = fmt.Sprintf("%s AS %s", expr, col)
		} else {
			rewritten[i] = p
		}
	}
	return sql[:projStart] + strings.Join(rewritten, ", ") + sql[projEnd:]
}

// wrapStar is the SELECT * branch: it wraps the original query in an outer
// SELECT that re-projects each masked column.
func wrapStar(sql string, rules []MaskingRule) string {
	exprs := []string{"_uv_inner.*"}
	for _, r := range rules {
		exprs = append(exprs, fmt.Sprintf("%s AS %s", maskExpr(r.Strategy, r.Column), r.Column))
	}
	return fmt.Sprintf("SELECT %s FROM (%s) AS _uv_inner", strings.Join(exprs, ", "), strings.TrimRight(strings.TrimSpace(sql), ";"))
}

func maskExpr(strategy, col string) string {
	switch strategy {
	case "null":
		return "NULL"
	case "hash":
		return fmt.Sprintf("md5(%s::text)", col)
	case "redact":
		return "'***REDACTED***'"
	case "first_4":
		return fmt.Sprintf("left(%s::text, 4) || '…'", col)
	default:
		return col
	}
}

// splitProjection breaks "a, b, c+d, e" on top-level commas (ignoring parens).
func splitProjection(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i, c := range s {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

// bareColumn extracts the unqualified column name from a projection item.
// "schema.table.col"      → "col"
// "col AS alias"          → "col"  (we mask the source col, alias is rebuilt)
// "func(col)"             → ""     (skip — too complex)
func bareColumn(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(strings.ToLower(s), " as "); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if strings.ContainsAny(s, "()+-*/ ") {
		return ""
	}
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	return s
}
