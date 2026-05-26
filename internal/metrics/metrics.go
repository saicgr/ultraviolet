// Package metrics is a stdlib-only counter/gauge surface that doubles as a
// minimal Prometheus exposition. Exposed via /metrics on the API binary's
// health port (and on the proxy/sync health endpoints).
//
// The audit (§observability) flagged that no Prometheus metrics existed.
// We deliberately do NOT pull in prometheus/client_golang here — the dep tree
// is heavy for the four counters we actually emit today. Promote when more
// than ~10 metrics land or when histograms are needed.
package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
)

type counter struct {
	value atomic.Int64
}

var (
	mu       sync.RWMutex
	counters = map[string]*counter{}
)

// Inc bumps a labeled counter. Labels are encoded into the Prometheus name as
// `metric{label="value"}` on render so any scraper can read them.
func Inc(name string, labels map[string]string) {
	IncBy(name, labels, 1)
}

func IncBy(name string, labels map[string]string, n int64) {
	key := encodeKey(name, labels)
	mu.RLock()
	c, ok := counters[key]
	mu.RUnlock()
	if !ok {
		mu.Lock()
		c = counters[key]
		if c == nil {
			c = &counter{}
			counters[key] = c
		}
		mu.Unlock()
	}
	c.value.Add(n)
}

// Handler returns an http.Handler that emits text/plain Prometheus format.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		mu.RLock()
		keys := make([]string, 0, len(counters))
		for k := range counters {
			keys = append(keys, k)
		}
		mu.RUnlock()
		sort.Strings(keys)
		for _, k := range keys {
			mu.RLock()
			c := counters[k]
			mu.RUnlock()
			_, _ = io.WriteString(w, fmt.Sprintf("%s %d\n", k, c.value.Load()))
		}
	})
}

func encodeKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := name + "{"
	for i, k := range keys {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprintf("%s=%q", k, labels[k])
	}
	out += "}"
	return out
}
