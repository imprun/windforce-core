package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

const (
	defaultCapabilityGatewayTimeout = 15 * time.Second
	maxCapabilityGatewayBodyBytes   = 1 << 20
	maxCapabilityGatewayTTL         = time.Hour
	capabilityRunIDHeader           = "X-Windforce-Run-ID"
	capabilityJobIDHeader           = "X-Windforce-Job-ID"
	capabilityJobAttemptHeader      = "X-Windforce-Job-Attempt"
)

type CapabilityGatewayBinding struct {
	ServiceURL   string
	WorkerToken  string
	Timeout      time.Duration
	Labels       []string
	Capabilities []string
	client       *http.Client
}

type capabilityGatewaySession struct {
	RunRef   string
	RunToken string
}

type capabilityDiscoveryResponse struct {
	Capabilities []struct {
		ID             string   `json:"id"`
		Operations     []string `json:"operations"`
		Ready          bool     `json:"ready"`
		MaxConcurrency int      `json:"maxConcurrency"`
	} `json:"capabilities"`
}

type capabilityRunResponse struct {
	RunRef           string `json:"runRef"`
	RunToken         string `json:"runToken"`
	ExpiresInSeconds uint64 `json:"expiresInSeconds"`
}

func NewCapabilityGatewayBinding(
	serviceURL string,
	tokenEnv string,
	tokenFile string,
	timeout time.Duration,
	labels []string,
) (CapabilityGatewayBinding, error) {
	serviceURL = strings.TrimSpace(serviceURL)
	if serviceURL == "" {
		return CapabilityGatewayBinding{}, nil
	}
	normalizedURL, err := normalizeCapabilityGatewayURL(serviceURL)
	if err != nil {
		return CapabilityGatewayBinding{}, err
	}
	normalizedLabels, err := contract.NormalizeLabels(labels, false)
	if err != nil {
		return CapabilityGatewayBinding{}, fmt.Errorf("capability gateway labels: %w", err)
	}
	if len(normalizedLabels) == 0 {
		return CapabilityGatewayBinding{}, errors.New("capability gateway requires at least one placement label")
	}

	workerToken := ""
	if tokenEnv = strings.TrimSpace(tokenEnv); tokenEnv != "" {
		workerToken = strings.TrimSpace(os.Getenv(tokenEnv))
	}
	if tokenFile = strings.TrimSpace(tokenFile); workerToken == "" && tokenFile != "" {
		data, readErr := os.ReadFile(tokenFile)
		if readErr != nil {
			return CapabilityGatewayBinding{}, fmt.Errorf("read capability gateway token file: %w", readErr)
		}
		workerToken = strings.TrimSpace(string(data))
	}
	if workerToken == "" {
		return CapabilityGatewayBinding{}, errors.New("capability gateway requires a worker token from its token env or token file")
	}
	if timeout <= 0 {
		timeout = defaultCapabilityGatewayTimeout
	}
	binding := CapabilityGatewayBinding{
		ServiceURL:  normalizedURL,
		WorkerToken: workerToken,
		Timeout:     timeout,
		Labels:      normalizedLabels,
		client:      newCapabilityGatewayHTTPClient(timeout),
	}
	capabilities, err := binding.discover(context.Background())
	if err != nil {
		return CapabilityGatewayBinding{}, err
	}
	binding.Capabilities = capabilities
	return binding, nil
}

func (b CapabilityGatewayBinding) Enabled() bool {
	return b.ServiceURL != ""
}

func (b CapabilityGatewayBinding) Matches(requiredLabels []string) bool {
	if !b.Enabled() || len(requiredLabels) == 0 {
		return false
	}
	offered := make(map[string]struct{}, len(b.Labels))
	for _, label := range b.Labels {
		offered[label] = struct{}{}
	}
	for _, label := range requiredLabels {
		if _, ok := offered[label]; ok {
			return true
		}
	}
	return false
}

