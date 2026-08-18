package state

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	mathrand "math/rand"
	"strings"
	"testing"
	"time"
)

type leaseModelEventKind string

const (
	leaseModelAdvance   leaseModelEventKind = "advance"
	leaseModelClaim     leaseModelEventKind = "claim"
	leaseModelHeartbeat leaseModelEventKind = "heartbeat"
	leaseModelComplete  leaseModelEventKind = "complete"
	leaseModelCancel    leaseModelEventKind = "cancel"
	leaseModelDrain     leaseModelEventKind = "drain"
	leaseModelShutdown  leaseModelEventKind = "shutdown"
)

type leaseModelEvent struct {
	Kind    leaseModelEventKind
	Worker  string
	Attempt int
	Ticks   int
	TTL     int
}

func (event leaseModelEvent) String() string {
	switch event.Kind {
	case leaseModelAdvance:
		return fmt.Sprintf("advance(%d)", event.Ticks)
	case leaseModelClaim:
		return fmt.Sprintf("claim(%s,ttl=%d)", event.Worker, event.TTL)
	case leaseModelHeartbeat:
		return fmt.Sprintf("heartbeat(%s,attempt=%d,ttl=%d)", event.Worker, event.Attempt, event.TTL)
	case leaseModelComplete:
		return fmt.Sprintf("complete(%s,attempt=%d)", event.Worker, event.Attempt)
	case leaseModelDrain, leaseModelShutdown:
		return fmt.Sprintf("%s(%s)", event.Kind, event.Worker)
	default:
		return string(event.Kind)
	}
}

type leaseModelJobState string

const (
	leaseModelQueued    leaseModelJobState = "queued"
	leaseModelRunning   leaseModelJobState = "running"
	leaseModelSucceeded leaseModelJobState = "succeeded"
	leaseModelCanceled  leaseModelJobState = "canceled"
)

type leaseFencingModel struct {
	now                 int
	state               leaseModelJobState
	attempt             int
	owner               string
	deadline            int
	cancelRequested     bool
	pinnedBundle        string
	claimPins           []string
	acceptedCompletions []int
	draining            map[string]bool
	stopped             map[string]bool
	disableAttemptFence bool
}

type leaseModelFailure struct {
	Index int
	Event leaseModelEvent
	Cause string
}

func (failure *leaseModelFailure) Error() string {
	return fmt.Sprintf("event %d %s: %s", failure.Index, failure.Event, failure.Cause)
}

func newLeaseFencingModel(disableAttemptFence bool) *leaseFencingModel {
	return &leaseFencingModel{
		state:               leaseModelQueued,
		pinnedBundle:        "sha256:pinned-release-a",
		draining:            map[string]bool{},
		stopped:             map[string]bool{},
		disableAttemptFence: disableAttemptFence,
	}
}

func (model *leaseFencingModel) apply(event leaseModelEvent) {
	switch event.Kind {
	case leaseModelAdvance:
		model.now += max(event.Ticks, 0)
	case leaseModelClaim:
		model.claim(event)
	case leaseModelHeartbeat:
		model.heartbeat(event)
	case leaseModelComplete:
		model.complete(event)
	case leaseModelCancel:
		model.cancel()
	case leaseModelDrain:
		model.draining[event.Worker] = true
	case leaseModelShutdown:
		model.stopped[event.Worker] = true
	}
}

func (model *leaseFencingModel) claim(event leaseModelEvent) {
	// Core makes lease recovery part of the next claim transaction. Merely
	// advancing time does not mutate durable Job state.
	if model.state == leaseModelRunning && model.deadline < model.now {
		if model.cancelRequested {
			model.state = leaseModelCanceled
			return
		}
		model.state = leaseModelQueued
		model.owner = ""
		model.deadline = 0
	}
	if model.state != leaseModelQueued || model.draining[event.Worker] || model.stopped[event.Worker] {
		return
	}
	model.state = leaseModelRunning
	model.attempt++
	model.owner = event.Worker
	model.deadline = model.now + positiveTTL(event.TTL)
	model.claimPins = append(model.claimPins, model.pinnedBundle)
}

func (model *leaseFencingModel) heartbeat(event leaseModelEvent) {
	if model.stopped[event.Worker] || model.state != leaseModelRunning || model.owner != event.Worker || model.attempt != event.Attempt {
		return
	}
	// A claim transaction is the authority that recovers an expired lease.
	// Until recovery wins, the same attempt may renew it.
	model.deadline = model.now + positiveTTL(event.TTL)
}

