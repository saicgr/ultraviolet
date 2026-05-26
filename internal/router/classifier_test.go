package router

import "testing"

func TestClassify(t *testing.T) {
	tests := []struct {
		sql  string
		want QueryClass
	}{
		{"SELECT 1", ClassSelect},
		{"  select * from t", ClassSelect},
		{"WITH x AS (SELECT 1) SELECT * FROM x", ClassSelect},
		{"INSERT INTO t VALUES (1)", ClassDML},
		{"DELETE FROM t WHERE id = 1", ClassDML},
		{"CREATE TABLE t (id INT)", ClassDDL},
		{"ALTER TABLE t ADD COLUMN c INT", ClassDDL},
		{"SET search_path = public", ClassUtility},
		{"BEGIN", ClassUtility},
		{"-- comment\nSELECT 2", ClassSelect},
		{"/* block */ INSERT INTO t VALUES (1)", ClassDML},
		{"SELECT ai_generate('hi','gpt-4o-mini')", ClassAIGenerate},
		{"", ClassUnknown},
		{"FOO BAR", ClassUnknown},
	}
	for _, tt := range tests {
		got := Classify(tt.sql)
		if got != tt.want {
			t.Errorf("Classify(%q) = %s, want %s", tt.sql, got, tt.want)
		}
	}
}

func TestExtractTables_Scrape(t *testing.T) {
	refs := ExtractTables("SELECT * FROM analytics.events JOIN public.users u ON u.id = events.user_id")
	have := map[string]bool{}
	for _, r := range refs {
		have[r.Schema+"."+r.Table] = true
	}
	if !have["analytics.events"] {
		t.Errorf("missing analytics.events; got %+v", refs)
	}
	if !have["public.users"] {
		t.Errorf("missing public.users; got %+v", refs)
	}
}
