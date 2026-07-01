// Package srcanalysis derives column-level lineage from repository source — dbt
// models and raw .sql files — as opposed to the runtime path that observes
// executed queries. Both feed the same lineage_edge table (distinguished by
// origin='source_code') via the shared internal/lineage extractor, so there is
// one column-resolution implementation, not two.
//
// Limitations (documented honestly, surfaced as `Unparsed` to the caller):
//   - No dbt macro expansion, no var()/env resolution, no is_incremental()
//     branch evaluation — both arms of any templated branch are analyzed as
//     written.
//   - Jinja is preprocessed, not compiled: {{ ref() }}/{{ source() }} are
//     resolved to relation names; other {{ ... }} expressions collapse to a
//     placeholder so the SQL parses. A model that still fails to parse is
//     reported in Unparsed, never silently dropped.
package srcanalysis

import "regexp"

var (
	// {{ source('schema','table') }} → schema.table
	sourceRe = regexp.MustCompile(`(?s)\{\{\s*source\(\s*['"]([^'"]+)['"]\s*,\s*['"]([^'"]+)['"]\s*\)\s*\}\}`)
	// {{ ref('model') }} or {{ ref('pkg','model') }} → model
	refRe = regexp.MustCompile(`(?s)\{\{\s*ref\(\s*['"]([^'"]+)['"]\s*(?:,\s*['"]([^'"]+)['"]\s*)?\)\s*\}\}`)
	// {{ config(...) }} is a directive that emits no SQL → removed entirely
	// (placeholdering it would break a model whose first line is config()).
	configRe = regexp.MustCompile(`(?s)\{\{\s*config\(.*?\)\s*\}\}`)
	// {% ... %} statement/control blocks (config, if, for, set) → removed.
	blockRe = regexp.MustCompile(`(?s)\{%.*?%\}`)
	// Any remaining {{ ... }} expression (var, this, …) → a parseable
	// placeholder so surrounding SQL still parses.
	exprRe = regexp.MustCompile(`(?s)\{\{.*?\}\}`)
)

// renderJinja preprocesses dbt Jinja into parseable SQL. It does NOT compile
// macros; it resolves the relation-defining tags (ref/source) and neutralizes
// the rest. Returns the rendered SQL and whether any Jinja was present.
func renderJinja(sql string) (string, bool) {
	had := false
	out := sourceRe.ReplaceAllString(sql, "$1.$2")
	if out != sql {
		had = true
	}
	prev := out
	out = refRe.ReplaceAllStringFunc(out, func(m string) string {
		sub := refRe.FindStringSubmatch(m)
		if sub[2] != "" { // two-arg ref('pkg','model')
			return sub[2]
		}
		return sub[1]
	})
	if out != prev {
		had = true
	}
	prev = out
	out = configRe.ReplaceAllString(out, " ")
	if out != prev {
		had = true
	}
	prev = out
	out = blockRe.ReplaceAllString(out, " ")
	if out != prev {
		had = true
	}
	prev = out
	out = exprRe.ReplaceAllString(out, "null")
	if out != prev {
		had = true
	}
	return out, had
}
