package pgwire

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

// SCRAM-SHA-256 server-side authenticator (RFC 5802) used by the pgwire AuthSASL
// path. Phase-1 stores per-API-key salts in api_keys.scram_salt + iter_count
// when a client opts into SCRAM (prefer-modern-auth flow); when not provisioned,
// the proxy falls back to AuthCleartextPassword over TLS.
//
// We deliberately keep the implementation in pgwire/ — the wire-level message
// shapes (`AuthenticationSASL`, `AuthenticationSASLContinue`, `SASLResponse`)
// are tightly coupled to the FE/BE message framing.

const (
	scramMechanism = "SCRAM-SHA-256"
	scramIterCount = 4096
)

// ScramVerifier is what the proxy stores per API key (instead of the cleartext key).
// Customers POST their key once at provisioning time and we keep only this.
type ScramVerifier struct {
	Salt       []byte
	IterCount  int
	StoredKey  []byte
	ServerKey  []byte
}

// NewScramVerifier derives a verifier from a cleartext password using PBKDF2.
func NewScramVerifier(password string) (*ScramVerifier, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	saltedPassword := pbkdf2.Key([]byte(password), salt, scramIterCount, 32, sha256.New)
	clientKey := hmacSHA256(saltedPassword, []byte("Client Key"))
	storedKey := sha256Sum(clientKey)
	serverKey := hmacSHA256(saltedPassword, []byte("Server Key"))
	return &ScramVerifier{
		Salt:      salt,
		IterCount: scramIterCount,
		StoredKey: storedKey,
		ServerKey: serverKey,
	}, nil
}

// ScramExchange holds the server-side state across the two SASL message rounds.
type ScramExchange struct {
	verifier      *ScramVerifier
	clientNonce   string
	serverNonce   string
	clientFirst   string // bare (n=user,r=...)
	serverFirst   string
}

func NewScramExchange(v *ScramVerifier) *ScramExchange { return &ScramExchange{verifier: v} }

// ServerFirst processes the client's initial SASLInitialResponse and returns
// the server-first message. The mechanism is enforced; only SCRAM-SHA-256 supported.
func (e *ScramExchange) ServerFirst(initialClientFirstWithMechanism string) (string, error) {
	// Body shape: `n,,n=user,r=clientNonce`
	parts := strings.SplitN(initialClientFirstWithMechanism, ",", 4)
	if len(parts) < 4 {
		return "", fmt.Errorf("scram: malformed client-first")
	}
	bare := parts[2] + "," + parts[3] // n=user,r=...
	for _, kv := range strings.Split(bare, ",") {
		if strings.HasPrefix(kv, "r=") {
			e.clientNonce = strings.TrimPrefix(kv, "r=")
		}
	}
	if e.clientNonce == "" {
		return "", fmt.Errorf("scram: missing client nonce")
	}
	srvBytes := make([]byte, 18)
	if _, err := rand.Read(srvBytes); err != nil {
		return "", err
	}
	e.serverNonce = base64.StdEncoding.EncodeToString(srvBytes)
	e.clientFirst = bare
	e.serverFirst = fmt.Sprintf("r=%s%s,s=%s,i=%d",
		e.clientNonce, e.serverNonce,
		base64.StdEncoding.EncodeToString(e.verifier.Salt),
		e.verifier.IterCount)
	return e.serverFirst, nil
}

// ServerFinal validates the client-final-message and returns the server-final.
// Returns error when the proof doesn't match (= bad password).
func (e *ScramExchange) ServerFinal(clientFinal string) (string, error) {
	parts := map[string]string{}
	for _, kv := range strings.Split(clientFinal, ",") {
		if i := strings.Index(kv, "="); i > 0 {
			parts[kv[:i]] = kv[i+1:]
		}
	}
	clientProofB64, ok := parts["p"]
	if !ok {
		return "", fmt.Errorf("scram: missing client proof")
	}
	clientProof, err := base64.StdEncoding.DecodeString(clientProofB64)
	if err != nil {
		return "", err
	}
	channelBinding := parts["c"]
	clientNonce := parts["r"]
	withoutProof := fmt.Sprintf("c=%s,r=%s", channelBinding, clientNonce)
	authMessage := e.clientFirst + "," + e.serverFirst + "," + withoutProof
	clientSignature := hmacSHA256(e.verifier.StoredKey, []byte(authMessage))
	clientKey := xor(clientProof, clientSignature)
	storedKeyDerived := sha256Sum(clientKey)
	if subtle.ConstantTimeCompare(storedKeyDerived, e.verifier.StoredKey) != 1 {
		return "", fmt.Errorf("scram: bad password")
	}
	serverSignature := hmacSHA256(e.verifier.ServerKey, []byte(authMessage))
	return "v=" + base64.StdEncoding.EncodeToString(serverSignature), nil
}

func hmacSHA256(key, msg []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(msg)
	return h.Sum(nil)
}

func sha256Sum(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

func xor(a, b []byte) []byte {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = a[i] ^ b[i]
	}
	return out
}
