package srcanalysis

import (
	"sort"
	"strings"
	"testing"
)

func edgeStrings(r Result) []string {
	var out []string
	for _, e := range r.Edges {
		out = append(out, e.EdgeType+":"+e.UpstreamFQN+"->"+e.DownstreamFQN+"@"+e.Origin)
	}
	sort.Strings(out)
	return out
}

func TestAnalyzeDbtRefAndColumns(t *testing.T) {
	files := []ModelFile{{
		Path:    "models/marts/orders.sql",
		Content: "{{ config(materialized='table') }}\nSELECT id AS order_id, amount AS total\nFROM {{ ref('stg_orders') }}\nWHERE amount > {{ var('floor') }}",
	}}
	got := edgeStrings(Analyze(files))
	want := []string{
		"column:stg_orders.amount->orders.total@source_code",
		"column:stg_orders.id->orders.order_id@source_code",
		"table:stg_orders->orders@source_code",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("edges mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestAnalyzeSourceFunction(t *testing.T) {
	files := []ModelFile{{
		Path:    "models/staging/stg_events.sql",
		Content: "SELECT event_id FROM {{ source('raw', 'events') }}",
	}}
	got := edgeStrings(Analyze(files))
	want := []string{
		"column:raw.events.event_id->stg_events.event_id@source_code",
		"table:raw.events->stg_events@source_code",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("edges mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestAnalyzeUnparsedReported(t *testing.T) {
	// A model that stays non-Postgres after rendering should be reported, not
	// silently dropped.
	files := []ModelFile{{
		Path:    "models/x.sql",
		Content: "SELECT * FROM t QUALIFY ROW_NUMBER() OVER (PARTITION BY a ORDER BY b) = 1",
	}}
	r := Analyze(files)
	if len(r.Unparsed) != 1 {
		t.Fatalf("expected 1 unparsed model, got %d (edges=%d)", len(r.Unparsed), len(r.Edges))
	}
}
