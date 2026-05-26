// Package exports turns a result set into CSV / JSON / NDJSON for download
// from the workbench result grid + email subscriptions. Streams to the writer
// to avoid buffering large results in memory.
package exports

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type Format string

const (
	CSV    Format = "csv"
	JSON   Format = "json"
	NDJSON Format = "ndjson"
	TSV    Format = "tsv"
)

// Result is the row-set shape exporters consume.
type Result struct {
	Columns []string
	Rows    [][]any
}

func Write(w io.Writer, r Result, f Format) error {
	switch f {
	case CSV:
		return writeCSV(w, r, ',')
	case TSV:
		return writeCSV(w, r, '\t')
	case JSON:
		return writeJSON(w, r)
	case NDJSON:
		return writeNDJSON(w, r)
	}
	return fmt.Errorf("unknown export format %q", f)
}

func writeCSV(w io.Writer, r Result, sep rune) error {
	c := csv.NewWriter(w)
	c.Comma = sep
	if err := c.Write(r.Columns); err != nil {
		return err
	}
	for _, row := range r.Rows {
		strs := make([]string, len(row))
		for i, v := range row {
			strs[i] = stringify(v)
		}
		if err := c.Write(strs); err != nil {
			return err
		}
	}
	c.Flush()
	return c.Error()
}

func writeJSON(w io.Writer, r Result) error {
	out := make([]map[string]any, 0, len(r.Rows))
	for _, row := range r.Rows {
		obj := make(map[string]any, len(r.Columns))
		for i, c := range r.Columns {
			if i < len(row) {
				obj[c] = row[i]
			}
		}
		out = append(out, obj)
	}
	return json.NewEncoder(w).Encode(out)
}

func writeNDJSON(w io.Writer, r Result) error {
	enc := json.NewEncoder(w)
	for _, row := range r.Rows {
		obj := make(map[string]any, len(r.Columns))
		for i, c := range r.Columns {
			if i < len(row) {
				obj[c] = row[i]
			}
		}
		if err := enc.Encode(obj); err != nil {
			return err
		}
	}
	return nil
}

func stringify(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", x))
	}
}

// MIMEType returns the best-fit content-type header value.
func MIMEType(f Format) string {
	switch f {
	case CSV:
		return "text/csv; charset=utf-8"
	case TSV:
		return "text/tab-separated-values; charset=utf-8"
	case JSON:
		return "application/json"
	case NDJSON:
		return "application/x-ndjson"
	}
	return "application/octet-stream"
}
