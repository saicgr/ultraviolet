// Package api is the REST control-plane (chi router). Frontend talks to this on :8080.
// Endpoints follow docs/reference/product-brief.md §8.
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/ultraviolet-dev/ultraviolet/internal/ai"
	"github.com/ultraviolet-dev/ultraviolet/internal/config"
	"github.com/ultraviolet-dev/ultraviolet/internal/metrics"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
	"github.com/ultraviolet-dev/ultraviolet/internal/workers"
)

type Server struct {
	cfg     *config.Config
	db      *store.DB
	enc     *store.Encryptor
	log     zerolog.Logger
	limiter *Limiter
	// rewriter is the optional LLM client for AI-backed endpoints (copilot,
	// explain-query). nil when no provider is configured; handlers must return
	// 503 in that case rather than degrade silently.
	rewriter *ai.Rewriter
	// udfs is the optional per-workspace UDF registry (pu-2). nil disables
	// the /udfs surface (503).
	udfs *workers.UDFRegistry
	// queryRunner executes Workbench SQL on a real DuckDB worker pool. nil in the
	// plain control-plane (the proxy owns the pool); the demo wires a
	// *workers.Pool so the in-app Workbench runs real queries and logs them.
	queryRunner QueryRunner
}

// QueryRunner runs ad-hoc SQL on the DuckDB engine and returns string-encoded
// columns/rows, the row count, and the bytes produced. *workers.Pool satisfies
// it directly (its ExecuteRows method matches this signature).
type QueryRunner interface {
	ExecuteRows(ctx context.Context, customerSlug, query string) (cols []string, rows [][]string, rowCount int64, bytesScanned int64, err error)
}

// SetRewriter wires the optional LLM client used by AI endpoints. Safe to call
// before ListenAndServe; nil leaves AI endpoints disabled (503).
func (s *Server) SetRewriter(r *ai.Rewriter) { s.rewriter = r }

// SetQueryRunner wires a real DuckDB executor for the Workbench. nil leaves the
// Workbench in stub mode (returns the psql connect hint).
func (s *Server) SetQueryRunner(qr QueryRunner) { s.queryRunner = qr }

