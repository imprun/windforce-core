package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/imprun/windforce-core/internal/attestation"
	"github.com/imprun/windforce-core/internal/opaquehttp"
	"github.com/imprun/windforce-core/internal/telemetry"
)

const (
	defaultOpaqueIngressMaxRequestBytes  = 1 << 20
	defaultOpaqueIngressMaxResponseBytes = 1 << 20
	defaultOpaqueIngressMaxWait          = 30 * time.Second
	defaultOpaqueIngressPollInterval     = 50 * time.Millisecond
	opaqueIngressShutdownBudget          = 15 * time.Second
)

// opaqueIngressFlags configures the isolated opaque HTTP ingress listener and
// the execution attestation issuer that a deployment mounting it may need.
// The listener stays unmounted until an address is given.
type opaqueIngressFlags struct {
	addr                *string
	maxRequestBytes     *int
	maxResponseBytes    *int
	maxWait             *time.Duration
	pollInterval        *time.Duration
	maxConcurrent       *int
	acquireWait         *time.Duration
	attestationKeyFile  *string
	attestationKeyID    *string
	attestationAudience *string
	attestationTTL      *time.Duration
}

func bindOpaqueIngressFlags(flags *flag.FlagSet, prefix string) opaqueIngressFlags {
	return opaqueIngressFlags{
		addr: flags.String(prefix+"addr", envString("WINDFORCE_CORE_OPAQUE_INGRESS_ADDR"),
			"address of the isolated private listener that admits trusted opaque HTTP envelopes; empty leaves it unmounted"),
		maxRequestBytes: flags.Int(prefix+"max-request-bytes", envInt("WINDFORCE_CORE_OPAQUE_INGRESS_MAX_REQUEST_BYTES", defaultOpaqueIngressMaxRequestBytes),
			"maximum decoded request body bytes admitted through the opaque ingress"),
		maxResponseBytes: flags.Int(prefix+"max-response-bytes", envInt("WINDFORCE_CORE_OPAQUE_INGRESS_MAX_RESPONSE_BYTES", defaultOpaqueIngressMaxResponseBytes),
			"maximum decoded response body bytes returned through the opaque ingress"),
		maxWait: flags.Duration(prefix+"max-wait", envParsedDuration("WINDFORCE_CORE_OPAQUE_INGRESS_MAX_WAIT", defaultOpaqueIngressMaxWait),
			"maximum synchronous wait for an admitted Run before the ingress reports a deadline"),
		pollInterval: flags.Duration(prefix+"poll-interval", envParsedDuration("WINDFORCE_CORE_OPAQUE_INGRESS_POLL_INTERVAL", defaultOpaqueIngressPollInterval),
			"how often the opaque ingress polls an admitted Run for completion"),
		maxConcurrent: flags.Int(prefix+"max-concurrent", envInt("WINDFORCE_CORE_OPAQUE_INGRESS_MAX_CONCURRENT", opaquehttp.DefaultMaxConcurrent),
			"maximum concurrent deliveries admitted by the opaque ingress listener"),
		acquireWait: flags.Duration(prefix+"acquire-wait", envParsedDuration("WINDFORCE_CORE_OPAQUE_INGRESS_ACQUIRE_WAIT", opaquehttp.DefaultAcquireWait),
			"how long a delivery waits for an admission slot before the listener sheds it"),
		attestationKeyFile: flags.String(prefix+"attestation-key-file", envString("WINDFORCE_CORE_EXECUTION_ATTESTATION_KEY_FILE"),
			"PEM PKCS#8 Ed25519 private key that signs execution attestations; empty mints none"),
		attestationKeyID: flags.String(prefix+"attestation-key-id", envString("WINDFORCE_CORE_EXECUTION_ATTESTATION_KEY_ID"),
			"key id a downstream verifier uses to select the trusted execution attestation public key"),
		attestationAudience: flags.String(prefix+"attestation-audience", envString("WINDFORCE_CORE_EXECUTION_ATTESTATION_AUDIENCE"),
			"audience of the downstream capability service execution attestations are minted for"),
		attestationTTL: flags.Duration(prefix+"attestation-ttl", envParsedDuration("WINDFORCE_CORE_EXECUTION_ATTESTATION_TTL", attestation.DefaultTTL),
			"execution attestation lifetime"),
	}
}

// enabled reports whether this deployment mounts the isolated ingress listener.
func (f opaqueIngressFlags) enabled() bool {
	return strings.TrimSpace(*f.addr) != ""
}

