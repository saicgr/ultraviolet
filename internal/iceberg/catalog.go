package iceberg

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
)

// CatalogServer implements a minimal Iceberg REST catalog. Phase 1 supports just enough
// to let DuckDB ATTACH ICEBERG_CATALOG and pyiceberg point at the same metadata.
//
// Endpoints:
//
//	GET  /v1/config
//	GET  /v1/namespaces
//	GET  /v1/namespaces/{ns}/tables
//	GET  /v1/namespaces/{ns}/tables/{table}
type CatalogServer struct {
	log    zerolog.Logger
	prefix string
	tables map[string]TableMetadata // key = "{ns}.{table}"
	// BearerToken, if non-empty, requires `Authorization: Bearer <token>` on every
	// request. Closes the AUDIT.md §iceberg gap "Catalog server has no auth".
	BearerToken string
}

type TableMetadata struct {
	Name     string `json:"name"`
	Location string `json:"location"`
}

func NewCatalogServer(log zerolog.Logger, prefix string) *CatalogServer {
	return &CatalogServer{
		log:    log.With().Str("component", "iceberg-catalog").Logger(),
		prefix: prefix,
		tables: map[string]TableMetadata{},
	}
}

// Register adds/updates a table in the in-memory catalog.
func (c *CatalogServer) Register(ns, name, location string) {
	c.tables[ns+"."+name] = TableMetadata{Name: name, Location: location}
}

func (c *CatalogServer) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(c.bearerAuth)
	r.Get("/v1/config", c.handleConfig)
	r.Get("/v1/namespaces", c.handleNamespaces)
	r.Get("/v1/namespaces/{ns}/tables", c.handleTables)
	r.Get("/v1/namespaces/{ns}/tables/{table}", c.handleTable)
	return r
}

func (c *CatalogServer) bearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c.BearerToken == "" {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("Authorization")
		want := "Bearer " + c.BearerToken
		if got != want {
			w.Header().Set("WWW-Authenticate", `Bearer realm="iceberg-catalog"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (c *CatalogServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"defaults":  map[string]string{},
		"overrides": map[string]string{},
	})
}

func (c *CatalogServer) handleNamespaces(w http.ResponseWriter, r *http.Request) {
	seen := map[string]struct{}{}
	for k := range c.tables {
		ns := strings.SplitN(k, ".", 2)[0]
		seen[ns] = struct{}{}
	}
	out := []map[string]interface{}{}
	for ns := range seen {
		out = append(out, map[string]interface{}{"namespace": []string{ns}})
	}
	writeJSON(w, map[string]interface{}{"namespaces": out})
}

func (c *CatalogServer) handleTables(w http.ResponseWriter, r *http.Request) {
	ns := chi.URLParam(r, "ns")
	out := []map[string]string{}
	for k, t := range c.tables {
		parts := strings.SplitN(k, ".", 2)
		if parts[0] == ns {
			out = append(out, map[string]string{"namespace": ns, "name": t.Name})
		}
	}
	writeJSON(w, map[string]interface{}{"identifiers": out})
}

func (c *CatalogServer) handleTable(w http.ResponseWriter, r *http.Request) {
	ns := chi.URLParam(r, "ns")
	tab := chi.URLParam(r, "table")
	t, ok := c.tables[ns+"."+tab]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]interface{}{
		"metadata-location": fmt.Sprintf("%s/metadata/v1.metadata.json", t.Location),
		"metadata":          map[string]string{"location": t.Location},
	})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
