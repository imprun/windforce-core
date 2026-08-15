package state

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

const (
	executionRateOutcomeConsumed = "consumed"
	executionRateOutcomeBlocked  = "blocked"
)

// executionMetrics deliberately keeps only low-cardinality limiter outcomes.
// Workspace, App, policy, and opaque key identities must never become labels.
type executionMetrics struct {
	mu         sync.RWMutex
	rateClaims map[string]uint64
}

func newExecutionMetrics() *executionMetrics {
	return &executionMetrics{rateClaims: map[string]uint64{}}
}

func (metrics *executionMetrics) observeRateClaims(outcome string, count int) {
	if metrics == nil || count <= 0 || (outcome != executionRateOutcomeConsumed && outcome != executionRateOutcomeBlocked) {
		return
	}
	metrics.mu.Lock()
	metrics.rateClaims[outcome] += uint64(count)
	metrics.mu.Unlock()
}

func (metrics *executionMetrics) Handler(backend string, next http.Handler) http.Handler {
	backend = strings.TrimSpace(backend)
	if backend == "" {
		backend = "unknown"
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if next != nil {
			next.ServeHTTP(response, request)
		} else {
			response.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		}
		_, _ = fmt.Fprint(response, metrics.render(backend))
	})
}

func (metrics *executionMetrics) render(backend string) string {
	if metrics == nil {
		return ""
	}
	metrics.mu.RLock()
	values := make(map[string]uint64, len(metrics.rateClaims))
	for outcome, value := range metrics.rateClaims {
		values[outcome] = value
	}
	metrics.mu.RUnlock()

	outcomes := make([]string, 0, len(values))
	for outcome := range values {
		outcomes = append(outcomes, outcome)
	}
	sort.Strings(outcomes)
	var output strings.Builder
	output.WriteString("# HELP windforce_execution_rate_claims_total Rate-limited claim outcomes observed by this Core process.\n")
	output.WriteString("# TYPE windforce_execution_rate_claims_total counter\n")
	for _, outcome := range outcomes {
		fmt.Fprintf(&output, "windforce_execution_rate_claims_total{backend=%q,outcome=%q} %d\n", backend, outcome, values[outcome])
	}
	return output.String()
}

type executionMetricsProvider interface {
	executionMetricsState() (*executionMetrics, string)
}

// ExecutionMetricsHandler adds Store execution-limit metrics to the existing
// /metrics handler chain without expanding the public Store interface.
func ExecutionMetricsHandler(store Store, next http.Handler) http.Handler {
	provider, ok := store.(executionMetricsProvider)
	if !ok {
		return next
	}
	metrics, backend := provider.executionMetricsState()
	return metrics.Handler(backend, next)
}
