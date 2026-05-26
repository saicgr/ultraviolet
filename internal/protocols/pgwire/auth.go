package pgwire

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
)

// SessionIdentity is what auth resolves: which customer, which warehouse, which key.
type SessionIdentity struct {
	APIKeyID      uuid.UUID
	CustomerID    uuid.UUID
	CustomerSlug  string
	WarehouseType string // "bigquery" | "snowflake" | "databricks"
}

// ErrAuthFailed means the API key was unknown or revoked.
var ErrAuthFailed = errors.New("authentication failed")

// ErrBadDatabase means the "database" startup parameter wasn't `{customer}_{warehouse}`.
var ErrBadDatabase = errors.New("database must be {customer_slug}_{warehouse_type}")

// ParseDatabase splits "acme_bigquery" → ("acme", "bigquery"). Warehouse type is the
// suffix after the LAST underscore so customer slugs with underscores work.
func ParseDatabase(db string) (slug, warehouse string, err error) {
	idx := strings.LastIndex(db, "_")
	if idx < 1 || idx == len(db)-1 {
		return "", "", ErrBadDatabase
	}
	wh := db[idx+1:]
	switch wh {
	case "bigquery", "snowflake", "databricks":
	default:
		return "", "", fmt.Errorf("%w: unknown warehouse %q", ErrBadDatabase, wh)
	}
	return db[:idx], wh, nil
}

// Authenticator resolves a (apiKey, database) pair to a SessionIdentity using the control-plane.
type Authenticator struct {
	DB *store.DB
}

func (a *Authenticator) Authenticate(ctx context.Context, apiKey, database string) (*SessionIdentity, error) {
	slug, warehouse, err := ParseDatabase(database)
	if err != nil {
		return nil, err
	}
	keyID, customerID, customerSlug, err := a.DB.LookupAPIKey(ctx, apiKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrAuthFailed
		}
		return nil, err
	}
	if !strings.EqualFold(slug, customerSlug) {
		// API key belongs to a different customer than the one in the database string.
		return nil, ErrAuthFailed
	}
	return &SessionIdentity{
		APIKeyID:      keyID,
		CustomerID:    customerID,
		CustomerSlug:  customerSlug,
		WarehouseType: warehouse,
	}, nil
}
