# AI Rewriter — `ai_generate()`

`internal/ai/`. Detects `ai_generate(prompt_expr, model_name)` calls and rewrites for execution.

## Goal

Warehouse-agnostic AI SQL. Same query runs on BigQuery, Snowflake, or Databricks (via Ultraviolet) with identical syntax. Compare:
- BigQuery: `AI.GENERATE_TEXT(MODEL ..., STRUCT(... AS prompt))`
- Snowflake: `SNOWFLAKE.CORTEX.COMPLETE('llama2-70b-chat', text)`
- Databricks: `ai_query('endpoint', text)`

Each is incompatible. Ultraviolet's `ai_generate(prompt, model)` works the same on all.

## Two paths

### Path A — DuckDB `llm` extension (≤500 rows)

For small result sets, rewrite to use DuckDB's community `llm` extension. The whole query runs inside the DuckDB worker.

```sql
-- User SQL
SELECT order_id,
       ai_generate('Classify: ' || complaint, 'gpt-4o-mini') AS sentiment
FROM customer_complaints
LIMIT 100;

-- Rewritten
SELECT order_id,
       llm_complete(complaint, 'Classify: ', 'gpt-4o-mini') AS sentiment
FROM acme_public_customer_complaints
LIMIT 100;
```

DuckDB worker has `llm` extension preloaded with customer's OpenAI/Anthropic API key (decrypted from control-plane DB, injected as session variable).

### Path B — Batch LLM API (>500 rows)

For large result sets, executing per-row via DuckDB-LLM is too slow + expensive. Rewrite to a 2-step:

1. Run the base query without `ai_generate` to get the prompt-input column.
2. Batch-call the LLM provider's batch API (OpenAI Batch, Anthropic Batch).
3. Join results back as a result-set transformation.

```
SELECT order_id, ai_generate(...) FROM ... LIMIT 5000
   ↓
Step 1: SELECT order_id, complaint FROM ... LIMIT 5000  (run on DuckDB)
   ↓
Step 2: build batch JSONL with custom_id=order_id, body=prompt
   ↓
Step 3: POST /batches; poll for completion; download output
   ↓
Step 4: join output by custom_id back into result set; stream to client
```

Batch wait time: target <5 min for OpenAI Batch (50% discount); Anthropic Batch up to 24h (50% discount). For interactive use, use realtime API (no discount, ~per-row latency).

## Path selection (router)

Pre-flight `COUNT(*)` on the base query (with same WHERE/JOIN, no `ai_generate`):
- ≤500 rows → Path A
- 501–5000 rows → Path B realtime (parallel up to 50 concurrent calls)
- >5000 rows → Path B batch API

Cached for 60s per `(customer_id, normalized_sql)` tuple to avoid repeated COUNT.

## Model name mapping

| User SQL token | Provider | Model ID | API |
|---|---|---|---|
| `'gpt-4o-mini'` | OpenAI | `gpt-4o-mini` | chat / batch |
| `'gpt-4o'` | OpenAI | `gpt-4o-2024-08-06` (latest) | chat / batch |
| `'claude-3-haiku'` | Anthropic | `claude-3-haiku-20240307` | messages / batch |
| `'claude-3-5-sonnet'` | Anthropic | `claude-3-5-sonnet-20241022` | messages / batch |
| `'claude-opus-4-7'` | Anthropic | `claude-opus-4-7` | messages / batch |
| `'gemini-flash'` | Google | `gemini-1.5-flash` | generateContent |

Mapping in `internal/ai/models.go`. Add new model IDs without breaking old aliases.

## Cost tracking

Every LLM call logged to `query_log` with `tokens_in`, `tokens_out`, `provider`, `cost_usd`. `internal/cost.LLMCost(provider, model, in, out)` computes from a static rate table updated quarterly.

## Performance — Arrow-native batch (Phase 1.5)

When the protocol surface is ADBC (Phase 1.5), Path B can pull the prompt column as an Arrow `StringArray`, batch via the LLM provider's API, then attach the result column as another Arrow array — zero row-marshal overhead. Phase 1 (PG-wire) does row-oriented; Phase 1.5 graduates to columnar.

## Failure handling (no silent fallback)

- LLM provider 5xx → retry with exponential backoff (3 tries) → on exhaustion, return Postgres error code `08006` `connection_failure` with provider error wrapped.
- LLM provider 4xx (auth, rate limit, content policy) → no retry; return `42501` `insufficient_privilege` (auth) or `53400` `configuration_limit_exceeded` (rate limit) or `22023` `invalid_parameter_value` (content policy).
- DuckDB `llm` extension error → no fallback to direct LLM call; surface error.

## Files

| File | Purpose |
|---|---|
| `rewriter.go` | AST detection of `ai_generate`, Path A rewrite |
| `path_b.go` | Path B orchestration (count → batch → join) |
| `llm_client.go` | Unified provider client (OpenAI / Anthropic / Google) |
| `batch.go` | Batch API integration (OpenAI Batch, Anthropic Batch) |
| `models.go` | Model name → provider/ID mapping |
| `cost_tracker.go` | Per-call cost computation |
