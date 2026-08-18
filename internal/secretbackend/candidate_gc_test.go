package secretbackend

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSideEffectingBackend struct {
	mu         sync.Mutex
	now        time.Time
	sequence   int
	created    int
	listCalls  int
	candidates map[string]fakeStoredCandidate
}

type fakeStoredCandidate struct {
	candidate RuntimeCandidate
	plaintext string
	stored    string
}

func newFakeSideEffectingBackend(now time.Time) *fakeSideEffectingBackend {
	return &fakeSideEffectingBackend{now: now.UTC(), candidates: map[string]fakeStoredCandidate{}}
}

func (backend *fakeSideEffectingBackend) Store(_ context.Context, reference Reference, plaintext string) (string, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	key := runtimeCandidateReferenceKey(reference)
	item, found := backend.candidates[key]
	if !found {
		backend.created++
	}
	backend.sequence++
	id := reference.Path[strings.LastIndex(reference.Path, "/")+1:]
	item.candidate = RuntimeCandidate{
		Reference: reference, CandidateID: id, LastTouchedAt: backend.now,
		Version: strconv.Itoa(backend.sequence),
	}
	item.plaintext = plaintext
	item.stored = "fake-reference:" + id
	backend.candidates[key] = item
	return item.stored, nil
}

func (backend *fakeSideEffectingBackend) Resolve(_ context.Context, reference Reference, stored string) (string, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	item, found := backend.candidates[runtimeCandidateReferenceKey(reference)]
	if !found || item.stored != stored {
		return "", errors.New("candidate not found")
	}
	return item.plaintext, nil
}

func (backend *fakeSideEffectingBackend) ListRuntimeCandidates(_ context.Context, request ListRuntimeCandidatesRequest) (RuntimeCandidatePage, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.listCalls++
	keys := make([]string, 0, len(backend.candidates))
	for key, item := range backend.candidates {
		if !item.candidate.LastTouchedAt.After(request.Before) && key > request.Cursor {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) > request.Limit {
		keys = keys[:request.Limit]
	}
	page := RuntimeCandidatePage{Candidates: make([]RuntimeCandidate, 0, len(keys))}
	for _, key := range keys {
		page.Candidates = append(page.Candidates, backend.candidates[key].candidate)
	}
	if len(keys) == request.Limit {
		page.NextCursor = keys[len(keys)-1]
	}
	return page, nil
}

func (backend *fakeSideEffectingBackend) DeleteRuntimeCandidate(_ context.Context, candidate RuntimeCandidate) (bool, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	key := runtimeCandidateReferenceKey(candidate.Reference)
	current, found := backend.candidates[key]
	if !found || current.candidate.Version != candidate.Version {
		return false, nil
	}
	delete(backend.candidates, key)
	return true, nil
}

func (backend *fakeSideEffectingBackend) setNow(now time.Time) {
	backend.mu.Lock()
	backend.now = now.UTC()
	backend.mu.Unlock()
}

func (backend *fakeSideEffectingBackend) candidateCount() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return len(backend.candidates)
}

type staticLiveReferences struct {
	references []Reference
	err        error
}

func (source staticLiveReferences) ListLiveRuntimeSecretCandidateReferences(context.Context) ([]Reference, error) {
	return append([]Reference(nil), source.references...), source.err
}

func TestSideEffectingBackendExactRetryTouchesOneReadableCandidate(t *testing.T) {
	now := time.Date(2026, 8, 19, 1, 0, 0, 0, time.UTC)
	backend := newFakeSideEffectingBackend(now)
	reference := runtimeCandidateTestReference("0123456789abcdef0123456789abcdef")
	first, err := backend.Store(context.Background(), reference, "secret-value")
	if err != nil {
		t.Fatal(err)
	}
	backend.setNow(now.Add(time.Minute))
	second, err := backend.Store(context.Background(), reference, "secret-value")
	if err != nil {
		t.Fatal(err)
	}
	if backend.created != 1 || backend.candidateCount() != 1 || first != second {
		t.Fatalf("created=%d candidates=%d first=%q second=%q", backend.created, backend.candidateCount(), first, second)
	}
	resolved, err := backend.Resolve(context.Background(), reference, second)
	if err != nil || resolved != "secret-value" {
		t.Fatalf("resolved=%q err=%v", resolved, err)
	}
}

func TestDatabaseBackendDoesNotRequireRuntimeCandidateCollection(t *testing.T) {
	if _, ok := any(NewDatabase(nil, "instance-secret", "")).(RuntimeCandidateCleaner); ok {
		t.Fatal("Database backend unexpectedly implements external candidate cleanup")
	}
}

