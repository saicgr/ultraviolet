// Package rbac implements workspace + role checks for the Phase-4 enterprise
// surface. Roles: admin, editor, viewer, auditor.
package rbac

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Role string

const (
	RoleAdmin   Role = "admin"
	RoleEditor  Role = "editor"
	RoleViewer  Role = "viewer"
	RoleAuditor Role = "auditor"
)

// canDo encodes the permission matrix. Adding a new action = one row.
var canDo = map[string]map[Role]bool{
	"connection.create":   {RoleAdmin: true, RoleEditor: true},
	"connection.delete":   {RoleAdmin: true},
	"dashboard.create":    {RoleAdmin: true, RoleEditor: true},
	"dashboard.view":      {RoleAdmin: true, RoleEditor: true, RoleViewer: true},
	"audit.read":          {RoleAdmin: true, RoleAuditor: true},
	"workspace.manage":    {RoleAdmin: true},
	"semantic.write":      {RoleAdmin: true, RoleEditor: true},
	"sso.configure":       {RoleAdmin: true},
	"billing.read":        {RoleAdmin: true},
	"reverse_etl.create":  {RoleAdmin: true, RoleEditor: true},
}

type Checker struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Checker { return &Checker{pool: pool} }

var ErrForbidden = errors.New("rbac: forbidden")

// RoleFor returns the user's role in a workspace, or ErrForbidden when the user
// has no membership at all.
func (c *Checker) RoleFor(ctx context.Context, userID, workspaceID uuid.UUID) (Role, error) {
	row := c.pool.QueryRow(ctx,
		`SELECT role FROM workspace_membership WHERE user_id = $1 AND workspace_id = $2`,
		userID, workspaceID)
	var r string
	if err := row.Scan(&r); err != nil {
		return "", ErrForbidden
	}
	return Role(r), nil
}

// Allow returns nil when role can perform action; ErrForbidden otherwise.
func Allow(role Role, action string) error {
	matrix, ok := canDo[action]
	if !ok {
		// Unknown action: deny by default. Forces every new endpoint to register
		// here — closes the "I forgot to add an auth check" failure mode.
		return ErrForbidden
	}
	if matrix[role] {
		return nil
	}
	return ErrForbidden
}
