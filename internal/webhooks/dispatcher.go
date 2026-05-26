// Package webhooks dispatches outbound HTTPS notifications for customer-
// registered endpoints. Payloads are signed with HMAC-SHA256 using the
// per-endpoint secret so the receiver can verify authenticity. Failures are
// logged but not retried — a durable queue lands in a follow-up.
package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"

	"github.com/ultraviolet-dev/ultraviolet/internal/store"
)

// httpClient is package-level so tests can replace it. Timeout is intentionally
// short — a slow webhook receiver shouldn't pin our goroutines.
var httpClient = &http.Client{Timeout: 5 * time.Second}

type endpointRow struct {
	id     uuid.UUID
	url    string
	secret []byte
}

// Dispatch looks up active webhook_endpoint rows for the given customer that
// subscribe to eventKind, then POSTs the JSON-encoded payload to each. The
// X-UV-Signature header is `sha256=<hex(hmac(secret, body))>`. Errors are
// logged per-endpoint but never returned — best-effort delivery for now.
func Dispatch(ctx context.Context, pool *pgxpool.Pool, enc *store.Encryptor, customerID uuid.UUID, eventKind string, payload any) error {
	rows, err := pool.Query(ctx,
		`SELECT id, url, secret_encrypted
		   FROM webhook_endpoint
		  WHERE customer_id = $1 AND active = true AND $2 = ANY(event_kinds)`,
		customerID, eventKind)
	if err != nil {
		return err
	}
	var eps []endpointRow
	for rows.Next() {
		var ep endpointRow
		var ct []byte
		if err := rows.Scan(&ep.id, &ep.url, &ct); err != nil {
			rows.Close()
			return err
		}
		// secret_encrypted is stored as nonce||ciphertext (first 12 bytes are
		// the GCM nonce). Rows shorter than the nonce prefix predate
		// encryption — skip rather than panic.
		if len(ct) <= 12 {
			log.Warn().Str("endpoint", ep.id.String()).Msg("webhook secret too short, skipping")
			continue
		}
		pt, derr := enc.Decrypt(ct[12:], ct[:12])
		if derr != nil {
			log.Warn().Err(derr).Str("endpoint", ep.id.String()).Msg("webhook secret decrypt")
			continue
		}
		ep.secret = pt
		eps = append(eps, ep)
	}
	rows.Close()

	body, err := json.Marshal(map[string]any{
		"event":     eventKind,
		"customer":  customerID,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"data":      payload,
	})
	if err != nil {
		return err
	}

	for _, ep := range eps {
		mac := hmac.New(sha256.New, ep.secret)
		mac.Write(body)
		sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.url, bytes.NewReader(body))
		if err != nil {
			log.Warn().Err(err).Str("endpoint", ep.id.String()).Msg("webhook build request")
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-UV-Signature", sig)
		req.Header.Set("X-UV-Event", eventKind)
		resp, err := httpClient.Do(req)
		if err != nil {
			log.Warn().Err(err).Str("endpoint", ep.id.String()).Str("url", ep.url).Msg("webhook deliver")
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			log.Warn().Int("status", resp.StatusCode).Str("endpoint", ep.id.String()).Str("url", ep.url).Msg("webhook non-2xx")
			continue
		}
	}
	return nil
}

// SignBody is exported so callers (e.g. tests, agents wanting to compute a
// signature without dispatching) can produce the same header.
func SignBody(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return fmt.Sprintf("sha256=%s", hex.EncodeToString(mac.Sum(nil)))
}
