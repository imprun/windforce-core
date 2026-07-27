package trigger

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

type Metrics struct {
	mu         sync.RWMutex
	admissions map[string]uint64
	active     map[string]int
}

func NewMetrics() *Metrics {
	return &Metrics{
		admissions: map[string]uint64{},
		active:     map[string]int{},
	}
}

func (m *Metrics) ObserveAdmission(kind string, state string) {
	if m == nil {
		return
	}
	key := normalizedMetricLabel(kind) + "\x00" + normalizedMetricLabel(state)
	m.mu.Lock()
	m.admissions[key]++
	m.mu.Unlock()
}

func (m *Metrics) SetActive(counts map[string]int) {
	if m == nil {
		return
	}
	next := make(map[string]int, len(counts))
	for kind, count := range counts {
		next[normalizedMetricLabel(kind)] = count
	}
	m.mu.Lock()
	m.active = next
	m.mu.Unlock()
}

func (m *Metrics) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if next != nil {
			next.ServeHTTP(w, r)
		} else {
			w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		}
		_, _ = fmt.Fprint(w, m.render())
	})
}

func (m *Metrics) render() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	admissions := make(map[string]uint64, len(m.admissions))
	for key, value := range m.admissions {
		admissions[key] = value
	}
	active := make(map[string]int, len(m.active))
	for key, value := range m.active {
		active[key] = value
	}
	m.mu.RUnlock()

	var output strings.Builder
	output.WriteString("# HELP windforce_trigger_admissions_total Trigger admission attempts by adapter kind and result.\n")
	output.WriteString("# TYPE windforce_trigger_admissions_total counter\n")
	keys := sortedMetricStrings(admissions)
	for _, key := range keys {
		parts := strings.SplitN(key, "\x00", 2)
		fmt.Fprintf(&output, "windforce_trigger_admissions_total{kind=%q,state=%q} %d\n", parts[0], parts[1], admissions[key])
	}
	output.WriteString("# HELP windforce_trigger_active Active configured trigger adapters by kind.\n")
	output.WriteString("# TYPE windforce_trigger_active gauge\n")
	activeKeys := make([]string, 0, len(active))
	for key := range active {
		activeKeys = append(activeKeys, key)
	}
	sort.Strings(activeKeys)
	for _, key := range activeKeys {
		fmt.Fprintf(&output, "windforce_trigger_active{kind=%q} %d\n", key, active[key])
	}
	return output.String()
}

func sortedMetricStrings(values map[string]uint64) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func normalizedMetricLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return strings.NewReplacer("\\", "_", "\"", "_", "\n", "_").Replace(value)
}
