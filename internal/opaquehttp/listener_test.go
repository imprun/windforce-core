package opaquehttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func decodeOutcome(t *testing.T, recorder *httptest.ResponseRecorder) ExecutionOutcomeV1 {
	t.Helper()
	var outcome ExecutionOutcomeV1
	if err := json.Unmarshal(recorder.Body.Bytes(), &outcome); err != nil {
		t.Fatalf("decode outcome: %v", err)
	}
	return outcome
}

func newTestListener(t *testing.T, ingress http.Handler, options ListenerOptions) *Listener {
	t.Helper()
	listener, err := NewListener(ingress, options)
	if err != nil {
		t.Fatalf("new listener: %v", err)
	}
	return listener
}

func TestListenerServesIngressOnlyOnTheIngressPath(t *testing.T) {
	served := 0
	listener := newTestListener(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		served++
		w.WriteHeader(http.StatusOK)
	}), ListenerOptions{})

	recorder := httptest.NewRecorder()
	listener.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, IngressPath, nil))
	if recorder.Code != http.StatusOK || served != 1 {
		t.Fatalf("ingress path: status %d served %d", recorder.Code, served)
	}

	for _, path := range []string{"/", "/api/v1/runs", IngressPath + "/extra", "/ingress/opaque-http/v2"} {
		recorder := httptest.NewRecorder()
		listener.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("path %s: status %d, want 404", path, recorder.Code)
		}
		outcome := decodeOutcome(t, recorder)
		if outcome.Outcome != ExecutionOutcomePlatformFailed || outcome.Failure == nil {
			t.Fatalf("path %s: outcome %+v", path, outcome)
		}
		if outcome.Failure.Category != FailureApplicationProtocolViolation || outcome.Failure.Retryable {
			t.Fatalf("path %s: failure %+v", path, outcome.Failure)
		}
	}
	if served != 1 {
		t.Fatalf("ingress handler served %d requests, want 1", served)
	}
}

func TestListenerReadinessReportsTheProbe(t *testing.T) {
	ready := true
	listener := newTestListener(t, http.NotFoundHandler(), ListenerOptions{Ready: func() bool { return ready }})

	recorder := httptest.NewRecorder()
	listener.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, ReadinessPath, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("ready status %d, want 200", recorder.Code)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache control %q", recorder.Header().Get("Cache-Control"))
	}
	var body map[string]bool
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readiness: %v", err)
	}
	if !body["ready"] {
		t.Fatalf("readiness body %+v", body)
	}

	ready = false
	recorder = httptest.NewRecorder()
	listener.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, ReadinessPath, nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("unready status %d, want 503", recorder.Code)
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readiness: %v", err)
	}
	if body["ready"] {
		t.Fatalf("readiness body %+v", body)
	}
}

func TestListenerReadinessRejectsWritingMethods(t *testing.T) {
	listener := newTestListener(t, http.NotFoundHandler(), ListenerOptions{})
	recorder := httptest.NewRecorder()
	listener.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, ReadinessPath, nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405", recorder.Code)
	}
	outcome := decodeOutcome(t, recorder)
	if outcome.Failure == nil || outcome.Failure.Category != FailureApplicationProtocolViolation {
		t.Fatalf("outcome %+v", outcome)
	}
}

func TestListenerShedsDeliveriesBeyondTheConcurrencyBound(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	listener := newTestListener(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	}), ListenerOptions{MaxConcurrent: 1, AcquireWait: 20 * time.Millisecond})

	first := make(chan int, 1)
	go func() {
		recorder := httptest.NewRecorder()
		listener.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, IngressPath, nil))
		first <- recorder.Code
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("first delivery never reached the ingress handler")
	}

	recorder := httptest.NewRecorder()
	listener.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, IngressPath, nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("shed status %d, want 503", recorder.Code)
	}
	outcome := decodeOutcome(t, recorder)
	if outcome.Failure == nil || outcome.Failure.Category != FailureCapacityUnavailable || !outcome.Failure.Retryable {
		t.Fatalf("shed outcome %+v", outcome)
	}

	close(release)
	if code := <-first; code != http.StatusOK {
		t.Fatalf("first delivery status %d, want 200", code)
	}

	// Shedding released the wait without consuming a slot, so the same listener
	// admits again once the first delivery finishes.
	recorder = httptest.NewRecorder()
	listener.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, IngressPath, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("post-shed status %d, want 200", recorder.Code)
	}
}

func TestNewListenerRejectsUnusableConfiguration(t *testing.T) {
	cases := []struct {
		name    string
		ingress http.Handler
		options ListenerOptions
	}{
		{name: "no ingress handler", ingress: nil, options: ListenerOptions{}},
		{name: "negative concurrency", ingress: http.NotFoundHandler(), options: ListenerOptions{MaxConcurrent: -1}},
		{name: "concurrency above the bound", ingress: http.NotFoundHandler(), options: ListenerOptions{MaxConcurrent: maxListenerConcurrency + 1}},
		{name: "acquire wait above the bound", ingress: http.NotFoundHandler(), options: ListenerOptions{AcquireWait: maxListenerAcquireWait + time.Millisecond}},
		{name: "negative acquire wait", ingress: http.NotFoundHandler(), options: ListenerOptions{AcquireWait: -time.Millisecond}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := NewListener(testCase.ingress, testCase.options); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestListenerDefaultsAdmitDeliveries(t *testing.T) {
	listener := newTestListener(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), ListenerOptions{})
	if cap(listener.slots) != DefaultMaxConcurrent {
		t.Fatalf("slots %d, want %d", cap(listener.slots), DefaultMaxConcurrent)
	}
	if listener.acquireWait != DefaultAcquireWait {
		t.Fatalf("acquire wait %s, want %s", listener.acquireWait, DefaultAcquireWait)
	}
	if !listener.Ready() {
		t.Fatal("a listener without a probe reports unready")
	}
}
