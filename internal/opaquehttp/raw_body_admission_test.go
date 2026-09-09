package opaquehttp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/execution"
	"github.com/imprun/windforce-core/internal/state"
)

type rawAdmissionStore interface {
	execution.Store
	execution.Catalog
	PublishRelease(context.Context, contract.Deployment, time.Time) (catalog.ReleasePublication, error)
	ClaimJob(context.Context, string, time.Duration) (state.Job, state.Lease, error)
	CompleteJobSucceeded(context.Context, state.Lease, contract.JobResult) error
}

// rawRoundtripAdmission executes only newly admitted Jobs. Replays must return
// the persisted output without claiming or creating another Job.
type rawRoundtripAdmission struct {
	*recordingAdmission
	store rawAdmissionStore
}

func (a *rawRoundtripAdmission) CreateRun(ctx context.Context, request execution.CreateRunRequest) (execution.Admission, error) {
	admitted, err := a.recordingAdmission.CreateRun(ctx, request)
	if err != nil || admitted.Replayed {
		return admitted, err
	}
	job, lease, err := a.store.ClaimJob(ctx, "synthetic-raw-roundtrip-worker", time.Minute)
	if err != nil {
		return execution.Admission{}, fmt.Errorf("claim synthetic Job: %w", err)
	}
	if job.RunID != admitted.Run.ID || job.ID != admitted.Job.ID || !job.Payload.InputConfigResolved {
		return execution.Admission{}, fmt.Errorf("claimed Job did not preserve the admission identity and resolved input")
	}
	var input OpaqueHTTPAppInputV1
	if err := json.Unmarshal(job.Payload.Input, &input); err != nil {
		return execution.Admission{}, err
	}
	// A normal App response may itself have status 409. Only the platform
	// execution-outcome envelope below denotes an idempotency conflict.
	output, err := json.Marshal(ApplicationWireResponseV1{
		Kind: ApplicationWireResponseKindV1, Status: http.StatusConflict,
		Headers: []ResponseHeaderV1{{Name: "content-type", Value: "application/octet-stream"}},
		Body:    input.Body,
	})
	if err != nil {
		return execution.Admission{}, err
	}
	if err := a.store.CompleteJobSucceeded(ctx, lease, contract.JobResult{
		JobID: job.ID, App: testApp, Action: testAction, Output: output,
	}); err != nil {
		return execution.Admission{}, fmt.Errorf("complete synthetic Job: %w", err)
	}
	admitted.Run, err = a.store.GetRun(ctx, admitted.Run.ID)
	return admitted, err
}

