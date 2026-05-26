// Package branding centralizes the product display name so the project can be
// rebranded by changing one file (or one env var) instead of grepping the tree.
//
// Scope: USER-VISIBLE strings only — UI headers, OpenAPI title, PG ParameterStatus
// server_version, log app-name. Identifiers that form stable contracts are
// intentionally NOT routed through this package and stay as-is:
//
//   - Go module path (`github.com/ultraviolet-dev/ultraviolet`) — renaming requires
//     a coordinated module rename, not a string swap.
//   - Env-var prefix `UV_` — customer config + ops runbooks depend on it.
//   - DuckDB ATTACH names (`uv_iceberg`), SQL comment markers (`--uv-query-hash:`),
//     and Snowflake stream names (`Ultraviolet_stream_*`) — these are wire / data
//     contracts. Changing them silently breaks running customers.
//   - File system paths under `s3://...-data/` — kept stable for installed deployments.
//
// To rebrand: set UV_BRAND_NAME (and optionally UV_BRAND_TAGLINE / UV_BRAND_DOMAIN)
// at startup, or edit the defaults in this file.
package branding

import "os"

// Defaults are the compile-time fallback. Override at runtime via env vars.
const (
	defaultName    = "Ultraviolet"
	defaultTagline = "The query cache for your warehouse."
	defaultDomain  = "ultraviolet.dev"
)

// Name is the product's display name (e.g. "Ultraviolet"). Used in UI headers,
// OpenAPI titles, and log app-name fields.
func Name() string {
	if v := os.Getenv("UV_BRAND_NAME"); v != "" {
		return v
	}
	return defaultName
}

// Tagline is a one-line caption shown on marketing surfaces and CLI banners.
func Tagline() string {
	if v := os.Getenv("UV_BRAND_TAGLINE"); v != "" {
		return v
	}
	return defaultTagline
}

// Domain is the canonical product domain (e.g. used to template default
// connection strings shown in the UI).
func Domain() string {
	if v := os.Getenv("UV_BRAND_DOMAIN"); v != "" {
		return v
	}
	return defaultDomain
}

// ServerVersion is the value advertised in the Postgres ParameterStatus
// `server_version` field. Format: "16.0 (<Brand>)" — keeping the PG version
// prefix so clients that probe it (dbt, JDBC drivers) still match correctly.
func ServerVersion() string {
	return "16.0 (" + Name() + ")"
}
