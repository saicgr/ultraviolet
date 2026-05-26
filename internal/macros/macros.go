// Package macros expands dbt-style `{{ ref('NAME') }}` macros in workbench SQL
// to their fully-qualified table names. Resolution is delegated to a caller-
// supplied lookup so this package stays storage-agnostic (callers wire it to
// synced_tables / table_metadata.alias_name).
package macros

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// refRe matches {{ ref('NAME') }} with arbitrary whitespace; single OR double
// quotes; NAME limited to identifier-ish characters.
var refRe = regexp.MustCompile(`\{\{\s*ref\(\s*['"]([A-Za-z_][\w.]*)['"]\s*\)\s*\}\}`)

// Resolve substitutes every `{{ ref('NAME') }}` with its FQN. Returns the
// expanded SQL plus the de-duplicated list of expanded refs. When any ref
// cannot be resolved an error listing every missing name is returned (no
// silent fallback).
func Resolve(sql string, customerID uuid.UUID, fqnLookup func(string) (string, bool)) (string, []string, error) {
	_ = customerID // reserved — callers may key the lookup by customer.
	if fqnLookup == nil {
		return "", nil, fmt.Errorf("macros.Resolve: fqnLookup required")
	}

	missing := []string{}
	seen := map[string]struct{}{}
	resolved := []string{}

	out := refRe.ReplaceAllStringFunc(sql, func(match string) string {
		sub := refRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		name := sub[1]
		fqn, ok := fqnLookup(name)
		if !ok {
			missing = append(missing, name)
			return match
		}
		if _, dup := seen[name]; !dup {
			seen[name] = struct{}{}
			resolved = append(resolved, name+" -> "+fqn)
		}
		return fqn
	})

	if len(missing) > 0 {
		return "", nil, fmt.Errorf("unresolved refs: %s", strings.Join(missing, ", "))
	}
	return out, resolved, nil
}
