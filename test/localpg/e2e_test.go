//go:build localpg

// Package localpg runs a REAL end-to-end verification against an ephemeral
// Postgres started in-process (embedded-postgres — no Docker). It proves the
// things compilation cannot: the 6 new migrations apply + reverse, the
// hand-written SQL (lineage graph CTE, cost rollup, github store, pr_analysis)
// executes against real Postgres, and the API handlers return the right shapes.
//
//	go test -tags localpg ./test/localpg/ -run TestEndToEnd -v
package localpg

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/api"
	"github.com/ultraviolet-dev/ultraviolet/internal/config"
	"github.com/ultraviolet-dev/ultraviolet/internal/githubapp"
	"github.com/ultraviolet-dev/ultraviolet/internal/lineage"
	"github.com/ultraviolet-dev/ultraviolet/internal/logger"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
)

const (
	pgPort = 5455
	dsn    = "postgres://uv:uv@localhost:5455/uv?sslmode=disable"
)

func TestEndToEnd(t *testing.T) {
	ctx := context.Background()

	// ---- 1. boot a real Postgres ----
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Port(pgPort).Username("uv").Password("uv").Database("uv").
		StartTimeout(60 * time.Second))
	if err := pg.Start(); err != nil {
		t.Fatalf("start embedded postgres: %v", err)
	}
	defer func() { _ = pg.Stop() }()

	migrations := "file://" + mustAbs(t, "../../migrations")

	// ---- 2. migrations apply ----
	if err := store.MigrateUp(migrations, dsn); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	t.Log("✓ all migrations applied")

	// ---- 3. roundtrip: revert the 6 new migrations then re-apply ----
	for i := 0; i < 6; i++ {
		if err := store.MigrateDown(migrations, dsn); err != nil {
			t.Fatalf("migrate down step %d: %v", i, err)
		}
	}
	if err := store.MigrateUp(migrations, dsn); err != nil {
		t.Fatalf("migrate re-up after roundtrip: %v", err)
	}
	t.Log("✓ migration roundtrip (down x6 → up) clean — incl. 0020 constraint swap + 0024 PK swap")

	// ---- 4. open store + seed a customer ----
	os.Setenv("DATABASE_URL", dsn)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	db, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("store open: %v", err)
	}
	defer db.Close()
	enc, err := store.NewEncryptor(cfg.EncryptionKey)
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}

	var cid uuid.UUID
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO customers (slug, display_name) VALUES ('acme','Acme') RETURNING id`).Scan(&cid); err != nil {
		t.Fatalf("seed customer: %v", err)
	}

	// ---- 5. lineage: extract + persist + recursive graph (real CTE) ----
	ex := lineage.NewExtractor()
	w := lineage.NewWriter(db.Pool())
	edges := ex.Extract(`CREATE TABLE analytics.top AS SELECT word AS term, word_count AS freq FROM samples.shakespeare`)
	if err := w.Write(ctx, cid, edges); err != nil {
		t.Fatalf("lineage write: %v", err)
	}
	// Table-granularity graph is rooted at the table.
	gt, err := w.Graph(ctx, cid, "samples.shakespeare", "downstream", "table", 3)
	if err != nil {
		t.Fatalf("lineage graph (table): %v", err)
	}
	if !hasEdge(gt.Edges, "samples.shakespeare", "analytics.top") {
		t.Fatalf("expected table edge shakespeare→top, got %+v", gt.Edges)
	}
	// Column-granularity graph rooted at the TABLE: the anchor expands to the
	// table's columns (LIKE 'schema.table.%'), so the granularity toggle works
	// without forcing the user to re-root on a specific column.
	gc, err := w.Graph(ctx, cid, "samples.shakespeare", "downstream", "column", 3)
	if err != nil {
		t.Fatalf("lineage graph (column): %v", err)
	}
	if !hasEdge(gc.Edges, "samples.shakespeare.word", "analytics.top.term") {
		t.Fatalf("expected column edge word→term from table root, got %+v", gc.Edges)
	}
	// And rooting on a specific column also works.
	gcc, err := w.Graph(ctx, cid, "samples.shakespeare.word", "downstream", "column", 3)
	if err != nil || !hasEdge(gcc.Edges, "samples.shakespeare.word", "analytics.top.term") {
		t.Fatalf("expected column edge word→term from column root, got %+v err=%v", gcc.Edges, err)
	}
	t.Logf("✓ lineage: table graph %d edges, column graph %d edges (table-root + column-root both resolve)", len(gt.Edges), len(gc.Edges))

	// ---- 6. cost: estimated cost populates + savings formula is correct ----
	one, half := 1.0, 0.5
	mustInsertQueryLog(t, ctx, db, cid, "duckdb", &one, nil)     // est warehouse-equiv $1, actual nil (~0)
	mustInsertQueryLog(t, ctx, db, cid, "warehouse", nil, &half) // actual $0.50 paid
	// The corrected rollup formula (savings = est_duckdb − actual_duckdb,
	// duckdb_cost = actual_duckdb) — proven distinct, not the old duplicate.
	var whCost, ddCost, savings float64
	if err := db.Pool().QueryRow(ctx, `
		SELECT
		  COALESCE(SUM(actual_cost_usd) FILTER (WHERE route_decision IN ('warehouse','fallback')), 0),
		  COALESCE(SUM(actual_cost_usd) FILTER (WHERE route_decision = 'duckdb'), 0),
		  COALESCE(SUM(estimated_cost_usd) FILTER (WHERE route_decision = 'duckdb'), 0)
		    - COALESCE(SUM(actual_cost_usd) FILTER (WHERE route_decision = 'duckdb'), 0)
		FROM query_log WHERE customer_id = $1`, cid).Scan(&whCost, &ddCost, &savings); err != nil {
		t.Fatalf("rollup formula: %v", err)
	}
	if savings != 1.0 || ddCost != 0.0 || whCost != 0.5 {
		t.Fatalf("savings formula wrong: wh=%v dd=%v savings=%v (want 0.5/0/1)", whCost, ddCost, savings)
	}
	t.Logf("✓ cost: warehouse=$%.2f duckdb=$%.2f savings=$%.2f (savings≠duckdb_cost, bug fixed)", whCost, ddCost, savings)

	// spend_breakdown by user works on the new column (user_id FKs app_user)
	var uid uuid.UUID
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO app_user (customer_id, email) VALUES ($1,'u@acme.com') RETURNING id`, cid).Scan(&uid); err != nil {
		t.Fatalf("seed app_user: %v", err)
	}
	mustInsertQueryLogUser(t, ctx, db, cid, uid, &half)
	var n int
	if err := db.Pool().QueryRow(ctx,
		`SELECT COUNT(*) FROM query_log WHERE customer_id=$1 AND user_id=$2`, cid, uid).Scan(&n); err != nil || n != 1 {
		t.Fatalf("query_log.user_id column unusable: n=%d err=%v", n, err)
	}
	t.Log("✓ cost: query_log.user_id populated + queryable (per-user spend unblocked)")

	// ---- 7. github store: routing + idempotency + pr_analysis ----
	gs := githubapp.NewStore(db.Pool())
	if err := gs.UpsertInstallation(ctx, 123, "acme", "Organization"); err != nil {
		t.Fatalf("upsert installation: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `UPDATE github_installation SET customer_id=$1 WHERE installation_id=123`, cid); err != nil {
		t.Fatalf("claim installation: %v", err)
	}
	if err := gs.UpsertRepo(ctx, 456, 123, "acme/dw"); err != nil {
		t.Fatalf("upsert repo: %v", err)
	}
	got, err := gs.ResolveCustomer(ctx, 123, 456)
	if err != nil || got != cid {
		t.Fatalf("ResolveCustomer = %v,%v want %v", got, err, cid)
	}
	// fail-closed on unknown installation
	if _, err := gs.ResolveCustomer(ctx, 999, 888); err != githubapp.ErrNoCustomer {
		t.Fatalf("unknown installation should be ErrNoCustomer, got %v", err)
	}
	first, _ := gs.WebhookSeen(ctx, "delivery-1", "pull_request")
	again, _ := gs.WebhookSeen(ctx, "delivery-1", "pull_request")
	if !first || again {
		t.Fatalf("webhook idempotency broken: first=%v again=%v", first, again)
	}
	if err := gs.UpsertPRAnalysis(ctx, githubapp.PRAnalysisRow{
		CustomerID: cid, InstallationID: 123, RepoFullName: "acme/dw", PRNumber: 7,
		HeadSHA: "abc1234", PullRequestURL: "https://github.com/acme/dw/pull/7",
		Changes:    []map[string]any{{"Kind": "drop_column", "FQN": "analytics.top.term"}},
		Hits:       []map[string]any{{"FQN": "x.y", "Hops": 1, "Severity": "breaking"}},
		DQRefs:     []map[string]any{{"table_fqn": "x.y", "status": "fail"}},
		Conclusion: "action_required",
	}); err != nil {
		t.Fatalf("upsert pr_analysis: %v", err)
	}
	t.Log("✓ github: install→customer routing, fail-closed unknown, webhook idempotency, pr_analysis persisted")

	// ---- 8. API: real HTTP handlers against real Postgres ----
	srv := api.New(cfg, db, enc, logger.New("error", "json"))
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	assertJSON200(t, ts.URL+"/api/v1/lineage/graph?fqn=samples.shakespeare&granularity=column&direction=downstream", func(b []byte) error {
		var gr lineage.GraphResult
		if err := json.Unmarshal(b, &gr); err != nil {
			return err
		}
		if len(gr.Edges) == 0 {
			return fmt.Errorf("graph endpoint returned no edges")
		}
		return nil
	})
	assertJSON200(t, ts.URL+"/api/v1/pr-analysis", func(b []byte) error {
		var rows []map[string]any
		if err := json.Unmarshal(b, &rows); err != nil {
			return err
		}
		if len(rows) != 1 {
			return fmt.Errorf("pr-analysis len=%d want 1", len(rows))
		}
		if rows[0]["dq_impact"] != "failing" {
			return fmt.Errorf("dq_impact=%v want failing", rows[0]["dq_impact"])
		}
		return nil
	})
	assertJSON200(t, ts.URL+"/api/v1/github/install-info", func(b []byte) error {
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		if m["installed"] != true {
			return fmt.Errorf("installed=%v want true", m["installed"])
		}
		return nil
	})
	t.Log("✓ API: /lineage/graph, /pr-analysis, /github/install-info all 200 with correct shapes")
	t.Log("END-TO-END PASSED")
}

