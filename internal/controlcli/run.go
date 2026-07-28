package controlcli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/imprun/windforce-core/pkg/controlplane"
)

const (
	ExitOK        = 0
	ExitFailure   = 1
	ExitUsage     = 2
	ExitConfig    = 3
	ExitAuth      = 4
	ExitForbidden = 5
	ExitTransport = 10
	ExitAPIClient = 20
	ExitAPIServer = 21
)

var Version = "dev"

type runner struct {
	stdin          io.Reader
	stdout         io.Writer
	stderr         io.Writer
	pretty         bool
	program        string
	contextCommand bool
	store          CredentialStore
	configPath     string
	config         ConfigFile
	resolved       resolvedConfig
	client         *controlplane.Client
	outputFields   string
	jqExpression   string
	outputTemplate string
	humanOutput    bool
	openBrowser    func(string) error
}

type usageError struct{ message string }

type commandFailure struct{ message string }

func (e usageError) Error() string { return e.message }

func (e commandFailure) Error() string { return e.message }

func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runWithProgram(legacyProgram, args, stdin, stdout, stderr)
}

func RunWF(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return RunWFWithCredentialStore(args, stdin, stdout, stderr, nil)
}

func RunWFWithCredentialStore(args []string, stdin io.Reader, stdout, stderr io.Writer, store CredentialStore) int {
	return runWithProgram(wfProgram, args, stdin, stdout, stderr, store)
}

func runWithProgram(program programConfig, args []string, stdin io.Reader, stdout, stderr io.Writer, stores ...CredentialStore) int {
	var store CredentialStore
	if len(stores) > 0 {
		store = stores[0]
	}
	return runWithProgramDependencies(program, args, stdin, stdout, stderr, store, openSystemBrowser)
}

func runWithProgramDependencies(
	program programConfig,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	store CredentialStore,
	openBrowser func(string) error,
) int {
	global := flag.NewFlagSet(program.Name, flag.ContinueOnError)
	global.SetOutput(stderr)
	global.Usage = func() {}
	var profileName string
	var overrides Profile
	var pretty bool
	var outputFields string
	var jqExpression string
	var outputTemplate string
	var timeout time.Duration
	var showVersion bool
	global.StringVar(&profileName, "profile", "", "connection profile")
	if program.Name == wfProgram.Name {
		global.StringVar(&profileName, "context", "", "connection context")
	}
	global.StringVar(&overrides.APIURL, "api-url", "", "control-plane API base URL")
	global.StringVar(&overrides.Workspace, "workspace", "", "workspace id")
	global.StringVar(&overrides.Actor, "actor", "", "actor sent as X-Windforce-Actor")
	global.StringVar(&overrides.TokenEnv, "token-env", "", "environment variable containing the bearer token")
	global.BoolVar(&pretty, "pretty", false, "pretty-print JSON")
	global.StringVar(&outputFields, "json", "", "select comma-separated JSON fields, or *")
	global.StringVar(&jqExpression, "jq", "", "filter JSON output with a jq expression")
	global.StringVar(&outputTemplate, "template", "", "format output with a Go template")
	global.DurationVar(&timeout, "request-timeout", 60*time.Second, "HTTP request timeout")
	global.BoolVar(&showVersion, "version", false, "print version")
	if err := global.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage(stdout, program.Name)
			return ExitOK
		}
		return ExitUsage
	}
	remaining := global.Args()
	if showVersion {
		if len(remaining) != 0 {
			writeError(stderr, usageError{"--version does not accept a command"})
			return ExitUsage
		}
		_, _ = fmt.Fprintln(stdout, Version)
		return ExitOK
	}
	if len(remaining) == 0 {
		printUsage(stderr, program.Name)
		return ExitUsage
	}
	if jqExpression != "" && outputTemplate != "" {
		writeError(stderr, usageError{"--jq and --template are mutually exclusive"})
		return ExitUsage
	}
	if (jqExpression != "" || outputTemplate != "") && outputFields == "" {
		outputFields = "*"
	}
	if helpPath, requested := requestedCommandHelp(remaining); requested {
		if program.Name != legacyProgram.Name || remaining[0] != "help" {
			if !printCommandHelp(stdout, program.Name, helpPath) {
				writeError(stderr, fmt.Errorf("unknown help topic %q", strings.Join(helpPath, " ")))
				return ExitUsage
			}
			return ExitOK
		}
	}
	if program.Name == wfProgram.Name && remaining[0] == "completion" {
		if len(remaining) != 2 {
			writeError(stderr, usageError{"usage: wf completion bash|zsh|fish|powershell"})
			return ExitUsage
		}
		if err := writeCompletion(stdout, remaining[1]); err != nil {
			var usage usageError
			if errors.As(err, &usage) {
				writeError(stderr, err)
				return ExitUsage
			}
			writeError(stderr, err)
			return ExitConfig
		}
		return ExitOK
	}
	if remaining[0] == "version" {
		if len(remaining) != 1 {
			writeError(stderr, usageError{"usage: " + program.Name + " version"})
			return ExitUsage
		}
		_, _ = fmt.Fprintln(stdout, Version)
		return ExitOK
	}

	path, err := configPathFor(program)
	if err != nil {
		writeError(stderr, err)
		return ExitConfig
	}
	config, err := loadProgramConfig(program, path)
	if err != nil {
		writeError(stderr, err)
		return ExitConfig
	}
	r := &runner{
		stdin: stdin, stdout: stdout, stderr: stderr, pretty: pretty, program: program.Name,
		store: store, configPath: path, config: config,
		outputFields: outputFields, jqExpression: jqExpression, outputTemplate: outputTemplate,
		humanOutput: isTerminalOutput(stdout) && !pretty && outputFields == "" && jqExpression == "" && outputTemplate == "",
		openBrowser: openBrowser,
	}
	if remaining[0] == "profile" || (program.Name == wfProgram.Name && remaining[0] == "context") {
		r.contextCommand = remaining[0] == "context"
		if err := r.profile(path, config, remaining[1:]); err != nil {
			var usage usageError
			if errors.As(err, &usage) {
				return r.finishError(err)
			}
			writeError(stderr, err)
			return ExitConfig
		}
		return ExitOK
	}
	resolved, err := resolveProfileFor(program, config, profileName, overrides)
	if err != nil {
		writeError(stderr, err)
		return ExitConfig
	}
	r.resolved = resolved
	r.client = &controlplane.Client{
		BaseURL: resolved.APIURL, Workspace: resolved.Workspace, Actor: resolved.Actor,
		Token: resolved.Token, HTTP: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	if program.Name == wfProgram.Name && remaining[0] == "auth" {
		if err := r.auth(remaining[1:]); err != nil {
			return r.finishError(err)
		}
		return ExitOK
	}
	token, _, err := r.resolveCredential()
	if err != nil {
		writeError(stderr, err)
		return ExitConfig
	}
	r.client.Token = token
	if err := r.command(remaining); err != nil {
		return r.finishError(err)
	}
	return ExitOK
}

