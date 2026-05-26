// Package router classifies SQL and decides DuckDB vs warehouse vs hybrid.
// Phase 1: classify + log; full decision logic in decision.go.
package router

import (
	"strings"
)

// QueryClass is the coarse SQL category.
type QueryClass int

const (
	ClassUnknown QueryClass = iota
	ClassSelect
	ClassDDL
	ClassDML
	ClassUtility
	ClassAIGenerate
)

func (c QueryClass) String() string {
	switch c {
	case ClassSelect:
		return "select"
	case ClassDDL:
		return "ddl"
	case ClassDML:
		return "dml"
	case ClassUtility:
		return "utility"
	case ClassAIGenerate:
		return "ai_generate"
	default:
		return "unknown"
	}
}

// Classify returns the coarse class for a SQL string.
// Phase 1 uses prefix-only classification — pg_query_go-based AST classification is
// done in TableExtract for table references; classification is intentionally fast.
func Classify(sql string) QueryClass {
	s := strings.TrimSpace(sql)
	if s == "" {
		return ClassUnknown
	}
	// Strip leading SQL comments.
	for {
		switch {
		case strings.HasPrefix(s, "--"):
			if i := strings.IndexByte(s, '\n'); i >= 0 {
				s = strings.TrimSpace(s[i+1:])
				continue
			}
			return ClassUnknown
		case strings.HasPrefix(s, "/*"):
			if i := strings.Index(s, "*/"); i >= 0 {
				s = strings.TrimSpace(s[i+2:])
				continue
			}
			return ClassUnknown
		}
		break
	}
	upper := strings.ToUpper(s)
	if strings.Contains(upper, "AI_GENERATE(") {
		return ClassAIGenerate
	}
	first := strings.Fields(upper)[0]
	switch first {
	case "SELECT", "WITH", "TABLE", "VALUES":
		return ClassSelect
	case "INSERT", "UPDATE", "DELETE", "MERGE", "TRUNCATE":
		return ClassDML
	case "CREATE", "DROP", "ALTER", "GRANT", "REVOKE", "COMMENT":
		return ClassDDL
	case "SET", "SHOW", "BEGIN", "COMMIT", "ROLLBACK", "RESET", "DISCARD",
		"DECLARE", "FETCH", "MOVE", "CLOSE", "PREPARE", "DEALLOCATE", "EXPLAIN":
		return ClassUtility
	default:
		return ClassUnknown
	}
}
