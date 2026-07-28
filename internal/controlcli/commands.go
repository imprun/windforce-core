package controlcli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
)

type sourceDeployRequest struct {
	Confirm bool   `json:"confirm"`
	Message string `json:"message,omitempty"`
}

func (r *runner) profile(path string, config ConfigFile, args []string) error {
	noun := r.profileNoun()
	if len(args) == 0 {
		return usageError{noun + " requires list, show, use, or set"}
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return usageError{"usage: windforce profile list"}
		}
		names := make([]string, 0, len(config.Profiles))
		for name := range config.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)
		rows := make([]map[string]any, 0, len(names))
		for _, name := range names {
			profile := config.Profiles[name]
			rows = append(rows, map[string]any{"name": name, "current": name == config.CurrentProfile, "api_url": profile.APIURL, "workspace": profile.Workspace, "actor": profile.Actor, "token_env": profile.TokenEnv, "account": profile.Account})
		}
		return r.outputJSON(rows)
	case "show":
		if len(args) > 2 {
			return usageError{"usage: windforce profile show [name]"}
		}
		name := config.CurrentProfile
		if len(args) > 1 {
			name = args[1]
		}
		profile, ok := config.Profiles[name]
		if !ok || name == "" {
			return fmt.Errorf("%s %q does not exist", noun, name)
		}
		return r.outputJSON(map[string]any{"name": name, "current": name == config.CurrentProfile, "api_url": profile.APIURL, "workspace": profile.Workspace, "actor": profile.Actor, "token_env": profile.TokenEnv, "account": profile.Account})
	case "use":
		if len(args) != 2 {
			return usageError{"usage: windforce profile use <name>"}
		}
		if _, ok := config.Profiles[args[1]]; !ok {
			return fmt.Errorf("%s %q does not exist", noun, args[1])
		}
		config.CurrentProfile = args[1]
		return saveConfig(path, config)
	case "set":
		if len(args) < 2 {
			return usageError{"usage: windforce profile set <name> [flags]"}
		}
		name := strings.TrimSpace(args[1])
		fs := r.flags("profile set")
		profile := config.Profiles[name]
		fs.StringVar(&profile.APIURL, "api-url", profile.APIURL, "control-plane API base URL")
		fs.StringVar(&profile.Workspace, "workspace", firstNonEmpty(profile.Workspace, defaultWorkspace), "workspace id")
		fs.StringVar(&profile.Actor, "actor", profile.Actor, "actor subject")
		fs.StringVar(&profile.TokenEnv, "token-env", firstNonEmpty(profile.TokenEnv, r.defaultProfileTokenEnv()), "bearer-token environment variable")
		makeCurrent := fs.Bool("use", false, "make this the current profile")
		if err := fs.Parse(args[2:]); err != nil {
			return usageError{err.Error()}
		}
		if fs.NArg() != 0 {
			return usageError{"usage: windforce profile set <name> [flags]"}
		}
		if name == "" || profile.APIURL == "" {
			return usageError{noun + " name and --api-url are required"}
		}
		config.Profiles[name] = profile
		if *makeCurrent || config.CurrentProfile == "" {
			config.CurrentProfile = name
		}
		if err := saveConfig(path, config); err != nil {
			return err
		}
		return r.outputJSON(map[string]any{"name": name, "current": config.CurrentProfile == name, "api_url": profile.APIURL, "workspace": profile.Workspace, "actor": profile.Actor, "token_env": profile.TokenEnv, "account": profile.Account})
	default:
		return usageError{fmt.Sprintf("unknown %s command %q", noun, args[0])}
	}
}

func (r *runner) profileNoun() string {
	if r.contextCommand {
		return "context"
	}
	return "profile"
}

func (r *runner) defaultProfileTokenEnv() string {
	if r.program == wfProgram.Name {
		return ""
	}
	return defaultTokenEnv
}

