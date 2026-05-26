// Phase-6 Wave-1 an-5: rich hover card for a fully-qualified table name.
// One round-trip returns columns, recent SQL, dashboards that reference the
// FQN, owner/SLA, and a deterministic sample-rows URI the frontend resolves
// asynchronously. All sub-queries are bounded (LIMIT 5, 7-day window) so a
// hover never costs more than a catalog-page render.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type hoverColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type hoverRecentQuery struct {
	SQLNormalized string    `json:"sql_normalized"`
	Count         int64     `json:"count"`
	LastRun       time.Time `json:"last_run"`
}

type hoverDashboardRef struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
}

type hoverResponse struct {
	FQN              string              `json:"fqn"`
	SchemaColumns    []hoverColumn       `json:"schema_columns"`
	SampleRowsURI    string              `json:"sample_rows_uri"`
	RecentQueries    []hoverRecentQuery  `json:"recent_queries"`
	DashboardsUsing  []hoverDashboardRef `json:"dashboards_using_it"`
	Owner            string              `json:"owner"`
	SLAMinutes       int                 `json:"sla_minutes"`
}

func (s *Server) catalogHover(w http.ResponseWriter, r *http.Request) {
	fqn := strings.TrimSpace(r.URL.Query().Get("fqn"))
	cidStr := strings.TrimSpace(r.URL.Query().Get("customer_id"))
	if fqn == "" || cidStr == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("fqn + customer_id required"))
		return
	}
	cid, err := uuid.Parse(cidStr)
	if err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("invalid customer_id: %w", err))
		return
	}

	ctx := r.Context()
	resp := hoverResponse{FQN: fqn, SchemaColumns: []hoverColumn{}, RecentQueries: []hoverRecentQuery{}, DashboardsUsing: []hoverDashboardRef{}}

	// table_metadata: owner, sla_minutes, and any column payload. Multiple
	// `source` rows may exist per FQN — take the most recently updated.
	var payload []byte
	var owner *string
	var sla *int
	row := s.db.Pool().QueryRow(ctx, `
		SELECT owner, sla_minutes, payload
		FROM table_metadata
		WHERE customer_id = $1 AND fqn = $2
		ORDER BY updated_at DESC
		LIMIT 1`, cid, fqn)
	switch err := row.Scan(&owner, &sla, &payload); {
	case err == nil:
		if owner != nil {
			resp.Owner = *owner
		}
		if sla != nil {
			resp.SLAMinutes = *sla
		}
		resp.SchemaColumns = extractColumns(payload)
	default:
		// missing metadata is not an error — leave defaults.
	}

	// recent_queries: top 5 normalized SQL referencing the FQN in the last 7d.
	// We use a case-insensitive LIKE on normalized_sql, which is acceptable for
	// the hover use case (false positives cost a row, not correctness).
	pat := "%" + fqn + "%"
	qrows, err := s.db.Pool().Query(ctx, `
		SELECT normalized_sql, COUNT(*) AS n, MAX(started_at) AS last_run
		FROM query_log
		WHERE customer_id = $1
		  AND started_at > now() - interval '7 days'
		  AND normalized_sql ILIKE $2
		GROUP BY normalized_sql
		ORDER BY n DESC
		LIMIT 5`, cid, pat)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	for qrows.Next() {
		var rec hoverRecentQuery
		if err := qrows.Scan(&rec.SQLNormalized, &rec.Count, &rec.LastRun); err != nil {
			qrows.Close()
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		resp.RecentQueries = append(resp.RecentQueries, rec)
	}
	qrows.Close()

	// dashboards_using_it: jsonb text match on the tiles array. tiles is JSONB
	// of [{sql, ...}, ...]; ::text LIKE is good enough at Phase-6 scale.
	drows, err := s.db.Pool().Query(ctx, `
		SELECT id, name FROM dashboard
		WHERE customer_id = $1 AND tiles::text ILIKE $2
		ORDER BY updated_at DESC
		LIMIT 20`, cid, pat)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	for drows.Next() {
		var ref hoverDashboardRef
		if err := drows.Scan(&ref.ID, &ref.Title); err != nil {
			drows.Close()
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		resp.DashboardsUsing = append(resp.DashboardsUsing, ref)
	}
	drows.Close()

	// sample_rows_uri: deterministic so the frontend can dedupe in-flight fetches.
	schema, name := splitFQN(fqn)
	resp.SampleRowsURI = fmt.Sprintf("uv://table/%s/%s/%s/sample", cid.String(), schema, name)

	writeJSON(w, http.StatusOK, resp)
}

// extractColumns pulls a `{columns: [{name, type}, ...]}` block out of the
// table_metadata.payload JSONB if present. Different sources (dbt vs tableau)
// use different shapes — we accept the canonical one and skip the rest.
func extractColumns(payload []byte) []hoverColumn {
	if len(payload) == 0 {
		return []hoverColumn{}
	}
	var p struct {
		Columns []hoverColumn `json:"columns"`
	}
	if err := json.Unmarshal(payload, &p); err != nil || p.Columns == nil {
		return []hoverColumn{}
	}
	return p.Columns
}

func splitFQN(fqn string) (schema, name string) {
	parts := strings.Split(fqn, ".")
	switch len(parts) {
	case 0, 1:
		return "_", fqn
	case 2:
		return parts[0], parts[1]
	default:
		return parts[len(parts)-2], parts[len(parts)-1]
	}
}
