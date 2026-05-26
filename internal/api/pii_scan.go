// Phase 6 Wave 3 go-1: PII auto-tagger.
//
// POST /api/v1/connections/{id}/pii/scan
//
// Synchronously introspects the connection's tables and applies the regex
// rules exposed by internal/pii against every column name. Rows are upserted
// into pii_tag with confidence = 1.0 (column-name match). Value-sampling is
// intentionally deferred until the Connector interface grows a sample-rows
// helper — running a SELECT through the streaming pgwire.Writer would require
// a fake-writer adapter outside the scope of this handler. We fail loud rather
// than fabricate a 0.5 confidence value-match (no mock data rule).

package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/ultraviolet-dev/ultraviolet/internal/connectors"
	"github.com/ultraviolet-dev/ultraviolet/internal/pii"
)

type taggedColumn struct {
	FQN        string  `json:"fqn"`
	Column     string  `json:"column"`
	Kind       string  `json:"kind"`
	Confidence float64 `json:"confidence"`
}

func (s *Server) piiScanConnection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	connID, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	var customerID uuid.UUID
	var warehouseType string
	if err := s.db.Pool().QueryRow(ctx,
		`SELECT customer_id, warehouse_type FROM connections WHERE id = $1`, connID,
	).Scan(&customerID, &warehouseType); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, fmt.Errorf("connection not found"))
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	factory, err := connectors.NewFactory(s.cfg, s.db, s.enc, s.log)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	conn, err := factory.For(ctx, customerID, warehouseType)
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Errorf("connector: %w", err))
		return
	}
	defer conn.Close()

	intro, ok := conn.(connectors.Introspecter)
	if !ok {
		writeErr(w, http.StatusNotImplemented, fmt.Errorf("connector %q does not support introspection", warehouseType))
		return
	}
	icx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tables, err := intro.Introspect(icx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Errorf("introspect: %w", err))
		return
	}

	tagged := make([]taggedColumn, 0, 32)
	for _, t := range tables {
		fqn := t.Schema + "." + t.Name
		for _, c := range t.Columns {
			tag, conf := pii.ScoreName(c.Name)
			if tag == "" {
				continue
			}
			// Confidence 1.0 per spec for column-name match (overrides pii.go's
			// per-rule NameWeight: the spec is the binding contract here).
			conf = 1.0
			if _, err := s.db.Pool().Exec(ctx,
				`INSERT INTO pii_tag (customer_id, fqn, column_name, kind, confidence)
				 VALUES ($1,$2,$3,$4,$5)
				 ON CONFLICT (customer_id, fqn, column_name, kind)
				 DO UPDATE SET confidence = GREATEST(pii_tag.confidence, EXCLUDED.confidence),
				               detected_at = now()`,
				customerID, fqn, c.Name, string(tag), conf); err != nil {
				writeErr(w, http.StatusInternalServerError, fmt.Errorf("insert pii_tag: %w", err))
				return
			}
			tagged = append(tagged, taggedColumn{
				FQN: fqn, Column: c.Name, Kind: string(tag), Confidence: conf,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"tagged_columns": tagged})
}
