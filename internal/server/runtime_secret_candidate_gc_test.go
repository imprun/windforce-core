package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/secretbackend"
	"github.com/imprun/windforce-core/internal/state"
)

type serverCandidateBackend struct {
	mu         sync.Mutex
	now        time.Time
	sequence   int
	created    int
	candidates map[string]serverCandidate
}

type serverCandidate struct {
	metadata  secretbackend.RuntimeCandidate
	stored    string
	plaintext string
}

func newServerCandidateBackend(now time.Time) *serverCandidateBackend {
	return &serverCandidateBackend{now: now.UTC(), candidates: map[string]serverCandidate{}}
}

func (backend *serverCandidateBackend) Store(_ context.Context, reference secretbackend.Reference, plaintext string) (string, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	key := serverCandidateKey(reference)
	item, found := backend.candidates[key]
	if !found {
		backend.created++
	}
	backend.sequence++
	id := reference.Path[strings.LastIndex(reference.Path, "/")+1:]
	item.metadata = secretbackend.RuntimeCandidate{
		Reference: reference, CandidateID: id, LastTouchedAt: backend.now,
		Version: strconv.Itoa(backend.sequence),
	}
	item.stored = "server-fake:" + id
	item.plaintext = plaintext
	backend.candidates[key] = item
	return item.stored, nil
}

func (backend *serverCandidateBackend) Resolve(_ context.Context, reference secretbackend.Reference, stored string) (string, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	item, found := backend.candidates[serverCandidateKey(reference)]
	if !found || item.stored != stored {
		return "", errors.New("candidate not found")
	}
	return item.plaintext, nil
}

func (backend *serverCandidateBackend) ListRuntimeCandidates(_ context.Context, request secretbackend.ListRuntimeCandidatesRequest) (secretbackend.RuntimeCandidatePage, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	keys := make([]string, 0, len(backend.candidates))
	for key, item := range backend.candidates {
		if key > request.Cursor && !item.metadata.LastTouchedAt.After(request.Before) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) > request.Limit {
		keys = keys[:request.Limit]
	}
	page := secretbackend.RuntimeCandidatePage{Candidates: make([]secretbackend.RuntimeCandidate, 0, len(keys))}
	for _, key := range keys {
		page.Candidates = append(page.Candidates, backend.candidates[key].metadata)
	}
	if len(keys) == request.Limit {
		page.NextCursor = keys[len(keys)-1]
	}
	return page, nil
}

func (backend *serverCandidateBackend) DeleteRuntimeCandidate(_ context.Context, candidate secretbackend.RuntimeCandidate) (bool, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	key := serverCandidateKey(candidate.Reference)
	current, found := backend.candidates[key]
	if !found || current.metadata.Version != candidate.Version {
		return false, nil
	}
	delete(backend.candidates, key)
	return true, nil
}

func (backend *serverCandidateBackend) candidateCount() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return len(backend.candidates)
}

func serverCandidateKey(reference secretbackend.Reference) string {
	return strings.Join([]string{reference.WorkspaceID, reference.Kind, reference.Path}, "\x00")
}

