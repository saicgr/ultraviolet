package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Record appends a row to activity_event so the per-customer activity feed
// can surface who-did-what across catalog, sync, dashboards, and queries.
//
//	kind   — namespaced verb, e.g. "dashboard.create", "sync.completed".
//	target — "type:id" reference; split into target_type/target_id columns.
//	payload — arbitrary JSON; marshaled in-place. Pass nil to omit.
//
// summary is derived as "<kind> <target>" for the inbox view; callers that
// need a custom summary should insert directly.
func Record(ctx context.Context, pool *pgxpool.Pool, customerID uuid.UUID, kind, target string, payload any) error {
	if pool == nil {
		return fmt.Errorf("audit.Record: nil pool")
	}
	if kind == "" {
		return fmt.Errorf("audit.Record: kind required")
	}
	var targetType, targetID string
	if i := indexColon(target); i >= 0 {
		targetType = target[:i]
		targetID = target[i+1:]
	} else {
		targetID = target
	}
	var payloadJSON []byte
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("audit.Record: marshal payload: %w", err)
		}
		payloadJSON = b
	}
	summary := kind
	if target != "" {
		summary = kind + " " + target
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO activity_event (customer_id, kind, target_type, target_id, summary, payload)
		VALUES ($1, $2, NULLIF($3,''), NULLIF($4,''), $5, $6)`,
		customerID, kind, targetType, targetID, summary, payloadJSON)
	return err
}

func indexColon(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return i
		}
	}
	return -1
}
