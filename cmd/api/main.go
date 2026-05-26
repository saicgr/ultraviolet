// Command uv-api serves the REST control plane on UV_API_PORT (default 8080).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ultraviolet-dev/ultraviolet/internal/ai"
	"github.com/ultraviolet-dev/ultraviolet/internal/api"
	"github.com/ultraviolet-dev/ultraviolet/internal/config"
	"github.com/ultraviolet-dev/ultraviolet/internal/logger"
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

	traceShutdown, err := tracing.Init(ctx, "uv-api", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if err != nil {
		log.Warn().Err(err).Msg("tracing init — continuing without tracing")
	}
	defer traceShutdown()

	if err := store.MigrateUp(cfg.MigrationsSource, cfg.DatabaseURL); err != nil {
		log.Warn().Err(err).Msg("migrate up — continuing")
	}

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("control-plane DB open")
	}
	defer db.Close()
	enc, err := store.NewEncryptor(cfg.EncryptionKey)
	if err != nil {
		log.Fatal().Err(err).Msg("encryptor")
	}
	srv := api.New(cfg, db, enc, log)
	if rw, err := ai.NewRewriter(cfg, log); err == nil {
		srv.SetRewriter(rw)
	} else {
		log.Warn().Err(err).Msg("ai rewriter init failed — copilot/explain endpoints disabled")
	}
	if err := srv.ListenAndServe(ctx); err != nil {
		log.Fatal().Err(err).Msg("api server exited")
	}
}
