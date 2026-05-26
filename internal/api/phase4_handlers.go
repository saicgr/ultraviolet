package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/ultraviolet-dev/ultraviolet/internal/budgets"
)

// requireRole returns nil if the caller's X-UV-Role header matches one of
// `allowed`. Phase-1 simplification — real RBAC lookups against
// workspace_membership land in Phase 2. Defaults to "viewer" when unset.
func requireRole(r *http.Request, allowed ...string) error {
	role := r.Header.Get("X-UV-Role")
	if role == "" {
		role = "viewer"
	}
	for _, a := range allowed {
		if role == a {
			return nil
		}
	}
	return fmt.Errorf("forbidden: role %q lacks permission", role)
}

// ----- ent-6 GDPR right-to-be-forgotten -----

// gdprForget redacts a user's PII across the control-plane in a single
// transaction. Accepts an email (preferred) — the matching app_user row is
// deleted outright; any rows in query_log / audit_log / annotation that
// reference the user_id are anonymised by NULLing the back-reference.
func (s *Server) gdprForget(w http.ResponseWriter, r *http.Request) {
	if err := requireRole(r, "admin", "superadmin"); err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	cid, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		EmailOrUserSlug string `json:"email_or_user_slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.EmailOrUserSlug == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("email_or_user_slug required"))
		return
	}

	ctx := r.Context()
	tx, err := s.db.Pool().BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Resolve the user_id once so subsequent UPDATEs/DELETE share it.
	var userID string
	err = tx.QueryRow(ctx,
		`SELECT id::text FROM app_user WHERE customer_id = $1 AND email = $2`,
		cid, body.EmailOrUserSlug).Scan(&userID)
	if err != nil && err != pgx.ErrNoRows {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	var deleted int64
	if userID != "" {
		ct, err := tx.Exec(ctx, `DELETE FROM app_user WHERE id = $1::uuid`, userID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		deleted += ct.RowsAffected()

		// NULL out the user_id references; the schema has no free-text user_slug
		// column, so this is the strongest available anonymisation short of
		// row-level deletion (which would destroy aggregate cost analytics).
		for _, q := range []string{
			`UPDATE query_log SET api_key_id = NULL WHERE customer_id = $1 AND api_key_id IN
			    (SELECT id FROM api_keys WHERE customer_id = $1)`, // best-effort: no direct user_id FK
			`UPDATE audit_log SET user_id = NULL, payload = jsonb_set(COALESCE(payload,'{}'::jsonb), '{actor}', '"__forgotten__"') WHERE user_id = $2::uuid`,
			`UPDATE annotation SET user_id = NULL, body = '__forgotten__' WHERE customer_id = $1 AND user_id = $2::uuid`,
			`UPDATE saved_query SET user_id = NULL WHERE customer_id = $1 AND user_id = $2::uuid`,
			`UPDATE notification SET user_id = NULL WHERE customer_id = $1 AND user_id = $2::uuid`,
		} {
			ct, err := tx.Exec(ctx, q, cid, userID)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, fmt.Errorf("redact: %w", err))
				return
			}
			deleted += ct.RowsAffected()
		}
	}

	if err := tx.Commit(ctx); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted_rows": deleted, "user_found": userID != ""})
}

// ----- bill-5 cost budget enforcement -----

// budgetCheck performs the pre-flight budget gate from internal/budgets in a
// SQL-aware way. We don't actually parse the SQL — the price is read from the
// last seen estimated_cost_usd for the same normalised query (Phase-1 proxy
// already records this). When no history exists we treat it as $0 (i.e.
// always allow), but still return the live cap/spend snapshot.
func (s *Server) budgetCheck(w http.ResponseWriter, r *http.Request) {
	cid, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		SQL              string  `json:"sql"`
		EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.SQL == "" && body.EstimatedCostUSD == 0 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("sql or estimated_cost_usd required"))
		return
	}
	if body.EstimatedCostUSD == 0 && body.SQL != "" {
		// Best-effort lookup: average historical cost for this customer's
		// previous runs of the same SQL. Falls back to zero on no rows.
		_ = s.db.Pool().QueryRow(r.Context(),
			`SELECT COALESCE(AVG(actual_cost_usd), 0)::float8 FROM query_log
			 WHERE customer_id = $1 AND normalized_sql = $2 AND actual_cost_usd IS NOT NULL`,
			cid, body.SQL).Scan(&body.EstimatedCostUSD)
	}

	allow, st, err := budgets.New(s.db.Pool()).PreflightAllow(r.Context(), cid, body.EstimatedCostUSD)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	reason := "ok"
	switch {
	case !allow:
		reason = "hard_block"
	case st.Status == "soft_breach":
		reason = "soft_warn"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"allowed":     allow,
		"current_usd": st.SpendMTDUSD,
		"cap_usd":     st.Budget.MonthlyCapUSD,
		"reason":      reason,
	})
}

// ----- bill-7 admin dashboard -----

// adminDashboard returns a tenant-fleet snapshot for the superadmin UI. Single
// roundtrip uses three CTE-style sub-selects (one for each aggregate) so we
// don't issue four sequential queries.
func (s *Server) adminDashboard(w http.ResponseWriter, r *http.Request) {
	if err := requireRole(r, "superadmin"); err != nil {
		writeErr(w, http.StatusForbidden, err)
		return
	}
	ctx := r.Context()
	out := struct {
		CustomerCount         int64 `json:"customer_count"`
		TotalQueryCount30d    int64 `json:"total_query_count_30d"`
		TotalUsageEvents30d   int64 `json:"total_usage_events_30d"`
		TopCustomersByCost    []struct {
			CustomerID string  `json:"customer_id"`
			Slug       string  `json:"slug"`
			CostUSD    float64 `json:"cost_usd"`
		} `json:"top_customers_by_cost"`
	}{}

	row := s.db.Pool().QueryRow(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM customers),
		  (SELECT COUNT(*) FROM query_log WHERE started_at > now() - interval '30 days'),
		  (SELECT COUNT(*) FROM usage_event WHERE created_at > now() - interval '30 days')
	`)
	if err := row.Scan(&out.CustomerCount, &out.TotalQueryCount30d, &out.TotalUsageEvents30d); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	rows, err := s.db.Pool().Query(ctx, `
		SELECT c.id::text, c.slug, COALESCE(SUM(q.actual_cost_usd), 0)::float8 AS cost
		FROM customers c
		LEFT JOIN query_log q
		  ON q.customer_id = c.id AND q.started_at > now() - interval '30 days'
		GROUP BY c.id, c.slug
		ORDER BY cost DESC
		LIMIT 10`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var rec struct {
			CustomerID string  `json:"customer_id"`
			Slug       string  `json:"slug"`
			CostUSD    float64 `json:"cost_usd"`
		}
		if err := rows.Scan(&rec.CustomerID, &rec.Slug, &rec.CostUSD); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		out.TopCustomersByCost = append(out.TopCustomersByCost, rec)
	}
	writeJSON(w, http.StatusOK, out)
}
