// Package proxy is the QueryHandler that pgwire dispatches to. It owns the lifecycle
// of warehouse connectors + DuckDB worker pool + router + AI rewriter, and decides
// per-query which backend to use.
package proxy

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/ultraviolet-dev/ultraviolet/internal/ai"
	"github.com/ultraviolet-dev/ultraviolet/internal/config"
	"github.com/ultraviolet-dev/ultraviolet/internal/connectors"
	"github.com/ultraviolet-dev/ultraviolet/internal/logger"
	"github.com/ultraviolet-dev/ultraviolet/internal/protocols/pgwire"
	"github.com/ultraviolet-dev/ultraviolet/internal/router"
	"github.com/ultraviolet-dev/ultraviolet/internal/store"
	"github.com/ultraviolet-dev/ultraviolet/internal/workers"
)

// Handler implements pgwire.QueryHandler.
type Handler struct {
	cfg *config.Config
	db  *store.DB
	enc *store.Encryptor
	log zerolog.Logger

	connFactory *connectors.Factory
	pool        *workers.Pool
	rt          *router.Router
	aiRewriter  *ai.Rewriter

	mu              sync.RWMutex
	connsByCustomer map[string]connectors.Connector
}

func New(ctx context.Context, cfg *config.Config, db *store.DB, enc *store.Encryptor, log zerolog.Logger) (*Handler, error) {
	cf, err := connectors.NewFactory(cfg, db, enc, log)
	if err != nil {
		return nil, fmt.Errorf("connector factory: %w", err)
	}
	pool, err := workers.NewPool(ctx, cfg, db, log)
	if err != nil {
		return nil, fmt.Errorf("duckdb pool: %w", err)
	}
	rew, err := ai.NewRewriter(cfg, log)
	if err != nil {
		return nil, fmt.Errorf("ai rewriter: %w", err)
	}
	return &Handler{
		cfg:             cfg,
		db:              db,
		enc:             enc,
		log:             log,
		connFactory:     cf,
		pool:            pool,
		rt:              router.New(db, log),
		aiRewriter:      rew,
		connsByCustomer: map[string]connectors.Connector{},
	}, nil
}

func (h *Handler) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.connsByCustomer {
		_ = c.Close()
	}
	if h.pool != nil {
		h.pool.Close()
	}
}

// HandleSimpleQuery executes Simple Query (Q) protocol.
func (h *Handler) HandleSimpleQuery(ctx context.Context, sess *pgwire.Session, sql string, w *pgwire.Writer) error {
	return h.execute(ctx, sess, sql, nil, w)
}

// HandleExtendedExecute executes Extended Query Execute messages.
func (h *Handler) HandleExtendedExecute(ctx context.Context, sess *pgwire.Session, p *pgwire.Portal, w *pgwire.Writer) error {
	if p == nil || p.Statement == nil {
		return fmt.Errorf("nil portal/statement")
	}
	return h.execute(ctx, sess, p.Statement.SQL, p.ParamValues, w)
}

