// Package logger provides a zerolog wrapper with PII scrubbing.
// Per docs/conventions/logging.md: never log plaintext credentials or raw SQL with literals;
// only normalized SQL is permitted in prod logs.
package logger

import (
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// New returns a configured logger. format = "pretty" or "json".
func New(level, format string) zerolog.Logger {
	zerolog.TimeFieldFormat = time.RFC3339Nano
	lvl, err := zerolog.ParseLevel(strings.ToLower(level))
	if err != nil {
		lvl = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(lvl)

	var out io.Writer = os.Stdout
	if format == "pretty" {
		out = zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	}
	return zerolog.New(out).With().Timestamp().Logger()
}

// Default is a process-global logger ready to use before config loads.
var Default = New("info", "json")

var (
	// Strip API keys / tokens / SQL literals before they hit logs.
	sqlLiteralRE = regexp.MustCompile(`'[^']*'`)
	apiKeyRE     = regexp.MustCompile(`(?i)(api[_-]?key|secret|password|token|bearer)\s*[:=]\s*\S+`)
)

// NormalizeSQL replaces every quoted literal with `?`. Use before logging any SQL in prod.
func NormalizeSQL(sql string) string {
	return sqlLiteralRE.ReplaceAllString(sql, "?")
}

// ScrubSecrets redacts api key / password assignments in arbitrary strings.
func ScrubSecrets(s string) string {
	return apiKeyRE.ReplaceAllString(s, "$1=[REDACTED]")
}
