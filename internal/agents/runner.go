package agents

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/ultraviolet-dev/ultraviolet/internal/audit"
	"github.com/ultraviolet-dev/ultraviolet/internal/billing"
)

// Runner wraps Agent.Run with cross-cutting concerns: audit-log every
// invocation, charge a usage event, capture latency, swallow panics.
type Runner struct {
	audit   *audit.Logger
	billing *billing.Recorder
	log     zerolog.Logger
}

func NewRunner(a *audit.Logger, b *billing.Recorder, log zerolog.Logger) *Runner {
	return &Runner{audit: a, billing: b, log: log.With().Str("component", "agent-runner").Logger()}
}

// Invoke runs `agent` with `in`, recording audit + usage. Panics are converted
// to errors so a buggy agent can never crash the scheduler.
func (r *Runner) Invoke(ctx context.Context, agent Agent, in Input) (out Output, err error) {
	start := time.Now()
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("agent %s panic: %v", agent.Name(), rec)
		}
		dur := time.Since(start)
		r.log.Info().
			Str("agent", agent.Name()).
			Str("kind", agent.Kind()).
			Str("customer", in.CustomerID.String()).
			Bool("dry_run", in.DryRun).
			Int("suggestions", len(out.Suggestions)).
			Dur("duration", dur).
			Msg("agent run")
		if r.audit != nil {
			cid := in.CustomerID
			_ = r.audit.Log(ctx, audit.Event{
				CustomerID: &cid,
				Action:     "agent." + agent.Name() + ".run",
				TargetType: agent.Kind(),
				Payload: map[string]any{
					"dry_run": in.DryRun, "suggestions": len(out.Suggestions),
					"applied": len(out.Applied), "duration_ms": dur.Milliseconds(),
					"confidence": out.Confidence,
				},
			})
		}
		if r.billing != nil {
			_ = r.billing.Record(ctx, in.CustomerID, billing.EventType("agent_run"), 1, map[string]any{
				"agent": agent.Name(), "dry_run": in.DryRun,
			})
		}
	}()
	in.Log = r.log.With().Str("agent", agent.Name()).Logger()
	return agent.Run(ctx, in)
}

// Schedule is the simplest possible recurring driver: every interval, run all
// `recurring` agents for every customer with the given input template. Returns
// when ctx cancels.
func (r *Runner) Schedule(ctx context.Context, interval time.Duration, customers []uuid.UUID, recurring []Agent) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			for _, c := range customers {
				for _, ag := range recurring {
					_, _ = r.Invoke(ctx, ag, Input{CustomerID: c, DryRun: true})
				}
			}
		}
	}
}
