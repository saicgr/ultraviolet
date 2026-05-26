//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/rs/zerolog"

	"github.com/ultraviolet-dev/ultraviolet/internal/protocols/pgwire"
)

// TestPgwire_HardcodedSelectOne boots the server with no auth + no handler so the
// hardcoded SELECT 1 path is exercised. Verifies a real lib/pq client can connect,
// negotiate, query, and disconnect cleanly. No external services required.
func TestPgwire_HardcodedSelectOne(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	srv := &pgwire.Server{
		Addr: addr,
		Log:  zerolog.Nop(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.ListenAndServe(ctx) }()

	dsn := fmt.Sprintf("postgres://anyuser@%s/anycustomer_bigquery?sslmode=disable", addr)
	var db *sql.DB
	deadline := time.Now().Add(2 * time.Second)
	for {
		db, err = sql.Open("pgx", dsn)
		if err == nil {
			err = db.Ping()
		}
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer db.Close()

	var n int
	if err := db.QueryRow("SELECT 1").Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 1 {
		t.Errorf("got %d, want 1", n)
	}
}
