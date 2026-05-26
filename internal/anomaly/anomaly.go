// Package anomaly detects sudden drops/spikes in metric time-series powered by
// the dashboard layer. Phase-1 algorithm: simple z-score over the trailing 14
// days, flag points where |z| > 3.0. Promote to ESD / Twitter AnomalyDetection
// when fixtures grow.
package anomaly

import "math"

type Point struct {
	Timestamp int64   // unix seconds
	Value     float64
}

type Anomaly struct {
	Point  Point
	Z      float64
	Reason string
}

// Detect returns anomalies in series. minPoints guards against tiny windows.
func Detect(series []Point, minPoints int, threshold float64) []Anomaly {
	if minPoints < 5 {
		minPoints = 14
	}
	if threshold == 0 {
		threshold = 3.0
	}
	if len(series) < minPoints+1 {
		return nil
	}
	var out []Anomaly
	for i := minPoints; i < len(series); i++ {
		window := series[i-minPoints : i]
		mean, stddev := stats(window)
		if stddev == 0 {
			continue
		}
		z := (series[i].Value - mean) / stddev
		if math.Abs(z) > threshold {
			reason := "spike"
			if z < 0 {
				reason = "drop"
			}
			out = append(out, Anomaly{Point: series[i], Z: z, Reason: reason})
		}
	}
	return out
}

func stats(pts []Point) (mean, stddev float64) {
	n := float64(len(pts))
	for _, p := range pts {
		mean += p.Value
	}
	mean /= n
	var ssq float64
	for _, p := range pts {
		d := p.Value - mean
		ssq += d * d
	}
	stddev = math.Sqrt(ssq / n)
	return
}
