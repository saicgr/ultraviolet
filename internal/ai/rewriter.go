// Package ai detects and rewrites ai_generate(...) calls. Path A uses the DuckDB llm
// extension when row count ≤ UV_AI_PATH_A_ROW_LIMIT; Path B batches via OpenAI / Anthropic
// (or DuckDB llm extension if no API key configured).
package ai

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"

	"github.com/rs/zerolog"

	"github.com/ultraviolet-dev/ultraviolet/internal/config"
)

// ErrBudgetExceeded is returned by CompleteOne when a customer has burned through
// their per-process external-AI call budget. Caller falls back to DuckDB llm.
var ErrBudgetExceeded = errors.New("ai: per-customer call budget exceeded")

// aiGenerateRe matches the SQL function call `ai_generate(...)` (case-insensitive).
// Captures the argument list verbatim. The argument list is passed straight to the
// DuckDB llm extension's `llm_complete(...)` function on Path A.
var aiGenerateRe = regexp.MustCompile(`(?i)\bai_generate\s*\(`)

type Rewriter struct {
	cfg      *config.Config
	log      zerolog.Logger
	provider Provider // nil when no external API key configured; falls back to DuckDB llm

	// budgetMu guards the per-customer external-call counter. Phase 1 uses a coarse
	// per-process budget (resets when the binary restarts) — promote to Redis with
	// a sliding window when multi-instance Path B lands.
	budgetMu  sync.Mutex
	callCount map[string]int // keyed by customer slug
}

func NewRewriter(cfg *config.Config, log zerolog.Logger) (*Rewriter, error) {
	r := &Rewriter{cfg: cfg, log: log.With().Str("component", "ai-rewriter").Logger(), callCount: map[string]int{}}
	if cfg.OpenAIKey != "" || cfg.AnthropicKey != "" {
		if p, err := SelectProvider(cfg.AIDefaultModel, cfg.OpenAIKey, cfg.AnthropicKey); err == nil {
			r.provider = p
			r.log.Info().Str("provider", p.Name()).Msg("ai Path B provider configured")
		}
	}
	return r, nil
}

// CompleteOne is the Path B entry point. Returns ErrNoProvider when no key is
// configured (caller falls back to DuckDB llm) and ErrBudgetExceeded when the
// per-customer call budget is exhausted (UV_AI_CALLS_PER_CUSTOMER, default 1000
// per process lifetime).
func (r *Rewriter) CompleteOne(ctx context.Context, model, prompt, customerSlug string) (string, error) {
	if r.provider == nil {
		return "", ErrNoProvider
	}
	if !r.takeBudget(customerSlug) {
		r.log.Warn().Str("customer", customerSlug).Msg("ai budget exceeded — refusing call")
		return "", ErrBudgetExceeded
	}
	return r.provider.Complete(ctx, model, prompt)
}

func (r *Rewriter) takeBudget(slug string) bool {
	limit := r.cfg.AICallsPerCustomer
	if limit <= 0 {
		return true // unlimited when unset
	}
	r.budgetMu.Lock()
	defer r.budgetMu.Unlock()
	if r.callCount[slug] >= limit {
		return false
	}
	r.callCount[slug]++
	return true
}

// Rewrite returns (newSQL, didRewrite, err). When the SQL contains no ai_generate(),
// returns (sql, false, nil) untouched.
//
// Phase 1 strategy:
//   - Path A (≤500 rows assumption based on LIMIT clause): leave the call in-place; the
//     DuckDB worker pool registers the llm extension function so it executes locally.
//   - Path B (>500 rows or no LIMIT): rewrite to a wrapper UDF (`uv_ai_batch`) that the
//     batch executor handles. For now the rewriter still defers to DuckDB llm because
//     wiring the batch executor is a follow-up; the call shape is preserved verbatim.
//
// We deliberately do NOT modify SQL in Phase 1 — but we leave the hook here so Path B
// can be wired without changing callers.
func (r *Rewriter) Rewrite(ctx context.Context, sql, customerSlug string) (string, bool, error) {
	if !ContainsAIGenerate(sql) {
		return sql, false, nil
	}
	path := "B"
	if IsPathA(sql, r.cfg.AIPathARowLimit) {
		path = "A"
	}
	// Path A: the DuckDB llm extension exposes `llm_complete(prompt, model)`. We rewrite
	// `ai_generate(...)` → `llm_complete(...)` with the argument list preserved. Path B
	// is the same function name today; the batch executor (tracked in IMPROVEMENTS) will
	// later swap it for `uv_ai_batch(...)` so external-provider calls amortize across rows.
	rewritten := aiGenerateRe.ReplaceAllString(sql, "llm_complete(")
	r.log.Debug().
		Str("customer", customerSlug).
		Str("path", path).
		Int("path_a_limit", r.cfg.AIPathARowLimit).
		Msg("ai_generate rewritten to llm_complete")
	return rewritten, true, nil
}

// ContainsAIGenerate is true when the SQL references the ai_generate(...) function.
func ContainsAIGenerate(sql string) bool {
	return strings.Contains(strings.ToLower(sql), "ai_generate(")
}