func TestRuntimeCandidateCollectorReclaimsOnlyOldUnreferencedCandidates(t *testing.T) {
	now := time.Date(2026, 8, 19, 2, 0, 0, 0, time.UTC)
	backend := newFakeSideEffectingBackend(now.Add(-2 * time.Hour))
	orphan := runtimeCandidateTestReference("0123456789abcdef0123456789abcdef")
	live := runtimeCandidateTestReference("1123456789abcdef0123456789abcdef")
	if _, err := backend.Store(context.Background(), orphan, "orphan-secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Store(context.Background(), live, "live-secret"); err != nil {
		t.Fatal(err)
	}
	backend.setNow(now.Add(-30 * time.Minute))
	if _, err := backend.Store(context.Background(), runtimeCandidateTestReference("2123456789abcdef0123456789abcdef"), "fresh-secret"); err != nil {
		t.Fatal(err)
	}
	metrics := NewRuntimeCandidateMetrics()
	collector := RuntimeCandidateCollector{
		Backend: backend, Live: staticLiveReferences{references: []Reference{live}},
		GracePeriod: time.Hour, PageSize: 1, SweepLimit: 10,
		Now: func() time.Time { return now }, Metrics: metrics,
	}
	result, err := collector.Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned != 2 || result.Reclaimed != 1 || result.SkippedLive != 1 || result.Failed != 0 {
		t.Fatalf("result = %#v", result)
	}
	if backend.candidateCount() != 2 {
		t.Fatalf("candidate count = %d, want live and fresh", backend.candidateCount())
	}
	recorder := httptest.NewRecorder()
	metrics.Handler(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain")
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := recorder.Body.String()
	for _, expected := range []string{
		`outcome="scanned"} 2`, `outcome="reclaimed"} 1`, `outcome="skipped_live"} 1`, `outcome="failed"} 0`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, body)
		}
	}
	for _, secret := range []string{"orphan-secret", "live-secret", "fresh-secret"} {
		if strings.Contains(body, secret) {
			t.Fatalf("metrics exposed Secret plaintext")
		}
	}
}

func TestRuntimeCandidateConditionalDeleteDefersConcurrentPublication(t *testing.T) {
	now := time.Date(2026, 8, 19, 3, 0, 0, 0, time.UTC)
	backend := newFakeSideEffectingBackend(now.Add(-2 * time.Hour))
	reference := runtimeCandidateTestReference("3123456789abcdef0123456789abcdef")
	if _, err := backend.Store(context.Background(), reference, "first-secret"); err != nil {
		t.Fatal(err)
	}
	page, err := backend.ListRuntimeCandidates(context.Background(), ListRuntimeCandidatesRequest{Before: now.Add(-time.Hour), Limit: 1})
	if err != nil || len(page.Candidates) != 1 {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	backend.setNow(now)
	stored, err := backend.Store(context.Background(), reference, "published-secret")
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := backend.DeleteRuntimeCandidate(context.Background(), page.Candidates[0])
	if err != nil || deleted {
		t.Fatalf("concurrently touched candidate deleted=%v err=%v", deleted, err)
	}
	resolved, err := backend.Resolve(context.Background(), reference, stored)
	if err != nil || resolved != "published-secret" {
		t.Fatalf("resolved=%q err=%v", resolved, err)
	}
}

func TestRuntimeCandidateCollectorsAreSafeAcrossReplicas(t *testing.T) {
	now := time.Date(2026, 8, 19, 4, 0, 0, 0, time.UTC)
	backend := newFakeSideEffectingBackend(now.Add(-2 * time.Hour))
	if _, err := backend.Store(context.Background(), runtimeCandidateTestReference("4123456789abcdef0123456789abcdef"), "orphan-secret"); err != nil {
		t.Fatal(err)
	}
	collector := RuntimeCandidateCollector{
		Backend: backend, Live: staticLiveReferences{}, GracePeriod: time.Hour,
		Now: func() time.Time { return now },
	}
	var wait sync.WaitGroup
	results := make(chan RuntimeCandidateSweepResult, 2)
	errorsSeen := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := collector.Sweep(context.Background())
			results <- result
			errorsSeen <- err
		}()
	}
	wait.Wait()
	close(results)
	close(errorsSeen)
	var reclaimed uint64
	for result := range results {
		reclaimed += result.Reclaimed
	}
	for err := range errorsSeen {
		if err != nil {
			t.Fatal(err)
		}
	}
	if reclaimed != 1 || backend.candidateCount() != 0 {
		t.Fatalf("reclaimed=%d remaining=%d", reclaimed, backend.candidateCount())
	}
}

func TestRuntimeCandidateCollectorFailsClosedWhenLiveSnapshotFails(t *testing.T) {
	now := time.Date(2026, 8, 19, 5, 0, 0, 0, time.UTC)
	backend := newFakeSideEffectingBackend(now.Add(-2 * time.Hour))
	if _, err := backend.Store(context.Background(), runtimeCandidateTestReference("5123456789abcdef0123456789abcdef"), "secret-value"); err != nil {
		t.Fatal(err)
	}
	collector := RuntimeCandidateCollector{
		Backend: backend, Live: staticLiveReferences{err: errors.New("state unavailable: secret-value")},
		GracePeriod: time.Hour, Now: func() time.Time { return now },
	}
	result, err := collector.Sweep(context.Background())
	if err == nil || result.Failed != 1 || backend.listCalls != 0 || backend.candidateCount() != 1 {
		t.Fatalf("result=%#v err=%v listCalls=%d candidates=%d", result, err, backend.listCalls, backend.candidateCount())
	}
	if strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("collector error exposed a backend or state value: %v", err)
	}
}

func runtimeCandidateTestReference(candidateID string) Reference {
	return Reference{
		WorkspaceID: "ws-a",
		Kind:        "variable-app",
		Path:        "shop/session/" + candidateID,
	}
}

var _ Backend = (*fakeSideEffectingBackend)(nil)
var _ RuntimeCandidateCleaner = (*fakeSideEffectingBackend)(nil)
