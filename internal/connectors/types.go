package connectors

import "github.com/ultraviolet-dev/ultraviolet/internal/protocols/pgwire"

// MapBigQueryFieldType maps a BigQuery field-type string to a Postgres OID + size.
// See docs/conventions/error-mapping.md and docs/architecture/pg-wire-protocol.md.
func MapBigQueryFieldType(bqType string) (oid uint32, size int16) {
	switch bqType {
	case "BOOL", "BOOLEAN":
		return pgwire.OIDBool, 1
	case "INT64", "INTEGER":
		return pgwire.OIDInt8, 8
	case "FLOAT64", "FLOAT":
		return pgwire.OIDFloat8, 8
	case "NUMERIC", "BIGNUMERIC", "DECIMAL":
		return pgwire.OIDNumeric, -1
	case "DATE":
		return pgwire.OIDDate, 4
	case "TIME":
		return pgwire.OIDTime, 8
	case "DATETIME", "TIMESTAMP":
		return pgwire.OIDTimestamptz, 8
	case "BYTES":
		return pgwire.OIDBytea, -1
	case "JSON":
		return pgwire.OIDJSONB, -1
	case "GEOGRAPHY":
		return pgwire.OIDText, -1
	case "ARRAY", "STRUCT", "RECORD":
		// BQ nested types are JSON-encoded by the connector (encodeBQValue marshals
		// map[string]Value / []Value via json.Marshal). PG-side, JSONB is the closest
		// fit for clients to .json_extract(...) without losing structure.
		return pgwire.OIDJSONB, -1
	default:
		return pgwire.OIDText, -1
	}
}

// MapSnowflakeType maps a Snowflake type name to PG OID.
func MapSnowflakeType(sfType string) (oid uint32, size int16) {
	switch sfType {
	case "BOOLEAN":
		return pgwire.OIDBool, 1
	case "FIXED", "NUMBER", "INT", "INTEGER", "BIGINT":
		return pgwire.OIDInt8, 8
	case "REAL", "FLOAT", "DOUBLE":
		return pgwire.OIDFloat8, 8
	case "DATE":
		return pgwire.OIDDate, 4
	case "TIME":
		return pgwire.OIDTime, 8
	case "TIMESTAMP_NTZ", "TIMESTAMP_LTZ", "TIMESTAMP_TZ", "DATETIME":
		return pgwire.OIDTimestamptz, 8
	case "BINARY", "VARBINARY":
		return pgwire.OIDBytea, -1
	case "VARIANT", "OBJECT", "ARRAY":
		return pgwire.OIDJSONB, -1
	default:
		return pgwire.OIDText, -1
	}
}
