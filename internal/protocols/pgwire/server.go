package pgwire

// server.go is reserved for top-level server setup helpers; the listener +
// connection loop lives in session.go. Kept as a separate file so future
// additions (TLS-cert reload, conn-limit middleware, metrics interceptors)
// don't bloat session.go past the 1000-line ceiling per docs/process/file-size-limits.md.
