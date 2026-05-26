// Command uv-sync runs the CDC syncer + nightly cost backfill in one process.
// Each loop runs in its own goroutine; ctrl-C cancels both.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/ultraviolet-dev/ultraviolet/internal/agents"
	"github.com/ultraviolet-dev/ultraviolet/internal/audit"
	"github.com/ultraviolet-dev/ultraviolet/internal/billing"
	"github.com/ultraviolet-dev/ultraviolet/internal/config"
	"github.com/ultraviolet-dev/ultraviolet/internal/cost"
	"github.com/ultraviolet-dev/ultraviolet/internal/logger"
	"github.com/ultraviolet-dev/ultraviolet/internal/metrics"
	"github.com/ultraviolet-dev/ultraviolet/internal/scheduler"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
	uvsync "github.com/ultraviolet-dev/ultraviolet/internal/sync"
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

	traceShutdown, err := tracing.Init(ctx, "uv-sync", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if err != nil {
		log.Warn().Err(err).Msg("tracing init — continuing without tracing")
	}
	defer traceShutdown()

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("control-plane DB open")
	}
	defer db.Close()
	enc, err := store.NewEncryptor(cfg.EncryptionKey)
	if err != nil {
		log.Fatal().Err(err).Msg("encryptor")
	}

	syncer, err := uvsync.New(ctx, cfg, db, enc, log)
	if err != nil {
		log.Fatal().Err(err).Msg("sync init")
	}
	defer syncer.Close()
	bf := cost.New(cfg, db, enc, log)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return syncer.Run(gctx) })
	g.Go(func() error { return bf.Run(gctx, time.Hour) })

	// pu-1 scheduled-query runner. UV_SCHEDULED_INTERVAL=0 disables; default 1m.
	schedInterval := envDur("UV_SCHEDULED_INTERVAL", time.Minute)
	if schedInterval > 0 {
		runner := scheduler.NewRunner(db.Pool(), log)
		g.Go(func() error {
			t := time.NewTicker(schedInterval)
			defer t.Stop()
			for {
				select {
				case <-gctx.Done():
					return nil
				case <-t.C:
					if err := runner.Tick(gctx); err != nil {
						log.Warn().Err(err).Msg("scheduled-query tick")
					}
				}
			}
		})
	}

	// Agent scheduler — runs the recurring loop-closing agents in dry-run mode
	// across all customers on a configurable cadence (default 1h). Each tick
	// fan-outs through Runner.Invoke so audit + billing + panic-recovery apply.
	// Set UV_AGENT_SCHEDULE_INTERVAL=0 to disable.
	agentInterval := envDur("UV_AGENT_SCHEDULE_INTERVAL", time.Hour)
	if agentInterval > 0 {
		runner := agents.NewRunner(audit.New(db.Pool()), billing.New(db.Pool()), log)
		recurring := []agents.Agent{
			agents.NewCostOptimizer(db.Pool()),
			agents.NewSyncAutopilot(db.Pool()),
			agents.NewQueryRegressor(db.Pool()),
			agents.NewSelfHealingSync(db.Pool()),
		}
		g.Go(func() error {
			// Resolve customer list once per tick so newly-onboarded customers are picked
			// up without a restart.
			t := time.NewTicker(agentInterval)
			defer t.Stop()
			for {
				select {
				case <-gctx.Done():
					return nil
				case <-t.C:
					cs, err := db.ListCustomers(gctx)
					if err != nil {
						log.Warn().Err(err).Msg("agent scheduler: list customers")
						continue
					}
					ids := make([]uuid.UUID, 0, len(cs))
					for _, c := range cs {
						ids = append(ids, c.ID)
					}
					for _, cid := range ids {
						for _, ag := range recurring {
							if _, err := runner.Invoke(gctx, ag, agents.Input{CustomerID: cid, DryRun: true}); err != nil {
								log.Warn().Err(err).Str("agent", ag.Name()).Str("customer", cid.String()).Msg("agent run")
							}
						}
					}
				}
			}
		})
	}

	// Health endpoint so k8s/Render can liveness-probe uv-sync. Port from
	// UV_SYNC_HEALTH_PORT (default 8081). Failure to bind is a soft warn so a port
	// collision in dev doesn't crash the whole worker.
	healthPort := envInt("UV_SYNC_HEALTH_PORT", 8081)
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("/livez", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.Handle("/metrics", metrics.Handler())
	hsrv := &http.Server{Addr: fmt.Sprintf(":%d", healthPort), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	g.Go(func() error {
		if err := hsrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Warn().Err(err).Msg("sync health endpoint")
		}
		return nil
	})
	g.Go(func() error { <-gctx.Done(); shut, c := context.WithTimeout(context.Background(), 5*time.Second); defer c(); return hsrv.Shutdown(shut) })

	if err := g.Wait(); err != nil && ctx.Err() == nil {
		log.Fatal().Err(err).Msg("uv-sync exited")
	}
}

func envDur(k string, def time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func envInt(k string, def int) int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return def
	}
	return n
}