func (b CapabilityGatewayBinding) open(
	ctx context.Context,
	execution RuntimeBindingContext,
	ttl time.Duration,
) (capabilityGatewaySession, error) {
	if !validOpaqueValue(execution.RunID) {
		return capabilityGatewaySession{}, errors.New("capability gateway run context has an invalid run ID")
	}
	if !validOpaqueValue(execution.JobID) {
		return capabilityGatewaySession{}, errors.New("capability gateway run context has an invalid job ID")
	}
	if execution.Attempt <= 0 {
		return capabilityGatewaySession{}, errors.New("capability gateway run context has an invalid job attempt")
	}
	ttlSeconds := capabilityTTLSeconds(ttl)
	payload, err := json.Marshal(map[string]uint64{"ttlSeconds": ttlSeconds})
	if err != nil {
		return capabilityGatewaySession{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.ServiceURL+"/v1/runs", bytes.NewReader(payload))
	if err != nil {
		return capabilityGatewaySession{}, errors.New("create capability gateway run request")
	}
	req.Header.Set("Authorization", "Bearer "+b.WorkerToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(capabilityRunIDHeader, execution.RunID)
	req.Header.Set(capabilityJobIDHeader, execution.JobID)
	req.Header.Set(capabilityJobAttemptHeader, strconv.Itoa(execution.Attempt))
	resp, err := b.httpClient().Do(req)
	if err != nil {
		return capabilityGatewaySession{}, errors.New("capability gateway run creation failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return capabilityGatewaySession{}, fmt.Errorf("capability gateway run creation returned status %d", resp.StatusCode)
	}
	var output capabilityRunResponse
	if err := decodeCapabilityGatewayJSON(resp.Body, &output); err != nil {
		return capabilityGatewaySession{}, errors.New("capability gateway returned an invalid run response")
	}
	if !validOpaqueValue(output.RunRef) || !validOpaqueValue(output.RunToken) || output.ExpiresInSeconds == 0 {
		return capabilityGatewaySession{}, errors.New("capability gateway returned invalid run credentials")
	}
	if output.ExpiresInSeconds > uint64(maxCapabilityGatewayTTL/time.Second) {
		return capabilityGatewaySession{}, errors.New("capability gateway returned an excessive run credential lifetime")
	}
	return capabilityGatewaySession{RunRef: output.RunRef, RunToken: output.RunToken}, nil
}

func (b CapabilityGatewayBinding) close(ctx context.Context, session capabilityGatewaySession) error {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodDelete,
		b.ServiceURL+"/v1/runs/"+url.PathEscape(session.RunRef),
		nil,
	)
	if err != nil {
		return errors.New("create capability gateway cleanup request")
	}
	req.Header.Set("Authorization", "Bearer "+session.RunToken)
	resp, err := b.httpClient().Do(req)
	if err != nil {
		return errors.New("capability gateway cleanup failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return fmt.Errorf("capability gateway cleanup returned status %d", resp.StatusCode)
}

func (b CapabilityGatewayBinding) discover(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.ServiceURL+"/v1/capabilities", nil)
	if err != nil {
		return nil, errors.New("create capability gateway discovery request")
	}
	resp, err := b.httpClient().Do(req)
	if err != nil {
		return nil, errors.New("capability gateway discovery failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("capability gateway discovery returned status %d", resp.StatusCode)
	}
	var discovery capabilityDiscoveryResponse
	if err := decodeCapabilityGatewayJSON(resp.Body, &discovery); err != nil {
		return nil, errors.New("capability gateway returned an invalid discovery response")
	}
	seen := make(map[string]struct{})
	capabilities := make([]string, 0, len(discovery.Capabilities))
	for _, capability := range discovery.Capabilities {
		if !capability.Ready {
			continue
		}
		if !validCapabilityID(capability.ID) {
			return nil, errors.New("capability gateway returned an invalid capability identifier")
		}
		if _, ok := seen[capability.ID]; ok {
			continue
		}
		seen[capability.ID] = struct{}{}
		capabilities = append(capabilities, capability.ID)
	}
	if len(capabilities) == 0 {
		return nil, errors.New("capability gateway has no ready providers")
	}
	sort.Strings(capabilities)
	return capabilities, nil
}

func (b CapabilityGatewayBinding) httpClient() *http.Client {
	if b.client != nil {
		return b.client
	}
	timeout := b.Timeout
	if timeout <= 0 {
		timeout = defaultCapabilityGatewayTimeout
	}
	return newCapabilityGatewayHTTPClient(timeout)
}

func newCapabilityGatewayHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialCapabilityGatewayLoopback
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("capability gateway redirects are forbidden")
		},
	}
}

func dialCapabilityGatewayLoopback(ctx context.Context, network string, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("capability gateway transport requires a host and port")
	}

	var addresses []net.IPAddr
	if ip := net.ParseIP(host); ip != nil {
		addresses = []net.IPAddr{{IP: ip}}
	} else if strings.EqualFold(host, "localhost") {
		addresses, err = net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, errors.New("could not resolve capability gateway loopback host")
		}
	} else {
		return nil, errors.New("capability gateway transport requires a loopback host")
	}

	dialer := net.Dialer{}
	for _, candidate := range addresses {
		if !candidate.IP.IsLoopback() {
			continue
		}
		connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
		if dialErr == nil {
			return connection, nil
		}
	}
	return nil, errors.New("could not connect to capability gateway loopback")
}

func normalizeCapabilityGatewayURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" {
		return "", errors.New("capability gateway URL must be an absolute http loopback URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("capability gateway URL must not contain credentials, path, query, or fragment")
	}
	hostname := parsed.Hostname()
	ip := net.ParseIP(hostname)
	if !strings.EqualFold(hostname, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return "", errors.New("capability gateway URL must use a loopback host")
	}
	if parsed.Port() == "" {
		return "", errors.New("capability gateway URL must include an explicit port")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func decodeCapabilityGatewayJSON(body io.Reader, value any) error {
	data, err := io.ReadAll(io.LimitReader(body, maxCapabilityGatewayBodyBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxCapabilityGatewayBodyBytes {
		return errors.New("JSON response is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}

func capabilityTTLSeconds(ttl time.Duration) uint64 {
	if ttl <= 0 {
		ttl = maxCapabilityGatewayTTL
	} else {
		ttl += time.Minute
	}
	if ttl > maxCapabilityGatewayTTL {
		ttl = maxCapabilityGatewayTTL
	}
	seconds := uint64((ttl + time.Second - 1) / time.Second)
	if seconds == 0 {
		return 1
	}
	return seconds
}

func validOpaqueValue(value string) bool {
	return value != "" && len(value) <= 256 && !strings.ContainsAny(value, "\r\n")
}

func validCapabilityID(value string) bool {
	if value == "" || len(value) > 128 || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, "//") {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' || r == '/' {
			continue
		}
		return false
	}
	return true
}
