// Field-level access policy enforcement (Phase 6 Wave 3 go-2). The proxy calls
// Enforce(sql, userRoles, policies) before forwarding a query to the warehouse.
// Two outcomes:
//
//   - blocked=true  → caller returns a Postgres ERROR; query never runs.
//   - blocked=false → use rewritten SQL (which is `sql` with masking applied).
//
// We reuse Apply() (column masking) so there is exactly one SQL-rewrite path.
// Policy.Mode == "deny" short-circuits: any user role that matches the policy
// role triggers a hard block. Mode == "mask" lowers to a MaskingRule.

package security

import "fmt"

// Policy is a per-(column, role) governance rule.
//
//   Mode = "deny" → block the query if any user role intersects with Role.
//   Mode = "mask" → rewrite the SQL via Apply() using Strategy.
//
// Schema/Table may be empty for column-name-only policies (matches everywhere).
type Policy struct {
	Schema   string
	Table    string
	Column   string
	Role     string
	Mode     string // "deny" | "mask"
	Strategy string // for Mode == "mask"; one of null|hash|redact|first_4
}

// Enforce evaluates `policies` against `userRoles` and either blocks the query
// (returns rewritten="", blocked=true, reason="…") or returns the masked SQL.
// Policies with no applicable role match are skipped.
func Enforce(sql string, userRoles []string, policies []Policy) (rewritten string, blocked bool, reason string) {
	if sql == "" {
		return sql, false, ""
	}
	roleSet := make(map[string]struct{}, len(userRoles))
	for _, r := range userRoles {
		roleSet[r] = struct{}{}
	}

	var maskRules []MaskingRule
	for _, p := range policies {
		if _, ok := roleSet[p.Role]; !ok {
			continue
		}
		switch p.Mode {
		case "deny":
			return "", true, fmt.Sprintf("role %s cannot access column %s", p.Role, p.Column)
		case "mask":
			strat := p.Strategy
			if strat == "" {
				strat = "redact"
			}
			maskRules = append(maskRules, MaskingRule{
				Schema:   p.Schema,
				Table:    p.Table,
				Column:   p.Column,
				Role:     p.Role,
				Strategy: strat,
			})
		}
	}

	if len(maskRules) == 0 {
		return sql, false, ""
	}
	return Apply(sql, maskRules), false, ""
}