func (r *runner) command(args []string) error {
	switch args[0] {
	case "source":
		return r.source(args[1:])
	case "app":
		return r.app(args[1:])
	case "workspace":
		return r.workspace(args[1:])
	case "release":
		return r.release(args[1:])
	case "action":
		return r.action(args[1:])
	case "run":
		return r.run(args[1:])
	case "job":
		return r.job(args[1:])
	case "provisioning":
		return r.provisioning(args[1:])
	case "api":
		return r.api(args[1:])
	case "openapi":
		if len(args) != 1 {
			return usageError{"usage: windforce openapi"}
		}
		return r.json(http.MethodGet, r.client.WorkspacePath("openapi.json"), nil)
	case "help", "--help", "-h":
		if r.program == legacyProgram.Name && len(args) != 1 {
			return usageError{"usage: windforce help"}
		}
		if !printCommandHelp(r.stdout, r.program, args[1:]) {
			return usageError{fmt.Sprintf("unknown help topic %q", strings.Join(args[1:], " "))}
		}
		return nil
	default:
		return usageError{fmt.Sprintf("unknown command %q", args[0])}
	}
}

func (r *runner) json(method, path string, body any) error {
	result, err := r.client.DoJSON(context.Background(), method, path, body)
	if err != nil {
		return err
	}
	return r.outputJSON(result)
}

func (r *runner) jsonWithHeaders(method, path string, body any, headers map[string]string) error {
	result, err := r.client.DoJSONWithHeaders(context.Background(), method, path, body, headers)
	if err != nil {
		return err
	}
	return r.outputJSON(result)
}

func (r *runner) jsonDiscard(method, path string, body any) error {
	_, err := r.client.DoJSON(context.Background(), method, path, body)
	return err
}

func (r *runner) raw(method, path, contentType string, body []byte) error {
	result, _, err := r.client.DoRaw(context.Background(), method, path, contentType, body)
	if err != nil {
		return err
	}
	_, err = r.stdout.Write(result)
	return err
}

func (r *runner) outputJSON(value any) error {
	return r.writeOutput(value)
}