func TestRuntimeSecretCandidateGCReclaimsConflictsWithoutTouchingPublishedValue(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 19, 6, 0, 0, 0, time.UTC)
	store := state.NewLocalStore(t.TempDir() + "/state.json")
	job := enqueueServerRuntimeSecretJob(t, store)
	backend := newServerCandidateBackend(now)
	handler := &Handler{store: store, secretBackend: backend}

	first := runtimeSecretMutationRequest(t, handler, job, `{"value":"published-value","operationId":"publish"}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	replay := runtimeSecretMutationRequest(t, handler, job, `{"value":"published-value","operationId":"publish"}`)
	if replay.Code != http.StatusOK || !strings.Contains(replay.Body.String(), `"replayed":true`) {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	conflict := runtimeSecretMutationRequest(t, handler, job, `{"value":"conflicting-value","operationId":"publish"}`)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("idempotency conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	cas := runtimeSecretMutationRequest(t, handler, job, `{"value":"stale-cas-value","operationId":"stale-cas","expectedRevision":0}`)
	if cas.Code != http.StatusConflict {
		t.Fatalf("CAS conflict status=%d body=%s", cas.Code, cas.Body.String())
	}
	if backend.created != 3 || backend.candidateCount() != 3 {
		t.Fatalf("created=%d candidates=%d, want one published and two orphan candidates", backend.created, backend.candidateCount())
	}

	collector := secretbackend.RuntimeCandidateCollector{
		Backend:     backend,
		Live:        store,
		GracePeriod: time.Hour,
		Now:         func() time.Time { return now.Add(2 * time.Hour) },
	}
	result, err := collector.Sweep(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned != 3 || result.Reclaimed != 2 || result.SkippedLive != 1 || backend.candidateCount() != 1 {
		t.Fatalf("result=%#v remaining=%d", result, backend.candidateCount())
	}
	variable, found, err := store.GetVariableScoped(ctx, "ws-a", contract.RuntimeConfigScopeApp, "shop", "session")
	if err != nil || !found || variable.Revision != 1 {
		t.Fatalf("published Variable=%#v found=%v err=%v", variable, found, err)
	}
	reference, stored, err := secretbackend.OpenRuntimeCandidate(secretbackend.Reference{
		WorkspaceID: "ws-a", Kind: "variable-app", Path: "shop/session",
	}, variable.Value)
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := backend.Resolve(ctx, reference, stored)
	if err != nil || plaintext != "published-value" {
		t.Fatalf("published Secret resolved=%q err=%v", plaintext, err)
	}
	if _, err := store.SetAppRuntimeLifecycle(ctx, state.SetAppRuntimeLifecycleRequest{
		WorkspaceID: "ws-a", AppKey: "shop", State: state.AppRuntimeRevoked,
		Actor: "security", Reason: "incident",
	}); err != nil {
		t.Fatal(err)
	}
	collector.Now = func() time.Time { return now.Add(3 * time.Hour) }
	retained, err := collector.Sweep(ctx)
	if err != nil || retained.SkippedLive != 1 || backend.candidateCount() != 1 {
		t.Fatalf("revoked candidate result=%#v remaining=%d err=%v", retained, backend.candidateCount(), err)
	}
	if err := store.PurgeAppRuntimeConfig(ctx, state.PurgeAppRuntimeConfigRequest{
		WorkspaceID: "ws-a", AppKey: "shop", Actor: "operator", Reason: "retired", Force: true,
	}); err != nil {
		t.Fatal(err)
	}
	collector.Now = func() time.Time { return now.Add(4 * time.Hour) }
	purged, err := collector.Sweep(ctx)
	if err != nil || purged.Reclaimed != 1 || backend.candidateCount() != 0 {
		t.Fatalf("purged candidate result=%#v remaining=%d err=%v", purged, backend.candidateCount(), err)
	}
}

func enqueueServerRuntimeSecretJob(t *testing.T, store state.Store) state.Job {
	t.Helper()
	access := contract.RuntimeAccess{WriteVariables: []contract.RuntimeVariableWriteTarget{{
		RuntimeConfigTarget: contract.RuntimeConfigTarget{Scope: contract.RuntimeConfigScopeApp, Path: "session"},
		Storage:             contract.RuntimeVariableStorageSecret,
	}}}
	deployment := contract.Deployment{Workspace: "ws-a", App: "shop", Actions: map[string]contract.Action{
		"run": {RuntimeAccess: access},
	}}
	run := state.NewRun("ws-a", state.NewID("run"), "shop", "run", deployment, json.RawMessage(`{}`))
	job := state.NewActionJob(run, json.RawMessage(`{}`))
	job.Payload.RuntimeAccess = contract.CloneRuntimeAccess(access)
	if err := store.CreateRunAndEnqueue(context.Background(), run, job); err != nil {
		t.Fatal(err)
	}
	claimed, _, err := store.ClaimJob(context.Background(), "worker-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return claimed
}

func runtimeSecretMutationRequest(t *testing.T, handler *Handler, job state.Job, rawBody string) *httptest.ResponseRecorder {
	t.Helper()
	body := []byte(rawBody)
	token := "job-token"
	request := httptest.NewRequest(http.MethodPut, "/api/w/ws-a/variables/p/session?scope=app", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	digest := sha256.Sum256(body)
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write(digest[:])
	request.Header.Set(secretMaskRegistrationHeader, hex.EncodeToString(mac.Sum(nil)))
	request = request.WithContext(context.WithValue(request.Context(), principalContextKey{}, &jobPrincipal{
		Workspace: "ws-a", JobID: job.ID, Attempt: job.Attempt,
	}))
	response := httptest.NewRecorder()
	handler.handleRuntimeSetVariable(response, request, "ws-a", "session")
	return response
}

var _ secretbackend.Backend = (*serverCandidateBackend)(nil)
var _ secretbackend.RuntimeCandidateCleaner = (*serverCandidateBackend)(nil)
