// Package config loads Ultraviolet configuration from environment variables.
// Loaded once at process start; never mutated.
package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ProxyPort       int
	APIPort         int
	DatabaseURL     string
	RedisURL        string
	S3Endpoint      string
	S3Bucket        string
	AWSRegion       string
	AWSAccessKey    string
	AWSSecretKey    string
	EncryptionKey   []byte
	JWTSecret       string
	LogLevel        string
	LogFormat       string
	DevTLS          bool
	GCPProject      string
	GCPCredsPath    string
	OpenAIKey       string
	AnthropicKey    string
	AIDefaultModel  string
	AIPathARowLimit int
	QueryTimeout    time.Duration

	APIRequireAuth    bool
	MigrationsSource  string
	TLSCertPath       string
	TLSKeyPath        string
	IcebergCatalogURL  string
	APIRateLimitRPS    int
	AICallsPerCustomer int
	SnowflakeUSDPerTiB float64
	BigQueryUSDPerTiB  float64
	Prod               bool // UV_PROD=true disables all dev backdoors (auth bypass, weak secrets)

	// GitHub App (W2). PrivateKeyPEM is read from UV_GITHUB_PRIVATE_KEY or, if
	// that is empty, the file at UV_GITHUB_PRIVATE_KEY_PATH.
	GitHubAppID            int64
	GitHubAppSlug          string // for the install URL: github.com/apps/<slug>/installations/new
	GitHubPrivateKeyPEM    []byte
	GitHubWebhookSecret    string
	GitHubSetupRedirectURL string
	LineageBotPort         int
}

func Load() (*Config, error) {
	c := &Config{
		ProxyPort:         envInt("UV_PROXY_PORT", 5000),
		APIPort:           envInt("UV_API_PORT", 8080),
		DatabaseURL:       env("DATABASE_URL", "postgres://uv:uv@localhost:5432/uv?sslmode=disable"),
		RedisURL:          env("REDIS_URL", "redis://localhost:6379/0"),
		S3Endpoint:        env("S3_ENDPOINT", "http://localhost:4566"),
		S3Bucket:          env("S3_BUCKET", "uv-data-dev"),
		AWSRegion:         env("AWS_REGION", "us-east-1"),
		AWSAccessKey:      os.Getenv("AWS_ACCESS_KEY_ID"),
		AWSSecretKey:      os.Getenv("AWS_SECRET_ACCESS_KEY"),
		JWTSecret:         env("JWT_SECRET", "dev-jwt-secret-change-me"),
		LogLevel:          env("UV_LOG_LEVEL", "info"),
		LogFormat:         env("UV_LOG_FORMAT", "json"),
		DevTLS:            envBool("UV_DEV_TLS", false),
		GCPProject:        os.Getenv("GCP_PROJECT"),
		GCPCredsPath:      os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"),
		OpenAIKey:         os.Getenv("OPENAI_API_KEY"),
		AnthropicKey:      os.Getenv("ANTHROPIC_API_KEY"),
		AIDefaultModel:    env("UV_AI_DEFAULT_MODEL", "gpt-4o-mini"),
		AIPathARowLimit:   envInt("UV_AI_PATH_A_ROW_LIMIT", 500),
		QueryTimeout:      envDuration("UV_QUERY_TIMEOUT", 30*time.Second),
		APIRequireAuth:    envBool("UV_API_REQUIRE_AUTH", false),
		MigrationsSource:  env("UV_MIGRATIONS_SOURCE", "file://./migrations"),
		TLSCertPath:       os.Getenv("TLS_CERT_PATH"),
		TLSKeyPath:        os.Getenv("TLS_KEY_PATH"),
		IcebergCatalogURL:  os.Getenv("UV_ICEBERG_CATALOG_URL"),
		APIRateLimitRPS:    envInt("UV_API_RATE_LIMIT_RPS", 100),
		AICallsPerCustomer: envInt("UV_AI_CALLS_PER_CUSTOMER", 1000),
		SnowflakeUSDPerTiB: envFloat("UV_SF_USD_PER_TIB", 5.0),
		BigQueryUSDPerTiB:  envFloat("UV_BQ_USD_PER_TIB", 6.25),
		Prod:               envBool("UV_PROD", false),

		GitHubAppID:            int64(envInt("UV_GITHUB_APP_ID", 0)),
		GitHubAppSlug:          os.Getenv("UV_GITHUB_APP_SLUG"),
		GitHubWebhookSecret:    os.Getenv("UV_GITHUB_WEBHOOK_SECRET"),
		GitHubSetupRedirectURL: env("UV_GITHUB_SETUP_REDIRECT_URL", "http://localhost:5173/github"),
		LineageBotPort:         envInt("UV_LINEAGE_BOT_PORT", 8090),
	}
	if pem := os.Getenv("UV_GITHUB_PRIVATE_KEY"); pem != "" {
		c.GitHubPrivateKeyPEM = []byte(pem)
	} else if path := os.Getenv("UV_GITHUB_PRIVATE_KEY_PATH"); path != "" {
		if b, err := os.ReadFile(path); err == nil {
			c.GitHubPrivateKeyPEM = b
		}
	}

	// Production must never run with dev backdoors. Fail closed at boot.
	if c.Prod {
		c.APIRequireAuth = true // the X-UV-Dev-Bypass header is also ignored in api.authMiddleware when Prod
		if c.JWTSecret == "dev-jwt-secret-change-me" {
			return nil, fmt.Errorf("JWT_SECRET is the dev default; refusing to boot with UV_PROD=true")
		}
	}

	keyHex := env("ENCRYPTION_KEY", strings.Repeat("0", 64))
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("ENCRYPTION_KEY must be hex: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("ENCRYPTION_KEY must decode to 32 bytes (got %d)", len(key))
	}
	if isAllZero(key) && c.Prod {
		return nil, fmt.Errorf("ENCRYPTION_KEY is the all-zero default; refusing to boot with UV_PROD=true")
	}
	c.EncryptionKey = key
	return c, nil
}

func isAllZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	}
	return def
}

func envFloat(k string, def float64) float64 {
	if v := os.Getenv(k); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func envDuration(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
