// Package migrate provides static safety checks over a migration SQL blob
// before it's applied. The intent of ops-12 is a guard-rail in CI: any
// migration that drops a column or table fires a structural warning so the
// reviewer is forced to check row counts / data-loss impact first.
//
// This package is a library helper — no endpoint, no DB write side-effects.
// PreflightCheck deliberately does NOT issue COUNT(*) against the target DB
// (which would require migration-runner credentials and a live connection
// to the production replica). The structural warning is sufficient to block
// merge in review.
package migrate

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// PreflightCheck parses `migrationSQL` and returns a list of warnings for
// destructive operations (DROP COLUMN, DROP TABLE). `pool` is accepted for
// forward-compatibility (a future revision may consult pg_stat_user_tables
// to attach row-count context to each warning) but is currently unused.
func PreflightCheck(ctx context.Context, pool *pgxpool.Pool, migrationSQL string) ([]string, error) {
	_ = ctx
	_ = pool

	if migrationSQL == "" {
		return nil, nil
	}

	tree, err := pg_query.Parse(migrationSQL)
	if err != nil {
		return nil, fmt.Errorf("parse migration sql: %w", err)
	}

	var warnings []string
	for _, raw := range tree.GetStmts() {
		stmt := raw.GetStmt()
		if stmt == nil {
			continue
		}

		// ALTER TABLE ... DROP COLUMN
		if alter := stmt.GetAlterTableStmt(); alter != nil {
			rel := alter.GetRelation().GetRelname()
			for _, cmdNode := range alter.GetCmds() {
				cmd := cmdNode.GetAlterTableCmd()
				if cmd == nil {
					continue
				}
				if cmd.GetSubtype() == pg_query.AlterTableType_AT_DropColumn {
					warnings = append(warnings, fmt.Sprintf(
						"ALTER TABLE %s DROP COLUMN %s: destructive op on potentially large table — check row count first",
						rel, cmd.GetName(),
					))
				}
			}
		}

		// DROP TABLE (DropStmt with remove_type == OBJECT_TABLE)
		if drop := stmt.GetDropStmt(); drop != nil {
			if drop.GetRemoveType() == pg_query.ObjectType_OBJECT_TABLE {
				for _, obj := range drop.GetObjects() {
					name := tableNameFromList(obj)
					warnings = append(warnings, fmt.Sprintf(
						"DROP TABLE %s: destructive op on potentially large table — check row count first",
						name,
					))
				}
			}
		}
	}
	return warnings, nil
}

// tableNameFromList walks a DropStmt object node (a list of String nodes) and
// reconstructs a dotted schema.table form for the warning message.
func tableNameFromList(n *pg_query.Node) string {
	list := n.GetList()
	if list == nil {
		return "<unknown>"
	}
	out := ""
	for i, item := range list.GetItems() {
		s := item.GetString_()
		if s == nil {
			continue
		}
		if i > 0 {
			out += "."
		}
		out += s.GetSval()
	}
	if out == "" {
		return "<unknown>"
	}
	return out
}
