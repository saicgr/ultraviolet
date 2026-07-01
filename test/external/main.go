//go:build external

// Command external exercises the two paths that the in-browser demo could not:
//
//  1. Real DuckDB query execution through the actual worker pool (the proxy's
//     execution engine) — proving CGo DuckDB runs SQL and encodes PG result
//     frames, without needing S3/Iceberg (the Iceberg ATTACH is non-fatal).
//
//  2. The full PR-analysis pipeline on a REAL `git diff` from this machine —
//     diff → change detection → column-level impact → data-quality cross-ref →
//     rendered GitHub comment + Check Run. This is everything AnalyzePR does
//     after the diff is fetched; only GitHub's HTTP transport is mocked away.
//
//     go run -tags external ./test/external
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/google/uuid"

	"github.com/ultraviolet-dev/ultraviolet/internal/config"
	"github.com/ultraviolet-dev/ultraviolet/internal/dq"
	"github.com/ultraviolet-dev/ultraviolet/internal/githubapp"
	"github.com/ultraviolet-dev/ultraviolet/internal/impact"
	"github.com/ultraviolet-dev/ultraviolet/internal/lineage"
	"github.com/ultraviolet-dev/ultraviolet/internal/logger"
	"github.com/ultraviolet-dev/ultraviolet/internal/protocols/pgwire"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
	"github.com/ultraviolet-dev/ultraviolet/internal/workers"
)

const dsn = "postgres://uv:uv@localhost:5456/uv?sslmode=disable"

func main() {
	ctx := context.Background()
	log := logger.New("error", "pretty")

	dir := os.Getenv("HOME") + "/.uv-ext-pg"
	_ = os.RemoveAll(dir)
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Port(5456).Username("uv").Password("uv").Database("uv").
		RuntimePath(dir).DataPath(dir + "/data").StartTimeout(60 * time.Second))
	if err := pg.Start(); err != nil {
		fatal("start postgres", err)
	}
	defer func() { _ = pg.Stop() }()

	if err := store.MigrateUp("file://./migrations", dsn); err != nil {
		fatal("migrate", err)
	}
	os.Setenv("DATABASE_URL", dsn)
	cfg, err := config.Load()
	if err != nil {
		fatal("config", err)
	}
	cfg.RedisURL = "" // no Redis: skip the snapshot-refresh subscription
	db, err := store.Open(ctx, dsn)
	if err != nil {
		fatal("store", err)
	}
	defer db.Close()

	var cid uuid.UUID
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO customers (slug, display_name) VALUES ('acme','Acme') RETURNING id`).Scan(&cid); err != nil {
		fatal("seed customer", err)
	}

	_ = log
	part1DuckDB(ctx, cfg, db)
	part2PRPipeline(ctx, db, cid)
}

// part1DuckDB runs a real query through the actual DuckDB worker pool.
func part1DuckDB(ctx context.Context, cfg *config.Config, db *store.DB) {
	fmt.Println("\n===== PART 1: live DuckDB query execution (worker pool) =====")
	pool, err := workers.NewPool(ctx, cfg, db, logger.New("error", "pretty"))
	if err != nil {
		fatal("duckdb pool", err)
	}
	defer pool.Close()

	var buf bytes.Buffer
	wr := pgwire.NewWriter(&buf)
	query := `SELECT range AS n, range * range AS square FROM range(1, 6)`
	n, err := pool.ExecuteStreaming(ctx, "acme", query, nil, wr)
	if err != nil {
		fatal("duckdb execute", err)
	}
	if err := wr.Flush(); err != nil {
		fatal("flush", err)
	}
	fmt.Printf("query:    %s\n", query)
	fmt.Printf("rows:     %d (expected 5: 1..5 and their squares)\n", n)
	fmt.Printf("pg wire:  %d bytes of RowDescription + DataRow + CommandComplete frames encoded\n", buf.Len())
	if n != 5 || buf.Len() == 0 {
		fatal("duckdb result", fmt.Errorf("got %d rows / %d bytes", n, buf.Len()))
	}
	fmt.Println("✓ DuckDB executed real SQL via the proxy's worker pool and encoded PG result frames")
}

// part2PRPipeline runs the real analysis pipeline on a genuine git diff.
func part2PRPipeline(ctx context.Context, db *store.DB, cid uuid.UUID) {
	fmt.Println("\n===== PART 2: PR analysis on a REAL git diff =====")

	// Seed lineage (multi-hop, column-level) + a failing data-quality test so the
	// pipeline has something to walk and cross-reference.
	w := lineage.NewWriter(db.Pool())
	for _, sql := range []string{
		`CREATE TABLE analytics.top_words AS SELECT word AS term, word_count AS freq FROM samples.shakespeare`,
		`CREATE TABLE analytics.word_report AS SELECT term, freq FROM analytics.top_words`,
		`CREATE TABLE marts.exec_summary AS SELECT term AS keyword, freq AS volume FROM analytics.word_report`,
	} {
		if err := w.Write(ctx, cid, lineage.NewExtractor().Extract(sql)); err != nil {
			fatal("seed lineage", err)
		}
	}
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO dq_result (customer_id, table_fqn, test_name, status)
		 VALUES ($1,'analytics.word_report','not_null_freq','fail')`, cid); err != nil {
		fatal("seed dq", err)
	}

	// Produce a REAL unified diff with `git diff --no-index` (a dbt migration that
	// drops a column). This is genuine git output, parsed by the real ParseDiff.
	diff := realGitDiff()
	fmt.Println("--- real git diff (truncated) ---")
	fmt.Println(indent(firstLines(diff, 12)))

	changes := githubapp.DetectChanges(githubapp.ParseDiff(diff))
	fmt.Printf("detected changes: %d\n", len(changes))
	for _, c := range changes {
		fmt.Printf("  - %s %s\n", c.Kind, c.FQN)
	}
	if len(changes) == 0 {
		fatal("detect changes", fmt.Errorf("no changes detected from the diff"))
	}

	// Impact (column-aware) + data-quality cross-reference — exactly what
	// AnalyzePR does after fetching the diff.
	an := impact.New(w)
	dqRec := dq.New(db.Pool())
	merged := map[string]impact.Hit{}
	for _, c := range changes {
		hits, err := an.Preview(ctx, cid, c, 5)
		if err != nil {
			fatal("impact", err)
		}
		for _, h := range hits {
			merged[h.FQN] = h
		}
	}
	var hits []impact.Hit
	tables := map[string]struct{}{}
	for _, h := range merged {
		hits = append(hits, h)
		tables[tableOf(h.FQN)] = struct{}{}
	}
	for _, c := range changes {
		tables[tableOf(c.FQN)] = struct{}{}
	}
	var dqRefs []githubapp.DQRef
	for tf := range tables {
		if st, err := dqRec.LatestStatus(ctx, cid, tf); err == nil && st != "" {
			dqRefs = append(dqRefs, githubapp.DQRef{TableFQN: tf, Status: st})
		}
	}

	conclusion := githubapp.Conclusion(hits, dqRefs)
	comment := githubapp.RenderComment(changes, hits, dqRefs, "abc1234def")
	checkRun := githubapp.CheckRunPayload(changes, hits, dqRefs, "abc1234def")

	fmt.Printf("\nimpacted downstream: %d   data-quality refs: %d   conclusion: %s\n", len(hits), len(dqRefs), conclusion)
	fmt.Println("\n--- rendered GitHub PR comment (this is what gets posted) ---")
	fmt.Println(indent(comment))
	fmt.Println("--- Check Run payload ---")
	fmt.Println(indent(firstLines(string(checkRun), 4)))

	if conclusion != "action_required" || len(hits) == 0 {
		fatal("pipeline", fmt.Errorf("expected action_required with hits, got %s / %d hits", conclusion, len(hits)))
	}
	fmt.Println("✓ Full PR-analysis pipeline ran on a real git diff → impacted nodes + DQ + GitHub comment/check-run")
}

