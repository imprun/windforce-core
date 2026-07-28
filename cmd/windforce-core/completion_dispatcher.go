package main

import (
	"fmt"
	"strings"

	"github.com/imprun/windforce-core/internal/completion"
	"github.com/imprun/windforce-core/internal/state"
	"github.com/imprun/windforce-core/internal/webhook"
)

func newTriggerCompletionDispatcher(stateStore state.Store, flags webhookDispatcherFlags) (*completion.Dispatcher, error) {
	if *flags.requestTimeout <= 0 {
		return nil, fmt.Errorf("request timeout must be positive")
	}
	if *flags.leaseTTL <= *flags.requestTimeout {
		return nil, fmt.Errorf("completion lease must be longer than request timeout")
	}
	if *flags.maxAttempts <= 0 {
		return nil, fmt.Errorf("max attempts must be positive")
	}
	hosts, err := webhook.ParseAllowedHosts(*flags.allowedHosts)
	if err != nil {
		return nil, err
	}
	cidrs, err := webhook.ParseAllowedCIDRs(*flags.allowedCIDRs)
	if err != nil {
		return nil, err
	}
	insecureHTTPHosts, err := webhook.ParseAllowedHosts(*flags.allowedInsecureHTTPHosts)
	if err != nil {
		return nil, err
	}
	policy := webhook.EgressPolicy{
		AllowedHosts:             hosts,
		AllowedCIDRs:             cidrs,
		AllowedInsecureHTTPHosts: insecureHTTPHosts,
		AllowInsecureLoopback:    *flags.allowInsecureLoopback,
	}
	workerID := strings.TrimSpace(*flags.workerID)
	if workerID != "" {
		workerID += "-completion"
	}
	return &completion.Dispatcher{
		Store: stateStore,
		Callback: completion.CallbackSender{
			Policy:         policy,
			RequestTimeout: *flags.requestTimeout,
			UserAgent:      "windforce-core-trigger-completion/" + version,
		},
		Publish:     completion.RabbitMQSender{RequestTimeout: *flags.requestTimeout},
		WorkerID:    workerID,
		LeaseTTL:    *flags.leaseTTL,
		MaxAttempts: *flags.maxAttempts,
	}, nil
}
