// Package sharing mints + validates public share tokens for dashboards / saved
// queries. A share URL looks like:
//
//	https://app.ultraviolet.dev/public/dashboard/<token>
//
// On render the frontend hits /api/v1/public/{token} which returns the
// dashboard layout + tile rows already-resolved (no auth required, RBAC
// bypassed because the token *is* the auth). Tokens are revocable + can carry
// an optional expiry.
package sharing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Token struct {
	Token      string    `json:"token"`
	CustomerID uuid.UUID `json:"customer_id"`
	TargetType string    `json:"target_type"` // "dashboard" | "saved_query"
	TargetID   uuid.UUID `json:"target_id"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

var ErrTokenInvalid = errors.New("share token: invalid or revoked")

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Mint generates a fresh 24-byte hex token + persists it.
func (s *Store) Mint(ctx context.Context, t Token, ttl time.Duration) (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(buf)
	var expiresAt *time.Time
	if ttl > 0 {
		ex := time.Now().Add(ttl)
		expiresAt = &ex
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO share_token (token, customer_id, target_type, target_id, expires_at)
		VALUES ($1,$2,$3,$4,$5)`,
		tok, t.CustomerID, t.TargetType, t.TargetID, expiresAt)
	return tok, err
}

func (s *Store) Resolve(ctx context.Context, token string) (Token, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT token, customer_id, target_type, target_id, expires_at
		 FROM share_token
		 WHERE token = $1 AND revoked_at IS NULL
		   AND (expires_at IS NULL OR expires_at > now())`,
		token)
	var t Token
	if err := row.Scan(&t.Token, &t.CustomerID, &t.TargetType, &t.TargetID, &t.ExpiresAt); err != nil {
		return Token{}, ErrTokenInvalid
	}
	return t, nil
}

func (s *Store) Revoke(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `UPDATE share_token SET revoked_at = now() WHERE token = $1`, token)
	return err
}