func New(cfg *config.Config, db *store.DB, enc *store.Encryptor, log zerolog.Logger) *Server {
	return &Server{
		cfg:     cfg,
		db:      db,
		enc:     enc,
		log:     log.With().Str("component", "api").Logger(),
		limiter: NewLimiter(cfg.APIRateLimitRPS),
	}
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(s.logErrors)
	r.Use(s.cors)
	r.Use(s.limiter.Middleware)
	r.Use(s.authMiddleware)

	r.Get("/healthz", s.health)
	r.Get("/livez", s.health)
	r.Get("/readyz", s.health)
	r.Method(http.MethodGet, "/metrics", metrics.Handler())
	r.Get("/openapi.yaml", s.ServeOpenAPI)

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.login)
		r.Post("/auth/refresh", s.refresh)
		r.Get("/customers", s.listCustomers)
		r.Post("/customers", s.createCustomer)
		r.Get("/customers/{id}/connections", s.listConnections)
		r.Post("/customers/{id}/connections", s.createConnection)
		r.Get("/customers/{id}/api-keys", s.listAPIKeys)
		r.Post("/customers/{id}/api-keys", s.createAPIKey)
		r.Delete("/api-keys/{id}", s.revokeAPIKey)
		r.Get("/connections/{id}/synced-tables", s.listSyncedTables)
		r.Post("/connections/{id}/synced-tables", s.createSyncedTable)
		r.Get("/connections/{id}/test", s.testConnection)
		r.Get("/customers/{id}/queries", s.queryAnalytics)
		r.Get("/customers/{id}/savings", s.savings)
		// Phase-3 surface
		r.Get("/lineage/upstream", s.lineageUpstream)
		r.Get("/lineage/downstream", s.lineageDownstream)
		r.Get("/lineage/graph", s.lineageGraph)
		// W2 GitHub App settings + PR analysis history
		r.Get("/github/install-info", s.githubInstallInfo)
		r.Get("/github/setup", s.githubSetupCallback)
		r.Get("/github/installations", s.listGitHubInstallations)
		r.Get("/github/repos", s.listGitHubRepos)
		r.Post("/github/connect", s.connectGitHubRepo)
		r.Post("/github/disconnect", s.disconnectGitHubRepo)
		r.Get("/pr-analysis", s.listPRAnalysis)
		r.Get("/pr-analysis/{id}", s.getPRAnalysis)
		r.Get("/i18n/{locale}/messages.json", s.i18nMessages)
		r.Get("/catalog/search", s.catalogSearch)
		r.Get("/customers/{id}/dashboards", s.listDashboards)
		r.Post("/customers/{id}/dashboards", s.createDashboard)
		r.Get("/customers/{id}/activity", s.listActivity)
		// ux-7 dashboard versioning (git-style history + restore).
		r.Get("/dashboards/{id}/versions", s.listDashboardVersions)
		r.Get("/dashboards/{id}/versions/{version}", s.getDashboardVersion)
		r.Post("/dashboards/{id}/versions/{version}/restore", s.restoreDashboardVersion)
		// bu-9 shareable point-in-time snapshots (Iceberg time-travel handle).
		r.Post("/dashboards/{id}/snapshot", s.createDashboardSnapshot)
		r.Get("/snapshots/{token}", s.getDashboardSnapshot)
		r.Post("/workbench/run", s.workbenchRun)
		r.Post("/impact/preview", s.impactPreview)
		r.Post("/semantic", s.upsertSemantic)
		r.Get("/audit/log", s.listAudit)
		r.Get("/usage/events", s.listUsage)
		r.Get("/destinations", s.listDestinations)
		// Phase 5 — agentic AI surface.
		r.Get("/agents", s.listAgents)
		r.Post("/agents/{name}/run", s.runAgent)
		// Phase 4 deferred — GDPR, budget gate, superadmin dashboard.
		r.Post("/customers/{id}/gdpr/forget", s.gdprForget)
		r.Post("/customers/{id}/budget/check", s.budgetCheck)
		r.Get("/admin/dashboard", s.adminDashboard)
		// Phase 6 Wave 1 — pre-flight cost, per-user quotas, schedule-suggest.
		r.Post("/cost/preflight", s.preflightCheck)
		r.Post("/cost/schedule-suggest", s.scheduleSuggest)
		r.Post("/quotas", s.upsertQuota)
		r.Get("/customers/{id}/quotas", s.listQuotas)
		// Phase 6 Wave 1 — hover, promote query → synced, dashboard cost regression, lineage watches.
		r.Get("/catalog/hover", s.catalogHover)
		r.Post("/workbench/promote", s.workbenchPromote)
		r.Get("/customers/{id}/alerts/cost-regression", s.costRegressionAlerts)
		r.Post("/lineage/watches", s.createLineageWatch)
		r.Get("/lineage/watches", s.listLineageWatches)
		r.Delete("/lineage/watches/{id}", s.deleteLineageWatch)
		// Phase 6 Wave 2 — analyst velocity: autocomplete, chart suggestion, query diff.
		r.Get("/workbench/autocomplete", s.workbenchAutocomplete)
		r.Post("/workbench/chart-suggest", s.workbenchChartSuggest)
		r.Get("/queries/history/diff", s.queryHistoryDiff)
		// AI surface — embedded copilot (ai-7) + explain-query (ai-u2).
		r.Post("/copilot/chat", s.copilotChat)
		r.Post("/queries/explain", s.explainQuery)
		// Phase 6 Wave 2 — data-engineer ergonomics: sync DAG + schema diff.
		r.Get("/customers/{id}/sync/dag", s.syncDAG)
		r.Post("/connections/{id}/schema/capture", s.captureSchema)
		r.Get("/connections/{id}/schema/diff", s.diffSchema)
		// Phase 6 FinOps — spend breakdown (fo-3) + cost forecast (fo-2).
		r.Get("/customers/{id}/spend/breakdown", s.spendBreakdown)
		r.Get("/customers/{id}/cost/forecast", s.costForecast)
		// Phase 6 Wave 3 — governance: PII auto-tag, privacy preview, query approvals.
		r.Post("/connections/{id}/pii/scan", s.piiScanConnection)
		r.Post("/workbench/privacy-preview", s.privacyPreview)
		r.Post("/queries/approvals", s.submitQueryApproval)
		r.Get("/queries/approvals", s.listQueryApprovals)
		r.Post("/queries/approvals/{id}/decide", s.decideQueryApproval)
		// Phase governance / SIEM — go-4 dictionary CSV, go-5 audit NDJSON, go-6 access reviews.
		r.Get("/customers/{id}/dictionary.csv", s.dictionaryCSV)
		r.Get("/customers/{id}/audit-log.ndjson", s.auditLogNDJSON)
		r.Post("/access-reviews", s.openAccessReview)
		r.Get("/customers/{id}/access-reviews", s.listAccessReviews)
		r.Post("/access-reviews/{id}/decisions", s.recordAccessReviewDecision)
		r.Post("/access-reviews/{id}/close", s.closeAccessReview)
		// pu-1 scheduled queries (executor lives in internal/scheduler).
		r.Post("/scheduled-queries", s.createScheduledQuery)
		r.Get("/customers/{id}/scheduled-queries", s.listScheduledQueries)
		r.Delete("/scheduled-queries/{id}", s.deleteScheduledQuery)
		// pu-6 outbound webhook registry (dispatcher lives in internal/webhooks).
		r.Post("/webhooks", s.createWebhook)
		r.Get("/customers/{id}/webhooks", s.listWebhooks)
		r.Delete("/webhooks/{id}", s.deleteWebhook)
		// an-7 / an-8 saved-query ${param} render.
		r.Post("/saved-queries/{id}/run", s.runSavedQuery)
		// co-1 in-app inbox (notifications package).
		r.Get("/inbox", s.inboxList)
		r.Get("/inbox/unread-count", s.inboxBadgeCount)
		r.Post("/inbox/{id}/read", s.inboxMarkRead)
		r.Post("/inbox/read-all", s.inboxMarkAllRead)
		// bu-2 dashboard email subscriptions (CRUD + test-send).
		r.Post("/dashboards/{id}/subscriptions", s.createDashboardSubscription)
		r.Get("/dashboards/{id}/subscriptions", s.listDashboardSubscriptions)
		r.Delete("/subscriptions/{id}", s.deleteSubscription)
		r.Post("/subscriptions/{id}/test-send", s.testSendSubscription)
		// pu-5 anomaly scan trigger (routes anomalies through notifications.Router).
		r.Post("/customers/{id}/anomalies/scan", s.scanAnomalies)
		// Phase 6 ergonomics — plan visualizer (de-8), SQL tools (an-3),
		// sync replay (de-5), macro expand (pu-7).
		r.Post("/queries/{hash}/plan-tree", s.queryPlanTree)
		r.Post("/sql/format", s.sqlFormat)
		r.Post("/sql/lint", s.sqlLint)
		r.Post("/synced-tables/{id}/replay", s.syncReplay)
		r.Post("/workbench/expand", s.workbenchExpand)
		// Phase 6 AI batch — dashboard AI-edit (ai-u4), semantic search (ai-u5),
		// auto-generated dashboard (ai-u7), catalog narrator (ai-u10).
		r.Post("/dashboards/{id}/ai-edit", s.dashboardAIEdit)
		r.Post("/embeddings/reindex", s.embeddingsReindex)
		r.Get("/catalog/semantic-search", s.catalogSemanticSearch)
		r.Post("/customers/{id}/dashboards/auto", s.dashboardsAuto)
		r.Get("/customers/{id}/catalog/narrative", s.catalogNarrative)
		// an-9 result export + fo-6 cost-attribution CSV.
		r.Post("/workbench/export", s.workbenchExport)
		r.Get("/customers/{id}/cost-attribution.csv", s.costAttributionCSV)
		// bu-3 public dashboard share tokens. The /public/ path is whitelisted
		// in authMiddleware so it bypasses Bearer-token enforcement.
		r.Post("/dashboards/{id}/share", s.createDashboardShare)
		r.Get("/public/dashboards/{token}", s.publicDashboard)
		r.Delete("/share-tokens/{token}", s.revokeDashboardShare)
		// bu-4 dashboard PDF (501 unless built with -tags chromedp).
		r.Post("/dashboards/{id}/pdf", s.dashboardPDF)
		// pu-2 per-workspace custom DuckDB UDF registry.
		r.Get("/customers/{id}/udfs", s.listUDFs)
		r.Post("/customers/{id}/udfs", s.createUDF)
		r.Delete("/udfs/{name}", s.deleteUDF)
		// pu-8 workspace backup (export-only; restore is out of scope).
		r.Get("/customers/{id}/backup.json", s.workspaceBackup)
		// pu-4 dashboards-as-code is handled by lineage-bot's push webhook;
		// no API route is registered here.
		// an-10 row-level annotations (query results, dashboard tiles, lineage nodes).
		r.Post("/annotations", s.createAnnotation)
		r.Get("/annotations", s.listAnnotations)
		r.Delete("/annotations/{id}", s.deleteAnnotation)
		// bu-8 semantic what-if: parameter overrides → rewritten SQL (no execution).
		r.Post("/semantic/{id}/what-if", s.semanticWhatIf)
		// bu-11 per-workspace custom branding.
		r.Get("/workspaces/{id}/branding", s.getWorkspaceBranding)
		r.Put("/workspaces/{id}/branding", s.putWorkspaceBranding)
	})
	return r
}

