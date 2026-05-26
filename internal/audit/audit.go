// Package audit appends immutable rows to audit_log so SOC 2 controls can
// reproduce who did what, when, and from where.
package audit

import (
	"context"
	"encoding/json"
	"net"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Event struct {
	CustomerID *uuid.UUID
	UserID     *uuid.UUID
	Action     string
	TargetType string
	TargetID   string
	Payload    any
	RemoteIP   string
	UserAgent  string
}

type Logger struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Logger { return &Logger{pool: pool} }

func (l *Logger) Log(ctx context.Context, e Event) error {
	var payload []byte
	if e.Payload != nil {
		payload, _ = json.Marshal(e.Payload)
	}
	var ip *net.IP
	if e.RemoteIP != "" {
		host, _, _ := net.SplitHostPort(e.RemoteIP)
		if host == "" {
			host = e.RemoteIP
		}
		if parsed := net.ParseIP(host); parsed != nil {
			ip = &parsed
		}
	}
	_, err := l.pool.Exec(ctx, `
		INSERT INTO audit_log (customer_id, user_id, action, target_type, target_id, payload, remote_ip, user_agent)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		e.CustomerID, e.UserID, e.Action, e.TargetType, e.TargetID, payload, ip, e.UserAgent)
	return err
}
