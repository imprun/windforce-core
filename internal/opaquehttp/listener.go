package opaquehttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

const (
	// IngressPath is the only path that admits a trusted envelope. It is fixed
	// rather than configurable so the listener surface is enumerable: one
	// admitting path, one readiness path, and nothing else.
	IngressPath = "/ingress/opaque-http/v1"
	// ReadinessPath answers whether this listener may receive traffic. It
	// admits nothing and carries no trusted value.
	ReadinessPath = "/readyz"

	// DefaultMaxConcurrent bounds how many deliveries this listener admits at
	// once. Each one holds a Run wait, so the bound is what keeps a burst from
	// turning into unbounded in-flight work.
	DefaultMaxConcurrent = 64
	// DefaultAcquireWait is how long a delivery waits for a slot before the
	// listener sheds it. Shedding early is the point: a delivery that queues
	// until its own deadline reads as a Core hang at the gateway.
	DefaultAcquireWait = 50 * time.Millisecond

	maxListenerConcurrency = 4096
	maxListenerAcquireWait = time.Second
)

// ListenerOptions configures the isolated ingress listener.
type ListenerOptions struct {
	// MaxConcurrent bounds concurrent deliveries; zero uses DefaultMaxConcurrent.
	MaxConcurrent int
	// AcquireWait bounds how long a delivery waits for a slot; zero uses
	// DefaultAcquireWait.
	AcquireWait time.Duration
	// Ready reports whether the listener's dependencies are usable. A nil probe
	// reports ready.
	Ready func() bool
}

// Listener is the isolated private surface for the opaque HTTP ingress
// conformance handler. It exists so the trusted envelope is accepted on one
// listener and nowhere else: Core's primary listener never serves this handler,
// and this listener serves nothing else.
//
// It does not terminate TLS or authenticate a peer. Which mechanism proves the
// peer — mutually authenticated TLS or a network boundary — is a deployment
// property (ADR 0061), and Core neither implements nor detects it.
type Listener struct {
	ingress     http.Handler
	slots       chan struct{}
	acquireWait time.Duration
	ready       func() bool
}

// NewListener wraps the ingress handler with admission control and readiness.
func NewListener(ingress http.Handler, options ListenerOptions) (*Listener, error) {
	if ingress == nil {
		return nil, errors.New("opaque HTTP ingress handler is required")
	}
	maxConcurrent := options.MaxConcurrent
	if maxConcurrent == 0 {
		maxConcurrent = DefaultMaxConcurrent
	}
	if maxConcurrent < 1 || maxConcurrent > maxListenerConcurrency {
		return nil, fmt.Errorf("opaque HTTP ingress concurrency must be between 1 and %d", maxListenerConcurrency)
	}
	acquireWait := options.AcquireWait
	if acquireWait == 0 {
		acquireWait = DefaultAcquireWait
	}
	if acquireWait < 0 || acquireWait > maxListenerAcquireWait {
		return nil, fmt.Errorf("opaque HTTP ingress acquire wait must be positive and no greater than %s", maxListenerAcquireWait)
	}
	return &Listener{
		ingress:     ingress,
		slots:       make(chan struct{}, maxConcurrent),
		acquireWait: acquireWait,
		ready:       options.Ready,
	}, nil
}

func (l *Listener) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case ReadinessPath:
		l.writeReadiness(w, request)
	case IngressPath:
		l.serveIngress(w, request)
	default:
		writePlatformFailureResponse(w, http.StatusNotFound, FailureApplicationProtocolViolation, false)
	}
}

// Ready reports the current readiness of this listener's dependencies.
func (l *Listener) Ready() bool {
	return l == nil || l.ready == nil || l.ready()
}

func (l *Listener) serveIngress(w http.ResponseWriter, request *http.Request) {
	if !l.acquire(request.Context()) {
		writePlatformFailureResponse(w, http.StatusServiceUnavailable, FailureCapacityUnavailable, true)
		return
	}
	defer l.release()
	l.ingress.ServeHTTP(w, request)
}

// acquire takes an admission slot, waiting no longer than the configured bound.
func (l *Listener) acquire(ctx context.Context) bool {
	timer := time.NewTimer(l.acquireWait)
	defer timer.Stop()
	select {
	case l.slots <- struct{}{}:
		return true
	case <-timer.C:
		return false
	case <-ctx.Done():
		return false
	}
}

func (l *Listener) release() {
	<-l.slots
}

func (l *Listener) writeReadiness(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writePlatformFailureResponse(w, http.StatusMethodNotAllowed, FailureApplicationProtocolViolation, false)
		return
	}
	ready := l.Ready()
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]bool{"ready": ready})
}
