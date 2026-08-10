package state

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/imprun/windforce-core/internal/contract"
)

// WorkerRecord is the worker registry entry (ADR 0009 §6): the observable
// truth of which capabilities are alive right now. Slots is the worker's
// quantitative concurrency cap; labels stay qualitative.
type WorkerRecord struct {
	ID                   string                      `json:"id"`
	Group                string                      `json:"group,omitempty"`
	EngineVersion        string                      `json:"engineVersion,omitempty"`
	BuildRevision        string                      `json:"buildRevision,omitempty"`
	Tags                 []string                    `json:"tags,omitempty"`
	Labels               []string                    `json:"labels,omitempty"`
	ExecutionProfiles    []contract.ExecutionProfile `json:"executionProfiles,omitempty"`
	Slots                int                         `json:"slots"`
	Status               string                      `json:"status"`
	CredentialID         string                      `json:"credentialId,omitempty"`
	CredentialGeneration int64                       `json:"credentialGeneration,omitempty"`
	StartedAt            time.Time                   `json:"startedAt"`
	LastHeartbeatAt      time.Time                   `json:"lastHeartbeatAt"`
}

const WorkerBuildValueMaxBytes = 128

// NormalizeWorkerBuildValue bounds self-reported build metadata before it is
// persisted or returned by the canonical worker registry.
func NormalizeWorkerBuildValue(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) > WorkerBuildValueMaxBytes || !utf8.ValidString(value) {
		return "", fmt.Errorf("worker build value must be valid UTF-8 and at most %d bytes", WorkerBuildValueMaxBytes)
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return "", fmt.Errorf("worker build value must not contain control characters")
		}
	}
	return value, nil
}

const (
	WorkerStatusActive   = "active"
	WorkerStatusDraining = "draining"
)

func NormalizeWorkerStatus(value string) (string, error) {
	switch value {
	case "", WorkerStatusActive:
		return WorkerStatusActive, nil
	case WorkerStatusDraining:
		return WorkerStatusDraining, nil
	default:
		return "", fmt.Errorf("unsupported worker status %q", value)
	}
}

// WorkerLiveTTL is how recent a heartbeat must be for a worker to count as
// live in observability surfaces.
const WorkerLiveTTL = 90 * time.Second

// WorkerRegistryExpiry is how long a silent record survives before the
// registry drops it — crashed workers (no graceful deregister) must not
// accumulate forever. Live workers heartbeat every ~15s, far inside this.
const WorkerRegistryExpiry = 15 * time.Minute

// Live reports whether the record's heartbeat is fresh at the given time.
func (w WorkerRecord) Live(now time.Time) bool {
	return now.Sub(w.LastHeartbeatAt) <= WorkerLiveTTL
}
