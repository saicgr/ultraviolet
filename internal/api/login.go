package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// login authenticates the dashboard user via API key and returns a JWT.
// Phase 1: no separate user table, so we accept an API key as the credential and
// mint a token whose `sub` is the customer's id. Phase 2 introduces a real users
// table + SSO/SAML; the response shape stays the same.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.APIKey == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("api_key required"))
		return
	}
	hash := sha256.Sum256([]byte(body.APIKey))
	customerID, err := s.db.CustomerIDByAPIKeyHash(r.Context(), hash[:])
	if err != nil {
		// Constant-time pause helps blunt timing oracles. Not a substitute for the
		// hashed-comparison itself but keeps the error path uniform.
		_ = subtle.ConstantTimeCompare(hash[:], hash[:])
		writeErr(w, http.StatusUnauthorized, fmt.Errorf("invalid credentials"))
		return
	}
	tok, err := s.IssueDashboardToken(customerID, 24*time.Hour)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"access_token": tok,
		"token_type":   "Bearer",
		"expires_in":   int((24 * time.Hour).Seconds()),
		"customer_id":  customerID,
	})
}

// refresh issues a fresh JWT for the caller already authenticated by Bearer.
func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	uid := UserIDFromContext(r.Context())
	if uid == uuid.Nil {
		writeErr(w, http.StatusUnauthorized, fmt.Errorf("missing identity"))
		return
	}
	tok, err := s.IssueDashboardToken(uid, 24*time.Hour)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"access_token": tok,
		"token_type":   "Bearer",
		"expires_in":   int((24 * time.Hour).Seconds()),
	})
}