// executionAttestationIssuer builds the issuer when a deployment configured one.
// It returns no issuer and no error when none is configured: minting is opt-in,
// and a Core without a key admits Runs exactly as before. A partial
// configuration is an error rather than a silent downgrade.
func (f opaqueIngressFlags) executionAttestationIssuer() (*attestation.Issuer, error) {
	keyFile := strings.TrimSpace(*f.attestationKeyFile)
	keyID := strings.TrimSpace(*f.attestationKeyID)
	audience := strings.TrimSpace(*f.attestationAudience)
	if keyFile == "" && keyID == "" && audience == "" {
		return nil, nil
	}
	if keyFile == "" || keyID == "" || audience == "" {
		return nil, errors.New("execution attestation needs a key file, a key id and an audience")
	}
	return attestation.LoadIssuer(keyFile, keyID, audience, *f.attestationTTL)
}

// newOpaqueIngressListener builds the isolated listener surface: the resolver
// reads the projection store, the conformance handler admits one envelope, and
// the listener bounds concurrency and answers readiness.
func (f opaqueIngressFlags) newOpaqueIngressListener(
	store opaquehttp.ProjectionStore,
	admission opaquehttp.Admission,
	ready func() bool,
) (*opaquehttp.Listener, error) {
	resolver, err := opaquehttp.NewStoreResolver(store)
	if err != nil {
		return nil, err
	}
	handler, err := opaquehttp.NewHandler(resolver, admission, opaquehttp.Limits{
		MaxRequestBytes:  int64(*f.maxRequestBytes),
		MaxResponseBytes: int64(*f.maxResponseBytes),
		MaxWait:          *f.maxWait,
		PollInterval:     *f.pollInterval,
	})
	if err != nil {
		return nil, err
	}
	return opaquehttp.NewListener(handler, opaquehttp.ListenerOptions{
		MaxConcurrent: *f.maxConcurrent,
		AcquireWait:   *f.acquireWait,
		Ready:         ready,
	})
}

// opaqueIngressServer is the running isolated listener. A nil value is the
// deployment that does not mount one, and every method below tolerates it.
type opaqueIngressServer struct {
	server  *http.Server
	done    chan error
	ready   atomic.Bool
	stopped sync.Once
	stopErr error
	addr    string
}

// startOpaqueIngress binds and serves the isolated listener.
//
// Every configuration fault is fatal here rather than at the first delivery. A
// process that answers on the primary listener while the ingress is missing,
// misconfigured or unbound looks healthy and admits nothing, which is the one
// failure the gateway cannot see.
func startOpaqueIngress(
	f opaqueIngressFlags,
	mode string,
	store opaquehttp.ProjectionStore,
	admission opaquehttp.Admission,
) (*opaqueIngressServer, error) {
	if !f.enabled() {
		return nil, nil
	}
	ingress := &opaqueIngressServer{done: make(chan error, 1)}
	listener, err := f.newOpaqueIngressListener(store, admission, ingress.ready.Load)
	if err != nil {
		return nil, err
	}
	bound, err := net.Listen("tcp", strings.TrimSpace(*f.addr))
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	ingress.server = &http.Server{
		Handler: telemetry.HTTPHandler(listener, "windforce.opaque-ingress.server"),
	}
	ingress.addr = bound.Addr().String()
	ingress.ready.Store(true)
	fmt.Fprintf(os.Stderr, "windforce-core %s opaque ingress listening on %s\n", mode, bound.Addr())
	go func() {
		ingress.done <- ingress.server.Serve(bound)
	}()
	return ingress, nil
}

// wait reports the serve result. An unmounted ingress returns a nil channel, so
// a select case on it never fires.
func (s *opaqueIngressServer) wait() <-chan error {
	if s == nil {
		return nil
	}
	return s.done
}

// stop drains the listener within a fixed budget. Readiness drops first so a
// gateway stops sending deliveries into a listener that is going away. It runs
// once: the serve result is consumed by the first caller.
func (s *opaqueIngressServer) stop() error {
	if s == nil {
		return nil
	}
	s.stopped.Do(func() {
		s.ready.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), opaqueIngressShutdownBudget)
		defer cancel()
		err := s.server.Shutdown(ctx)
		if err != nil {
			_ = s.server.Close()
		}
		serveErr := <-s.done
		if err == nil && serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			err = serveErr
		}
		s.stopErr = err
	})
	return s.stopErr
}
