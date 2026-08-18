package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/bundle"
	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/diagnose"
	"github.com/imprun/windforce-core/internal/executionbundle"
	"github.com/imprun/windforce-core/internal/runtime"
	"github.com/imprun/windforce-core/internal/server"
	"github.com/imprun/windforce-core/internal/worker"
)

const diagnoseUsageExitCode = 64

func runDiagnose(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	flags.SetOutput(stderr)
	modeFlag := flags.String("mode", string(diagnose.ModeStandalone), "deployment mode: standalone, server, or remote-worker")
	jsonOutput := flags.Bool("json", false, "write the stable machine-readable report")
	timeout := flags.Duration("timeout", 15*time.Second, "overall read-only diagnostic timeout")
	stateBackend := flags.String("state-backend", "local", "runtime state backend: local or postgres")
	statePath := flags.String("state", defaultStatePath(), "local runtime state JSON path")
	databaseURL := flags.String("database-url", "", "PostgreSQL database URL for --state-backend postgres")
	storeDir := flags.String("store", defaultStoreDir(), "source snapshot and execution bundle store directory")
	cacheRoot := flags.String("cache", defaultCacheDir(), "runtime cache directory")
	apiURL := flags.String("api-url", "", "running Core server or remote Worker API base URL")
	adminTokenEnv := flags.String("admin-token-env", "", "environment variable containing the server admin token")
	workerTokenEnv := flags.String("worker-token-env", "", "environment variable containing the remote worker token")
	secretKeyEnv := flags.String("secret-key-env", "SECRET_KEY", "environment variable containing the Secret backend key")
	devMode := flags.Bool("dev", false, "report missing production credentials as development warnings")
	bunPath := flags.String("bun-path", "", "bun executable path")
	pythonPath := flags.String("python-path", "", "python executable path")
	goPath := flags.String("go-path", "", "go executable path")
	executionProfileID := flags.String("execution-profile-id", strings.TrimSpace(os.Getenv("WINDFORCE_EXECUTION_PROFILE_ID")), "immutable execution image/profile identity")
	capabilityGatewayURL := flags.String("capability-gateway-url", strings.TrimSpace(os.Getenv("WINDFORCE_CAPABILITY_GATEWAY_URL")), "worker-local capability gateway URL")
	capabilityGatewayTokenEnv := flags.String("capability-gateway-token-env", "WINDFORCE_CAPABILITY_GATEWAY_TOKEN", "environment variable containing the gateway worker token")
	capabilityGatewayTokenFile := flags.String("capability-gateway-token-file", strings.TrimSpace(os.Getenv("WINDFORCE_CAPABILITY_GATEWAY_TOKEN_FILE")), "file containing the gateway worker token")
	capabilityGatewayTimeout := flags.Duration("capability-gateway-timeout", envDuration("WINDFORCE_CAPABILITY_GATEWAY_TIMEOUT_MS", time.Millisecond, 15*time.Second), "capability gateway request timeout")
	capabilityGatewayLabels := flags.String("capability-gateway-labels", strings.TrimSpace(os.Getenv("WINDFORCE_CAPABILITY_GATEWAY_LABELS")), "comma-separated labels backed by the gateway")
	if err := flags.Parse(args); err != nil {
		return diagnoseUsageExitCode
	}
	mode, err := diagnose.ParseMode(*modeFlag)
	if err != nil {
		fmt.Fprintln(stderr, "diagnose: --mode must be standalone, server, or remote-worker")
		return diagnoseUsageExitCode
	}
	if *timeout <= 0 {
		fmt.Fprintln(stderr, "diagnose: --timeout must be greater than zero")
		return diagnoseUsageExitCode
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	options := diagnose.Options{
		Mode:         mode,
		Version:      version,
		Revision:     revision,
		Capabilities: server.EngineCapabilities(),
		StateBackend: strings.TrimSpace(*stateBackend),
		ServerURL:    strings.TrimSpace(*apiURL),
		Development:  *devMode,
	}

	if mode != diagnose.ModeRemoteWorker {
		stateStore, closeState, openErr := openStateStore(ctx, options.StateBackend, *statePath, *databaseURL, false)
		if openErr == nil {
			defer closeState()
			options.StateConnected = true
			options.Store = stateStore
			options.Catalog, _ = stateStore.(catalog.Store)
			options.SourceStore = bundle.NewLocalStore(*storeDir)
			options.ExecutionStore = executionbundle.NewLocalStore(executionBundleStoreRoot(*storeDir))
		}
		options.AuthConfigured = tokenFromEnv(*adminTokenEnv) != ""
		options.SecretBackendConfigured = tokenFromEnv(*secretKeyEnv) != ""
	}

	if mode != diagnose.ModeServer {
		runtimeRunner := &runtime.Runner{
			ArtifactStore:      options.ExecutionStore,
			CacheRoot:          *cacheRoot,
			BunPath:            *bunPath,
			PythonPath:         *pythonPath,
			GoPath:             *goPath,
			ExecutionProfileID: *executionProfileID,
		}
		options.RuntimeProfiles = runtimeRunner.ExecutionProfiles
		if mode == diagnose.ModeRemoteWorker {
			options.AuthConfigured = tokenFromEnv(*workerTokenEnv) != ""
		}
		if strings.TrimSpace(*capabilityGatewayURL) != "" {
			options.CapabilityGateway = func(context.Context) error {
				_, gatewayErr := worker.NewCapabilityGatewayBinding(
					*capabilityGatewayURL,
					*capabilityGatewayTokenEnv,
					*capabilityGatewayTokenFile,
					*capabilityGatewayTimeout,
					parseLabels(*capabilityGatewayLabels),
				)
				return gatewayErr
			}
		}
	}

	report := diagnose.Run(ctx, options)
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintln(stderr, "diagnose: failed to write report")
			return diagnoseUsageExitCode
		}
	} else if err := diagnose.WriteText(stdout, report); err != nil {
		fmt.Fprintln(stderr, "diagnose: failed to write report")
		return diagnoseUsageExitCode
	}
	return diagnose.ExitCode(report)
}
