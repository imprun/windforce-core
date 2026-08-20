//go:build !linux

package workerpressure

import (
	"context"
	"errors"

	"github.com/imprun/windforce-core/internal/state"
)

type unsupportedSampler struct{}

func DefaultSampler() Sampler { return unsupportedSampler{} }

func (unsupportedSampler) Sample(context.Context) (Sample, error) {
	return Sample{Scope: state.WorkerPressureScopeUnknown, Measurements: map[string]state.WorkerResourceMeasurement{}}, errors.New("worker pressure sampling is unsupported on this host")
}
