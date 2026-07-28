package controlcli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func (r *runner) auth(args []string) error {
	if len(args) == 0 {
		return usageError{"auth requires login, status, or logout"}
	}
	switch args[0] {
	case "login":
		return r.authLogin(args[1:])
	case "status":
		if len(args) != 1 {
			return usageError{"usage: wf auth status"}
		}
		return r.authStatus()
	case "logout":
		if len(args) != 1 {
			return usageError{"usage: wf auth logout"}
		}
		return r.authLogout()
	default:
		return usageError{fmt.Sprintf("unknown auth command %q", args[0])}
	}
}

func (r *runner) authLogin(args []string) error {
	fs := r.flags("auth login")
	withToken := fs.Bool("with-token", false, "read a direct Cell credential from standard input")
	account := fs.String("account", "direct", "local account label")
	if err := fs.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if fs.NArg() != 0 || !*withToken {
		return usageError{"usage: wf auth login --with-token [--account label]"}
	}
	if r.store == nil {
		return fmt.Errorf("secure credential storage is unavailable")
	}
	if r.resolved.ProfileName == "" {
		return fmt.Errorf("select or create a context before logging in")
	}
	label := strings.TrimSpace(*account)
	if label == "" || strings.ContainsAny(label, "/\\\x00\r\n") {
		return usageError{"--account must be a non-empty label without slashes or newlines"}
	}
	data, err := io.ReadAll(io.LimitReader(r.stdin, (64<<10)+1))
	if err != nil {
		return fmt.Errorf("read token: %w", err)
	}
	if len(data) > 64<<10 {
		return fmt.Errorf("token from standard input is too large")
	}
	token := strings.TrimSpace(string(data))
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return fmt.Errorf("standard input did not contain one token")
	}
	profile := r.resolved.Profile
	profile.Account = label
	key, err := credentialKey(profile)
	if err != nil {
		return err
	}
	if err := r.probe(token); err != nil {
		return fmt.Errorf("verify credential: %w", err)
	}
	previous, found, err := r.store.Get(key)
	if err != nil {
		return fmt.Errorf("read credential store: %w", err)
	}
	if err := r.store.Set(key, token); err != nil {
		return fmt.Errorf("write credential store: %w", err)
	}
	original := r.config.Profiles[r.resolved.ProfileName]
	updated := original
	updated.APIURL = r.resolved.APIURL
	updated.Workspace = r.resolved.Workspace
	updated.Actor = r.resolved.Actor
	updated.Account = label
	r.config.Profiles[r.resolved.ProfileName] = updated
	if err := saveConfig(r.configPath, r.config); err != nil {
		if found {
			_ = r.store.Set(key, previous)
		} else {
			_ = r.store.Delete(key)
		}
		return err
	}
	return r.outputJSON(map[string]any{
		"account":       label,
		"authenticated": true,
		"context":       r.resolved.ProfileName,
		"host":          key[:strings.LastIndex(key, "/")],
		"storage":       "system-credential-store",
		"workspace":     profile.Workspace,
	})
}

func (r *runner) authStatus() error {
	token, source, err := r.resolveCredential()
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("no credential is available for context %q", r.resolved.ProfileName)
	}
	if err := r.probe(token); err != nil {
		return fmt.Errorf("verify credential: %w", err)
	}
	key := ""
	if r.resolved.Account != "" {
		key, _ = credentialKey(r.resolved.Profile)
	}
	host := ""
	if index := strings.LastIndex(key, "/"); index >= 0 {
		host = key[:index]
	}
	return r.outputJSON(map[string]any{
		"account":       r.resolved.Account,
		"authenticated": true,
		"context":       r.resolved.ProfileName,
		"host":          host,
		"source":        source,
		"workspace":     r.resolved.Workspace,
	})
}

func (r *runner) authLogout() error {
	if r.resolved.ProfileName == "" {
		return fmt.Errorf("select a context before logging out")
	}
	if r.resolved.Account == "" {
		return fmt.Errorf("context %q has no stored account", r.resolved.ProfileName)
	}
	if r.store == nil {
		return fmt.Errorf("secure credential storage is unavailable")
	}
	key, err := credentialKey(r.resolved.Profile)
	if err != nil {
		return err
	}
	credential, found, err := r.store.Get(key)
	if err != nil {
		return fmt.Errorf("read credential store: %w", err)
	}
	if found {
		if err := r.store.Delete(key); err != nil {
			return fmt.Errorf("delete credential: %w", err)
		}
	}
	profile := r.config.Profiles[r.resolved.ProfileName]
	profile.Account = ""
	r.config.Profiles[r.resolved.ProfileName] = profile
	if err := saveConfig(r.configPath, r.config); err != nil {
		if found {
			_ = r.store.Set(key, credential)
		}
		return err
	}
	return r.outputJSON(map[string]any{
		"context":    r.resolved.ProfileName,
		"logged_out": true,
	})
}

func (r *runner) resolveCredential() (string, string, error) {
	if r.resolved.Token != "" {
		return r.resolved.Token, "environment", nil
	}
	if r.resolved.Account == "" {
		return "", "", nil
	}
	if r.store == nil {
		return "", "", fmt.Errorf("secure credential storage is unavailable")
	}
	key, err := credentialKey(r.resolved.Profile)
	if err != nil {
		return "", "", err
	}
	token, found, err := r.store.Get(key)
	if err != nil {
		return "", "", fmt.Errorf("read credential store: %w", err)
	}
	if !found {
		return "", "", nil
	}
	return strings.TrimSpace(token), "system-credential-store", nil
}

func (r *runner) probe(token string) error {
	client := *r.client
	client.Token = token
	_, err := client.DoJSON(context.Background(), http.MethodGet, client.WorkspacePath("apps")+"?summary=1", nil)
	return err
}
