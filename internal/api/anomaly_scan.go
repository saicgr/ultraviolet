// Package api — anomaly scan trigger (pu-5).
package api

import (
	"net/http"

	"github.com/ultraviolet-dev/ultraviolet/internal/anomaly"
	"github.com/ultraviolet-dev/ultraviolet/internal/notifications"
)

// scanAnomalies POST /api/v1/customers/{id}/anomalies/scan. Runs the detector
// over the trailing 30-day cost series and routes flagged anomalies through
// notifications.Router. Returns counts (samples / anomalies / sent).
func (s *Server) scanAnomalies(w http.ResponseWriter, r *http.Request) {
	cid, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	smtp, _ := notifications.NewSMTPSenderFromEnv() // nil if SMTP_HOST unset
	router := notifications.NewRouter(s.db.Pool(), smtp, nil)
	res, err := anomaly.NotifyAnomalies(r.Context(), s.db.Pool(), router, cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
