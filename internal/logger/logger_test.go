package logger

import "testing"

func TestNormalizeSQL(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"SELECT 1", "SELECT 1"},
		{"SELECT * FROM t WHERE name = 'alice'", "SELECT * FROM t WHERE name = ?"},
		{"INSERT INTO t VALUES ('a','b','c')", "INSERT INTO t VALUES (?,?,?)"},
	}
	for _, tt := range tests {
		got := NormalizeSQL(tt.in)
		if got != tt.want {
			t.Errorf("NormalizeSQL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestScrubSecrets(t *testing.T) {
	if got := ScrubSecrets("api_key=abcdef rest"); got != "api_key=[REDACTED] rest" {
		t.Errorf("got %q", got)
	}
	if got := ScrubSecrets("password: hunter2"); got != "password=[REDACTED]" {
		t.Errorf("got %q", got)
	}
}
