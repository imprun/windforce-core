package state

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

type workerPressureStore interface {
	Store
	HeartbeatWorkerPressure(context.Context, string, WorkerResourcePressure) error
}

func TestLocalWorkerPressureContract(t *testing.T) {
	exerciseWorkerPressureContract(t, NewLocalStore(t.TempDir()+"/state.json"))
}

func TestPostgresWorkerPressureContract(t *testing.T) {
	dsn := postgresTestDSN()
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	store := openIsolatedPostgresCatalogStore(t, dsn)
	exerciseWorkerPressureContract(t, store)

	var dataType string
	if err := store.pool.QueryRow(context.Background(), `
SELECT data_type
FROM information_schema.columns
WHERE table_schema=current_schema() AND table_name='worker_registry' AND column_name='resource_pressure'`).Scan(&dataType); err != nil {
		t.Fatalf("inspect resource_pressure migration: %v", err)
	}
	if dataType != "jsonb" {
		t.Fatalf("worker_registry.resource_pressure data type = %q, want jsonb", dataType)
	}
}

func exerciseWorkerPressureContract(t *testing.T, store workerPressureStore) {
	t.Helper()
	ctx := context.Background()
	high := 0.95
	paused := WorkerResourcePressure{
		AcceptingClaims: false, ReasonCode: WorkerPressureReasonMemoryHigh,
		Scope: WorkerPressureScopeCgroupV2, ObservedAt: time.Now().UTC().Add(-time.Second),
		Measurements: map[string]WorkerResourceMeasurement{
			WorkerPressureResourceMemory: {Supported: true, Ratio: &high},
		},
	}
	if err := store.RegisterWorker(ctx, WorkerRecord{
		ID: "worker-pressure", Group: "pressure", Tags: []string{"blue"}, Slots: 2,
		Status: WorkerStatusActive, ResourcePressure: &paused,
	}); err != nil {
		t.Fatal(err)
	}
	workers, err := store.ListWorkers(ctx)
	if err != nil || len(workers) != 1 {
		t.Fatalf("workers = %#v, err=%v", workers, err)
	}
	stored := workers[0].ResourcePressure
	if stored == nil || stored.AcceptingClaims || stored.ReasonCode != WorkerPressureReasonMemoryHigh || stored.FreshUntil.IsZero() {
		t.Fatalf("stored pressure = %#v", stored)
	}
	if !stored.Supported {
		t.Fatal("supported must be derived from supported measurements")
	}

	enqueueClaimIdentityJob(t, store, "run-pressure", "job-pressure", "pressure", "blue", nil, contract.ExecutionProfile{})
	if _, _, err := store.ClaimJobForWorker(ctx, "worker-pressure", []string{"blue"}, nil, time.Minute); !errors.Is(err, ErrForbidden) {
		t.Fatalf("pressure-paused claim err=%v, want ErrForbidden", err)
	}

	low := 0.2
	recovered := paused
	recovered.AcceptingClaims = true
	recovered.ReasonCode = ""
	recovered.ObservedAt = time.Now().UTC()
	recovered.Measurements[WorkerPressureResourceMemory] = WorkerResourceMeasurement{Supported: true, Ratio: &low}
	if err := store.HeartbeatWorkerPressure(ctx, "worker-pressure", recovered); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ClaimJobForWorker(ctx, "worker-pressure", []string{"blue"}, nil, time.Minute); err != nil {
		t.Fatalf("recovered claim: %v", err)
	}
}

func TestNormalizeWorkerResourcePressureRejectsUnsafeOrInvalidShape(t *testing.T) {
	now := time.Now().UTC()
	ratio := 1.1
	_, err := NormalizeWorkerResourcePressure(&WorkerResourcePressure{
		AcceptingClaims: true, Scope: WorkerPressureScopeCgroupV2,
		Measurements: map[string]WorkerResourceMeasurement{
			WorkerPressureResourceMemory: {Supported: true, Ratio: &ratio},
		},
	}, now)
	if err == nil {
		t.Fatal("ratio above one was accepted")
	}
	_, err = NormalizeWorkerResourcePressure(&WorkerResourcePressure{
		AcceptingClaims: false, ReasonCode: "free-form secret/path", Scope: WorkerPressureScopeUnknown,
	}, now)
	if err == nil {
		t.Fatal("free-form reason was accepted")
	}

	safe, err := NormalizeWorkerResourcePressure(&WorkerResourcePressure{
		AcceptingClaims: true, ReasonCode: WorkerPressureReasonObservationUnknown, Scope: WorkerPressureScopeUnknown,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(safe)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"credential", "token", "path", "raw_error"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("pressure observation leaked %q: %s", forbidden, raw)
		}
	}
}

func BenchmarkRegisteredWorkerClaim16Slots(b *testing.B) {
	now := time.Now().UTC()
	base := WorkerRecord{
		ID: "worker-benchmark", Status: WorkerStatusActive, LastHeartbeatAt: now,
		Tags: []string{"ready"}, Labels: []string{"linux"}, Slots: 16,
	}
	ratio := 0.2
	healthy := base
	healthy.ResourcePressure = &WorkerResourcePressure{
		Supported: true, AcceptingClaims: true, Scope: WorkerPressureScopeCgroupV2,
		ObservedAt: now, FreshUntil: now.Add(time.Minute),
		Measurements: map[string]WorkerResourceMeasurement{
			WorkerPressureResourceMemory: {Supported: true, Ratio: &ratio},
		},
	}
	for _, benchmark := range []struct {
		name   string
		worker WorkerRecord
	}{
		{name: "legacy-no-observation", worker: base},
		{name: "healthy-pressure-observation", worker: healthy},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			for iteration := 0; iteration < b.N; iteration++ {
				for slot := 0; slot < 16; slot++ {
					if _, _, _, err := RegisteredWorkerClaim(benchmark.worker, []string{"ready"}, []string{"linux"}, now); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
