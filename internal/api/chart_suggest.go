package api

// Workbench chart suggestion (an-4). Parses a SELECT statement via pg_query_go,
// inspects the target list, and returns a recommended chart type:
//   - time-series column + numeric  → "line"
//   - one categorical + numeric     → "bar"
//   - single numeric column         → "big_number"
//   - everything else               → "table"
//
// This is a heuristic suggestion engine — the frontend owns rendering.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

type chartSuggestRequest struct {
	CustomerID string `json:"customer_id"`
	SQL        string `json:"sql"`
}

type chartSuggestResponse struct {
	ChartType string `json:"chart_type"`
	XAxis     string `json:"x_axis,omitempty"`
	YAxis     string `json:"y_axis,omitempty"`
}

func (s *Server) workbenchChartSuggest(w http.ResponseWriter, r *http.Request) {
	var body chartSuggestRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.CustomerID == "" || body.SQL == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("customer_id and sql required"))
		return
	}

	cols, err := selectColumns(body.SQL)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("parse sql: %w", err))
		return
	}
	if len(cols) == 0 {
		writeJSON(w, http.StatusOK, chartSuggestResponse{ChartType: "table"})
		return
	}

	writeJSON(w, http.StatusOK, suggestChart(cols))
}

// columnHint describes one SELECT target with a coarse type bucket.
type columnHint struct {
	Name string
	Kind string // "time", "numeric", "categorical", "unknown"
}

func selectColumns(sql string) ([]columnHint, error) {
	tree, err := pg_query.Parse(sql)
	if err != nil {
		return nil, err
	}
	for _, raw := range tree.Stmts {
		sel := raw.Stmt.GetSelectStmt()
		if sel == nil {
			continue
		}
		out := make([]columnHint, 0, len(sel.TargetList))
		for i, t := range sel.TargetList {
			rt := t.GetResTarget()
			if rt == nil {
				continue
			}
			name := rt.Name
			kind := "unknown"
			if rt.Val != nil {
				if cr := rt.Val.GetColumnRef(); cr != nil {
					if name == "" && len(cr.Fields) > 0 {
						if s := cr.Fields[len(cr.Fields)-1].GetString_(); s != nil {
							name = s.Sval
						}
					}
					kind = classifyByName(name)
				} else if fc := rt.Val.GetFuncCall(); fc != nil {
					fn := ""
					if len(fc.Funcname) > 0 {
						if s := fc.Funcname[len(fc.Funcname)-1].GetString_(); s != nil {
							fn = strings.ToLower(s.Sval)
						}
					}
					kind = classifyByFunc(fn)
					if name == "" {
						name = fn
					}
				} else if rt.Val.GetAConst() != nil {
					kind = "numeric"
				}
			}
			if name == "" {
				name = fmt.Sprintf("col_%d", i)
			}
			out = append(out, columnHint{Name: name, Kind: kind})
		}
		return out, nil
	}
	return nil, nil
}

func classifyByName(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "date"), strings.Contains(n, "time"),
		strings.HasSuffix(n, "_at"), strings.HasSuffix(n, "_ts"), n == "ts":
		return "time"
	case strings.Contains(n, "count"), strings.Contains(n, "sum"),
		strings.Contains(n, "amount"), strings.Contains(n, "total"),
		strings.Contains(n, "qty"), strings.Contains(n, "num"),
		strings.Contains(n, "price"), strings.Contains(n, "revenue"),
		strings.Contains(n, "cost"):
		return "numeric"
	default:
		return "categorical"
	}
}

func classifyByFunc(fn string) string {
	switch fn {
	case "count", "sum", "avg", "min", "max", "stddev", "variance", "median":
		return "numeric"
	case "date_trunc", "date_bin", "to_timestamp", "now", "current_date", "current_timestamp":
		return "time"
	default:
		return "unknown"
	}
}

func suggestChart(cols []columnHint) chartSuggestResponse {
	var timeCol, catCol, numCol string
	var numCount, otherCount int
	for _, c := range cols {
		switch c.Kind {
		case "time":
			if timeCol == "" {
				timeCol = c.Name
			}
		case "numeric":
			if numCol == "" {
				numCol = c.Name
			}
			numCount++
		case "categorical":
			if catCol == "" {
				catCol = c.Name
			}
			otherCount++
		default:
			otherCount++
		}
	}
	switch {
	case timeCol != "" && numCol != "":
		return chartSuggestResponse{ChartType: "line", XAxis: timeCol, YAxis: numCol}
	case catCol != "" && numCol != "" && len(cols) == 2:
		return chartSuggestResponse{ChartType: "bar", XAxis: catCol, YAxis: numCol}
	case len(cols) == 1 && numCol != "":
		return chartSuggestResponse{ChartType: "big_number", YAxis: numCol}
	default:
		return chartSuggestResponse{ChartType: "table"}
	}
}
