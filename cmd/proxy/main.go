// Command uv-proxy serves the Postgres wire protocol on UV_PROXY_PORT (default 5000).
// Phase 1 step 1: hardcoded SELECT 1 handler. Wired to BigQuery + DuckDB in later batches.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/ultraviolet-dev/ultraviolet/internal/config"
	"github.com/ultraviolet-dev/ultraviolet/internal/logger"
	"github.com/ultraviolet-dev/ultraviolet/internal/metrics"
	"github.com/ultraviolet-dev/ultraviolet/internal/protocols/pgwire"
	"github.com/ultraviolet-dev/ultraviolet/internal/proxy"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
	"github.com/ultraviolet-dev/ultraviolet/internal/tracing"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(2)
	}
	log := logger.New(cfg.LogLevel, cfg.LogFormat)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	traceShutdown, err := tracing.Init(ctx, "uv-proxy", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if err != nil {
		log.Warn().Err(err).Msg("tracing init — continuing without tracing")
	}
	defer traceShutdown()

	if err := store.MigrateUp(cfg.MigrationsSource, cfg.DatabaseURL); err != nil {
		log.Warn().Err(err).Msg("migrate up — continuing")
	}

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Str("dsn", cfg.DatabaseURL).Msg("control-plane DB open")
	}
	defer db.Close()

	enc, err := store.NewEncryptor(cfg.EncryptionKey)
	if err != nil {
		log.Fatal().Err(err).Msg("encryptor")
	}

	handler, err := proxy.New(ctx, cfg, db, enc, log)
	if err != nil {
		log.Fatal().Err(err).Msg("proxy handler")
	}
	defer handler.Close()

	var tlsCfg *tls.Config
	if cfg.DevTLS || (cfg.TLSCertPath != "" && cfg.TLSKeyPath != "") {
		tlsCfg, err = pgwire.LoadOrGenerateTLS(cfg.TLSCertPath, cfg.TLSKeyPath)
		if err != nil {
			log.Warn().Err(err).Msg("TLS init — disabled")
		}
	}

	srv := &pgwire.Server{
		Addr:        fmt.Sprintf(":%d", cfg.ProxyPort),
		TLSConfig:   tlsCfg,
		Auth:        &pgwire.Authenticator{DB: db},
		Handler:     handler,
		Log:         log.With().Str("component", "pgwire").Logger(),
		MaxInflight: 4096,
		DrainGrace:  30 * time.Second,
	}

	healthPort, _ := strconv.Atoi(envOr("UV_PROXY_HEALTH_PORT", "8082"))
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("/livez", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.Handle("/metrics", metrics.Handler())
	hsrv := &http.Server{Addr: fmt.Sprintf(":%d", healthPort), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := hsrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Warn().Err(err).Msg("proxy health endpoint")
		}
	}()
	go func() { <-ctx.Done(); shut, c := context.WithTimeout(context.Background(), 5*time.Second); defer c(); _ = hsrv.Shutdown(shut) }()

	if err := srv.ListenAndServe(ctx); err != nil {
		log.Error().Err(err).Msg("pgwire server exited")
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
