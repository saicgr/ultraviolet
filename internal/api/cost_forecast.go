// Package api — fo-2: Cost forecast.
//
// GET /api/v1/customers/{id}/cost/forecast?days=30
//
// Pulls daily sum(actual_cost_usd) over the last 60 days, fits a plain
// least-squares line, projects forward `days` days, and emits a ±2σ band
// from the residual standard deviation. No external stats lib.
//
// Response: {forecast_usd, confidence_low, confidence_high, method, samples}
// where forecast_usd is the total projected cost across the forecast horizon
// (sum of per-day predictions), and the band is computed at horizon end.
//
// If there are fewer than 2 daily samples we return zeros + method=insufficient.
package api

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"
)

type forecastResp struct {
	ForecastUSD     float64 `json:"forecast_usd"`
	ConfidenceLow   float64 `json:"confidence_low"`
	ConfidenceHigh  float64 `json:"confidence_high"`
	Method          string  `json:"method"`
	Samples         int     `json:"samples"`
}

func (s *Server) costForecast(w http.ResponseWriter, r *http.Request) {
	cid, err := parseUUID(r, "id")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 {
		days = 30
	}
	if days > 365 {
		days = 365
	}

	// Pull 60d of daily cost. date_trunc('day', started_at) avoids fractional days.
	const q = `
		SELECT date_trunc('day', started_at) AS d,
		       COALESCE(SUM(actual_cost_usd), 0)::float8 AS cost
		  FROM query_log
		 WHERE customer_id = $1
		   AND started_at >= now() - interval '60 days'
		   AND actual_cost_usd IS NOT NULL
		 GROUP BY d
		 ORDER BY d ASC`
	rows, err := s.db.Pool().Query(r.Context(), q, cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	var xs, ys []float64
	var t0 time.Time
	idx := 0
	for rows.Next() {
		var d time.Time
		var cost float64
		if err := rows.Scan(&d, &cost); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if idx == 0 {
			t0 = d
		}
		x := d.Sub(t0).Hours() / 24.0
		xs = append(xs, x)
		ys = append(ys, cost)
		idx++
	}
	if err := rows.Err(); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	if len(xs) < 2 {
		writeJSON(w, http.StatusOK, forecastResp{
			ForecastUSD: 0, ConfidenceLow: 0, ConfidenceHigh: 0,
			Method: "insufficient", Samples: len(xs),
		})
		return
	}

	slope, intercept, residStd := linearRegression(xs, ys)

	// Project `days` ahead from the last observed x. Forecast total is the
	// sum of per-day predictions over the horizon, clamped at zero (costs
	// cannot be negative even if the slope is sharply down).
	lastX := xs[len(xs)-1]
	var forecastTotal float64
	for i := 1; i <= days; i++ {
		yhat := slope*(lastX+float64(i)) + intercept
		if yhat < 0 {
			yhat = 0
		}
		forecastTotal += yhat
	}
	// Band: forecast_total ± 2σ * sqrt(days). Sum of `days` independent
	// residuals has stddev σ√n under the iid assumption.
	band := 2.0 * residStd * math.Sqrt(float64(days))
	low := forecastTotal - band
	if low < 0 {
		low = 0
	}
	high := forecastTotal + band

	writeJSON(w, http.StatusOK, forecastResp{
		ForecastUSD:    forecastTotal,
		ConfidenceLow:  low,
		ConfidenceHigh: high,
		Method:         fmt.Sprintf("ols-linear-60d"),
		Samples:        len(xs),
	})
}

// linearRegression fits y = slope*x + intercept by ordinary least squares
// and returns the residual standard deviation (sample stddev, n-2 dof).
// Pure arithmetic, no external dependency.
func linearRegression(xs, ys []float64) (slope, intercept, residStd float64) {
	n := len(xs)
	if n == 0 || n != len(ys) {
		return 0, 0, 0
	}
	var sx, sy, sxx, sxy float64
	for i := 0; i < n; i++ {
		sx += xs[i]
		sy += ys[i]
		sxx += xs[i] * xs[i]
		sxy += xs[i] * ys[i]
	}
	fn := float64(n)
	denom := fn*sxx - sx*sx
	if denom == 0 {
		// All x equal: degenerate; fall back to mean.
		intercept = sy / fn
		return 0, intercept, 0
	}
	slope = (fn*sxy - sx*sy) / denom
	intercept = (sy - slope*sx) / fn

	// Residual stddev with n-2 dof; n==2 → 0.
	if n < 3 {
		return slope, intercept, 0
	}
	var ssr float64
	for i := 0; i < n; i++ {
		r := ys[i] - (slope*xs[i] + intercept)
		ssr += r * r
	}
	residStd = math.Sqrt(ssr / float64(n-2))
	return slope, intercept, residStd
}
