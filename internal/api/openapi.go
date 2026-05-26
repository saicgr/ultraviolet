package api

import (
	"bytes"
	_ "embed"
	"net/http"
	"sync"

	"github.com/ultraviolet-dev/ultraviolet/internal/branding"
)

//go:embed openapi.yaml
var openAPIYAML []byte

var (
	renderedOnce sync.Once
	rendered     []byte
)

// renderOpenAPI substitutes branding placeholders in the embedded YAML.
// Cached on first call — branding is read once at process start.
func renderOpenAPI() []byte {
	renderedOnce.Do(func() {
		rendered = bytes.ReplaceAll(openAPIYAML, []byte("{{BRAND_NAME}}"), []byte(branding.Name()))
	})
	return rendered
}

// ServeOpenAPI serves the static OpenAPI spec at /openapi.yaml. Frontend OpenAPI
// codegen (Phase 1.5) consumes this directly.
func (s *Server) ServeOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(renderOpenAPI())
}
