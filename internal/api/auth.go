package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type ctxKey string

const ctxUserID ctxKey = "user_id"

// IssueDashboardToken returns a signed JWT for a dashboard user. Dashboard users are
// resolved out-of-band (Phase 1: any caller with the JWT secret can mint; real OAuth/
// SSO arrives in Phase 1.5).
func (s *Server) IssueDashboardToken(userID uuid.UUID, ttl time.Duration) (string, error) {
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": userID.String(),
		"exp": time.Now().Add(ttl).Unix(),
		"iat": time.Now().Unix(),
	})
	return tok.SignedString([]byte(s.cfg.JWTSecret))
}

// authMiddleware enforces a Bearer JWT on every request except /healthz and OPTIONS.
// Phase 1 mode: when UV_API_REQUIRE_AUTH=false (default for dev), middleware no-ops
// so the React frontend can talk to the API without an auth flow yet.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		// bu-3: public share-token routes carry their own auth (the token itself
		// is the bearer). Whitelist the prefix so an Authorization header is not
		// required even when APIRequireAuth is on.
		if strings.HasPrefix(r.URL.Path, "/api/v1/public/") {
			next.ServeHTTP(w, r)
			return
		}
		// GitHub's post-install redirect lands here without a Bearer token; it
		// carries its own ?installation_id and resolves the active customer.
		if r.URL.Path == "/api/v1/github/setup" {
			next.ServeHTTP(w, r)
			return
		}
		// Localization bundle is public (no tenant data) — the frontend fetches it
		// before any auth flow, so don't require a token even when auth is on.
		if strings.HasPrefix(r.URL.Path, "/api/v1/i18n/") {
			next.ServeHTTP(w, r)
			return
		}
		// Dev bypass — local-only escape hatch so the React frontend can talk to
		// the API without an auth flow. HARD-DISABLED in production: config.Load
		// forces APIRequireAuth and refuses weak secrets when UV_PROD=true, and
		// the header is ignored here regardless of what a caller sends.
		if !s.cfg.Prod && r.Header.Get("X-UV-Dev-Bypass") == "1" {
			next.ServeHTTP(w, r)
			return
		}
		hdr := r.Header.Get("Authorization")
		if !strings.HasPrefix(hdr, "Bearer ") {
			if s.cfg.APIRequireAuth {
				writeErr(w, http.StatusUnauthorized, fmt.Errorf("missing bearer token"))
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		raw := strings.TrimPrefix(hdr, "Bearer ")
		uid, err := s.parseToken(raw)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, err)
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserID, uid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) parseToken(raw string) (uuid.UUID, error) {
	tok, err := jwt.Parse(raw, func(t *jwt.Token) (interface{}, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(s.cfg.JWTSecret), nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok || !tok.Valid {
		return uuid.Nil, errors.New("invalid token")
	}
	sub, _ := claims["sub"].(string)
	uid, err := uuid.Parse(sub)
	if err != nil {
		return uuid.Nil, fmt.Errorf("bad sub: %w", err)
	}
	return uid, nil
}

// UserIDFromContext returns the JWT subject if present; uuid.Nil otherwise.
func UserIDFromContext(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(ctxUserID).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}
