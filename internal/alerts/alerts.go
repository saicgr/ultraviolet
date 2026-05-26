// Package alerts evaluates freshness, anomaly, schema-change, and DQ rules
// against the catalog + lineage graph. Hits land in audit_log + go to webhook
// destinations (PagerDuty/Slack — wired by the platform team's Phase-4 work).
package alerts

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Rule struct {
	ID         uuid.UUID
	CustomerID uuid.UUID
	RuleType   string // freshness|anomaly|dq|schema_change
	TargetFQN  string
	Config     map[string]any
	Enabled    bool
}

type Engine struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Engine { return &Engine{pool: pool} }

// Evaluate freshness: if any synced_table for customer is staler than the
// configured SLA, return a list of breaches.
type Breach struct {
	Rule      Rule
	Message   string
	FiredAt   time.Time
}

func (e *Engine) EvaluateFreshness(ctx context.Context, customerID uuid.UUID) ([]Breach, error) {
	rows, err := e.pool.Query(ctx,
		`SELECT t.schema_name || '.' || t.table_name, t.last_sync_at, m.sla_minutes
		 FROM synced_tables t
		 LEFT JOIN table_metadata m ON m.fqn = t.schema_name || '.' || t.table_name
		   AND m.customer_id = $1
		 WHERE t.state = 'live'`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Breach
	now := time.Now()
	for rows.Next() {
		var fqn string
		var last *time.Time
		var sla *int
		if err := rows.Scan(&fqn, &last, &sla); err != nil {
			return nil, err
		}
		if sla == nil || last == nil {
			continue
		}
		if now.Sub(*last) > time.Duration(*sla)*time.Minute {
			out = append(out, Breach{
				Rule:    Rule{CustomerID: customerID, RuleType: "freshness", TargetFQN: fqn},
				Message: fmt.Sprintf("table %s is %dm stale; SLA %dm", fqn, int(now.Sub(*last).Minutes()), *sla),
				FiredAt: now,
			})
		}
	}
	return out, rows.Err()
}