func (model *leaseFencingModel) complete(event leaseModelEvent) {
	if model.stopped[event.Worker] || model.state != leaseModelRunning || model.owner != event.Worker {
		return
	}
	if event.Attempt > model.attempt {
		return
	}
	if !model.disableAttemptFence && model.attempt != event.Attempt {
		return
	}
	if model.deadline < model.now {
		return
	}
	model.acceptedCompletions = append(model.acceptedCompletions, event.Attempt)
	if model.cancelRequested {
		model.state = leaseModelCanceled
		return
	}
	model.state = leaseModelSucceeded
}

func (model *leaseFencingModel) cancel() {
	switch model.state {
	case leaseModelQueued:
		model.state = leaseModelCanceled
	case leaseModelRunning:
		model.cancelRequested = true
	}
}

func (model *leaseFencingModel) invariant() error {
	if len(model.acceptedCompletions) > 1 {
		return fmt.Errorf("%d completions were accepted", len(model.acceptedCompletions))
	}
	if len(model.acceptedCompletions) == 1 && model.acceptedCompletions[0] != model.attempt {
		return fmt.Errorf("attempt %d completed while attempt %d owned the lease", model.acceptedCompletions[0], model.attempt)
	}
	for _, pin := range model.claimPins {
		if pin != model.pinnedBundle {
			return fmt.Errorf("claim observed mutable pin %q, want %q", pin, model.pinnedBundle)
		}
	}
	return nil
}

func checkLeaseModelTrace(trace []leaseModelEvent, disableAttemptFence bool) error {
	model := newLeaseFencingModel(disableAttemptFence)
	for index, event := range trace {
		model.apply(event)
		if err := model.invariant(); err != nil {
			return &leaseModelFailure{Index: index, Event: event, Cause: err.Error()}
		}
	}
	return nil
}

func formatLeaseModelTrace(seed int64, trace []leaseModelEvent) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "seed=%d", seed)
	for index, event := range trace {
		fmt.Fprintf(&builder, "\n%02d %s", index, event)
	}
	return builder.String()
}

func shrinkLeaseModelTrace(trace []leaseModelEvent, fails func([]leaseModelEvent) bool) []leaseModelEvent {
	shrunk := append([]leaseModelEvent(nil), trace...)
	for {
		changed := false
		for index := range shrunk {
			candidate := append([]leaseModelEvent(nil), shrunk[:index]...)
			candidate = append(candidate, shrunk[index+1:]...)
			if fails(candidate) {
				shrunk = candidate
				changed = true
				break
			}
		}
		if !changed {
			return shrunk
		}
	}
}

func randomLeaseModelTrace(seed int64, count int) []leaseModelEvent {
	rng := mathrand.New(mathrand.NewSource(seed))
	workers := []string{"worker-a", "worker-b", "worker-c"}
	kinds := []leaseModelEventKind{
		leaseModelAdvance,
		leaseModelClaim,
		leaseModelHeartbeat,
		leaseModelComplete,
		leaseModelCancel,
		leaseModelDrain,
		leaseModelShutdown,
	}
	trace := make([]leaseModelEvent, 0, count)
	for range count {
		trace = append(trace, leaseModelEvent{
			Kind:    kinds[rng.Intn(len(kinds))],
			Worker:  workers[rng.Intn(len(workers))],
			Attempt: 1 + rng.Intn(6),
			Ticks:   rng.Intn(5),
			TTL:     1 + rng.Intn(5),
		})
	}
	return trace
}

func positiveTTL(ttl int) int {
	if ttl <= 0 {
		return 1
	}
	return ttl
}