func (r *runner) source(args []string) error {
	if len(args) == 0 {
		return usageError{"source requires list, register, probe, sync, or publish"}
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return usageError{"usage: windforce source list"}
		}
		return r.json(http.MethodGet, r.client.WorkspacePath("git_sources"), nil)
	case "sync":
		if len(args) != 2 {
			return usageError{"usage: windforce source sync <source-id>"}
		}
		return r.json(http.MethodPost, r.client.WorkspacePath("git_sources", args[1], "sync"), nil)
	case "deploy", "publish":
		if len(args) < 2 {
			return usageError{"usage: windforce source " + args[0] + " <source-id> [--message note]"}
		}
		fs := r.flags("source " + args[0])
		message := fs.String("message", "", "audit note")
		if err := fs.Parse(args[2:]); err != nil {
			return usageError{err.Error()}
		}
		if fs.NArg() != 0 {
			return usageError{"usage: windforce source " + args[0] + " <source-id> [--message note]"}
		}
		body := sourceDeployRequest{Confirm: true, Message: *message}
		return r.json(http.MethodPost, r.client.WorkspacePath("git_sources", args[1], "deploy"), body)
	case "register", "probe":
		return r.sourceRegistration(args[0], args[1:])
	default:
		return usageError{fmt.Sprintf("unknown source command %q", args[0])}
	}
}

func (r *runner) sourceRegistration(command string, args []string) error {
	fs := r.flags("source " + command)
	name := fs.String("name", "", "source name")
	repo := fs.String("repo-url", "", "repository URL")
	branch := fs.String("branch", "main", "repository branch")
	subpath := fs.String("subpath", "", "app root within repository")
	credsRef := fs.String("creds-ref", "", "existing credential variable path")
	authMethod := fs.String("auth-method", "", "pat or basic")
	username := fs.String("username", "", "Git username")
	passwordEnv := fs.String("password-env", "", "environment variable containing Git password/token")
	accessTokenEnv := fs.String("access-token-env", "", "environment variable containing Git PAT")
	if err := fs.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if fs.NArg() != 0 {
		return usageError{"usage: windforce source " + command + " [flags]"}
	}
	if *repo == "" || (command == "register" && *name == "") {
		return usageError{"--repo-url and, for register, --name are required"}
	}
	auth, credential, err := gitCredential(*authMethod, *username, *passwordEnv, *accessTokenEnv)
	if err != nil {
		return err
	}
	if command == "probe" {
		body := compact(map[string]any{"repo_url": *repo, "branch": *branch, "creds_ref": *credsRef})
		for key, value := range auth {
			body[key] = value
		}
		return r.json(http.MethodPost, r.client.WorkspacePath("git_sources", "probe"), body)
	}
	if credential != "" {
		if *credsRef == "" {
			*credsRef = defaultCredentialPath(*name)
		}
		if err := r.jsonDiscard(http.MethodPost, r.client.WorkspacePath("variables"), map[string]any{"path": *credsRef, "value": credential, "is_secret": true, "description": "Git credential for source " + *name}); err != nil {
			return err
		}
	} else if *authMethod != "" && *credsRef == "" {
		return fmt.Errorf("Git credential environment variable is not set; provide credentials or --creds-ref")
	}
	return r.json(http.MethodPost, r.client.WorkspacePath("git_sources"), compact(map[string]any{"name": *name, "repo_url": *repo, "branch": *branch, "subpath": *subpath, "creds_ref": *credsRef}))
}

func (r *runner) app(args []string) error {
	if len(args) == 0 {
		return usageError{"app requires list, show, history, source, or openapi"}
	}
	switch args[0] {
	case "list":
		fs := r.flags("app list")
		summary := fs.Bool("summary", false, "return summary rows")
		if err := fs.Parse(args[1:]); err != nil {
			return usageError{err.Error()}
		}
		if fs.NArg() != 0 {
			return usageError{"app list does not accept positional arguments"}
		}
		suffix := ""
		if *summary {
			suffix = "?view=summary"
		}
		return r.json(http.MethodGet, r.client.WorkspacePath("apps")+suffix, nil)
	case "show", "history", "source", "openapi":
		if len(args) != 2 {
			return usageError{"usage: windforce app " + args[0] + " <app>"}
		}
		parts := []string{"apps", args[1]}
		if args[0] == "history" || args[0] == "source" {
			parts = append(parts, args[0])
		}
		if args[0] == "openapi" {
			parts = append(parts, "openapi.json")
		}
		return r.json(http.MethodGet, r.client.WorkspacePath(parts...), nil)
	default:
		return usageError{fmt.Sprintf("unknown app command %q", args[0])}
	}
}

