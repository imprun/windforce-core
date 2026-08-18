//go:build linux

package workerpressure

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/imprun/windforce-core/internal/state"
)

type LinuxSampler struct {
	ProcRoot   string
	CgroupRoot string
	PID        int
}

func DefaultSampler() Sampler {
	return LinuxSampler{}
}

func (s LinuxSampler) Sample(ctx context.Context) (Sample, error) {
	procRoot := s.ProcRoot
	if procRoot == "" {
		procRoot = "/proc"
	}
	cgroupRoot := s.CgroupRoot
	if cgroupRoot == "" {
		cgroupRoot = "/sys/fs/cgroup"
	}
	pid := s.PID
	if pid <= 0 {
		pid = os.Getpid()
	}
	measurements := map[string]state.WorkerResourceMeasurement{}
	scope := state.WorkerPressureScopeProcessTreeHost
	if cgroupDir, ok := linuxCgroupV2Dir(procRoot, cgroupRoot); ok {
		scope = state.WorkerPressureScopeCgroupV2
		if measurement, found := cgroupMemoryMeasurement(cgroupDir); found {
			measurements[state.WorkerPressureResourceMemory] = measurement
		}
		if measurement, found := cpuPressureMeasurement(filepath.Join(cgroupDir, "cpu.pressure")); found {
			measurements[state.WorkerPressureResourceCPU] = measurement
		}
	}
	if _, ok := measurements[state.WorkerPressureResourceMemory]; !ok {
		if measurement, found := hostProcessTreeMemory(ctx, procRoot, pid); found {
			measurements[state.WorkerPressureResourceMemory] = measurement
		}
	}
	if _, ok := measurements[state.WorkerPressureResourceCPU]; !ok {
		if measurement, found := hostLoadMeasurement(procRoot); found {
			measurements[state.WorkerPressureResourceCPU] = measurement
		}
	}
	if measurement, found := fileDescriptorMeasurement(procRoot, pid); found {
		measurements[state.WorkerPressureResourceFileDescriptors] = measurement
	}
	if len(measurements) == 0 {
		return Sample{Scope: state.WorkerPressureScopeUnknown, Measurements: measurements}, errors.New("no supported Linux pressure metrics")
	}
	return Sample{Scope: scope, Measurements: measurements}, nil
}

func linuxCgroupV2Dir(procRoot string, cgroupRoot string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(procRoot, "self", "cgroup"))
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "0::") {
			continue
		}
		relative := strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(line, "0::")), "/")
		if relative == "." || strings.HasPrefix(relative, "..") {
			return "", false
		}
		dir := filepath.Join(cgroupRoot, filepath.FromSlash(relative))
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir, true
		}
	}
	return "", false
}

func cgroupMemoryMeasurement(dir string) (state.WorkerResourceMeasurement, bool) {
	usage, ok := readUint(filepath.Join(dir, "memory.current"))
	if !ok {
		return state.WorkerResourceMeasurement{}, false
	}
	measurement := state.WorkerResourceMeasurement{Supported: true, Usage: uint64Pointer(usage)}
	data, err := os.ReadFile(filepath.Join(dir, "memory.max"))
	if err != nil || strings.TrimSpace(string(data)) == "max" {
		return measurement, true
	}
	limit, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || limit == 0 {
		return measurement, true
	}
	measurement.Limit = uint64Pointer(limit)
	measurement.Ratio = ratioPointer(usage, limit)
	return measurement, true
}

func cpuPressureMeasurement(path string) (state.WorkerResourceMeasurement, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return state.WorkerResourceMeasurement{}, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "some" {
			continue
		}
		for _, field := range fields[1:] {
			if !strings.HasPrefix(field, "avg10=") {
				continue
			}
			value, err := strconv.ParseFloat(strings.TrimPrefix(field, "avg10="), 64)
			if err != nil {
				return state.WorkerResourceMeasurement{}, false
			}
			ratio := clampRatio(value / 100)
			return state.WorkerResourceMeasurement{Supported: true, Ratio: &ratio}, true
		}
	}
	return state.WorkerResourceMeasurement{}, false
}

