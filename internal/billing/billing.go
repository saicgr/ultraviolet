// Package billing emits usage events (queries, dashboard renders, AI tokens,
// storage GB-month) that downstream feed Stripe metered billing. Phase 1 stores
// to usage_event; Phase 2 adds a Stripe reporter that batches events to the
// /v1/billing/meter_events endpoint.
package billing

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EventType string

const (
	EventQuery           EventType = "query"
	EventDashboardRender EventType = "dashboard_render"
	EventAIToken         EventType = "ai_token"
	EventStorageGBMonth  EventType = "storage_gb_month"
	EventReverseETLRow   EventType = "reverse_etl_row"
)

type Recorder struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Recorder { return &Recorder{pool: pool} }

func (r *Recorder) Record(ctx context.Context, customerID uuid.UUID, t EventType, qty float64, meta map[string]any) error {
	var payload []byte
	if meta != nil {
		payload, _ = json.Marshal(meta)
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO usage_event (customer_id, event_type, quantity, metadata) VALUES ($1,$2,$3,$4)`,
		customerID, string(t), qty, payload)
	return err
}

// Plan is the subscription tier used by feature flags + soft caps.
type Plan string

const (
	PlanFree       Plan = "free"
	PlanTeam       Plan = "team"
	PlanBusiness   Plan = "business"
	PlanEnterprise Plan = "enterprise"
)

// FeatureFlags returns which subsystems are unlocked for a plan tier.
func FeatureFlags(p Plan) map[string]bool {
	base := map[string]bool{"queries": true, "savings": true, "lineage": false, "dashboards": false, "sso": false, "audit_log": false}
	switch p {
	case PlanTeam:
		base["lineage"] = true
		base["dashboards"] = true
	case PlanBusiness:
		base["lineage"] = true
		base["dashboards"] = true
		base["audit_log"] = true
	case PlanEnterprise:
		for k := range base {
			base[k] = true
		}
	}
	return base
}
