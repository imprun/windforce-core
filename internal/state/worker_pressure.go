package state

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const (
	WorkerPressureResourceMemory          = "memory"
	WorkerPressureResourceCPU             = "cpu"
	WorkerPressureResourceFileDescriptors = "file_descriptors"

	WorkerPressureReasonMemoryHigh          = "memory_high"
	WorkerPressureReasonCPUHigh             = "cpu_high"
	WorkerPressureReasonFileDescriptorsHigh = "file_descriptors_high"
	WorkerPressureReasonObservationUnknown  = "observation_unknown"

	WorkerPressureScopeCgroupV2        = "cgroup_v2"
	WorkerPressureScopeProcessTreeHost = "process_tree_host"
	WorkerPressureScopeUnknown         = "unknown"
)

// WorkerPressureFreshTTL bounds how long one resource sample is described as
// fresh. Claim admission uses AcceptingClaims, not this presentation timestamp;
// a paused controller remains paused across unknown samples until a low-water
// sample proves recovery.
const WorkerPressureFreshTTL = 45 * time.Second

// WorkerResourceMeasurement is a privacy-safe numeric observation. Usage and
// Limit are bytes for memory and counts for file descriptors. CPU exposes only
// a normalized ratio. Nil Ratio means that no finite limit or comparable host
// capacity was available and must never be interpreted as zero pressure.
type WorkerResourceMeasurement struct {
	Supported bool     `json:"supported"`
	Usage     *uint64  `json:"usage,omitempty"`
	Limit     *uint64  `json:"limit,omitempty"`
	Ratio     *float64 `json:"ratio,omitempty"`
}

// WorkerResourcePressure is independent from Worker lifecycle status. A live,
// active Worker can be pressure-paused while it continues lease heartbeats,
// completion, drain, and cleanup for already-running Jobs.
type WorkerResourcePressure struct {
	Supported       bool                                 `json:"supported"`
	AcceptingClaims bool                                 `json:"accepting_claims"`
	ReasonCode      string                               `json:"reason_code,omitempty"`
	Scope           string                               `json:"scope"`
	ObservedAt      time.Time                            `json:"observed_at"`
	FreshUntil      time.Time                            `json:"fresh_until"`
	Measurements    map[string]WorkerResourceMeasurement `json:"measurements,omitempty"`
}

// AcceptingClaims preserves compatibility for Workers that predate pressure
// reporting. Once an observation exists its explicit admission decision is
// authoritative at the final claim boundary.
func (w WorkerRecord) AcceptingClaims() bool {
	return w.ResourcePressure == nil || w.ResourcePressure.AcceptingClaims
}

func (p WorkerResourcePressure) Fresh(now time.Time) bool {
	return !p.FreshUntil.IsZero() && !now.After(p.FreshUntil)
}

// NormalizeWorkerResourcePressure validates untrusted remote Worker input,
// copies maps/pointers, and assigns a server-relative freshness bound.
func NormalizeWorkerResourcePressure(value *WorkerResourcePressure, now time.Time) (*WorkerResourcePressure, error) {
	if value == nil {
		return nil, nil
	}
	now = now.UTC()
	out := *value
	out.Scope = strings.TrimSpace(out.Scope)
	switch out.Scope {
	case WorkerPressureScopeCgroupV2, WorkerPressureScopeProcessTreeHost, WorkerPressureScopeUnknown:
	default:
		return nil, fmt.Errorf("unsupported worker pressure scope")
	}
	out.ReasonCode = strings.TrimSpace(out.ReasonCode)
	if !validWorkerPressureReason(out.ReasonCode, out.AcceptingClaims) {
		return nil, fmt.Errorf("unsupported worker pressure reason")
	}
	if out.ObservedAt.IsZero() {
		out.ObservedAt = now
	} else {
		out.ObservedAt = out.ObservedAt.UTC()
	}
	out.FreshUntil = now.Add(WorkerPressureFreshTTL)
	out.Measurements = map[string]WorkerResourceMeasurement{}
	out.Supported = false
	keys := make([]string, 0, len(value.Measurements))
	for resource := range value.Measurements {
		keys = append(keys, resource)
	}
	sort.Strings(keys)
	for _, resource := range keys {
		if !validWorkerPressureResource(resource) {
			return nil, fmt.Errorf("unsupported worker pressure resource")
		}
		measurement := value.Measurements[resource]
		if measurement.Ratio != nil && (math.IsNaN(*measurement.Ratio) || math.IsInf(*measurement.Ratio, 0) || *measurement.Ratio < 0 || *measurement.Ratio > 1) {
			return nil, fmt.Errorf("worker pressure ratio for %s must be between 0 and 1", resource)
		}
		if measurement.Usage != nil {
			usage := *measurement.Usage
			measurement.Usage = &usage
		}
		if measurement.Limit != nil {
			limit := *measurement.Limit
			measurement.Limit = &limit
		}
		if measurement.Ratio != nil {
			ratio := *measurement.Ratio
			measurement.Ratio = &ratio
		}
		if measurement.Supported {
			out.Supported = true
		}
		out.Measurements[resource] = measurement
	}
	return &out, nil
}

func validWorkerPressureResource(value string) bool {
	switch value {
	case WorkerPressureResourceMemory, WorkerPressureResourceCPU, WorkerPressureResourceFileDescriptors:
		return true
	default:
		return false
	}
}

func validWorkerPressureReason(value string, acceptingClaims bool) bool {
	if acceptingClaims {
		return value == "" || value == WorkerPressureReasonObservationUnknown
	}
	switch value {
	case WorkerPressureReasonMemoryHigh, WorkerPressureReasonCPUHigh, WorkerPressureReasonFileDescriptorsHigh, WorkerPressureReasonObservationUnknown:
		return true
	default:
		return false
	}
}

func marshalWorkerResourcePressure(value *WorkerResourcePressure) ([]byte, error) {
	if value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(value)
}

func unmarshalWorkerResourcePressure(data []byte) (*WorkerResourcePressure, error) {
	if len(bytes.TrimSpace(data)) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, nil
	}
	var value WorkerResourcePressure
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return &value, nil
}
