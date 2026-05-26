package ai

// Path A keeps `ai_generate(prompt, model)` calls in-place; the DuckDB worker pool
// registers a corresponding scalar function backed by the DuckDB `llm` extension.
// The actual extension load lives in internal/workers; here we just decide to take
// Path A and pass through.

// IsPathA returns true when the SQL is small enough to execute via DuckDB llm scalar
// (LIMIT clause ≤ rowLimit, or no FROM clause meaning a scalar prompt). Heuristic only.
func IsPathA(sql string, rowLimit int) bool {
	return looksScalarOrLimited(sql, rowLimit)
}