// realGitDiff writes two versions of a migration and returns the genuine
// `git diff --no-index` output between them.
func realGitDiff() string {
	tmp, _ := os.MkdirTemp("", "uvdiff")
	defer os.RemoveAll(tmp)
	old := filepath.Join(tmp, "old.sql")
	newF := filepath.Join(tmp, "new.sql")
	_ = os.WriteFile(old, []byte("-- migrations/0030_x.sql\nCREATE INDEX idx_top ON analytics.top_words(term);\n"), 0o644)
	_ = os.WriteFile(newF, []byte("-- migrations/0030_x.sql\nCREATE INDEX idx_top ON analytics.top_words(term);\nALTER TABLE analytics.top_words DROP COLUMN freq;\n"), 0o644)
	out, _ := exec.Command("git", "diff", "--no-index", "--no-color", old, newF).Output() // exit 1 when differ
	return string(out)
}

func tableOf(fqn string) string {
	parts := bytesSplit(fqn)
	if len(parts) >= 3 {
		return parts[0] + "." + parts[1]
	}
	return fqn
}

func bytesSplit(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '.' {
			out = append(out, cur)
			cur = ""
		} else {
			cur += string(r)
		}
	}
	return append(out, cur)
}

func firstLines(s string, n int) string {
	lines := 0
	for i, r := range s {
		if r == '\n' {
			lines++
			if lines == n {
				return s[:i]
			}
		}
	}
	return s
}

func indent(s string) string {
	out := "    "
	for _, r := range s {
		out += string(r)
		if r == '\n' {
			out += "    "
		}
	}
	return out
}

func fatal(tag string, err error) {
	fmt.Fprintf(os.Stderr, "FATAL %s: %v\n", tag, err)
	os.Exit(1)
}
