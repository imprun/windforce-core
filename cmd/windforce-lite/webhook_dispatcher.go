package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/imprun/windforce-lite/internal/state"
	"github.com/imprun/windforce-lite/internal/webhook"
)

const (
	defaultWebhookDispatchInterval = 500 * time.Millisecond
	defaultWebhookRequestTimeout   = 10 * time.Second
	defaultWebhookLeaseTTL         = 30 * time.Second
	defaultWebhookMaxAttempts      = 8
)

type webhookDispatcherFlags struct {
	dispatchInterval      *time.Duration
	requestTimeout        *time.Duration
	leaseTTL              *time.Duration
	maxAttempts           *int
	allowedHosts          *string
	allowedCIDRs          *string
	allowInsecureLoopback *bool
	workerID              *string
}

func bindWebhookDispatcherFlags(flags *flag.FlagSet, prefix string) webhookDispatcherFlags {
	return webhookDispatcherFlags{
		dispatchInterval:      flags.Duration(prefix+"dispatch-interval", envParsedDuration("WINDFORCE_LITE_WEBHOOK_DISPATCH_INTERVAL", defaultWebhookDispatchInterval), "how often an idle webhook dispatcher polls for delivery work"),
		requestTimeout:        flags.Duration(prefix+"request-timeout", envParsedDuration("WINDFORCE_LITE_WEBHOOK_REQUEST_TIMEOUT", defaultWebhookRequestTimeout), "webhook HTTP request timeout"),
		leaseTTL:              flags.Duration(prefix+"lease", envParsedDuration("WINDFORCE_LITE_WEBHOOK_LEASE_TTL", defaultWebhookLeaseTTL), "webhook delivery claim lease TTL"),
		maxAttempts:           flags.Int(prefix+"max-attempts", envInt("WINDFORCE_LITE_WEBHOOK_MAX_ATTEMPTS", defaultWebhookMaxAttempts), "maximum webhook delivery attempts"),
		allowedHosts:          flags.String(prefix+"allowed-hosts", os.Getenv("WINDFORCE_LITE_WEBHOOK_ALLOWED_HOSTS"), "comma-separated private webhook endpoint host allowlist"),
		allowedCIDRs:          flags.String(prefix+"allowed-cidrs", os.Getenv("WINDFORCE_LITE_WEBHOOK_ALLOWED_CIDRS"), "comma-separated private webhook endpoint CIDR allowlist"),
		allowInsecureLoopback: flags.Bool(prefix+"allow-insecure-loopback", envBool("WINDFORCE_LITE_WEBHOOK_ALLOW_INSECURE_LOOPBACK", false), "allow HTTP loopback webhook endpoints for local development"),
		workerID:              flags.String(prefix+"worker-id", "", "webhook dispatcher identity"),
	}
}

func runWebhookDispatcher(args []string) int {
	flags := flag.NewFlagSet("webhook-dispatcher", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	stateBackend := flags.String("state-backend", "local", "runtime state backend: local or postgres")
	statePath := flags.String("state", defaultStatePath(), "local runtime state JSON path")
	databaseURL := flags.String("database-url", "", "PostgreSQL database URL for --state-backend postgres")
	migrate := flags.Bool("migrate", false, "run state backend schema migration before starting")
	secretKeyEnv := flags.String("secret-key-env", "SECRET_KEY", "environment variable that contains the instance secret used for webhook encryption")
	secretKeyPreviousEnv := flags.String("secret-key-previous-env", "SECRET_KEY_PREVIOUS", "environment variable that contains the previous instance secret during rotation")
	dispatcherFlags := bindWebhookDispatcherFlags(flags, "")
	once := flags.Bool("once", false, "process at most one pending webhook delivery and exit")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	stateStore, closeState, err := openStateStore(ctx, *stateBackend, *statePath, *databaseURL, *migrate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "webhook-dispatcher state: %v\n", err)
		return 1
	}
	defer closeState()
	configureInputCrypto(stateStore, effectiveSecretKey(tokenFromEnv(*secretKeyEnv)), tokenFromEnv(*secretKeyPreviousEnv))
	dispatcher, err := newWebhookDispatcher(stateStore, dispatcherFlags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "webhook-dispatcher config: %v\n", err)
		return 1
	}
	if *once {
		processed, err := dispatcher.ProcessOne(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "webhook-dispatcher: %v\n", err)
			return 1
		}
		_ = writeJSON(os.Stdout, map[string]bool{"processed": processed})
		return 0
	}
	if err := dispatcher.RunLoop(ctx, *dispatcherFlags.dispatchInterval); err != nil {
		fmt.Fprintf(os.Stderr, "webhook-dispatcher: %v\n", err)
		return 1
	}
	return 0
}

func newWebhookDispatcher(stateStore state.Store, flags webhookDispatcherFlags) (*webhook.Dispatcher, error) {
	webhookStore, ok := stateStore.(webhook.Store)
	if !ok {
		return nil, fmt.Errorf("state backend does not provide webhook delivery storage")
	}
	if *flags.requestTimeout <= 0 {
		return nil, fmt.Errorf("request timeout must be positive")
	}
	if *flags.leaseTTL <= *flags.requestTimeout {
		return nil, fmt.Errorf("delivery lease must be longer than request timeout")
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
	policy := webhook.EgressPolicy{
		AllowedHosts:          hosts,
		AllowedCIDRs:          cidrs,
		AllowInsecureLoopback: *flags.allowInsecureLoopback,
	}
	sender := webhook.NewHTTPSender(webhook.SenderConfig{
		Policy:         policy,
		RequestTimeout: *flags.requestTimeout,
		UserAgent:      "windforce-lite-webhook/" + version,
	})
	return &webhook.Dispatcher{
		Store:       webhookStore,
		Sender:      sender,
		WorkerID:    strings.TrimSpace(*flags.workerID),
		LeaseTTL:    *flags.leaseTTL,
		MaxAttempts: *flags.maxAttempts,
	}, nil
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envParsedDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < 0 {
		fmt.Fprintf(os.Stderr, "ignoring %s=%q: expected a non-negative duration\n", name, value)
		return fallback
	}
	return parsed
}