func hostProcessTreeMemory(ctx context.Context, procRoot string, rootPID int) (state.WorkerResourceMeasurement, bool) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return state.WorkerResourceMeasurement{}, false
	}
	children := map[int][]int{}
	rssPages := map[int]uint64{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return state.WorkerResourceMeasurement{}, false
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		ppid, ok := procParentPID(filepath.Join(procRoot, entry.Name(), "stat"))
		if !ok {
			continue
		}
		children[ppid] = append(children[ppid], pid)
		if pages, ok := procRSSPages(filepath.Join(procRoot, entry.Name(), "statm")); ok {
			rssPages[pid] = pages
		}
	}
	var pages uint64
	queue := []int{rootPID}
	seen := map[int]struct{}{}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		pages += rssPages[pid]
		queue = append(queue, children[pid]...)
	}
	limit, ok := memTotalBytes(filepath.Join(procRoot, "meminfo"))
	if !ok || limit == 0 {
		return state.WorkerResourceMeasurement{}, false
	}
	usage := pages * uint64(os.Getpagesize())
	return state.WorkerResourceMeasurement{Supported: true, Usage: uint64Pointer(usage), Limit: uint64Pointer(limit), Ratio: ratioPointer(usage, limit)}, true
}

func hostLoadMeasurement(procRoot string) (state.WorkerResourceMeasurement, bool) {
	data, err := os.ReadFile(filepath.Join(procRoot, "loadavg"))
	if err != nil {
		return state.WorkerResourceMeasurement{}, false
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return state.WorkerResourceMeasurement{}, false
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || runtime.NumCPU() <= 0 {
		return state.WorkerResourceMeasurement{}, false
	}
	ratio := clampRatio(load / float64(runtime.NumCPU()))
	return state.WorkerResourceMeasurement{Supported: true, Ratio: &ratio}, true
}

func fileDescriptorMeasurement(procRoot string, pid int) (state.WorkerResourceMeasurement, bool) {
	entries, err := os.ReadDir(filepath.Join(procRoot, strconv.Itoa(pid), "fd"))
	if err != nil {
		return state.WorkerResourceMeasurement{}, false
	}
	usage := uint64(len(entries))
	measurement := state.WorkerResourceMeasurement{Supported: true, Usage: uint64Pointer(usage)}
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "limits"))
	if err != nil {
		return measurement, true
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || strings.Join(fields[:3], " ") != "Max open files" || fields[3] == "unlimited" {
			continue
		}
		limit, err := strconv.ParseUint(fields[3], 10, 64)
		if err == nil && limit > 0 {
			measurement.Limit = uint64Pointer(limit)
			measurement.Ratio = ratioPointer(usage, limit)
		}
		break
	}
	return measurement, true
}

func procParentPID(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	closing := strings.LastIndexByte(string(data), ')')
	if closing < 0 {
		return 0, false
	}
	fields := strings.Fields(string(data)[closing+1:])
	if len(fields) < 2 {
		return 0, false
	}
	pid, err := strconv.Atoi(fields[1])
	return pid, err == nil
}

func procRSSPages(path string) (uint64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0, false
	}
	value, err := strconv.ParseUint(fields[1], 10, 64)
	return value, err == nil
}

func memTotalBytes(path string) (uint64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			value, err := strconv.ParseUint(fields[1], 10, 64)
			return value * 1024, err == nil
		}
	}
	return 0, false
}

func readUint(path string) (uint64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	return value, err == nil
}

func uint64Pointer(value uint64) *uint64 { return &value }

func ratioPointer(usage uint64, limit uint64) *float64 {
	ratio := clampRatio(float64(usage) / float64(limit))
	return &ratio
}

func clampRatio(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
