package controlcli

import (
	"fmt"
	"io"
	"strings"
)

var wfCommandHelp = map[string]string{
	"auth": `Authenticate wf with the selected context.

USAGE
  wf auth login [--web | --with-token] [--no-browser] [--account label]
  wf auth switch <account>
  wf auth status
  wf auth logout [--local-only]`,
	"auth login": `Authenticate with a hosted Identity provider or a direct Cell credential.

USAGE
  wf auth login [flags]

FLAGS
  --web             Use hosted Device Authorization
  --with-token      Read one direct Cell credential from standard input
  --no-browser      Print the verification URL instead of opening a browser
  --account string  Local account label`,
	"auth logout": `Remove the selected account credential.

Hosted credentials are revoked at the Identity provider before local state is
removed. A revocation failure preserves the local credential so the operation
can be retried. This does not end the central browser session.

USAGE
  wf auth logout [--local-only]

FLAGS
  --local-only  Skip hosted token revocation and remove only local state`,
	"context": `Manage non-secret Cell connection contexts.

USAGE
  wf context list
  wf context show [name]
  wf context set <name> --api-url <url> [flags]
  wf context use <name>`,
	"context set": `Create or update a connection context.

USAGE
  wf context set <name> --api-url <url> [flags]

FLAGS
  --workspace string  Workspace ID
  --actor string      Direct Cell audit actor
  --token-env string  Compatibility bearer-token environment variable name
  --use               Select the context after saving`,
	"workspace": `Inspect and select a workspace in the current context.

USAGE
  wf workspace list
  wf workspace show
  wf workspace view <workspace>
  wf workspace use <workspace>

Workspace switching verifies access before updating the context. Listing and
viewing the global workspace registry require instance-admin or equivalent
hosted delegation.`,
	"source": `Manage low-level Git source connections.

USAGE
  wf source list
  wf source register [flags]
  wf source probe [flags]
  wf source sync <source-id> [--expected-commit sha]
  wf source publish <source-id> [--expected-commit sha] [--message text]`,
	"source register": `Register a Git source. Credentials are read from an environment variable and stored server-side.

USAGE
  wf source register --name <name> --repo-url <url> [flags]

FLAGS
  --branch string            Remote branch (default "main")
  --subpath string           App directory inside the repository
  --creds-ref string         Existing server-side credential reference
  --auth-method string       Credential type for a new reference
  --access-token-env string  Environment variable containing a Git token`,
	"source probe": `Validate a Git source without registering it.

USAGE
  wf source probe --repo-url <url> [flags]`,
	"source sync": `Synchronize and validate the selected remote branch.

USAGE
  wf source sync <source-id> [--expected-commit sha]`,
	"source publish": `Publish the latest synchronized source candidate as an immutable release.

USAGE
  wf source publish <source-id> [--expected-commit sha] [--message text]`,
	"app": `Manage apps in the selected workspace.

USAGE
  wf app publish [path] [flags]
  wf app list [--summary]
  wf app show <app>
  wf app history <app>
  wf app source <app>
  wf app openapi <app>`,
	"app publish": `Publish the exact Git commit containing a Windforce app.

The command finds windforce.json, resolves the Git repository, branch, subpath,
and HEAD commit, finds or registers a matching source, synchronizes with an
exact-commit precondition, and publishes an immutable release.

USAGE
  wf app publish [path] [flags]

FLAGS
  --source-id int       Use an existing Git source ID
  --source-name string  Select or register a source by name
  --creds-ref string    Server-side credential reference for a new private source
  --remote string       Git remote name
  --branch string       Remote branch
  --message string      Release audit message
  --allow-dirty         Ignore uncommitted files and publish HEAD only
  --quiet               Suppress progress messages`,
	"release": `Inspect and change immutable app releases.

USAGE
  wf release list <app>
  wf release view <app> <release-id>
  wf release activate <app> <release-id> --reason <text> --yes
  wf release rollback <app> <release-id> --reason <text> --yes`,
	"release activate": `Make an existing immutable release active.

USAGE
  wf release activate <app> <release-id> --reason <text> --yes`,
	"release rollback": `Restore an earlier immutable release.

USAGE
  wf release rollback <app> <release-id> --reason <text> --yes`,
	"run": `Create, wait for, inspect, and cancel Runs.

USAGE
  wf run create <app> <action> [flags]
  wf run wait <app> <action> [flags]
  wf run show <run-id>
  wf run watch <run-id> [flags]
  wf run result <run-id>
  wf run cancel <run-id> [--reason text]`,
	"run create": `Create a Run and return immediately.

USAGE
  wf run create <app> <action> [flags]

FLAGS
  --input string            JSON input (default "{}")
  --input-file string       JSON input file, or - for standard input
  --idempotency-key string  Principal-scoped idempotency key
  --correlation-id string   Caller correlation ID`,
	"run wait": `Create a Run and wait server-side for completion.

USAGE
  wf run wait <app> <action> [flags]

FLAGS
  --input string            JSON input (default "{}")
  --input-file string       JSON input file, or - for standard input
  --idempotency-key string  Principal-scoped idempotency key
  --correlation-id string   Caller correlation ID
  --timeout duration        Server wait duration`,
	"run watch": `Poll a Run until it reaches a terminal state.

USAGE
  wf run watch <run-id> [flags]

FLAGS
  --interval duration  Polling interval (default 2s)
  --timeout duration   Maximum wait duration (default 10m)
  --result             Print the result after a successful Run
  --quiet              Suppress state-change progress`,
	"job": `Inspect low-level Jobs.

USAGE
  wf job list [flags]
  wf job show <job-id>
  wf job result <job-id>
  wf job logs <job-id> [--tail-bytes n]
  wf job cancel <job-id> [--reason text]`,
	"action": `Inspect an app action and its schemas.

USAGE
  wf action show <app> <action>
  wf action schema <app> <action>`,
	"provisioning": `Export or apply workspace provisioning documents.

USAGE
  wf provisioning export [--format json|yaml] [--output path]
  wf provisioning apply --file <path> [--dry-run]`,
	"completion": `Generate shell completion code.

USAGE
  wf completion bash|zsh|fish|powershell`,
	"version": `Print the wf version.

USAGE
  wf version`,
	"openapi": `Print the selected workspace Control Plane OpenAPI document.

USAGE
  wf openapi`,
	"api": `Call a Control Plane endpoint using the selected context and credential.

Relative endpoints are resolved below the selected workspace. Paths beginning
with / are resolved on the same context host. Absolute URLs are rejected.

USAGE
  wf api <endpoint> [flags]

FLAGS
  --method string     GET, POST, PUT, PATCH, or DELETE
  --field key=value   Typed JSON field; repeatable
  --raw-field key=value
                      String JSON field; repeatable
  --input path        JSON request body file, or - for standard input`,
}

func requestedCommandHelp(args []string) ([]string, bool) {
	if len(args) == 0 {
		return nil, false
	}
	if args[0] == "help" {
		return args[1:], true
	}
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return commandHelpPath(args), true
		}
	}
	return nil, false
}

func commandHelpPath(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	path := []string{args[0]}
	if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
		candidate := strings.Join(args[:2], " ")
		if _, ok := wfCommandHelp[candidate]; ok {
			path = append(path, args[1])
		}
	}
	return path
}

func printCommandHelp(writer io.Writer, program string, path []string) bool {
	if len(path) == 0 {
		printUsage(writer, program)
		return true
	}
	if program != wfProgram.Name {
		printUsage(writer, program)
		return true
	}
	key := strings.Join(path, " ")
	help, ok := wfCommandHelp[key]
	if !ok && len(path) > 1 {
		help, ok = wfCommandHelp[path[0]]
	}
	if !ok {
		return false
	}
	_, _ = fmt.Fprintln(writer, help)
	return true
}