func TestRawBodyAdmissionTransportParity(t *testing.T) {
	for _, backend := range []string{"local", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			for _, order := range []struct {
				name          string
				first, replay TransportMode
			}{
				{"raw-to-envelope", TransportRawBodyV1, TransportEnvelopeV1},
				{"envelope-to-raw", TransportEnvelopeV1, TransportRawBodyV1},
			} {
				t.Run(order.name, func(t *testing.T) {
					store, counts := newRawAdmissionStore(t, backend)
					publishRawAdmissionRelease(t, store)
					admission := &rawRoundtripAdmission{
						recordingAdmission: &recordingAdmission{inner: execution.NewAdmissionService(store, store, nil)},
						store:              store,
					}
					servers := make(map[TransportMode]*httptest.Server)
					for _, mode := range []TransportMode{TransportEnvelopeV1, TransportRawBodyV1} {
						limits := testLimits(5 * time.Second)
						limits.Transport = mode
						handler, err := NewHandler(resolverFunc(func(context.Context, ResolutionRequest) (ResolvedAdmission, error) {
							return validResolvedAdmission(), nil
						}), admission, limits)
						if err != nil {
							t.Fatal(err)
						}
						listener, err := NewListener(handler, ListenerOptions{})
						if err != nil {
							t.Fatal(err)
						}
						servers[mode] = httptest.NewServer(listener)
						t.Cleanup(servers[mode].Close)
					}

					// Invalid UTF-8, NUL and JSON-like bytes must survive both transports.
					original := []byte{0xff, 0x00, 0xc3, 0x28, '{', '}', '\n', 0x80}
					deliver := func(mode TransportMode, deliveryID string, body []byte) (int, http.Header, []byte) {
						t.Helper()
						invocation := invocationValue(t, func(invocation *OpaqueHTTPInvocationV1) {
							invocation.TrustedIngress.DeliveryID = deliveryID
							invocation.ReceivedAt = time.Now().UTC()
							invocation.DeadlineAt = invocation.ReceivedAt.Add(5 * time.Second)
							invocation.Body = rawAdmissionBody(body)
						})
						request := rawAdmissionHTTPRequest(t, servers[mode].URL, mode, invocation, body)
						client := servers[mode].Client()
						client.Timeout = 10 * time.Second
						response, err := client.Do(request)
						if err != nil {
							t.Fatal(err)
						}
						defer response.Body.Close()
						result, err := io.ReadAll(response.Body)
						if err != nil {
							t.Fatal(err)
						}
						return response.StatusCode, response.Header, result
					}
					assertCompleted := func(mode TransportMode, deliveryID string) {
						t.Helper()
						status, headers, result := deliver(mode, deliveryID, original)
						if status != http.StatusConflict || headers.Get("Content-Type") != "application/octet-stream" || !bytes.Equal(result, original) {
							t.Fatalf("completed App output changed: status=%d content-type=%q bytes=%x", status, headers.Get("Content-Type"), result)
						}
					}
					assertCounts := func(want int) {
						t.Helper()
						runs, jobs := counts(t)
						if runs != want || jobs != want {
							t.Fatalf("durable counts Runs=%d Jobs=%d, want %d pairs", runs, jobs, want)
						}
					}

					assertCompleted(order.first, "synthetic-raw-attempt-one")
					assertCounts(1)
					assertCompleted(order.replay, "synthetic-raw-attempt-one")
					assertCounts(1)
					requests, results := admission.snapshot()
					if len(results) != 2 || results[0].Replayed || !results[1].Replayed ||
						results[0].Run.ID != results[1].Run.ID || results[0].Job.ID != results[1].Job.ID {
						t.Fatalf("cross-transport replay did not return the original Run/Job: %+v", results)
					}
					if len(requests) != 2 || requests[0].IdempotencyKey != requests[1].IdempotencyKey || !bytes.Equal(requests[0].Input, requests[1].Input) {
						t.Fatal("transport or refreshed deadlines changed the admission identity/input")
					}
					var appInput map[string]json.RawMessage
					if err := json.Unmarshal(requests[0].Input, &appInput); err != nil {
						t.Fatal(err)
					}
					if len(appInput) != 3 || appInput["kind"] == nil || appInput["http"] == nil || appInput["body"] == nil {
						t.Fatal("trusted context or transport metadata leaked into the App input")
					}

					changed := append(append([]byte(nil), original...), '!')
					status, headers, failure := deliver(order.replay, "synthetic-raw-attempt-one", changed)
					if status != http.StatusConflict || headers.Get("Content-Type") != "application/json" {
						t.Fatalf("idempotency conflict status=%d content-type=%q", status, headers.Get("Content-Type"))
					}
					assertPlatformFailureCategory(t, failure, FailureApplicationProtocolViolation)
					var outcome ExecutionOutcomeV1
					if err := json.Unmarshal(failure, &outcome); err != nil || outcome.Failure.Retryable {
						t.Fatalf("conflict must be a non-retryable execution outcome: %s", failure)
					}
					assertCounts(1)

					assertCompleted(order.replay, "synthetic-raw-attempt-two")
					assertCounts(2)
					_, results = admission.snapshot()
					if len(results) != 3 || results[2].Replayed || results[2].Run.ID == results[0].Run.ID || results[2].Job.ID == results[0].Job.ID {
						t.Fatal("new delivery identity did not create a distinct Run/Job")
					}
					assertConcurrentRawAdmissionReplay(t, servers, admission, original)
					assertCounts(3)
				})
			}
		})
	}
}