// logErrors emits a structured warn log for every handler that returns 5xx.
// Closes AUDIT.md §api gap "No middleware error log when handler returns 500".
func (s *Server) logErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if rec.status >= 500 {
			s.log.Warn().
				Int("status", rec.status).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Str("remote", r.RemoteAddr).
				Msg("api handler 5xx")
		}
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", s.cfg.APIPort),
		Handler:           s.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	s.log.Info().Int("port", s.cfg.APIPort).Msg("api listening")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// ----- handlers -----

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) listCustomers(w http.ResponseWriter, r *http.Request) {
	cs, err := s.db.ListCustomers(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, cs)
}

func (s *Server) createCustomer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Slug        string `json:"slug"`
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.Slug == "" || body.DisplayName == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("slug + display_name required"))
		return
	}
	c, err := s.db.CreateCustomer(r.Context(), body.Slug, body.DisplayName)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

func (s *Server) listConnections(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	conns, err := s.db.ListConnections(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	limit, offset := pageParams(r)
	writeJSON(w, http.StatusOK, applyPage(conns, limit, offset))
}

func (s *Server) createConnection(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		WarehouseType string          `json:"warehouse_type"`
		Name          string          `json:"name"`
		Credentials   json.RawMessage `json:"credentials"`
		StorageMode   string          `json:"storage_mode"`
		S3Bucket      *string         `json:"s3_bucket"`
		S3Prefix      *string         `json:"s3_prefix"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.StorageMode == "" {
		body.StorageMode = "managed"
	}
	if body.WarehouseType == "" || body.Name == "" || len(body.Credentials) == 0 {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("warehouse_type, name, credentials required"))
		return
	}
	ct, nonce, err := s.enc.Encrypt(body.Credentials)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	conn, err := s.db.CreateConnection(r.Context(), id, body.WarehouseType, body.Name, ct, nonce, body.StorageMode, body.S3Bucket, body.S3Prefix)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, conn)
}

func (s *Server) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	keys, err := s.db.ListAPIKeys(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	limit, offset := pageParams(r)
	writeJSON(w, http.StatusOK, applyPage(keys, limit, offset))
}

func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		Description *string `json:"description"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	apiKey, prefix, err := generateAPIKey()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	k, err := s.db.CreateAPIKey(r.Context(), id, apiKey, prefix, body.Description)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Plaintext is returned only on creation; never stored, never shown again.
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"api_key": apiKey,
		"key":     k,
	})
}

