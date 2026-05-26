// Customer-managed key (CMK) resolution for warehouse-credential encryption.
//
// Today the proxy uses a single global AES-256-GCM key sourced from
// ENCRYPTION_KEY env (see Encryptor in crypto.go). Enterprise customers want
// per-tenant keys backed by their own KMS / Cloud HSM so a key compromise on
// our side never decrypts their warehouse secrets.
//
// This file ships the resolver abstraction so the encryption call sites can be
// migrated incrementally. The default resolver returns the global env-derived
// key for every customer (zero behaviour change). An AWS KMS stub is included
// so the wire-up surface is visible; the actual SDK calls are deliberately
// deferred until the AWS dependency lands.
package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrCMKNotConfigured is returned by resolvers that cannot service a
// customer's CMK request — e.g. AWS KMS resolver when the SDK glue has not yet
// been wired up. Call sites must fail loudly rather than silently fall back to
// the global key (per the no-mock/no-silent-fallback invariant in CLAUDE.md).
var ErrCMKNotConfigured = errors.New("store: customer-managed key not configured")

// KeyResolver returns the 32-byte AES data key to use for encrypting /
// decrypting warehouse credentials for the given customer. Implementations
// must be safe for concurrent use.
type KeyResolver interface {
	ResolveDataKey(ctx context.Context, customerID uuid.UUID) ([]byte, error)
}

// EnvKeyResolver is the default resolver: it returns the same global key
// (typically sourced from ENCRYPTION_KEY) for every customer. This preserves
// today's behaviour while exposing the KeyResolver seam.
type EnvKeyResolver struct {
	Key []byte
}

func NewEnvKeyResolver(key []byte) *EnvKeyResolver {
	return &EnvKeyResolver{Key: key}
}

func (r *EnvKeyResolver) ResolveDataKey(_ context.Context, _ uuid.UUID) ([]byte, error) {
	if len(r.Key) != 32 {
		return nil, ErrCMKNotConfigured
	}
	// Return a defensive copy so callers cannot mutate the cached key.
	out := make([]byte, 32)
	copy(out, r.Key)
	return out, nil
}

// AWSKMSResolver is a stub for the AWS KMS-backed resolver. The ARN should
// reference a symmetric KMS key the customer has shared with our IAM role. On
// ResolveDataKey we would:
//
//  1. Look up the per-customer encrypted data key (EDK) from a control-plane
//     table such as customer_cmk(customer_id, edk_ciphertext, key_arn).
//  2. Call kms:Decrypt with the EDK + the customer-specified ARN. Bound by a
//     5s context deadline and a per-customer rate-limit token.
//  3. Cache the plaintext data key in a process-local LRU keyed by customer_id
//     with a 5-minute TTL to amortise KMS costs without holding plaintext
//     indefinitely.
//
// TODO(ent-8): wire up github.com/aws/aws-sdk-go-v2/service/kms once the
// AWS SDK is on the main go.mod (it is already an indirect dep via S3).
type AWSKMSResolver struct {
	// DefaultKeyARN is the fallback KMS key ARN if a customer row has no
	// override. Empty disables this resolver entirely.
	DefaultKeyARN string

	// LookupARN returns the per-customer override ARN, or "" to fall back to
	// DefaultKeyARN. Optional.
	LookupARN func(ctx context.Context, customerID uuid.UUID) (string, error)
}

func NewAWSKMSResolver(defaultARN string) *AWSKMSResolver {
	return &AWSKMSResolver{DefaultKeyARN: defaultARN}
}

// ResolveDataKey is currently a stub: it returns ErrCMKNotConfigured so call
// sites surface a typed error rather than silently falling back to the global
// key. Replace with real KMS:Decrypt + LRU cache when wiring lands.
func (r *AWSKMSResolver) ResolveDataKey(ctx context.Context, customerID uuid.UUID) ([]byte, error) {
	arn := r.DefaultKeyARN
	if r.LookupARN != nil {
		override, err := r.LookupARN(ctx, customerID)
		if err != nil {
			return nil, err
		}
		if override != "" {
			arn = override
		}
	}
	if arn == "" {
		return nil, ErrCMKNotConfigured
	}
	// TODO(ent-8): kms.Decrypt(ctx, &kms.DecryptInput{KeyId: &arn, CiphertextBlob: edk}).
	return nil, ErrCMKNotConfigured
}

// Compile-time interface conformance checks.
var (
	_ KeyResolver = (*EnvKeyResolver)(nil)
	_ KeyResolver = (*AWSKMSResolver)(nil)
)
