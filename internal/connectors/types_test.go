package connectors

import (
	"testing"

	"github.com/ultraviolet-dev/ultraviolet/internal/protocols/pgwire"
)

func TestMapBigQueryFieldType(t *testing.T) {
	tests := []struct {
		bq   string
		oid  uint32
		size int16
	}{
		{"INT64", pgwire.OIDInt8, 8},
		{"FLOAT64", pgwire.OIDFloat8, 8},
		{"BOOL", pgwire.OIDBool, 1},
		{"STRING", pgwire.OIDText, -1},
		{"TIMESTAMP", pgwire.OIDTimestamptz, 8},
		{"BYTES", pgwire.OIDBytea, -1},
		{"JSON", pgwire.OIDJSONB, -1},
		{"GEOGRAPHY", pgwire.OIDText, -1},
		{"WHATEVER", pgwire.OIDText, -1},
	}
	for _, tt := range tests {
		oid, size := MapBigQueryFieldType(tt.bq)
		if oid != tt.oid || size != tt.size {
			t.Errorf("Map(%q): got (%d,%d), want (%d,%d)", tt.bq, oid, size, tt.oid, tt.size)
		}
	}
}

func TestMapSnowflakeType(t *testing.T) {
	tests := []struct {
		sf  string
		oid uint32
	}{
		{"FIXED", pgwire.OIDInt8},
		{"VARIANT", pgwire.OIDJSONB},
		{"TIMESTAMP_NTZ", pgwire.OIDTimestamptz},
		{"GEOGRAPHY_THING", pgwire.OIDText},
	}
	for _, tt := range tests {
		oid, _ := MapSnowflakeType(tt.sf)
		if oid != tt.oid {
			t.Errorf("Map(%q): got %d, want %d", tt.sf, oid, tt.oid)
		}
	}
}
