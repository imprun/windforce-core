package remoteworker

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/server"
	"github.com/imprun/windforce-core/internal/state"
)

func TestClientRoundTripsWorkerPressure(t *testing.T) {
	tempDir := t.TempDir()
	store := state.NewLocalStore(filepath.Join(tempDir, "state.json"))
	srv := httptest.NewServer(server.New(server.Config{
		Store: store, Catalog: catalog.NewFileCatalog(filepath.Join(tempDir, "catalog.json")), AdminToken: "admin-secret",
	}))
	defer srv.Close()

	high := 0.95
	client := New(srv.URL, "admin-secret")
	if err := client.RegisterWorker(context.Background(), state.WorkerRecord{
		ID: "remote-pressure", Status: state.WorkerStatusActive,
		ResourcePressure: &state.WorkerResourcePressure{
			AcceptingClaims: false, ReasonCode: state.WorkerPressureReasonMemoryHigh,
			Scope: state.WorkerPressureScopeCgroupV2, ObservedAt: time.Now().UTC(),
			Measurements: map[string]state.WorkerResourceMeasurement{
				state.WorkerPressureResourceMemory: {Supported: true, Ratio: &high},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	workers, err := store.ListWorkers(context.Background())
	if err != nil || len(workers) != 1 || workers[0].ResourcePressure == nil || workers[0].ResourcePressure.AcceptingClaims {
		t.Fatalf("registered pressure = %#v, err=%v", workers, err)
	}

	low := 0.2
	if err := client.HeartbeatWorkerPressure(context.Background(), "remote-pressure", state.WorkerResourcePressure{
		AcceptingClaims: true, Scope: state.WorkerPressureScopeCgroupV2, ObservedAt: time.Now().UTC(),
		Measurements: map[string]state.WorkerResourceMeasurement{
			state.WorkerPressureResourceMemory: {Supported: true, Ratio: &low},
		},
	}); err != nil {
		t.Fatal(err)
	}
	workers, err = store.ListWorkers(context.Background())
	if err != nil || len(workers) != 1 || workers[0].ResourcePressure == nil || !workers[0].ResourcePressure.AcceptingClaims {
		t.Fatalf("heartbeat pressure = %#v, err=%v", workers, err)
	}
}
