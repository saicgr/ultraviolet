// pu-8 — workspace backup / export.
//
// GET /api/v1/customers/{id}/backup.json dumps a single JSON archive containing
// the customer row and the workspace state we currently consider "user data":
// connections (credentials REDACTED), synced_tables, api_keys (redacted),
// dashboards, semantic_models, saved_queries.
//
// TODO(pu-8 part B): the corresponding restore endpoint
// `POST /api/v1/customers/{id}/restore` is out of scope for this round —
// designing a safe restore needs idempotency keys, conflict resolution, and
// a credentials re-entry flow (we never store plaintext, so backups can't
// fully round-trip). When we get there, mirror the schema below 1:1.
//
// Real rows only — per docs/process/no-fallback-data.md this endpoint must
// surface a 500 instead of silently returning an empty archive.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type workspaceBackupEnvelope struct {
	Version    int                      `json:"version"`
	ExportedAt time.Time                `json:"exported_at"`
	Customer   map[string]any           `json:"customer"`
	Connections   []map[string]any      `json:"connections"`
	SyncedTables  []map[string]any      `json:"synced_tables"`
	APIKeys       []map[string]any      `json:"api_keys"`
	Dashboards    []map[string]any      `json:"dashboards"`
	SemanticModels []map[string]any     `json:"semantic_models"`
	SavedQueries  []map[string]any      `json:"saved_queries"`
}

const redactedSentinel = "***REDACTED***"

func (s *Server) workspaceBackup(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	out := workspaceBackupEnvelope{Version: 1, ExportedAt: time.Now().UTC()}

	// Customer row.
	cust := map[string]any{}
	{
		var cid uuid.UUID
		var slug, dn string
		var createdAt time.Time
		row := s.db.Pool().QueryRow(r.Context(),
			`SELECT id, slug, display_name, created_at FROM customers WHERE id = $1`, id)
		if err := row.Scan(&cid, &slug, &dn, &createdAt); err != nil {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		cust["id"] = cid
		cust["slug"] = slug
		cust["display_name"] = dn
		cust["created_at"] = createdAt
	}
	out.Customer = cust

	// Connections — REDACT the encrypted credentials columns explicitly so the
	// archive cannot be replayed without re-entering them.
	conns, err := s.db.ListConnections(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("list connections: %w", err))
		return
	}
	for _, c := range conns {
		out.Connections = append(out.Connections, map[string]any{
			"id":             c.ID,
			"warehouse_type": c.WarehouseType,
			"name":           c.Name,
			"storage_mode":   c.StorageMode,
			"s3_bucket":      c.S3Bucket,
			"s3_prefix":      c.S3Prefix,
			"credentials":    redactedSentinel,
			"created_at":     c.CreatedAt,
		})

		// Synced tables roll up under each connection but flat-listed here.
		st, err := s.db.ListSyncedTables(r.Context(), c.ID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, fmt.Errorf("list synced: %w", err))
			return
		}
		for _, t := range st {
			out.SyncedTables = append(out.SyncedTables, map[string]any{
				"id":               t.ID,
				"connection_id":    t.ConnectionID,
				"schema_name":      t.SchemaName,
				"table_name":       t.TableName,
				"sync_mode":        t.SyncMode,
				"watermark_column": t.WatermarkColumn,
				"iceberg_path":     t.IcebergPath,
				"state":            t.State,
				"row_count":        t.RowCount,
			})
		}
	}

	// API keys (prefix only; hashes redacted — we never stored plaintext).
	keys, err := s.db.ListAPIKeys(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("list api keys: %w", err))
		return
	}
	for _, k := range keys {
		out.APIKeys = append(out.APIKeys, map[string]any{
			"id":          k.ID,
			"key_prefix":  k.KeyPrefix,
			"description": k.Description,
			"created_at":  k.CreatedAt,
			"revoked_at":  k.RevokedAt,
			"key_hash":    redactedSentinel,
		})
	}

	// Dashboards.
	out.Dashboards, err = dumpRows(r, s, id,
		`SELECT id, name, tiles, is_public, created_at FROM dashboard WHERE customer_id = $1 ORDER BY created_at`,
		[]string{"id", "name", "tiles", "is_public", "created_at"})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("dashboards: %w", err))
		return
	}

	// Semantic models.
	out.SemanticModels, err = dumpRows(r, s, id,
		`SELECT id, name, yaml_source, compiled_sql, version, updated_at FROM semantic_model WHERE customer_id = $1 ORDER BY name`,
		[]string{"id", "name", "yaml_source", "compiled_sql", "version", "updated_at"})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("semantic: %w", err))
		return
	}

	// Saved queries.
	out.SavedQueries, err = dumpRows(r, s, id,
		`SELECT id, name, description, sql, params, favorite, created_at FROM saved_query WHERE customer_id = $1 ORDER BY created_at`,
		[]string{"id", "name", "description", "sql", "params", "favorite", "created_at"})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("saved queries: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="workspace-%s.json"`, id))
	_ = json.NewEncoder(w).Encode(out)
}

// dumpRows is a tiny generic helper: runs SELECT, scans every column as a raw
// value, and zips with `cols` into a map. Returns real rows or an error —
// never a fake / empty slice for connection errors.
func dumpRows(r *http.Request, s *Server, customerID uuid.UUID, sql string, cols []string) ([]map[string]any, error) {
	rows, err := s.db.Pool().Query(r.Context(), sql, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		rec := make(map[string]any, len(cols))
		for i, c := range cols {
			if i >= len(vals) {
				break
			}
			rec[c] = vals[i]
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}
