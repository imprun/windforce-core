//go:build linux

package workerpressure

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/imprun/windforce-core/internal/state"
)

func TestLinuxSamplerTreatsUnlimitedCgroupMemoryAsNoComparableLimit(t *testing.T) {
	root := t.TempDir()
	procRoot := filepath.Join(root, "proc")
	cgroupRoot := filepath.Join(root, "cgroup")
	writeLinuxFixture(t, filepath.Join(procRoot, "self", "cgroup"), "0::/workers/a\n")
	writeLinuxFixture(t, filepath.Join(cgroupRoot, "workers", "a", "memory.current"), "943718400\n")
	writeLinuxFixture(t, filepath.Join(cgroupRoot, "workers", "a", "memory.max"), "max\n")
	writeLinuxFixture(t, filepath.Join(cgroupRoot, "workers", "a", "cpu.pressure"), "some avg10=12.50 avg60=1.00 avg300=0.50 total=1\n")

	sample, err := (LinuxSampler{ProcRoot: procRoot, CgroupRoot: cgroupRoot, PID: 100}).Sample(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sample.Scope != state.WorkerPressureScopeCgroupV2 {
		t.Fatalf("scope = %q", sample.Scope)
	}
	memory := sample.Measurements[state.WorkerPressureResourceMemory]
	if !memory.Supported || memory.Usage == nil || memory.Limit != nil || memory.Ratio != nil {
		t.Fatalf("unlimited memory measurement = %#v", memory)
	}
}

func TestLinuxSamplerHostMemoryIncludesWorkerProcessTree(t *testing.T) {
	procRoot := filepath.Join(t.TempDir(), "proc")
	writeLinuxFixture(t, filepath.Join(procRoot, "meminfo"), "MemTotal:       1024000 kB\n")
	writeLinuxFixture(t, filepath.Join(procRoot, "loadavg"), "0.25 0.10 0.05 1/100 1\n")
	writeLinuxFixture(t, filepath.Join(procRoot, "100", "stat"), "100 (worker) S 0\n")
	writeLinuxFixture(t, filepath.Join(procRoot, "100", "statm"), "20 2 0 0 0 0 0\n")
	writeLinuxFixture(t, filepath.Join(procRoot, "101", "stat"), "101 (child process) S 100\n")
	writeLinuxFixture(t, filepath.Join(procRoot, "101", "statm"), "20 3 0 0 0 0 0\n")
	writeLinuxFixture(t, filepath.Join(procRoot, "100", "limits"), "Max open files            10                   10                   files\n")
	if err := os.MkdirAll(filepath.Join(procRoot, "100", "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeLinuxFixture(t, filepath.Join(procRoot, "100", "fd", "1"), "")
	writeLinuxFixture(t, filepath.Join(procRoot, "100", "fd", "2"), "")

	sample, err := (LinuxSampler{ProcRoot: procRoot, CgroupRoot: filepath.Join(t.TempDir(), "missing"), PID: 100}).Sample(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sample.Scope != state.WorkerPressureScopeProcessTreeHost {
		t.Fatalf("scope = %q", sample.Scope)
	}
	memory := sample.Measurements[state.WorkerPressureResourceMemory]
	wantUsage := uint64(5 * os.Getpagesize())
	if memory.Usage == nil || *memory.Usage != wantUsage {
		t.Fatalf("process-tree RSS = %#v, want %d", memory.Usage, wantUsage)
	}
	fd := sample.Measurements[state.WorkerPressureResourceFileDescriptors]
	if fd.Usage == nil || *fd.Usage != 2 || fd.Limit == nil || *fd.Limit != 10 {
		t.Fatalf("file descriptor measurement = %#v", fd)
	}
}

func writeLinuxFixture(t *testing.T, path string, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}
