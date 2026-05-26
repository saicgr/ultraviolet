package api

// bu-4 Dashboard PDF export.
//
//   POST /api/v1/dashboards/{id}/pdf
//
// Phase-1 stub. chromedp is NOT in go.mod (checked at write time) so pulling
// it in unconditionally would balloon the dep graph + force a headless-Chrome
// runtime on every operator. This file returns 501 with an explicit message.
//
// To enable real rendering:
//   1. `go get github.com/chromedp/chromedp`
//   2. Add a sibling `pdf_dashboard_chromedp.go` guarded by `//go:build chromedp`
//      that registers a replacement handler — the build tag flips wiring at
//      compile time without changing this file.
//   3. Set UV_PDF_RENDERER=chromedp + rebuild with `go build -tags chromedp ./...`
//
// The build-tagged variant should fetch the dashboard, drive a Chromium
// instance against `/public/dashboards/{token}` (mint a short-lived share
// token internally), and stream the PDF bytes back with Content-Type
// application/pdf + Content-Disposition: attachment; filename="dashboard.pdf".

import (
	"fmt"
	"net/http"
)

func (s *Server) dashboardPDF(w http.ResponseWriter, r *http.Request) {
	if _, err := parseUUID(r, "id"); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeErr(w, http.StatusNotImplemented, fmt.Errorf(
		"PDF renderer disabled — set UV_PDF_RENDERER=chromedp and rebuild with `go build -tags chromedp ./...` (chromedp is not in the default dep set)"))
}
