// Package anomaly — bridge from detector output to notifications.Router (pu-5).
//
// NotifyAnomalies pulls the last 30 days of daily query-cost samples for a
// customer from query_log, runs Detect, and for each flagged point dispatches
// notifications.Router.Send(kind=anomaly). Returns counts so the REST trigger
// can report progress.
package anomaly

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ultraviolet-dev/ultraviolet/internal/notifications"
)

// Result reports the work done by NotifyAnomalies for a single customer.
type Result struct {
	Samples      int `json:"samples"`
	Anomalies    int `json:"anomalies"`
	Notifications int `json:"notifications_sent"`
}

// NotifyAnomalies loads a 30-day cost time-series, runs Detect with a 14-day
// trailing window + z-threshold 3.0, and routes each anomaly through the
// supplied Router. The router writes the in-app inbox row AND fans out to every
// enabled delivery_subscription for the customer.
//
// pool: control-plane DB (query_log lives here).
// router: notifications fanout. Required (nil returns an error — no silent skip).
// customerID: workspace to scan.
func NotifyAnomalies(ctx context.Context, pool *pgxpool.Pool, router *notifications.Router, customerID uuid.UUID) (Result, error) {
	var res Result
	if router == nil {
		return res, fmt.Errorf("anomaly: router nil")
	}
	if pool == nil {
		return res, fmt.Errorf("anomaly: pool nil")
	}
	rows, err := pool.Query(ctx,
		`SELECT date_trunc('day', started_at)::timestamptz AS day,
		        COALESCE(SUM(actual_cost_usd), 0)::float8 AS cost
		 FROM query_log
		 WHERE customer_id = $1 AND started_at > now() - interval '30 days'
		 GROUP BY 1 ORDER BY 1`, customerID)
	if err != nil {
		return res, fmt.Errorf("anomaly query: %w", err)
	}
	defer rows.Close()
	var series []Point
	for rows.Next() {
		var ts time.Time
		var cost float64
		if err := rows.Scan(&ts, &cost); err != nil {
			return res, err
		}
		series = append(series, Point{Timestamp: ts.Unix(), Value: cost})
	}
	if err := rows.Err(); err != nil {
		return res, err
	}
	res.Samples = len(series)

	flagged := Detect(series, 14, 3.0)
	res.Anomalies = len(flagged)

	for _, a := range flagged {
		when := time.Unix(a.Point.Timestamp, 0).UTC().Format("2006-01-02")
		title := fmt.Sprintf("Cost anomaly (%s) on %s", a.Reason, when)
		body := fmt.Sprintf("Daily warehouse cost was $%.2f (z=%.2f)", a.Point.Value, a.Z)
		severity := "warn"
		if a.Reason == "spike" {
			severity = "fatal"
		}
		if err := router.Send(ctx, customerID, notifications.KindAnomaly, notifications.Payload{
			Title:    title,
			Body:     body,
			Link:     "/cost/spend",
			Severity: severity,
			Extra: map[string]any{
				"z_score":   a.Z,
				"reason":    a.Reason,
				"value_usd": a.Point.Value,
				"date":      when,
			},
		}); err != nil {
			// Continue on per-anomaly failure; the inbox row still landed
			// for some channels. Return error count via Result.
			continue
		}
		res.Notifications++
	}
	return res, nil
}