func (r *runner) action(args []string) error {
	if len(args) != 3 || (args[0] != "show" && args[0] != "schema") {
		return usageError{"usage: windforce action show|schema <app> <action>"}
	}
	parts := []string{"apps", args[1], "actions", args[2]}
	if args[0] == "schema" {
		parts = append(parts, "schema")
	}
	return r.json(http.MethodGet, r.client.WorkspacePath(parts...), nil)
}

func (r *runner) run(args []string) error {
	if len(args) == 0 {
		return usageError{"run requires create, wait, show, result, or cancel"}
	}
	switch args[0] {
	case "create", "wait":
		if len(args) < 3 {
			return usageError{"usage: windforce run " + args[0] + " <app> <action> [flags]"}
		}
		fs := r.flags("run " + args[0])
		input := fs.String("input", "{}", "JSON input")
		inputFile := fs.String("input-file", "", "JSON input file, or - for stdin")
		idempotencyKey := fs.String("idempotency-key", "", "principal-scoped idempotency key")
		correlationID := fs.String("correlation-id", "", "caller correlation id")
		timeout := fs.Duration("timeout", 0, "server wait duration")
		if err := fs.Parse(args[3:]); err != nil {
			return usageError{err.Error()}
		}
		if fs.NArg() != 0 || (args[0] == "create" && *timeout != 0) || *timeout < 0 {
			return usageError{"usage: windforce run " + args[0] + " <app> <action> [flags]"}
		}
		inputValue, err := r.readJSON(*input, *inputFile)
		if err != nil {
			return err
		}
		body := map[string]any{
			"app":    args[1],
			"action": args[2],
			"input":  inputValue,
		}
		if *correlationID != "" {
			body["correlation_id"] = *correlationID
		}
		parts := []string{"runs"}
		query := ""
		if args[0] == "wait" {
			parts = append(parts, "wait")
			if *timeout > 0 {
				query = "?timeout=" + url.QueryEscape((*timeout).String())
			}
		}
		return r.jsonWithHeaders(
			http.MethodPost,
			r.client.InvocationPath(parts...)+query,
			body,
			map[string]string{"Idempotency-Key": *idempotencyKey},
		)
	case "show", "result", "cancel":
		if len(args) < 2 {
			return usageError{"usage: windforce run " + args[0] + " <run-id>"}
		}
		if (args[0] == "show" || args[0] == "result") && len(args) != 2 {
			return usageError{"usage: windforce run " + args[0] + " <run-id>"}
		}
		parts := []string{"runs", args[1]}
		method := http.MethodGet
		var body any
		if args[0] == "result" {
			parts = append(parts, "result")
		}
		if args[0] == "cancel" {
			fs := r.flags("run cancel")
			reason := fs.String("reason", "", "cancellation reason")
			if err := fs.Parse(args[2:]); err != nil {
				return usageError{err.Error()}
			}
			if fs.NArg() != 0 {
				return usageError{"usage: windforce run cancel <run-id> [--reason note]"}
			}
			method = http.MethodPost
			parts = append(parts, "cancel")
			body = compact(map[string]any{"reason": *reason})
		}
		return r.json(method, r.client.InvocationPath(parts...), body)
	default:
		return usageError{fmt.Sprintf("unknown run command %q", args[0])}
	}
}

