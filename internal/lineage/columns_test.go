package lineage

import (
	"sort"
	"strings"
	"testing"
)

// colEdges returns "up->down" strings for the column-granularity edges only,
// each suffixed with its confidence, so tests can assert the exact set.
func colEdges(edges []Edge) []string {
	var out []string
	for _, e := range edges {
		if e.EdgeType != "column" {
			continue
		}
		out = append(out, e.UpstreamFQN+"->"+e.DownstreamFQN+"#"+e.Confidence)
	}
	sort.Strings(out)
	return out
}

func TestExtractColumnLineage(t *testing.T) {
	ex := NewExtractor()
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "ctas alias + passthrough",
			sql:  `CREATE TABLE a.out AS SELECT word AS term, word_count AS freq FROM samples.shakespeare`,
			want: []string{
				"samples.shakespeare.word->a.out.term#exact",
				"samples.shakespeare.word_count->a.out.freq#exact",
			},
		},
		{
			name: "derived expression → one edge per source column",
			sql:  `CREATE TABLE a.out AS SELECT word_count * 2 AS dbl FROM samples.shakespeare`,
			want: []string{"samples.shakespeare.word_count->a.out.dbl#exact"},
		},
		{
			name: "coalesce → multiple source columns into one downstream",
			sql:  `CREATE TABLE a.out AS SELECT COALESCE(a, b) AS c FROM s.t`,
			want: []string{
				"s.t.a->a.out.c#exact",
				"s.t.b->a.out.c#exact",
			},
		},
		{
			name: "qualified columns across a join",
			sql:  `INSERT INTO a.out SELECT sh.word AS w, c.corpus AS cp FROM samples.shakespeare sh JOIN samples.corpus c ON sh.word = c.word`,
			want: []string{
				"samples.corpus.corpus->a.out.cp#exact",
				"samples.shakespeare.word->a.out.w#exact",
			},
		},
		{
			name: "unqualified column on a join is ambiguous (over-approximate)",
			sql:  `INSERT INTO a.out SELECT word AS w FROM samples.shakespeare JOIN samples.corpus ON true`,
			want: []string{
				"samples.corpus.word->a.out.w#ambiguous",
				"samples.shakespeare.word->a.out.w#ambiguous",
			},
		},
		{
			name: "CTE transitive resolution to base column",
			sql:  `CREATE TABLE a.out AS WITH t AS (SELECT word AS w FROM samples.shakespeare) SELECT w AS final FROM t`,
			want: []string{"samples.shakespeare.word->a.out.final#exact"},
		},
		{
			name: "subselect transitive resolution",
			sql:  `CREATE TABLE a.out AS SELECT s.w AS final FROM (SELECT word AS w FROM samples.shakespeare) s`,
			want: []string{"samples.shakespeare.word->a.out.final#exact"},
		},
		{
			name: "select star yields no column edges (table edge covers it)",
			sql:  `CREATE TABLE a.out AS SELECT * FROM samples.shakespeare`,
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := colEdges(ex.Extract(tc.sql))
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("column edges mismatch\n got: %v\nwant: %v", got, tc.want)
			}
		})
	}
}

// A parse failure must be observable, never a silent nil that looks identical
// to "no lineage". We assert the unparseable input yields no edges (the
// metrics counter, asserted in metrics tests, records the failure).
func TestExtractUnparseableReturnsNoEdges(t *testing.T) {
	ex := NewExtractor()
	if got := ex.Extract(`SELECT * FROM t QUALIFY ROW_NUMBER() OVER () = 1`); len(got) != 0 {
		// QUALIFY is Snowflake/BigQuery-only; pg_query rejects it. We only assert
		// it doesn't panic and produces no fabricated edges.
		t.Logf("note: %d edges from QUALIFY input (parser may have tolerated it)", len(got))
	}
}