func (r *runner) finishError(err error) int {
	var apiErr *controlplane.APIError
	var usage usageError
	var failure commandFailure
	switch {
	case errors.As(err, &apiErr):
		if json.Valid(apiErr.Body) {
			_ = r.outputErrorJSON(apiErr.Body)
		} else {
			writeError(r.stderr, err)
		}
		switch apiErr.StatusCode {
		case http.StatusUnauthorized:
			return ExitAuth
		case http.StatusForbidden:
			return ExitForbidden
		}
		if apiErr.StatusCode >= 500 {
			return ExitAPIServer
		}
		return ExitAPIClient
	case errors.As(err, &usage):
		message := usage.message
		if r.program != "" && r.program != legacyProgram.Name {
			message = strings.ReplaceAll(message, legacyProgram.Name, r.program)
		}
		if r.contextCommand {
			message = strings.ReplaceAll(message, " profile ", " context ")
		}
		writeError(r.stderr, usageError{message: message})
		return ExitUsage
	case errors.As(err, &failure):
		writeError(r.stderr, failure)
		return ExitFailure
	default:
		writeError(r.stderr, err)
		return ExitTransport
	}
}

func (r *runner) outputErrorJSON(data []byte) error {
	data = redactErrorJSON(data)
	if r.pretty {
		var value any
		if err := json.Unmarshal(data, &value); err == nil {
			data, _ = json.MarshalIndent(value, "", "  ")
		}
	}
	_, err := fmt.Fprintln(r.stderr, string(data))
	return err
}

func (r *runner) flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(r.stderr)
	return fs
}

func (r *runner) readJSON(inline, file string) (any, error) {
	data := []byte(inline)
	var err error
	if file != "" {
		data, err = r.readFile(file)
		if err != nil {
			return nil, err
		}
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("invalid JSON input: %w", err)
	}
	return value, nil
}

func (r *runner) readFile(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(r.stdin)
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return data, nil
}

func gitCredential(method, username, passwordEnv, tokenEnv string) (map[string]any, string, error) {
	password, token := "", ""
	if passwordEnv != "" {
		password = os.Getenv(passwordEnv)
	}
	if tokenEnv != "" {
		token = os.Getenv(tokenEnv)
	}
	if method == "" {
		if username != "" || password != "" {
			method = "basic"
		} else if token != "" {
			method = "pat"
		}
	}
	switch method {
	case "":
		return map[string]any{}, "", nil
	case "pat":
		if token == "" {
			return nil, "", fmt.Errorf("environment variable %s is not set", tokenEnv)
		}
		value, _ := json.Marshal(map[string]string{"type": "pat", "token": token})
		return map[string]any{"auth_method": "pat", "access_token": token}, string(value), nil
	case "basic":
		if username == "" || password == "" {
			return nil, "", fmt.Errorf("--username and a populated --password-env are required for basic auth")
		}
		value, _ := json.Marshal(map[string]string{"type": "basic", "username": username, "password": password})
		return map[string]any{"auth_method": "basic", "username": username, "password": password}, string(value), nil
	default:
		return nil, "", usageError{"--auth-method must be pat or basic"}
	}
}

func defaultCredentialPath(name string) string {
	re := regexp.MustCompile(`[/\\\s\x00-\x1f\x7f]+`)
	segment := strings.Trim(re.ReplaceAllString(strings.TrimSpace(name), "-"), "-")
	if segment == "" || segment == "." || segment == ".." {
		segment = "source"
	}
	return "git/" + segment + "/credential"
}

func compact(input map[string]any) map[string]any {
	output := map[string]any{}
	for key, value := range input {
		if value != nil && value != "" {
			output[key] = value
		}
	}
	return output
}
func addQuery(values url.Values, key, value string) {
	if value != "" {
		values.Set(key, value)
	}
}
func query(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	return "?" + values.Encode()
}
func writeError(writer io.Writer, err error) {
	_, _ = fmt.Fprintln(writer, redactDiagnostic(err.Error()))
}

func printUsage(writer io.Writer, program string) {
	if program == wfProgram.Name {
		fmt.Fprintln(writer, `usage: wf [global flags] <command>

Global flags: --context, --api-url, --workspace, --actor, --token-env, --json, --jq, --template, --pretty, --version

Commands:
  auth login|switch|status|logout
  context list|show|set|use
  workspace list|show|view|use
  source list|register|probe|sync|publish
  app publish|list|show|history|source|openapi
  release list|view|activate|rollback
  action show|schema
  run create|wait|show|watch|result|cancel
  job list|show|result|logs|cancel
  provisioning export|apply
  api
  openapi
  version`)
		return
	}
	fmt.Fprintln(writer, `usage: windforce [global flags] <command>

Global flags: --profile, --api-url, --workspace, --actor, --token-env, --json, --jq, --template, --pretty, --version

Commands:
  profile list|show|set|use
  source list|register|probe|sync|deploy
  app publish|list|show|history|source|openapi
  release list|view|activate|rollback
  action show|schema
  run create|wait|show|watch|result|cancel
  job list|show|result|logs|cancel
  provisioning export|apply
  openapi
  version`)
}
