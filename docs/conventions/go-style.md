# Go Style

Defaults: `gofmt`-formatted, `go vet`-clean, `golangci-lint`-clean. This doc captures the conventions on top of those.

## Errors

- **Wrap with `%w`.** `return fmt.Errorf("connectors.bigquery.execute: %w", err)`.
- **Sentinel errors only when callers need to switch.** `errors.Is(err, sql.ErrNoRows)`.
- **Never `panic` in library code.** Top-level proxy goroutine has a `recover()`; nothing else should rely on it.
- **Custom error types only when callers need typed access** (e.g., `*ConnectorError` with a `SQLState() string` method for the PG-wire layer).

```go
// ✅
result, err := conn.Query(ctx, sql)
if err != nil {
    return nil, fmt.Errorf("snowflake.exec: %w", err)
}
```

## Context

- **First parameter** of any function with I/O.
- **Never store contexts in structs.** Pass them.
- **Always check `ctx.Err()` after long-running operations.**
- **Always set deadlines for external calls.** `ctx, cancel := context.WithTimeout(parent, 30*time.Second); defer cancel()`.

## Goroutines

- **Never naked.** Every `go f()` paired with cancellation + cleanup.
- **Use `errgroup.Group` for fan-out.** It propagates errors and respects context.
- **Worker goroutines own their lifecycle.** `Worker.Run(ctx)` blocks until ctx cancels; `Worker.Close()` cancels it.
- **No goroutine leaks.** Tests using `goleak.VerifyNone(t)` in TestMain.

## Channels

- **Buffered when you know the bound.** Otherwise unbuffered.
- **Sender closes** (or no one closes if multiple senders — use a sync.Once gated by ctx done).
- **`select` with `<-ctx.Done()`** in any blocking receive.

## Interfaces

- **Define on consumer side.** `internal/router` defines `Connector` interface; `internal/connectors/bigquery` implements it.
- **Small interfaces.** Prefer 1–3 methods. Compose if needed.
- **Accept interfaces, return concrete types.** Idiomatic Go.

## Logging

`zerolog` (per `conventions/logging.md`). Structured fields, no string interpolation.

```go
log.Info().
    Str("customer", principal.CustomerID).
    Str("route", "duckdb").
    Int64("rows", rowCount).
    Msg("query.completed")
```

## Naming

- **Acronyms keep case.** `httpServer`, `apiKey`, not `HTTPServer`/`APIKey` in field names. Standard Go style: exported types follow PascalCase with full uppercase acronyms (`HTTPServer`); local variables use camelCase with full lowercase (`httpServer`).
- **Constants ALL_CAPS only if a true constant.** Otherwise `MaxPoolSize` (exported) or `maxPoolSize` (unexported).
- **Test files end in `_test.go`.** Test funcs `TestThing_DoesX`.

## File layout per package

```
package_name/
  doc.go        // package doc comment
  types.go      // shared types
  <thing>.go    // primary thing
  <thing>_test.go
  helpers.go    // shared helpers
  errors.go     // sentinel errors / typed errors
```

## What to avoid

- Init functions doing real work — load configs from `cmd/` instead.
- Global state — pass dependencies in.
- `interface{}` / `any` outside of generic helpers — be specific.
- Reflection except in test helpers and serializers.
- `time.Sleep` in tests — use `eventually` patterns or fake clocks.
