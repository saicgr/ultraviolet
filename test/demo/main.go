//go:build demo

// Command demo boots the full control plane on an ephemeral in-process Postgres
// (no Docker), seeds demo data for every Foundational feature, and serves the
// REST API on :8080 — so the Vite frontend (proxying /api → :8080) and a
// Playwright run can exercise the whole webapp end to end without any cloud
// dependency. The warehouse "datasource" is intentionally absent; it's the thing
// you swap in later (BigQuery/Snowflake).
//
//	go run -tags demo ./test/demo
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/ultraviolet-dev/ultraviolet/internal/api"
	"github.com/ultraviolet-dev/ultraviolet/internal/config"
	"github.com/ultraviolet-dev/ultraviolet/internal/lineage"
	"github.com/ultraviolet-dev/ultraviolet/internal/logger"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
	"github.com/ultraviolet-dev/ultraviolet/internal/workers"
)

const dsn = "postgres://uv:uv@localhost:5432/uv?sslmode=disable"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	log := logger.New("info", "pretty")

	runtimeDir := os.Getenv("HOME") + "/.uv-demo-pg"
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Port(5432).Username("uv").Password("uv").Database("uv").
		RuntimePath(runtimeDir).DataPath(runtimeDir + "/data").
		StartTimeout(60 * time.Second))
	if err := pg.Start(); err != nil {
		log.Fatal().Err(err).Msg("start embedded postgres")
	}
	defer func() { _ = pg.Stop() }()
	log.Info().Msg("embedded postgres up on :5432")

	if err := store.MigrateUp("file://./migrations", dsn); err != nil {
		log.Fatal().Err(err).Msg("migrate")
	}
	os.Setenv("DATABASE_URL", dsn)
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("config")
	}
	db, err := store.Open(ctx, dsn)
	if err != nil {
		log.Fatal().Err(err).Msg("store")
	}
	defer db.Close()
	enc, err := store.NewEncryptor(cfg.EncryptionKey)
	if err != nil {
		log.Fatal().Err(err).Msg("encryptor")
	}
	// UV_DEMO_SEED=full pre-populates the cost dashboard (14 days of rollups +
	// query traffic) for a screenshot-ready first impression. The default
	// ("live") seeds only the structural data (lineage, PRs, GitHub) and leaves
	// the cost dashboard at $0 — so you build the numbers yourself by running
	// real queries in the Workbench and watch them appear. No invented figures.
	seedFull := os.Getenv("UV_DEMO_SEED") == "full"
	seed(ctx, log, db, enc, seedFull)

	srv := api.New(cfg, db, enc, log)
	// Wire a real DuckDB worker pool so the in-app Workbench executes real SQL
	// and logs it → cost rollup → Savings Dashboard. This is the same engine the
	// proxy uses; nothing here is mocked.
	pool, err := workers.NewPool(ctx, cfg, db, log)
	if err != nil {
		log.Fatal().Err(err).Msg("duckdb pool")
	}
	defer pool.Close()
	srv.SetQueryRunner(pool)
	h := &http.Server{Addr: ":8080", Handler: srv.Routes(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		s, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = h.Shutdown(s)
	}()
	log.Info().Msg("API serving on :8080 — open the frontend at http://localhost:5173")
	if err := h.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("api")
	}
}

