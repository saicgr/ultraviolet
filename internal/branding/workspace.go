// Per-workspace branding overrides (bu-11).
//
// Each workspace may override the product-wide branding (name, tagline, logo,
// primary color) by inserting a row in the `workspace_branding` table. When
// no row exists we fall back to the env-driven defaults in branding.go so a
// fresh workspace inherits the global look automatically.
package branding

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WorkspaceBranding mirrors the workspace_branding table. Fields use pointer
// types so we can distinguish "unset" (fall back to default) from "set empty
// string" once the UI starts supporting that.
type WorkspaceBranding struct {
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Name        string    `json:"name"`
	Tagline     string    `json:"tagline"`
	LogoURL     string    `json:"logo_url"`
	PrimaryHex  string    `json:"primary_hex"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// LoadForWorkspace returns the row for the given workspace, or a default-filled
// WorkspaceBranding (using env-driven Name/Tagline) when no row exists. Returns
// a non-nil error only on real DB failures — a missing row is not an error.
func LoadForWorkspace(ctx context.Context, pool *pgxpool.Pool, workspaceID uuid.UUID) (WorkspaceBranding, error) {
	var b WorkspaceBranding
	var name, tagline, logo, hex *string
	err := pool.QueryRow(ctx,
		`SELECT workspace_id, name, tagline, logo_url, primary_hex, updated_at
		 FROM workspace_branding WHERE workspace_id = $1`,
		workspaceID).Scan(&b.WorkspaceID, &name, &tagline, &logo, &hex, &b.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return WorkspaceBranding{
				WorkspaceID: workspaceID,
				Name:        Name(),
				Tagline:     Tagline(),
				PrimaryHex:  "#7c3aed", // sensible Ultraviolet default
				UpdatedAt:   time.Time{},
			}, nil
		}
		return WorkspaceBranding{}, err
	}
	// Fill in defaults for any null columns so callers can render without nil checks.
	if name != nil {
		b.Name = *name
	} else {
		b.Name = Name()
	}
	if tagline != nil {
		b.Tagline = *tagline
	} else {
		b.Tagline = Tagline()
	}
	if logo != nil {
		b.LogoURL = *logo
	}
	if hex != nil {
		b.PrimaryHex = *hex
	} else {
		b.PrimaryHex = "#7c3aed"
	}
	return b, nil
}

// UpsertForWorkspace writes (or replaces) the branding row for a workspace.
// Pass empty strings to clear a field; the row is still upserted so the
// updated_at timestamp moves forward.
func UpsertForWorkspace(ctx context.Context, pool *pgxpool.Pool, b WorkspaceBranding) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO workspace_branding (workspace_id, name, tagline, logo_url, primary_hex, updated_at)
		VALUES ($1, NULLIF($2,''), NULLIF($3,''), NULLIF($4,''), NULLIF($5,''), now())
		ON CONFLICT (workspace_id) DO UPDATE SET
			name        = EXCLUDED.name,
			tagline     = EXCLUDED.tagline,
			logo_url    = EXCLUDED.logo_url,
			primary_hex = EXCLUDED.primary_hex,
			updated_at  = now()`,
		b.WorkspaceID, b.Name, b.Tagline, b.LogoURL, b.PrimaryHex)
	return err
}
