package pgwire

import "testing"

func TestParseDatabase(t *testing.T) {
	tests := []struct {
		in            string
		wantSlug      string
		wantWarehouse string
		wantErr       bool
	}{
		{"acme_bigquery", "acme", "bigquery", false},
		{"acme_co_snowflake", "acme_co", "snowflake", false},
		{"acme_databricks", "acme", "databricks", false},
		{"acme", "", "", true},
		{"acme_unknown", "", "", true},
		{"_bigquery", "", "", true},
		{"acme_", "", "", true},
		{"", "", "", true},
	}
	for _, tt := range tests {
		slug, wh, err := ParseDatabase(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseDatabase(%q): want error, got nil", tt.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseDatabase(%q): unexpected error %v", tt.in, err)
			continue
		}
		if slug != tt.wantSlug || wh != tt.wantWarehouse {
			t.Errorf("ParseDatabase(%q): got (%q,%q), want (%q,%q)", tt.in, slug, wh, tt.wantSlug, tt.wantWarehouse)
		}
	}
}
