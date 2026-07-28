package controlcli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
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
	web := fs.Bool("web", false, "authenticate with the hosted identity provider")
	noBrowser := fs.Bool("no-browser", false, "print the verification URL without opening a browser")
	account := fs.String("account", "", "local account label")
	if err := fs.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if fs.NArg() != 0 {
		return usageError{"usage: wf auth login [--web | --with-token] [--no-browser] [--account label]"}
	}
	if *withToken && *web {
		return usageError{"--web and --with-token are mutually exclusive"}
	}
	if *withToken && *noBrowser {
		return usageError{"--no-browser can only be used with hosted authentication"}
	}
	if r.store == nil {
		return fmt.Errorf("secure credential storage is unavailable")
	}
	if r.resolved.ProfileName == "" {
		return fmt.Errorf("select or create a context before logging in")
	}
	label := strings.TrimSpace(*account)
	authType := authTypeOAuth2Device
	if *withToken {
		authType = authTypeToken
		if label == "" {
			label = "direct"
		}
	} else if label == "" {
		label = "identity"
	}
	if label == "" || strings.ContainsAny(label, "/\\\x00\r\n") {
		return usageError{"--account must be a non-empty label without slashes or newlines"}
	}

	profile := r.resolved.Profile
	profile.Account = label
	profile.AuthType = authType
	key, err := credentialKey(profile)
	if err != nil {
		return err
	}

	var accessToken, credential string
	if *withToken {
		data, err := io.ReadAll(io.LimitReader(r.stdin, (64<<10)+1))
		if err != nil {
			return fmt.Errorf("read token: %w", err)
		}
		if len(data) > 64<<10 {
			return fmt.Errorf("token from standard input is too large")
		}
		accessToken = strings.TrimSpace(string(data))
		if accessToken == "" || strings.ContainsAny(accessToken, "\r\n") {
			return fmt.Errorf("standard input did not contain one token")
		}
		credential = accessToken
	} else {
		hosted, err := r.loginWithDevice(context.Background(), *noBrowser)
		if err != nil {
			return err
		}
		accessToken = hosted.AccessToken
		credential, err = encodeStoredCredential(hosted)
		if err != nil {
			return err
		}
	}
	if err := r.probe(accessToken); err != nil {
		return fmt.Errorf("verify credential: %w", err)
	}
	previous, found, err := r.store.Get(key)
	if err != nil {
		return fmt.Errorf("read credential store: %w", err)
	}
	if err := r.store.Set(key, credential); err != nil {
		return fmt.Errorf("write credential store: %w", err)
	}
	original := r.config.Profiles[r.resolved.ProfileName]
	updated := original
	updated.APIURL = r.resolved.APIURL
	updated.Workspace = r.resolved.Workspace
	updated.Actor = r.resolved.Actor
	updated.Account = label
	updated.AuthType = authType
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
		"auth_type":     authType,
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
		"auth_type":     firstNonEmpty(r.resolved.AuthType, authTypeToken),
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
	profile.AuthType = ""
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
		r.resolved.AuthType = authTypeToken
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
	raw, found, err := r.store.Get(key)
	if err != nil {
		return "", "", fmt.Errorf("read credential store: %w", err)
	}
	if !found {
		return "", "", nil
	}
	raw = strings.TrimSpace(raw)
	credential, hosted, err := decodeStoredCredential(raw)
	if err != nil {
		return "", "", err
	}
	if !hosted {
		r.resolved.AuthType = authTypeToken
		return raw, "system-credential-store", nil
	}
	r.resolved.AuthType = authTypeOAuth2Device
	if time.Until(credential.ExpiresAt) > refreshBeforeExpiry {
		return credential.AccessToken, "system-credential-store", nil
	}
	refreshed, err := r.refreshStoredCredential(context.Background(), credential)
	if err != nil {
		return "", "", err
	}
	encoded, err := encodeStoredCredential(refreshed)
	if err != nil {
		return "", "", err
	}
	if err := r.store.Set(key, encoded); err != nil {
		return "", "", fmt.Errorf("write refreshed credential: %w", err)
	}
	return refreshed.AccessToken, "system-credential-store", nil
}

func (r *runner) probe(token string) error {
	client := *r.client
	client.Token = token
	_, err := client.DoJSON(context.Background(), http.MethodGet, client.WorkspacePath("apps")+"?summary=1", nil)
	return err
}