func TestLeaseFencingModelFixedSchedules(t *testing.T) {
	testCases := map[string][]leaseModelEvent{
		"claim immediately before and after expiry": {
			{Kind: leaseModelClaim, Worker: "worker-a", TTL: 2},
			{Kind: leaseModelAdvance, Ticks: 2},
			{Kind: leaseModelClaim, Worker: "worker-b", TTL: 2},
			{Kind: leaseModelAdvance, Ticks: 1},
			{Kind: leaseModelClaim, Worker: "worker-b", TTL: 2},
			{Kind: leaseModelComplete, Worker: "worker-b", Attempt: 2},
		},
		"stale completion and heartbeat after reclaim": {
			{Kind: leaseModelClaim, Worker: "worker-a", TTL: 2},
			{Kind: leaseModelAdvance, Ticks: 3},
			{Kind: leaseModelClaim, Worker: "worker-b", TTL: 2},
			{Kind: leaseModelHeartbeat, Worker: "worker-a", Attempt: 1, TTL: 5},
			{Kind: leaseModelComplete, Worker: "worker-a", Attempt: 1},
			{Kind: leaseModelComplete, Worker: "worker-b", Attempt: 2},
			{Kind: leaseModelClaim, Worker: "worker-c", TTL: 2},
		},
		"cancel wins before completion": {
			{Kind: leaseModelClaim, Worker: "worker-a", TTL: 2},
			{Kind: leaseModelCancel},
			{Kind: leaseModelComplete, Worker: "worker-a", Attempt: 1},
		},
		"completion wins before cancel": {
			{Kind: leaseModelClaim, Worker: "worker-a", TTL: 2},
			{Kind: leaseModelComplete, Worker: "worker-a", Attempt: 1},
			{Kind: leaseModelCancel},
		},
		"drain preserves owned attempt": {
			{Kind: leaseModelClaim, Worker: "worker-a", TTL: 2},
			{Kind: leaseModelDrain, Worker: "worker-a"},
			{Kind: leaseModelHeartbeat, Worker: "worker-a", Attempt: 1, TTL: 2},
			{Kind: leaseModelComplete, Worker: "worker-a", Attempt: 1},
		},
		"shutdown leaves recovery to expiry": {
			{Kind: leaseModelClaim, Worker: "worker-a", TTL: 2},
			{Kind: leaseModelShutdown, Worker: "worker-a"},
			{Kind: leaseModelAdvance, Ticks: 3},
			{Kind: leaseModelClaim, Worker: "worker-b", TTL: 2},
			{Kind: leaseModelComplete, Worker: "worker-b", Attempt: 2},
		},
		"draining worker cannot take new claim": {
			{Kind: leaseModelDrain, Worker: "worker-a"},
			{Kind: leaseModelClaim, Worker: "worker-a", TTL: 2},
			{Kind: leaseModelClaim, Worker: "worker-b", TTL: 2},
			{Kind: leaseModelComplete, Worker: "worker-b", Attempt: 1},
		},
	}
	for name, trace := range testCases {
		t.Run(name, func(t *testing.T) {
			if err := checkLeaseModelTrace(trace, false); err != nil {
				t.Fatalf("model invariant failed:\n%s\n%v", formatLeaseModelTrace(0, trace), err)
			}
		})
	}
}

func TestLeaseFencingModelSeededSchedules(t *testing.T) {
	seeds := []int64{249, 0x5eed, 0x1ea5efce}
	var seedBytes [8]byte
	if _, err := rand.Read(seedBytes[:]); err != nil {
		t.Fatalf("random seed: %v", err)
	}
	seeds = append(seeds, int64(binary.LittleEndian.Uint64(seedBytes[:])&uint64(^uint64(0)>>1)))

	const schedulesPerSeed = 256
	const eventsPerSchedule = 48
	started := time.Now()
	for _, seed := range seeds {
		for schedule := range schedulesPerSeed {
			traceSeed := seed + int64(schedule)
			trace := randomLeaseModelTrace(traceSeed, eventsPerSchedule)
			if err := checkLeaseModelTrace(trace, false); err != nil {
				t.Fatalf("model invariant failed:\n%s\n%v", formatLeaseModelTrace(traceSeed, trace), err)
			}
		}
	}
	elapsed := time.Since(started)
	t.Logf("checked %d deterministic schedules (%d events) in %s; random seed=%d", len(seeds)*schedulesPerSeed, len(seeds)*schedulesPerSeed*eventsPerSchedule, elapsed, seeds[len(seeds)-1])
	if elapsed > 5*time.Second {
		t.Fatalf("model checker exceeded 5s CI budget: %s", elapsed)
	}
}

func TestLeaseFencingModelBrokenAttemptFenceFindsMinimalCounterexample(t *testing.T) {
	seed := int64(249)
	trace := []leaseModelEvent{
		{Kind: leaseModelClaim, Worker: "worker-a", TTL: 2},
		{Kind: leaseModelAdvance, Ticks: 3},
		{Kind: leaseModelClaim, Worker: "worker-a", TTL: 2},
		{Kind: leaseModelComplete, Worker: "worker-a", Attempt: 1},
	}
	trace = append(trace, randomLeaseModelTrace(seed, 12)...)
	if err := checkLeaseModelTrace(trace, true); err == nil {
		t.Fatalf("broken attempt fence did not produce a counterexample:\n%s", formatLeaseModelTrace(seed, trace))
	}

	shrunk := shrinkLeaseModelTrace(trace, func(candidate []leaseModelEvent) bool {
		return checkLeaseModelTrace(candidate, true) != nil
	})
	if len(shrunk) != 4 {
		t.Fatalf("minimal counterexample has %d events, want 4:\n%s", len(shrunk), formatLeaseModelTrace(seed, shrunk))
	}
	if err := checkLeaseModelTrace(shrunk, false); err != nil {
		t.Fatalf("correct fence rejected the shrunk schedule:\n%s\n%v", formatLeaseModelTrace(seed, shrunk), err)
	}
	t.Logf("broken fencing counterexample:\n%s", formatLeaseModelTrace(seed, shrunk))
}
