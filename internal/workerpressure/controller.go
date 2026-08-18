package workerpressure

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/imprun/windforce-core/internal/state"
)

const (
	DefaultHighWatermark  = 0.90
	DefaultLowWatermark   = 0.80
	DefaultSampleInterval = 5 * time.Second
)

type Sample struct {
	Scope        string
	Measurements map[string]state.WorkerResourceMeasurement
}

type Sampler interface {
	Sample(context.Context) (Sample, error)
}

type Config struct {
	HighWatermark  float64
	LowWatermark   float64
	SampleInterval time.Duration
	Now            func() time.Time
}

// Controller owns the hysteresis state. Unknown samples fail open before the
// first pressure transition, but never resume an already-paused Worker.
type Controller struct {
	mu      sync.Mutex
	sampler Sampler
	config  Config
	paused  bool
	reason  string
	last    state.WorkerResourcePressure
	next    time.Time
}

func New(sampler Sampler, config Config) (*Controller, error) {
	if sampler == nil {
		return nil, errors.New("worker pressure sampler is required")
	}
	if config.HighWatermark == 0 {
		config.HighWatermark = DefaultHighWatermark
	}
	if config.LowWatermark == 0 {
		config.LowWatermark = DefaultLowWatermark
	}
	if config.SampleInterval <= 0 {
		config.SampleInterval = DefaultSampleInterval
	}
	if config.LowWatermark <= 0 || config.HighWatermark > 1 || config.LowWatermark >= config.HighWatermark {
		return nil, errors.New("worker pressure watermarks must satisfy 0 < low < high <= 1")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Controller{sampler: sampler, config: config}, nil
}

func (c *Controller) Observe(ctx context.Context) state.WorkerResourcePressure {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.config.Now().UTC()
	if !c.last.ObservedAt.IsZero() && now.Before(c.next) {
		return clonePressure(c.last)
	}
	sample, err := c.sampler.Sample(ctx)
	observation := state.WorkerResourcePressure{
		AcceptingClaims: true,
		Scope:           state.WorkerPressureScopeUnknown,
		ObservedAt:      now,
		FreshUntil:      now.Add(state.WorkerPressureFreshTTL),
		Measurements:    map[string]state.WorkerResourceMeasurement{},
	}
	if err == nil {
		observation.Scope = sample.Scope
		observation.Measurements = cloneMeasurements(sample.Measurements)
	}
	if observation.Scope == "" {
		observation.Scope = state.WorkerPressureScopeUnknown
	}

	comparable := 0
	allLow := true
	highReason := ""
	for _, resource := range []string{
		state.WorkerPressureResourceMemory,
		state.WorkerPressureResourceCPU,
		state.WorkerPressureResourceFileDescriptors,
	} {
		measurement, ok := observation.Measurements[resource]
		if !ok || !measurement.Supported {
			continue
		}
		observation.Supported = true
		if measurement.Ratio == nil {
			continue
		}
		comparable++
		if *measurement.Ratio >= c.config.LowWatermark {
			allLow = false
		}
		if highReason == "" && *measurement.Ratio >= c.config.HighWatermark {
			highReason = pressureReason(resource)
		}
	}

	switch {
	case highReason != "":
		c.paused = true
		c.reason = highReason
	case c.paused && comparable > 0 && allLow:
		c.paused = false
		c.reason = ""
	case c.paused && comparable == 0:
		c.reason = state.WorkerPressureReasonObservationUnknown
	case !c.paused && comparable == 0:
		c.reason = state.WorkerPressureReasonObservationUnknown
	}
	observation.AcceptingClaims = !c.paused
	observation.ReasonCode = c.reason
	c.last = clonePressure(observation)
	c.next = now.Add(c.config.SampleInterval)
	return observation
}

func pressureReason(resource string) string {
	switch resource {
	case state.WorkerPressureResourceMemory:
		return state.WorkerPressureReasonMemoryHigh
	case state.WorkerPressureResourceCPU:
		return state.WorkerPressureReasonCPUHigh
	default:
		return state.WorkerPressureReasonFileDescriptorsHigh
	}
}

func clonePressure(value state.WorkerResourcePressure) state.WorkerResourcePressure {
	value.Measurements = cloneMeasurements(value.Measurements)
	return value
}

func cloneMeasurements(values map[string]state.WorkerResourceMeasurement) map[string]state.WorkerResourceMeasurement {
	out := make(map[string]state.WorkerResourceMeasurement, len(values))
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := values[key]
		if value.Usage != nil {
			copy := *value.Usage
			value.Usage = &copy
		}
		if value.Limit != nil {
			copy := *value.Limit
			value.Limit = &copy
		}
		if value.Ratio != nil {
			copy := *value.Ratio
			value.Ratio = &copy
		}
		out[key] = value
	}
	return out
}
