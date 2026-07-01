package srcanalysis

import (
	"path/filepath"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"

	"github.com/ultraviolet-dev/ultraviolet/internal/lineage"
)

// ModelFile is one source file from the repo: its path (used to derive the
// model/relation name) and its raw contents (possibly Jinja-templated SQL).
type ModelFile struct {
	Path    string
	Content string
}

// Result is the outcome of analyzing a set of source files: the derived edges
// (tagged origin=source_code) plus the files we could not parse, so the caller
// can surface "lineage incomplete: N models unparsed" rather than imply full
// coverage.
type Result struct {
	Edges    []lineage.Edge
	Unparsed []string
}

// Analyze derives source-code lineage for a set of dbt/SQL model files. Each
// model's SELECT body is treated as defining its named relation (CREATE TABLE
// <model> AS <body>) so the shared column extractor produces both table- and
// column-level edges, with ref()/source() giving cross-file model→model edges.
func Analyze(files []ModelFile) Result {
	ex := lineage.NewExtractor()
	var res Result
	for _, f := range files {
		if !isSQLModel(f.Path) {
			continue
		}
		model := modelName(f.Path)
		if model == "" {
			continue
		}
		rendered, _ := renderJinja(f.Content)
		wrapped := wrapAsModel(model, rendered)
		if _, err := pg_query.Parse(wrapped); err != nil {
			// Couldn't render to parseable SQL — report it, don't fabricate.
			res.Unparsed = append(res.Unparsed, f.Path)
			continue
		}
		for _, e := range ex.Extract(wrapped) {
			e.Origin = "source_code"
			res.Edges = append(res.Edges, e)
		}
	}
	return res
}

// wrapAsModel turns a bare model SELECT into a CTAS so the extractor has a
// downstream node. If the body already is a CREATE/INSERT/etc. statement, it's
// passed through unchanged.
func wrapAsModel(model, body string) string {
	trimmed := strings.TrimSpace(body)
	upper := strings.ToUpper(trimmed)
	switch {
	case strings.HasPrefix(upper, "WITH"), strings.HasPrefix(upper, "SELECT"), strings.HasPrefix(upper, "("):
		return "CREATE TABLE " + model + " AS " + trimmed
	default:
		return trimmed // already DDL/DML — analyze as-is
	}
}

// modelName derives the relation name a dbt model compiles to: the filename
// without its extension (dbt names the relation after the file).
func modelName(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func isSQLModel(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".sql"
}