func (s *Server) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.db.RevokeAPIKey(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listSyncedTables(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ts, err := s.db.ListSyncedTables(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	limit, offset := pageParams(r)
	writeJSON(w, http.StatusOK, applyPage(ts, limit, offset))
}

func (s *Server) createSyncedTable(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		SchemaName      string  `json:"schema_name"`
		TableName       string  `json:"table_name"`
		SyncMode        string  `json:"sync_mode"`
		WatermarkColumn *string `json:"watermark_column"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	t := store.SyncedTable{
		ConnectionID:    id,
		SchemaName:      body.SchemaName,
		TableName:       body.TableName,
		SyncMode:        body.SyncMode,
		WatermarkColumn: body.WatermarkColumn,
		State:           "pending",
	}
	tid, err := s.db.UpsertSyncedTable(r.Context(), t)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]uuid.UUID{"id": tid})
}

func (s *Server) queryAnalytics(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// Phase 6 Wave 1 (de-7): include actual + estimated cost per route so the
	// query-history UI can show inline $-cost without a second round-trip.
	rows, err := s.db.Pool().Query(r.Context(),
		`SELECT route_decision, COUNT(*) AS n, AVG(duration_ms)::int AS avg_ms,
		        SUM(rows_returned) AS rows,
		        COALESCE(SUM(actual_cost_usd),    0)::float8 AS actual_cost,
		        COALESCE(SUM(estimated_cost_usd), 0)::float8 AS estimated_cost
		 FROM query_log WHERE customer_id = $1 AND started_at > now() - interval '24 hours'
		 GROUP BY 1 ORDER BY 1`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	type row struct {
		Route            string  `json:"route"`
		Count            int64   `json:"count"`
		AvgMS            int     `json:"avg_ms"`
		RowsReturned     int64   `json:"rows_returned"`
		ActualCostUSD    float64 `json:"actual_cost_usd"`
		EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	}
	out := []row{}
	for rows.Next() {
		var rec row
		if err := rows.Scan(&rec.Route, &rec.Count, &rec.AvgMS, &rec.RowsReturned, &rec.ActualCostUSD, &rec.EstimatedCostUSD); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, rec)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) savings(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rows, err := s.db.Pool().Query(r.Context(),
		`SELECT period_start, period_end, warehouse_cost_usd, duckdb_cost_usd, estimated_savings_usd,
		        queries_total, queries_duckdb
		 FROM cost_attribution WHERE customer_id = $1 ORDER BY period_start DESC LIMIT 30`, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()
	type row struct {
		PeriodStart   time.Time `json:"period_start"`
		PeriodEnd     time.Time `json:"period_end"`
		WarehouseCost float64   `json:"warehouse_cost_usd"`
		DuckDBCost    float64   `json:"duckdb_cost_usd"`
		Savings       float64   `json:"estimated_savings_usd"`
		QueriesTotal  int64     `json:"queries_total"`
		QueriesDuckDB int64     `json:"queries_duckdb"`
	}
	out := []row{}
	for rows.Next() {
		var rec row
		if err := rows.Scan(&rec.PeriodStart, &rec.PeriodEnd, &rec.WarehouseCost, &rec.DuckDBCost, &rec.Savings, &rec.QueriesTotal, &rec.QueriesDuckDB); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		out = append(out, rec)
	}
	writeJSON(w, http.StatusOK, out)
}

// testConnection probes a stored connection. Phase 1 returns OK if the row
// exists and the encrypted credentials decrypt cleanly — a real warehouse
// roundtrip lives behind the connector layer (avoids an api→connectors import
// cycle). Closes AUDIT.md §api gap "No /connections/{id}/test endpoint".
func (s *Server) testConnection(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	row := s.db.Pool().QueryRow(r.Context(),
		`SELECT warehouse_type, credentials_ciphertext, credentials_nonce
		 FROM connections WHERE id = $1`, id)
	var wh string
	var ct, nonce []byte
	if err := row.Scan(&wh, &ct, &nonce); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if _, err := s.enc.Decrypt(ct, nonce); err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("decrypt: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "warehouse": wh})
}

// ----- helpers -----

// pageParams parses ?limit & ?offset (caps at 200, default 50/0) so listing
// endpoints don't hand back unbounded result sets. Closes AUDIT.md §api gap
// "No pagination on listSyncedTables, listConnections, listAPIKeys".
func pageParams(r *http.Request) (limit, offset int) {
	limit, offset = 50, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	if limit > 200 {
		limit = 200
	}
	return
}

// applyPage slices a Go slice in-process. We don't push limit/offset down to SQL
// in Phase 1 because list cardinalities are tiny; revisit when a customer crosses
// ~10k rows on any endpoint.
func applyPage[T any](rows []T, limit, offset int) []T {
	if offset >= len(rows) {
		return []T{}
	}
	end := offset + limit
	if end > len(rows) {
		end = len(rows)
	}
	return rows[offset:end]
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func parseUUID(r *http.Request, name string) (uuid.UUID, error) {
	s := chi.URLParam(r, name)
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid %s: %w", name, err)
	}
	return id, nil
}

// generateAPIKey returns ("uvk_<32hex>", "uvk_xxxx") for storage display.
func generateAPIKey() (string, string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	full := "uvk_" + hex.EncodeToString(buf)
	prefix := full[:min(12, len(full))]
	return full, prefix, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ = strings.Builder{}
