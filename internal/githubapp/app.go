// Package githubapp implements Ultraviolet's GitHub App: app-JWT → per-installation
// token auth, webhook handling, PR-diff impact analysis, and the GitHub-side
// report (PR comment + Check Run). It replaces the prior single-PAT, single-repo
// bot. All multi-tenant routing flows through installation_id → customer_id; the
// package fails CLOSED on an unknown installation rather than defaulting to a
// placeholder tenant.
package githubapp

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// App mints GitHub App JWTs and exchanges them for per-installation access
// tokens, caching the latter until shortly before expiry.
type App struct {
	appID  int64
	key    *rsa.PrivateKey
	http   *http.Client
	apiURL string

	mu     sync.Mutex
	tokens map[int64]cachedToken
}

type cachedToken struct {
	token   string
	expires time.Time
}

// NewApp parses the RSA private key (PEM) and returns an App. A zero appID or
// empty key yields an error — the caller decides whether GitHub auth is
// required (the bot refuses to boot without it; the API treats it as optional).
func NewApp(appID int64, privateKeyPEM []byte) (*App, error) {
	if appID == 0 || len(privateKeyPEM) == 0 {
		return nil, fmt.Errorf("github app id and private key are required")
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM(privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse github app private key: %w", err)
	}
	return &App{
		appID:  appID,
		key:    key,
		http:   &http.Client{Timeout: 15 * time.Second},
		apiURL: "https://api.github.com",
		tokens: map[int64]cachedToken{},
	}, nil
}

// appJWT mints a short-lived (≤9m) RS256 JWT identifying the App. iat is
// back-dated 60s to tolerate clock skew (GitHub rejects future-dated tokens).
func (a *App) appJWT() (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    strconv.FormatInt(a.appID, 10),
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(a.key)
}

// InstallationToken returns a valid access token for an installation, minting +
// caching a fresh one when the cached token is missing or within 5m of expiry.
// Concurrent callers for the same installation serialize on the mutex so we
// don't stampede GitHub's token endpoint.
func (a *App) InstallationToken(ctx context.Context, installationID int64) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if t, ok := a.tokens[installationID]; ok && time.Until(t.expires) > 5*time.Minute {
		return t.token, nil
	}
	jwtStr, err := a.appJWT()
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", a.apiURL, installationID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	req.Header.Set("Authorization", "Bearer "+jwtStr)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := a.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("installation token http %d: %s", resp.StatusCode, b)
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	a.tokens[installationID] = cachedToken{token: out.Token, expires: out.ExpiresAt}
	return out.Token, nil
}
