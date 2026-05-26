package ai

import (
	"regexp"
	"strconv"
	"strings"
)

// Path B handles >rowLimit row counts. Phase 1 leaves the SQL untouched and lets the
// DuckDB llm extension batch internally — DuckDB's scalar UDF execution is column-wise
// so it batches automatically. When OPENAI_API_KEY / ANTHROPIC_API_KEY is set, a future
// rewriter will replace `ai_generate(...)` with a UDF that hits the external provider
// in micro-batches of 50 rows.

var limitRE = regexp.MustCompile(`(?i)\blimit\s+(\d+)`)

// looksScalarOrLimited returns true when the SQL has no FROM, or has LIMIT N where N ≤ rowLimit.
func looksScalarOrLimited(sql string, rowLimit int) bool {
	upper := strings.ToUpper(sql)
	if !strings.Contains(upper, " FROM ") {
		return true
	}
	if m := limitRE.FindStringSubmatch(sql); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil && n <= rowLimit {
			return true
		}
	}
	return false
}
