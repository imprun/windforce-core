package completion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/imprun/windforce-core/internal/state"
	"github.com/imprun/windforce-core/internal/webhook"
)

const (
	HeaderRunID     = "X-Windforce-Run-Id"
	HeaderTriggerID = "X-Windforce-Trigger-Id"
)

type Sender interface {
	Send(context.Context, *state.TriggerCompletionClaim) webhook.AttemptResult
}

type CallbackSender struct {
	Policy         webhook.EgressPolicy
	RequestTimeout time.Duration
	UserAgent      string
	Now            func() time.Time
}

func (sender CallbackSender) Send(ctx context.Context, claim *state.TriggerCompletionClaim) webhook.AttemptResult {
	started := time.Now()
	finish := func(result webhook.AttemptResult) webhook.AttemptResult {
		result.Latency = time.Since(started)
		return result
	}
	if claim == nil || claim.Delivery.Completion.Callback == nil {
		return finish(webhook.AttemptResult{Outcome: webhook.AttemptTerminal, ErrorSummary: "callback_config_missing"})
	}
	secrets, err := ParseSecrets(claim.Trigger.SecretConfig)
	if err != nil || secrets.SigningSecret == "" {
		return finish(webhook.AttemptResult{Outcome: webhook.AttemptTerminal, ErrorSummary: "signing_secret_missing"})
	}
	body, err := json.Marshal(NewEnvelope(claim))
	if err != nil {
		return finish(webhook.AttemptResult{Outcome: webhook.AttemptTerminal, ErrorSummary: "completion_encoding_failed"})
	}
	timeout := sender.RequestTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	attemptContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	endpoint, err := sender.Policy.ResolveEndpoint(attemptContext, claim.Delivery.Completion.Callback.Endpoint)
	if err != nil {
		if errors.Is(err, webhook.ErrEgressPolicy) {
			return finish(webhook.AttemptResult{Outcome: webhook.AttemptTerminal, ErrorSummary: "egress_policy_rejected"})
		}
		return finish(webhook.AttemptResult{Outcome: webhook.AttemptRetry, ErrorSummary: "endpoint_resolution_failed"})
	}
	now := sender.Now
	if now == nil {
		now = time.Now
	}
	timestamp := webhook.TimestampValue(now())
	request, err := http.NewRequestWithContext(attemptContext, http.MethodPost, endpoint.URL.String(), bytes.NewReader(body))
	if err != nil {
		return finish(webhook.AttemptResult{Outcome: webhook.AttemptTerminal, ErrorSummary: "request_creation_failed"})
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", firstNonEmpty(sender.UserAgent, "windforce-core-trigger-completion/1"))
	request.Header.Set(webhook.HeaderEventID, claim.Delivery.DeliveryID)
	request.Header.Set(webhook.HeaderEventType, EnvelopeType)
	request.Header.Set(webhook.HeaderDelivery, claim.Delivery.ID)
	request.Header.Set(webhook.HeaderTimestamp, timestamp)
	request.Header.Set(webhook.HeaderSignature, webhook.Sign(secrets.SigningSecret, timestamp, body))
	request.Header.Set(HeaderRunID, claim.Run.ID)
	request.Header.Set(HeaderTriggerID, claim.Trigger.ID)

	transport := endpoint.NewTransport(sender.Policy)
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(attemptContext.Err(), context.DeadlineExceeded) {
			return finish(webhook.AttemptResult{Outcome: webhook.AttemptRetry, ErrorSummary: "request_timeout"})
		}
		return finish(webhook.AttemptResult{Outcome: webhook.AttemptRetry, ErrorSummary: "network_error"})
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
	status := response.StatusCode
	result := webhook.AttemptResult{ResponseStatus: &status}
	switch {
	case status >= 200 && status < 300:
		result.Outcome = webhook.AttemptSucceeded
	case retryableHTTPStatus(status):
		result.Outcome = webhook.AttemptRetry
		result.ErrorSummary = "http_" + strconv.Itoa(status)
		if retryAt, ok := webhook.ParseRetryAfter(response.Header.Get("Retry-After"), now()); ok {
			result.RetryAt = &retryAt
		}
	default:
		result.Outcome = webhook.AttemptTerminal
		result.ErrorSummary = "http_" + strconv.Itoa(status)
	}
	return finish(result)
}

func retryableHTTPStatus(status int) bool {
	return status == http.StatusRequestTimeout ||
		status == http.StatusTooEarly ||
		status == http.StatusTooManyRequests ||
		status >= http.StatusInternalServerError
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
