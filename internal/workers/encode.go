package workers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/ultraviolet-dev/ultraviolet/internal/protocols/pgwire"
)

// mapDuckDBType maps DuckDB column-type names to PG OIDs.
func mapDuckDBType(t string) (uint32, int16) {
	switch t {
	case "BOOLEAN":
		return pgwire.OIDBool, 1
	case "TINYINT", "SMALLINT", "INTEGER", "INT":
		return pgwire.OIDInt4, 4
	case "BIGINT", "HUGEINT", "UBIGINT", "UINTEGER", "USMALLINT", "UTINYINT":
		return pgwire.OIDInt8, 8
	case "FLOAT", "REAL":
		return pgwire.OIDFloat4, 4
	case "DOUBLE":
		return pgwire.OIDFloat8, 8
	case "DECIMAL", "NUMERIC":
		return pgwire.OIDNumeric, -1
	case "VARCHAR", "TEXT", "STRING":
		return pgwire.OIDText, -1
	case "BLOB", "VARBINARY":
		return pgwire.OIDBytea, -1
	case "DATE":
		return pgwire.OIDDate, 4
	case "TIME":
		return pgwire.OIDTime, 8
	case "TIMESTAMP", "TIMESTAMPTZ", "TIMESTAMP WITH TIME ZONE":
		return pgwire.OIDTimestamptz, 8
	case "JSON":
		return pgwire.OIDJSONB, -1
	case "UUID":
		return pgwire.OIDUUID, 16
	default:
		return pgwire.OIDText, -1
	}
}

func encodeDuckDBValue(v interface{}) []byte {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case string:
		return []byte(x)
	case bool:
		if x {
			return []byte("t")
		}
		return []byte("f")
	case int:
		return []byte(strconv.Itoa(x))
	case int32:
		return []byte(strconv.FormatInt(int64(x), 10))
	case int64:
		return []byte(strconv.FormatInt(x, 10))
	case uint64:
		return []byte(strconv.FormatUint(x, 10))
	case float32:
		return []byte(strconv.FormatFloat(float64(x), 'g', -1, 32))
	case float64:
		return []byte(strconv.FormatFloat(x, 'g', -1, 64))
	case []byte:
		return []byte(base64.StdEncoding.EncodeToString(x))
	case time.Time:
		return []byte(x.UTC().Format("2006-01-02 15:04:05.000000-07"))
	default:
		buf, err := json.Marshal(x)
		if err == nil {
			return buf
		}
		return []byte(fmt.Sprintf("%v", x))
	}
}
