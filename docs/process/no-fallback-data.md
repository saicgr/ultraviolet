# No Fallback / Mock Data — Hard Rule

> Never silently degrade. Throw a proper error with full context. Mock-or-fallback paths hide real failures.

## What this means

- **Errors propagate.** `if err != nil { return fmt.Errorf("...: %w", err) }`. Never `return defaultValue`.
- **No mock data in tests where a real integration test is possible.** `bigquery-public-data.*` exists; use it (`testing.md`).
- **No "TODO: handle later" returning nil.** If you can't handle it now, error explicitly + file an `IMPROVEMENTS.md` entry.
- **No "graceful degrade" that hides failure.** A user gets a slow result OR a clear error — never a wrong-but-fast result.

## The DuckDB → warehouse fallback exception (only one)

When a DuckDB worker errors or exceeds 30s, the router falls back to the warehouse. **This is allowed** because:
- The user's request still gets answered.
- The semantic is identical (same SQL, same warehouse → same result).
- The fallback is logged loudly + counted in metrics.

Conditions for this fallback to be acceptable:
1. **Explicit log line:** `routing.fallback.warehouse reason=<...> error=<...>`.
2. **Metric increment:** `uv_router_fallback_total{from="duckdb",to="warehouse",reason=...}`.
3. **Operator alert** if rate exceeds 5%/hr.

If any of those three is missing, it is a silent fallback — a bug — and must be fixed.

## Anti-patterns to reject

```go
// ❌ silent default
if err != nil {
    return []Row{}, nil  // hides the warehouse error
}

// ❌ swallowed retry
for i := 0; i < 3; i++ {
    if data, err = fetch(); err == nil {
        return data, nil
    }
}
return cachedData, nil  // returns stale data without surfacing why

// ❌ mock when real path failed
result, err := connector.Execute(ctx, sql)
if err != nil {
    log.Warn("falling back to mock")
    return mockResult, nil
}
```

```go
// ✅ propagate
if err != nil {
    return nil, fmt.Errorf("connector.Execute: %w", err)
}

// ✅ retry with explicit exhaustion error
for i := 0; i < 3; i++ {
    data, err = fetch()
    if err == nil {
        return data, nil
    }
    log.WithError(err).Warn("fetch.retry attempt=%d", i)
}
return nil, fmt.Errorf("fetch: exhausted %d retries: %w", 3, err)

// ✅ explicit fallback with full instrumentation
result, err := duckdbWorker.Execute(ctx, sql)
if err != nil {
    log.WithError(err).Warn("routing.fallback.warehouse reason=duckdb_error")
    fallbackCounter.WithLabelValues("duckdb_error").Inc()
    return warehouse.Execute(ctx, sql)  // user gets correct result, slower
}
```

## Why

A silent fallback masks the bug that caused it. Bugs compound: a failing DuckDB worker that quietly forwards to warehouse looks fine to the user (slower bills) until the customer notices their bill 3x'd next month — and the dashboard "saved cost" still showed positive, because the route logic lied.

This rule is the user's hardest-stated invariant for Ultraviolet. The same rule from Reppora: "throw `StateError` with context; never silently degrade." Same source: 5 months of FitWiz incidents traced to this exact pattern.