func seed(ctx context.Context, log zerolog.Logger, db *store.DB, enc *store.Encryptor, full bool) {
	p := db.Pool()
	must := func(tag string, err error) {
		if err != nil {
			log.Fatal().Err(err).Str("seed", tag).Msg("seed failed")
		}
	}

	var cid uuid.UUID
	must("customer", p.QueryRow(ctx,
		`INSERT INTO customers (slug, display_name) VALUES ('acme','Acme Analytics') RETURNING id`).Scan(&cid))

	// Warehouse connection (encrypted dummy creds — no real warehouse needed).
	ct, nonce, err := enc.Encrypt([]byte(`{"project":"demo"}`))
	must("encrypt", err)
	var connID uuid.UUID
	must("connection", p.QueryRow(ctx,
		`INSERT INTO connections (customer_id, warehouse_type, name, credentials_ciphertext, credentials_nonce)
		 VALUES ($1,'bigquery','BigQuery (demo)',$2,$3) RETURNING id`, cid, ct, nonce).Scan(&connID))

	// Cost: only pre-seeded in UV_DEMO_SEED=full. In the default "live" mode the
	// cost dashboard starts empty ($0 / 0 queries) and is populated by the real
	// queries you run in the Workbench — so every number is one you generated and
	// can audit in /queries.
	if full {
		for d := 0; d < 14; d++ {
			day := fmt.Sprintf("now() - interval '%d days'", d)
			_, err := p.Exec(ctx, `INSERT INTO cost_attribution
				(customer_id, connection_id, period_start, period_end,
				 warehouse_cost_usd, duckdb_cost_usd, estimated_savings_usd, queries_total, queries_duckdb)
				VALUES ($1,$2, date_trunc('day', `+day+`), date_trunc('day', `+day+`) + interval '1 day',
				        12.40, 0.00, 41.80, 320, 270)`, cid, connID)
			must("cost_attribution", err)
		}
		for i := 0; i < 40; i++ {
			route := "duckdb"
			if i%4 == 0 {
				route = "warehouse"
			}
			must("query_log", db.InsertQueryLog(ctx, store.QueryLogEntry{
				CustomerID: cid, ConnectionID: &connID, QueryHash: fmt.Sprintf("h%d", i),
				NormalizedSQL: "select * from analytics.top_words", RouteDecision: route,
				DurationMS: 40 + i, RowsReturned: int64(100 + i),
			}))
		}
	}

	// Lineage: a small multi-hop dbt-style graph (table + column edges).
	w := lineage.NewWriter(p)
	for _, sql := range []string{
		`CREATE TABLE analytics.top_words AS SELECT word AS term, word_count AS freq FROM samples.shakespeare WHERE word_count > 100`,
		`CREATE TABLE analytics.word_report AS SELECT term, freq FROM analytics.top_words`,
		`CREATE TABLE marts.exec_summary AS SELECT term AS keyword, freq AS volume FROM analytics.word_report`,
	} {
		must("lineage", w.Write(ctx, cid, lineage.NewExtractor().Extract(sql)))
	}

	// Dashboard listing.
	_, err = p.Exec(ctx, `INSERT INTO dashboard (customer_id, name) VALUES ($1,'Exec Overview'),($1,'Word Trends')`, cid)
	must("dashboard", err)

	// GitHub App: a connected installation + repo (real tenant routing).
	_, err = p.Exec(ctx, `INSERT INTO github_installation (installation_id, account_login, account_type, customer_id)
		VALUES (123, 'acme-labs', 'Organization', $1)`, cid)
	must("installation", err)
	_, err = p.Exec(ctx, `INSERT INTO github_repo (repo_id, installation_id, repo_full_name, customer_id, connected)
		VALUES (456, 123, 'acme-labs/warehouse-dbt', $1, true), (457, 123, 'acme-labs/etl', NULL, false)`, cid)
	must("repo", err)

	// PR analysis history (with impact + data-quality refs).
	_, err = p.Exec(ctx, `INSERT INTO pr_analysis
		(customer_id, installation_id, repo_full_name, pr_number, head_sha, pull_request_url, changes, hits, dq_refs, conclusion)
		VALUES
		($1,123,'acme-labs/warehouse-dbt',42,'a1b2c3d4','https://github.com/acme-labs/warehouse-dbt/pull/42',
		 '[{"Kind":"drop_column","FQN":"analytics.top_words.freq"}]',
		 '[{"FQN":"analytics.word_report.freq","Hops":1,"Why":"top_words → word_report","Severity":"breaking"},
		   {"FQN":"marts.exec_summary.volume","Hops":2,"Why":"word_report → exec_summary","Severity":"breaking"}]',
		 '[{"table_fqn":"analytics.word_report","status":"fail"}]','action_required'),
		($1,123,'acme-labs/warehouse-dbt',41,'e5f6a7b8','https://github.com/acme-labs/warehouse-dbt/pull/41',
		 '[{"Kind":"rename_table","FQN":"analytics.stg_words"}]',
		 '[{"FQN":"analytics.top_words","Hops":1,"Why":"stg_words → top_words","Severity":"breaking"}]',
		 '[]','neutral')`, cid)
	must("pr_analysis", err)

	log.Info().Str("customer", cid.String()).Bool("cost_preseeded", full).
		Msg("seeded demo data (lineage, PRs, GitHub, dashboards); cost dashboard starts at $0 unless UV_DEMO_SEED=full")
}
