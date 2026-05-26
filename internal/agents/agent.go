// Package agents hosts the loop-closing AI agents that take action on the
// uniquely-rich substrate Ultraviolet observes (every executed query × cost ×
// lineage × catalog × dashboard usage).
//
// Each agent satisfies the Agent interface so the scheduler in cmd/sync can
// fan out runs deterministically (per-customer, with audit logging + budget).
//
// The taxonomy:
//
//   - LoopClosing — proposes a change, runs in dry-run or auto-apply mode.
//     Examples: cost_optimizer, sync_autopilot, schema_safeguard.
//   - Investigator — observes signals, generates a diagnosis, posts to chatops.
//     Examples: anomaly_investigator, query_regressor, oncall_summarizer.
//   - Productive — interactive surface for humans / other agents.
//     Examples: text2sql, auto_documenter, onboarding_planner.
//
// Every agent run records:
//   - action="agent.{name}.run", payload={dry_run, suggestions, applied}
//     into audit_log via internal/audit
//   - billing event {event_type: "agent_run"} into usage_event
//   - lineage edges where the agent created derived artifacts (materializations)
package agents

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// Agent is implemented by every concrete agent.
type Agent interface {
	// Name is the stable identifier used in audit_log + usage_event + chatops.
	Name() string
	// Kind = "loop_closing" | "investigator" | "productive". Drives scheduler.
	Kind() string
	// Run executes one tick / one invocation; returns Suggestions the harness
	// can either apply (if AutoApply on the run) or surface as PR-style draft.
	Run(ctx context.Context, in Input) (Output, error)
}

// Input is the shared request body for every agent. Most fields are optional;
// individual agents document which they require.
type Input struct {
	CustomerID uuid.UUID
	UserSlug   string
	DryRun     bool       // when false, agent may apply the change directly
	Window     time.Duration // how far back to look (0 → agent default)
	Args       map[string]any
	Log        zerolog.Logger
}

// Output is what the agent produces. `Suggestions` is the structured proposal;
// `Summary` is the human-readable explanation; `Applied` lists changes the
// agent actually committed (only when DryRun=false).
type Output struct {
	Summary     string
	Suggestions []Suggestion
	Applied     []Change
	Confidence  float64 // 0..1
	NextRunHint time.Duration
}

// Suggestion is a single recommendation. `Diff` is human-readable; `Apply` is
// the function the harness calls to commit.
type Suggestion struct {
	ID       string
	Title    string
	Rationale string
	Diff     string
	Severity string // info | warn | critical
	Estimate Estimate
	Apply    func(ctx context.Context) error `json:"-"`
}

type Change struct {
	ID    string
	What  string
	When  time.Time
}

// Estimate quantifies the suggestion's impact so a human / approval rule can
// pick the highest-leverage ones.
type Estimate struct {
	SavingsUSDPerMonth float64
	LatencyMSDelta     int64
	BytesScannedDelta  int64
}