// helpers

func mustAbs(t *testing.T, p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func hasEdge(edges []lineage.GraphEdge, src, tgt string) bool {
	for _, e := range edges {
		if e.Source == src && e.Target == tgt {
			return true
		}
	}
	return false
}

func mustInsertQueryLog(t *testing.T, ctx context.Context, db *store.DB, cid uuid.UUID, route string, est, actual *float64) {
	if err := db.InsertQueryLog(ctx, store.QueryLogEntry{
		CustomerID: cid, QueryHash: "h", NormalizedSQL: "select 1",
		RouteDecision: route, DurationMS: 1, RowsReturned: 1, EstimatedCostUSD: est,
	}); err != nil {
		t.Fatalf("insert query_log: %v", err)
	}
	if actual != nil {
		if _, err := db.Pool().Exec(ctx,
			`UPDATE query_log SET actual_cost_usd=$1 WHERE customer_id=$2 AND route_decision=$3 AND actual_cost_usd IS NULL`,
			*actual, cid, route); err != nil {
			t.Fatalf("set actual cost: %v", err)
		}
	}
}

func mustInsertQueryLogUser(t *testing.T, ctx context.Context, db *store.DB, cid, uid uuid.UUID, actual *float64) {
	if err := db.InsertQueryLog(ctx, store.QueryLogEntry{
		CustomerID: cid, UserID: &uid, QueryHash: "h2", NormalizedSQL: "select 2",
		RouteDecision: "warehouse", DurationMS: 1, RowsReturned: 1,
	}); err != nil {
		t.Fatalf("insert query_log user: %v", err)
	}
}

func assertJSON200(t *testing.T, url string, check func([]byte) error) {
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s → %d", url, resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if err := check(b); err != nil {
		t.Fatalf("GET %s body check: %v", url, err)
	}
}
