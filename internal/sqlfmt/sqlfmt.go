// Package sqlfmt is a thin SQL formatter + linter used by the workbench.
//
// Format goes through pg_query_go.Deparse to normalize whitespace and
// canonical keyword casing; a regex pass uppercases the keywords the deparser
// leaves lowercase ("group by", "order by", etc.) so editor output is
// consistent. Lint is a tiny rule-set covering the three issues that come up
// most often in customer support — SELECT *, missing WHERE on large tables,
// and ORDER BY without LIMIT.
package sqlfmt

import (
	"fmt"
	"regexp"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// Issue is a single lint finding.
type Issue struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// keywordRe matches SQL keywords + simple multi-word forms as whole tokens.
// Order matters: multi-word forms first so `GROUP BY` wins over `BY`.
var keywordRe = regexp.MustCompile(`(?i)\b(group by|order by|select|from|where|join|on|and|or|limit|having)\b`)

// Format normalizes the SQL. pg_query handles whitespace + identifier quoting;
// we layer a regex pass on top to uppercase keywords consistently.
func Format(sql string) (string, error) {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return "", fmt.Errorf("empty sql")
	}
	tree, err := pg_query.Parse(sql)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	out, err := pg_query.Deparse(tree)
	if err != nil {
		return "", fmt.Errorf("deparse: %w", err)
	}
	out = keywordRe.ReplaceAllStringFunc(out, strings.ToUpper)
	return out, nil
}

var (
	selectStarRe = regexp.MustCompile(`(?i)select\s+\*`)
	whereRe      = regexp.MustCompile(`(?i)\bwhere\b`)
	orderByRe    = regexp.MustCompile(`(?i)\border\s+by\b`)
	limitRe      = regexp.MustCompile(`(?i)\blimit\b`)
	fromRe       = regexp.MustCompile(`(?i)\bfrom\b`)
)

// Lint returns a list of issues. Best-effort string-level checks; AST-based
// rules can layer on top later without breaking the public shape.
func Lint(sql string) []Issue {
	out := []Issue{}
	if strings.TrimSpace(sql) == "" {
		return out
	}
	if selectStarRe.MatchString(sql) {
		out = append(out, Issue{
			Severity: "warn",
			Code:     "SELECT_STAR",
			Message:  "Avoid SELECT * — list explicit columns to keep schemas stable.",
		})
	}
	if fromRe.MatchString(sql) && !whereRe.MatchString(sql) {
		out = append(out, Issue{
			Severity: "warn",
			Code:     "MISSING_WHERE",
			Message:  "FROM without WHERE — full-table scan on a large table can be very expensive.",
		})
	}
	if orderByRe.MatchString(sql) && !limitRe.MatchString(sql) {
		out = append(out, Issue{
			Severity: "info",
			Code:     "ORDER_BY_NO_LIMIT",
			Message:  "ORDER BY without LIMIT forces a full sort — add LIMIT if you only need a prefix.",
		})
	}
	return out
}