func (r *runner) job(args []string) error {
	if len(args) == 0 {
		return usageError{"job requires list, show, result, logs, or cancel"}
	}
	switch args[0] {
	case "list":
		fs := r.flags("job list")
		status := fs.String("status", "", "job status")
		app := fs.String("app", "", "app key")
		action := fs.String("action", "", "action key")
		limit := fs.Int("limit", 0, "result limit")
		cursor := fs.String("cursor", "", "pagination cursor")
		if err := fs.Parse(args[1:]); err != nil {
			return usageError{err.Error()}
		}
		if fs.NArg() != 0 {
			return usageError{"usage: windforce job list [flags]"}
		}
		values := url.Values{}
		addQuery(values, "status", *status)
		addQuery(values, "app", *app)
		addQuery(values, "action", *action)
		addQuery(values, "cursor", *cursor)
		if *limit > 0 {
			values.Set("limit", strconv.Itoa(*limit))
		}
		return r.json(http.MethodGet, r.client.WorkspacePath("jobs")+query(values), nil)
	case "show", "result", "logs", "cancel":
		if len(args) < 2 {
			return usageError{"usage: windforce job " + args[0] + " <job-id>"}
		}
		if (args[0] == "show" || args[0] == "result") && len(args) != 2 {
			return usageError{"usage: windforce job " + args[0] + " <job-id>"}
		}
		parts := []string{"jobs", args[1]}
		if args[0] != "show" {
			parts = append(parts, args[0])
		}
		if args[0] == "logs" {
			fs := r.flags("job logs")
			tail := fs.Int("tail-bytes", 0, "tail bytes")
			if err := fs.Parse(args[2:]); err != nil {
				return usageError{err.Error()}
			}
			if fs.NArg() != 0 {
				return usageError{"usage: windforce job logs <job-id> [--tail-bytes bytes]"}
			}
			path := r.client.WorkspacePath(parts...)
			if *tail > 0 {
				path += "?tail_bytes=" + strconv.Itoa(*tail)
			}
			return r.raw(http.MethodGet, path, "", nil)
		}
		if args[0] == "cancel" {
			fs := r.flags("job cancel")
			reason := fs.String("reason", "", "cancellation reason")
			if err := fs.Parse(args[2:]); err != nil {
				return usageError{err.Error()}
			}
			if fs.NArg() != 0 {
				return usageError{"usage: windforce job cancel <job-id> [--reason note]"}
			}
			return r.json(http.MethodPost, r.client.WorkspacePath(parts...), compact(map[string]any{"reason": *reason}))
		}
		return r.json(http.MethodGet, r.client.WorkspacePath(parts...), nil)
	default:
		return usageError{fmt.Sprintf("unknown job command %q", args[0])}
	}
}

func (r *runner) provisioning(args []string) error {
	if len(args) == 0 {
		return usageError{"provisioning requires export or apply"}
	}
	switch args[0] {
	case "export":
		fs := r.flags("provisioning export")
		format := fs.String("format", "yaml", "yaml or json")
		includeValues := fs.Bool("include-values", false, "include allowed non-secret values")
		output := fs.String("output", "", "write response to file")
		if err := fs.Parse(args[1:]); err != nil {
			return usageError{err.Error()}
		}
		if fs.NArg() != 0 {
			return usageError{"usage: windforce provisioning export [flags]"}
		}
		values := url.Values{"format": []string{*format}}
		if *includeValues {
			values.Set("include_values", "true")
		}
		data, _, err := r.client.DoRaw(context.Background(), http.MethodGet, r.client.WorkspacePath("provisioning", "export")+query(values), "", nil)
		if err != nil {
			return err
		}
		if *output != "" {
			return os.WriteFile(*output, data, 0o600)
		}
		_, err = r.stdout.Write(data)
		return err
	case "apply":
		fs := r.flags("provisioning apply")
		file := fs.String("file", "", "JSON/YAML file, or - for stdin")
		dryRun := fs.Bool("dry-run", false, "validate without applying")
		if err := fs.Parse(args[1:]); err != nil {
			return usageError{err.Error()}
		}
		if fs.NArg() != 0 {
			return usageError{"usage: windforce provisioning apply --file <path> [--dry-run]"}
		}
		if *file == "" {
			return usageError{"--file is required"}
		}
		data, err := r.readFile(*file)
		if err != nil {
			return err
		}
		contentType := "application/json"
		if *file == "-" || strings.HasSuffix(strings.ToLower(*file), ".yaml") || strings.HasSuffix(strings.ToLower(*file), ".yml") {
			contentType = "application/yaml"
		}
		path := r.client.WorkspacePath("provisioning", "import")
		if *dryRun {
			path += "?dry_run=true"
		}
		response, _, err := r.client.DoRaw(context.Background(), http.MethodPost, path, contentType, data)
		if err != nil {
			return err
		}
		return r.outputJSON(json.RawMessage(response))
	default:
		return usageError{fmt.Sprintf("unknown provisioning command %q", args[0])}
	}
}
