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
	}

	keyHex := env("ENCRYPTION_KEY", strings.Repeat("0", 64))
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("ENCRYPTION_KEY must be hex: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("ENCRYPTION_KEY must decode to 32 bytes (got %d)", len(key))
	}
	if isAllZero(key) && envBool("UV_PROD", false) {
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
