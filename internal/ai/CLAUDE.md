# `internal/ai/` — `ai_generate()` Rewriter + LLM Client

Canonical: `docs/architecture/ai-rewriter.md`.

## Two execution paths

- **Path A (≤500 rows):** rewrite to DuckDB `llm` extension (`llm_complete(...)`) — runs inside the DuckDB worker. Fastest for small result sets.
- **Path B (>500 rows):** run base query without `ai_generate()`, batch-call OpenAI/Anthropic/Google batch APIs, join results back. Arrow-native batches when called via ADBC (Phase 1.5+).

Detection: pre-flight `COUNT()` on the base query (with the same WHERE/JOINs but no `ai_generate`). Cached for 60s per (customer, normalized SQL).

## Model name mapping

| User SQL | Provider | API |
|---|---|---|
| `'gpt-4o-mini'` / `'gpt-4o'` | OpenAI | chat / batch |
| `'claude-3-haiku'` / `'claude-3-5-sonnet'` / `'claude-opus-4-7'` | Anthropic | messages / batch |
| `'gemini-flash'` | Google | generateContent |

## Invariants

- **Per-customer API keys** stored encrypted (AES-256-GCM) in control-plane DB; injected into DuckDB worker env at runtime.
- **Cost tracker** logs every LLM call (`internal/cost`).
- **No silent failure** — LLM call failure returns proper Postgres error code with provider error message wrapped.
