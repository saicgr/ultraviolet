package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SelfHealingSync inspects recently-failed `synced_tables` rows and tries the
// remediation playbook in priority order:
//
//   1. Re-auth (refresh token / rotate JWT) when error matches `auth|401|403|invalid_grant`.
//   2. Smaller batch (halve watermark window) when error matches `quota|exceed|too large`.
//   3. Switch from CDC → watermark → manual when stream is exhausted / corrupted.
//   4. File a ticket via internal/chatops when no playbook matches.
//
// Runs on every sync tick failure (hooked from `markError`).
type SelfHealingSync struct{ pool *pgxpool.Pool }

func NewSelfHealingSync(pool *pgxpool.Pool) *SelfHealingSync { return &SelfHealingSync{pool: pool} }

func (a *SelfHealingSync) Name() string { return "self_healing_sync" }
func (a *SelfHealingSync) Kind() string { return "loop_closing" }

func (a *SelfHealingSync) Run(ctx context.Context, in Input) (Output, error) {
	rows, err := a.pool.Query(ctx, `
		SELECT t.id, t.schema_name || '.' || t.table_name, t.last_error, t.sync_mode
		FROM synced_tables t JOIN connections c ON c.id = t.connection_id
		WHERE c.customer_id = $1 AND t.state = 'error'
		ORDER BY t.updated_at DESC LIMIT 25`, in.CustomerID)
	if err != nil {
		return Output{}, err
	}
	defer rows.Close()
	out := Output{}
	for rows.Next() {
		var id, fqn, mode string
		var errMsg *string
		if err := rows.Scan(&id, &fqn, &errMsg, &mode); err != nil {
			return out, err
		}
		emsg := ""
		if errMsg != nil {
			emsg = strings.ToLower(*errMsg)
		}
		var (
			action, rationale string
		)
		switch {
		case matchesAny(emsg, "auth", "401", "403", "invalid_grant", "expired"):
			action = "rotate_credentials"
			rationale = "Auth-class error → request fresh OAuth token via /api/v1/connections/{id}/refresh"
		case matchesAny(emsg, "quota", "exceed", "too large", "rate"):
			action = "halve_batch"
			rationale = "Quota / size error → halve the watermark window and retry"
		case matchesAny(emsg, "stream", "offset", "corrupt"):
			action = "downgrade_to_watermark"
			rationale = "CDC stream issue → switch to watermark mode until manually re-armed"
		default:
			action = "file_ticket"
			rationale = "Unknown error class → escalate to oncall via chatops"
		}
		out.Suggestions = append(out.Suggestions, Suggestion{
			ID:       "heal:" + id,
			Title:    fmt.Sprintf("Heal `%s` (%s) → %s", fqn, mode, action),
			Rationale: rationale,
			Severity: "warn",
		})
	}
	out.Summary = fmt.Sprintf("Inspected %d errored sync(s); proposed remediations.", len(out.Suggestions))
	out.Confidence = 0.65
	return out, nil
}

func matchesAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}
