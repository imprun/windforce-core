package workerpressure

import (
	"context"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/state"
)

type sequenceSampler struct {
	samples []Sample
	errors  []error
	index   int
}

func (s *sequenceSampler) Sample(context.Context) (Sample, error) {
	index := s.index
	if index >= len(s.samples) {
		index = len(s.samples) - 1
	}
	s.index++
	var err error
	if index >= 0 && index < len(s.errors) {
		err = s.errors[index]
	}
	return s.samples[index], err
}

func TestControllerUsesHighLowHysteresisAndUnknownDoesNotResume(t *testing.T) {
	now := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	sampler := &sequenceSampler{samples: []Sample{
		sampleWithRatio(state.WorkerPressureResourceMemory, 0.91),
		sampleWithRatio(state.WorkerPressureResourceMemory, 0.85),
		{Scope: state.WorkerPressureScopeUnknown},
		sampleWithRatio(state.WorkerPressureResourceMemory, 0.80),
		sampleWithRatio(state.WorkerPressureResourceMemory, 0.79),
	}}
	controller, err := New(sampler, Config{HighWatermark: 0.9, LowWatermark: 0.8, SampleInterval: time.Second, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	first := controller.Observe(context.Background())
	if first.AcceptingClaims || first.ReasonCode != state.WorkerPressureReasonMemoryHigh {
		t.Fatalf("high observation = %#v", first)
	}
	now = now.Add(time.Second)
	if second := controller.Observe(context.Background()); second.AcceptingClaims || second.ReasonCode != state.WorkerPressureReasonMemoryHigh {
		t.Fatalf("between-watermarks observation = %#v", second)
	}
	now = now.Add(time.Second)
	if unknown := controller.Observe(context.Background()); unknown.AcceptingClaims || unknown.ReasonCode != state.WorkerPressureReasonObservationUnknown {
		t.Fatalf("unknown observation = %#v", unknown)
	}
	now = now.Add(time.Second)
	if boundary := controller.Observe(context.Background()); boundary.AcceptingClaims || boundary.ReasonCode != state.WorkerPressureReasonObservationUnknown {
		t.Fatalf("low-watermark boundary observation = %#v", boundary)
	}
	now = now.Add(time.Second)
	if recovered := controller.Observe(context.Background()); !recovered.AcceptingClaims || recovered.ReasonCode != "" {
		t.Fatalf("recovered observation = %#v", recovered)
	}
}

func TestControllerUnknownInitialObservationFailsOpen(t *testing.T) {
	now := time.Now().UTC()
	controller, err := New(&sequenceSampler{samples: []Sample{{Scope: state.WorkerPressureScopeUnknown}}}, Config{
		SampleInterval: time.Second, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	observation := controller.Observe(context.Background())
	if !observation.AcceptingClaims || observation.Supported || observation.ReasonCode != state.WorkerPressureReasonObservationUnknown {
		t.Fatalf("initial unknown observation = %#v", observation)
	}
}

func TestControllerCachesSamples(t *testing.T) {
	now := time.Now().UTC()
	sampler := &sequenceSampler{samples: []Sample{sampleWithRatio(state.WorkerPressureResourceCPU, 0.1)}}
	controller, err := New(sampler, Config{SampleInterval: time.Minute, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	controller.Observe(context.Background())
	controller.Observe(context.Background())
	if sampler.index != 1 {
		t.Fatalf("sample calls = %d, want 1", sampler.index)
	}
}

func BenchmarkControllerCachedObservation16Slots(b *testing.B) {
	now := time.Now().UTC()
	sampler := &sequenceSampler{samples: []Sample{sampleWithRatio(state.WorkerPressureResourceMemory, 0.25)}}
	controller, err := New(sampler, Config{SampleInterval: time.Hour, Now: func() time.Time { return now }})
	if err != nil {
		b.Fatal(err)
	}
	controller.Observe(context.Background())
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		for slot := 0; slot < 16; slot++ {
			_ = controller.Observe(context.Background())
		}
	}
}

func sampleWithRatio(resource string, ratio float64) Sample {
	return Sample{Scope: state.WorkerPressureScopeCgroupV2, Measurements: map[string]state.WorkerResourceMeasurement{
		resource: {Supported: true, Ratio: &ratio},
	}}
}