func assertConcurrentRawAdmissionReplay(t *testing.T, servers map[TransportMode]*httptest.Server, admission *rawRoundtripAdmission, original []byte) {
	t.Helper()
	beforeRequests, beforeResults := admission.snapshot()
	const callers = 8
	type deliveryResult struct {
		status      int
		contentType string
		body        []byte
		err         error
	}
	completed := make(chan deliveryResult, callers)
	start := make(chan struct{})
	for index := range callers {
		mode := TransportRawBodyV1
		if index%2 != 0 {
			mode = TransportEnvelopeV1
		}
		invocation := invocationValue(t, func(invocation *OpaqueHTTPInvocationV1) {
			invocation.TrustedIngress.DeliveryID = "synthetic-raw-attempt-concurrent"
			invocation.DeadlineAt = invocation.ReceivedAt.Add(5 * time.Second)
			invocation.Body = rawAdmissionBody(original)
		})
		request := rawAdmissionHTTPRequest(t, servers[mode].URL, mode, invocation, original)
		client := servers[mode].Client()
		go func() {
			<-start
			response, err := client.Do(request)
			if err != nil {
				completed <- deliveryResult{err: err}
				return
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			completed <- deliveryResult{status: response.StatusCode, contentType: response.Header.Get("Content-Type"), body: body, err: err}
		}()
	}
	close(start)
	for range callers {
		result := <-completed
		if result.err != nil {
			t.Errorf("concurrent delivery failed: %v", result.err)
			continue
		}
		if result.status != http.StatusConflict || result.contentType != "application/octet-stream" || !bytes.Equal(result.body, original) {
			t.Errorf("concurrent completed output changed: status=%d content-type=%q bytes=%x", result.status, result.contentType, result.body)
		}
	}
	requests, results := admission.snapshot()
	if len(requests) != len(beforeRequests)+callers || len(results) != len(beforeResults)+callers {
		t.Fatalf("concurrent calls did not all reach successful Admission: requests=%d results=%d", len(requests)-len(beforeRequests), len(results)-len(beforeResults))
	}
	concurrent := results[len(beforeResults):]
	created := 0
	for _, result := range concurrent {
		if !result.Replayed {
			created++
		}
		if result.Run.ID != concurrent[0].Run.ID || result.Job.ID != concurrent[0].Job.ID {
			t.Fatal("concurrent raw/envelope deliveries resolved to different Run/Job pairs")
		}
	}
	if created != 1 {
		t.Fatalf("concurrent first admissions=%d, want exactly one", created)
	}
	for _, previous := range beforeResults {
		if concurrent[0].Run.ID == previous.Run.ID || concurrent[0].Job.ID == previous.Job.ID {
			t.Fatal("new concurrent delivery reused an earlier attempt's Run/Job")
		}
	}
	concurrentRequests := requests[len(beforeRequests):]
	for _, request := range concurrentRequests {
		if request.IdempotencyKey != concurrentRequests[0].IdempotencyKey ||
			!bytes.Equal(request.Input, concurrentRequests[0].Input) ||
			request.Workspace != testWorkspace || !reflect.DeepEqual(request.Principal, testPrincipal()) ||
			!reflect.DeepEqual(request.InvocationPins, validResolvedAdmission().InvocationPins) {
			t.Fatal("concurrent transport changed input, identity, principal scope, or invocation pins")
		}
	}
}

func rawAdmissionBody(raw []byte) BodyBytesV1 {
	digest := sha256.Sum256(raw)
	return BodyBytesV1{Encoding: RFC4648Base64Encoding, Data: base64.StdEncoding.EncodeToString(raw), ByteLength: int64(len(raw)), Digest: "sha256:" + hex.EncodeToString(digest[:])}
}

func rawAdmissionHTTPRequest(t *testing.T, address string, mode TransportMode, invocation OpaqueHTTPInvocationV1, body []byte) *http.Request {
	t.Helper()
	envelope, err := json.Marshal(invocation)
	if err != nil {
		t.Fatal(err)
	}
	contentType := "application/json"
	requestBody := envelope
	contextHeader := ""
	if mode == TransportRawBodyV1 {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(envelope, &fields); err != nil {
			t.Fatal(err)
		}
		delete(fields, "body")
		fields["kind"], _ = json.Marshal(RawContextKindV1)
		contextJSON, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		contextHeader = string(contextJSON)
		contentType = "application/octet-stream"
		requestBody = body
	}
	request, err := http.NewRequest(http.MethodPost, address+IngressPath, bytes.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", contentType)
	if contextHeader != "" {
		request.Header.Set(RawContextHeader, contextHeader)
	}
	return request
}

func publishRawAdmissionRelease(t *testing.T, store rawAdmissionStore) {
	t.Helper()
	deploymentID := testDeploymentID
	_, err := store.PublishRelease(context.Background(), contract.Deployment{
		Workspace: testWorkspace, GitSourceID: "synthetic-source", APIVersion: contract.AppManifestV2,
		App: testApp, Commit: testCommit, DeploymentID: &deploymentID, BundleDigest: testBundleDigest,
		ObjectURI: "bundle://synthetic/source/commit",
		Actions: map[string]contract.Action{testAction: {
			Action: testAction, InputSchemaBody: contractFixture(t, "opaque-http-app-input.schema.json"),
			OutputSchemaBody: contractFixture(t, "application-wire-response.schema.json"),
		}},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("publish synthetic release: %v", err)
	}
}

func newRawAdmissionStore(t *testing.T, backend string) (rawAdmissionStore, func(*testing.T) (int, int)) {
	t.Helper()
	if backend == "local" {
		store := state.NewLocalStore(filepath.Join(t.TempDir(), "state.json"))
		return store, func(t *testing.T) (int, int) {
			t.Helper()
			snapshot, err := store.Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			return len(snapshot.Runs), len(snapshot.Jobs)
		}
	}
	dsn := strings.TrimSpace(os.Getenv("WINDFORCE_CORE_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("WINDFORCE_CORE_POSTGRES_TEST_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("connect to configured PostgreSQL test database failed")
	}
	schema := fmt.Sprintf("opaque_raw_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		_ = admin.Close(context.Background())
		t.Fatalf("create isolated test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Errorf("remove isolated test schema: %v", err)
		}
		_ = admin.Close(cleanupCtx)
	})
	scopedDSN := dsn + " search_path=" + schema
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			t.Fatal("parse configured PostgreSQL test URL failed")
		}
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		scopedDSN = parsed.String()
	}
	store, err := state.OpenPostgresStore(ctx, scopedDSN)
	if err != nil {
		t.Fatal("open isolated PostgreSQL test store failed")
	}
	t.Cleanup(store.Close)
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate isolated PostgreSQL test store: %v", err)
	}
	return store, func(t *testing.T) (int, int) {
		t.Helper()
		var runs, jobs int
		query := "SELECT (SELECT count(*) FROM " + identifier + ".runs), (SELECT count(*) FROM " + identifier + ".jobs)"
		if err := admin.QueryRow(context.Background(), query).Scan(&runs, &jobs); err != nil {
			t.Fatalf("count isolated durable records: %v", err)
		}
		return runs, jobs
	}
}
