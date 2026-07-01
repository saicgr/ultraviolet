// Command lineage-bot is the GitHub App webhook receiver. It verifies the
// signature, de-dupes redeliveries, resolves the tenant from the installation,
// runs real PR impact + data-quality analysis, and posts a PR comment + Check
// Run. All logic lives in internal/githubapp; this file is wiring only.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/ultraviolet-dev/ultraviolet/internal/config"
	"github.com/ultraviolet-dev/ultraviolet/internal/dq"
	"github.com/ultraviolet-dev/ultraviolet/internal/githubapp"
	"github.com/ultraviolet-dev/ultraviolet/internal/lineage"
	"github.com/ultraviolet-dev/ultraviolet/internal/logger"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		os.Stderr.WriteString("config: " + err.Error() + "\n")
		os.Exit(2)
	}
	log := logger.New(cfg.LogLevel, cfg.LogFormat)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Fail closed: the webhook secret is mandatory so a forged payload can never
	// trigger GitHub writes or dashboards-as-code upserts.
	if cfg.GitHubWebhookSecret == "" {
		log.Fatal().Msg("UV_GITHUB_WEBHOOK_SECRET is required")
	}

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("db open")
	}
	defer db.Close()

	// The GitHub App (token minting) is optional in local dev: installation +
	// signature handling still work; only diff-fetch / comment / check-run
	// degrade with a logged error when it's absent.
	var app *githubapp.App
	if cfg.GitHubAppID != 0 && len(cfg.GitHubPrivateKeyPEM) > 0 {
		app, err = githubapp.NewApp(cfg.GitHubAppID, cfg.GitHubPrivateKeyPEM)
		if err != nil {
			log.Fatal().Err(err).Msg("github app")
		}
	} else {
		log.Warn().Msg("github app id/key not set — PR diff fetch + comments disabled")
	}

	ghStore := githubapp.NewStore(db.Pool())
	client := githubapp.NewClient(app)
	analyzer := githubapp.NewAnalyzer(ghStore, client, lineage.NewWriter(db.Pool()), dq.New(db.Pool()))
	h := githubapp.NewHandler(ghStore, client, analyzer, []byte(cfg.GitHubWebhookSecret), log)
	h.OnPush = func(ctx context.Context, body []byte) {
		var ev pushEvent
		if json.Unmarshal(body, &ev) == nil {
			handleDashboardsAsCodePush(ctx, log, db.Pool(), &ev)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", h.ServeWebhook)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })

	port := cfg.LineageBotPort
	if port == 0 {
		port = 8090
	}
	addr := ":" + strconv.Itoa(port)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = srv.Shutdown(sctx)
	}()
	log.Info().Str("addr", addr).Msg("lineage-bot listening")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("lineage-bot")
	}
}
