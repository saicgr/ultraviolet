package ai

import "testing"

func TestContainsAIGenerate(t *testing.T) {
	for _, sql := range []string{
		"SELECT ai_generate('hi','m')",
		"select AI_GENERATE(prompt, 'gpt')",
		"with x as (select ai_generate(p,m) from t) select * from x",
	} {
		if !ContainsAIGenerate(sql) {
			t.Errorf("ContainsAIGenerate(%q) = false, want true", sql)
		}
	}
	for _, sql := range []string{
		"SELECT 1",
		"SELECT generate_series(1,10)",
	} {
		if ContainsAIGenerate(sql) {
			t.Errorf("ContainsAIGenerate(%q) = true, want false", sql)
		}
	}
}

func TestIsPathA(t *testing.T) {
	tests := []struct {
		sql   string
		limit int
		want  bool
	}{
		{"SELECT ai_generate('hi','m')", 500, true},                   // no FROM
		{"SELECT ai_generate(t.x,'m') FROM t LIMIT 100", 500, true},   // limit ≤ 500
		{"SELECT ai_generate(t.x,'m') FROM t LIMIT 500", 500, true},   // limit == 500
		{"SELECT ai_generate(t.x,'m') FROM t LIMIT 5000", 500, false}, // limit > 500
		{"SELECT ai_generate(t.x,'m') FROM t", 500, false},            // no limit
	}
	for _, tt := range tests {
		got := IsPathA(tt.sql, tt.limit)
		if got != tt.want {
			t.Errorf("IsPathA(%q,%d) = %v, want %v", tt.sql, tt.limit, got, tt.want)
		}
	}
}