func (h *Handler) execute(ctx context.Context, sess *pgwire.Session, sql string, params [][]byte, w *pgwire.Writer) error {
	start := time.Now()
	id := sess.Identity
	if id == nil || id.CustomerID == uuid.Nil {
		return fmt.Errorf("no session identity")
	}
	hash := sqlHash(sql)
	normalized := logger.NormalizeSQL(sql)

	// 1. AI-rewriter pass (may transform SQL).
	if h.aiRewriter != nil {
		rewritten, didRewrite, err := h.aiRewriter.Rewrite(ctx, sql, id.CustomerSlug)
		if err != nil {
			return h.respondError(ctx, sess, w, hash, normalized, "ai_rewrite", err, start)
		}
		if didRewrite {
			sql = rewritten
		}
	}

	// 2. Classify + route.
	decision := h.rt.Decide(ctx, id.CustomerID, id.WarehouseType, sql)
	h.log.Debug().
		Str("customer", id.CustomerSlug).
		Str("warehouse", id.WarehouseType).
		Str("route", string(decision.Target)).
		Str("reason", decision.Reason).
		Str("sql", normalized).
		Msg("route decision")

	var (
		rowsReturned int64
		routeUsed    string
		fallback     *string
		execErr      error
	)

	switch decision.Target {
	case router.TargetDuckDB:
		rowsReturned, execErr = h.runOnDuckDB(ctx, id, sql, params, w)
		routeUsed = "duckdb"
		if execErr != nil {
			// Explicit logged fallback per docs/process/no-fallback-data.md.
			h.log.Warn().Err(execErr).Str("customer", id.CustomerSlug).Msg("duckdb error — falling back to warehouse")
			reason := execErr.Error()
			fallback = &reason
			rowsReturned, execErr = h.runOnWarehouse(ctx, id, sql, params, w)
			routeUsed = "fallback"
		}

	case router.TargetWarehouse, router.TargetUnknown:
		rowsReturned, execErr = h.runOnWarehouse(ctx, id, sql, params, w)
		routeUsed = "warehouse"

	default:
		rowsReturned, execErr = h.runOnWarehouse(ctx, id, sql, params, w)
		routeUsed = "warehouse"
	}

	if execErr != nil {
		return h.respondError(ctx, sess, w, hash, normalized, routeUsed, execErr, start)
	}

	dur := int(time.Since(start).Milliseconds())
	apiKeyID := id.APIKeyID
	connID := h.activeConnectionID(ctx, id)
	_ = h.db.InsertQueryLog(ctx, store.QueryLogEntry{
		CustomerID:     id.CustomerID,
		ConnectionID:   connID,
		APIKeyID:       &apiKeyID,
		QueryHash:      hash,
		NormalizedSQL:  normalized,
		RouteDecision:  routeUsed,
		FallbackReason: fallback,
		DurationMS:     dur,
		RowsReturned:   rowsReturned,
	})
	return nil
}

func (h *Handler) respondError(ctx context.Context, sess *pgwire.Session, w *pgwire.Writer, hash, normalized, route string, err error, start time.Time) error {
	dur := int(time.Since(start).Milliseconds())
	id := sess.Identity
	apiKeyID := id.APIKeyID
	code := "XX000"
	_ = w.WriteErrorResponse("ERROR", code, err.Error())
	_ = h.db.InsertQueryLog(ctx, store.QueryLogEntry{
		CustomerID:    id.CustomerID,
		APIKeyID:      &apiKeyID,
		QueryHash:     hash,
		NormalizedSQL: normalized,
		RouteDecision: "error",
		DurationMS:    dur,
		ErrorCode:     &code,
	})
	_ = route
	return nil
}

func (h *Handler) connectorFor(ctx context.Context, id *pgwire.SessionIdentity) (connectors.Connector, error) {
	key := id.CustomerSlug + ":" + id.WarehouseType
	h.mu.RLock()
	c := h.connsByCustomer[key]
	h.mu.RUnlock()
	if c != nil {
		return c, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if c = h.connsByCustomer[key]; c != nil {
		return c, nil
	}
	c, err := h.connFactory.For(ctx, id.CustomerID, id.WarehouseType)
	if err != nil {
		return nil, err
	}
	h.connsByCustomer[key] = c
	return c, nil
}

func (h *Handler) activeConnectionID(ctx context.Context, id *pgwire.SessionIdentity) *uuid.UUID {
	c, err := h.connectorFor(ctx, id)
	if err != nil {
		return nil
	}
	cid := c.ConnectionID()
	return &cid
}

func (h *Handler) runOnWarehouse(ctx context.Context, id *pgwire.SessionIdentity, sql string, params [][]byte, w *pgwire.Writer) (int64, error) {
	c, err := h.connectorFor(ctx, id)
	if err != nil {
		return 0, err
	}
	return c.ExecuteStreaming(ctx, sql, params, w)
}

func (h *Handler) runOnDuckDB(ctx context.Context, id *pgwire.SessionIdentity, sql string, params [][]byte, w *pgwire.Writer) (int64, error) {
	if h.pool == nil {
		return 0, fmt.Errorf("duckdb pool not initialized")
	}
	return h.pool.ExecuteStreaming(ctx, id.CustomerSlug, sql, params, w)
}

func sqlHash(sql string) string {
	s := sha1.Sum([]byte(strings.TrimSpace(sql)))
	return hex.EncodeToString(s[:])
}
