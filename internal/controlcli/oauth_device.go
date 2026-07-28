package controlcli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	authTypeToken        = "token"
	authTypeOAuth2Device = "oauth2-device"

	cliMetadataPath     = "/.well-known/wf-cli.json"
	maxOAuthBodyBytes   = 1 << 20
	maxOAuthSecretBytes = 64 << 10
	refreshBeforeExpiry = time.Minute
)

type cliMetadata struct {
	SchemaVersion  int                       `json:"schema_version"`
	Authentication cliMetadataAuthentication `json:"authentication"`
}

type cliMetadataAuthentication struct {
	Type         string   `json:"type"`
	Issuer       string   `json:"issuer"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	Audience     string   `json:"audience"`
	Scopes       []string `json:"scopes"`
}

type oidcDiscovery struct {
	Issuer                      string `json:"issuer"`
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
}

type deviceClientConfig struct {
	Issuer                      string
	ClientID                    string
	Audience                    string
	Scopes                      []string
	DeviceAuthorizationEndpoint string
	TokenEndpoint               string
}

type deviceAuthorization struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
}

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
}

type storedCredential struct {
	Version      int       `json:"version"`
	Kind         string    `json:"kind"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
	TokenURL     string    `json:"token_endpoint"`
	ClientID     string    `json:"client_id"`
	Issuer       string    `json:"issuer"`
}

func (r *runner) loginWithDevice(ctx context.Context, noBrowser bool) (storedCredential, error) {
	client, err := r.discoverDeviceClient(ctx)
	if err != nil {
		return storedCredential{}, err
	}
	authorization, err := r.requestDeviceAuthorization(ctx, client)
	if err != nil {
		return storedCredential{}, err
	}

	verificationURL := authorization.VerificationURIComplete
	if verificationURL == "" {
		verificationURL = authorization.VerificationURI
	}
	fmt.Fprintf(r.stderr, "Copy this one-time code: %s\n", authorization.UserCode)
	if noBrowser {
		fmt.Fprintf(r.stderr, "Open this URL to continue: %s\n", verificationURL)
	} else if err := openSystemBrowser(verificationURL); err != nil {
		fmt.Fprintf(r.stderr, "Could not open a browser. Open this URL to continue: %s\n", verificationURL)
	} else {
		fmt.Fprintf(r.stderr, "Opening %s in your browser.\n", verificationURL)
	}

	expiresAt := time.Now().Add(time.Duration(authorization.ExpiresIn) * time.Second)
	pollContext, cancel := context.WithDeadline(ctx, expiresAt)
	defer cancel()
	credential, err := r.pollDeviceToken(pollContext, client, authorization)
	if err != nil {
		return storedCredential{}, err
	}
	return credential, nil
}

func (r *runner) discoverDeviceClient(ctx context.Context) (deviceClientConfig, error) {
	origin, err := targetOrigin(r.resolved.APIURL)
	if err != nil {
		return deviceClientConfig{}, err
	}
	metadataURL := origin.ResolveReference(&url.URL{Path: cliMetadataPath})

	var metadata cliMetadata
	if err := r.getJSON(ctx, metadataURL.String(), origin, &metadata); err != nil {
		return deviceClientConfig{}, fmt.Errorf("discover hosted authentication: %w", err)
	}
	auth := metadata.Authentication
	if metadata.SchemaVersion != 1 {
		return deviceClientConfig{}, fmt.Errorf("hosted authentication metadata uses unsupported schema version %d", metadata.SchemaVersion)
	}
	if auth.Type != authTypeOAuth2Device {
		return deviceClientConfig{}, fmt.Errorf("hosted authentication type %q is not supported", auth.Type)
	}
	if strings.TrimSpace(auth.ClientSecret) != "" {
		return deviceClientConfig{}, fmt.Errorf("hosted authentication metadata must not contain a client secret")
	}
	issuer, err := safeWebURL(auth.Issuer)
	if err != nil {
		return deviceClientConfig{}, fmt.Errorf("invalid hosted authentication issuer: %w", err)
	}
	clientID, err := validatePublicValue("client_id", auth.ClientID)
	if err != nil {
		return deviceClientConfig{}, err
	}
	audience, err := validatePublicValue("audience", auth.Audience)
	if err != nil {
		return deviceClientConfig{}, err
	}
	scopes, err := validateScopes(auth.Scopes)
	if err != nil {
		return deviceClientConfig{}, err
	}

	discoveryURL := strings.TrimRight(issuer.String(), "/") + "/.well-known/openid-configuration"
	discoveryOrigin := &url.URL{Scheme: issuer.Scheme, Host: issuer.Host}
	var discovery oidcDiscovery
	if err := r.getJSON(ctx, discoveryURL, discoveryOrigin, &discovery); err != nil {
		return deviceClientConfig{}, fmt.Errorf("discover identity provider: %w", err)
	}
	if discovery.Issuer != issuer.String() {
		return deviceClientConfig{}, fmt.Errorf("identity provider issuer does not match hosted authentication metadata")
	}
	deviceEndpoint, err := safeEndpointURL(discovery.DeviceAuthorizationEndpoint)
	if err != nil {
		return deviceClientConfig{}, fmt.Errorf("invalid device authorization endpoint: %w", err)
	}
	tokenEndpoint, err := safeEndpointURL(discovery.TokenEndpoint)
	if err != nil {
		return deviceClientConfig{}, fmt.Errorf("invalid token endpoint: %w", err)
	}
	return deviceClientConfig{
		Issuer:                      issuer.String(),
		ClientID:                    clientID,
		Audience:                    audience,
		Scopes:                      scopes,
		DeviceAuthorizationEndpoint: deviceEndpoint.String(),
		TokenEndpoint:               tokenEndpoint.String(),
	}, nil
}

func (r *runner) requestDeviceAuthorization(ctx context.Context, client deviceClientConfig) (deviceAuthorization, error) {
	form := url.Values{
		"audience":  {client.Audience},
		"client_id": {client.ClientID},
		"scope":     {strings.Join(client.Scopes, " ")},
	}
	var authorization deviceAuthorization
	status, err := r.postFormJSON(ctx, client.DeviceAuthorizationEndpoint, form, &authorization)
	if err != nil {
		return deviceAuthorization{}, fmt.Errorf("start device authorization: %w", err)
	}
	if status != http.StatusOK {
		return deviceAuthorization{}, fmt.Errorf("start device authorization: identity provider returned HTTP %d", status)
	}
	if err := authorization.validate(); err != nil {
		return deviceAuthorization{}, fmt.Errorf("start device authorization: %w", err)
	}
	return authorization, nil
}

func (r *runner) pollDeviceToken(ctx context.Context, client deviceClientConfig, authorization deviceAuthorization) (storedCredential, error) {
	interval := time.Duration(authorization.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	for {
		form := url.Values{
			"client_id":   {client.ClientID},
			"device_code": {authorization.DeviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		}
		var token oauthTokenResponse
		status, err := r.postFormJSON(ctx, client.TokenEndpoint, form, &token)
		if err != nil {
			return storedCredential{}, fmt.Errorf("complete device authorization: %w", err)
		}
		if status >= 200 && status < 300 && token.Error == "" {
			return newStoredCredential(client, token)
		}
		switch token.Error {
		case "authorization_pending":
			if err := sleepContext(ctx, interval); err != nil {
				return storedCredential{}, fmt.Errorf("device authorization expired")
			}
		case "slow_down":
			interval += 5 * time.Second
			if err := sleepContext(ctx, interval); err != nil {
				return storedCredential{}, fmt.Errorf("device authorization expired")
			}
		case "access_denied":
			return storedCredential{}, fmt.Errorf("device authorization was denied")
		case "expired_token":
			return storedCredential{}, fmt.Errorf("device authorization expired")
		default:
			if token.Error == "" {
				return storedCredential{}, fmt.Errorf("complete device authorization: identity provider returned HTTP %d", status)
			}
			return storedCredential{}, fmt.Errorf("complete device authorization: identity provider returned %q", token.Error)
		}
	}
}

func (r *runner) refreshStoredCredential(ctx context.Context, current storedCredential) (storedCredential, error) {
	form := url.Values{
		"client_id":     {current.ClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {current.RefreshToken},
	}
	var token oauthTokenResponse
	status, err := r.postFormJSON(ctx, current.TokenURL, form, &token)
	if err != nil {
		return storedCredential{}, fmt.Errorf("refresh authentication: %w", err)
	}
	if status < 200 || status >= 300 || token.Error != "" {
		if token.Error != "" {
			return storedCredential{}, fmt.Errorf("refresh authentication: identity provider returned %q", token.Error)
		}
		return storedCredential{}, fmt.Errorf("refresh authentication: identity provider returned HTTP %d", status)
	}
	if token.RefreshToken == "" {
		token.RefreshToken = current.RefreshToken
	}
	client := deviceClientConfig{
		Issuer:        current.Issuer,
		ClientID:      current.ClientID,
		TokenEndpoint: current.TokenURL,
	}
	return newStoredCredential(client, token)
}

func newStoredCredential(client deviceClientConfig, token oauthTokenResponse) (storedCredential, error) {
	if err := validateOAuthSecret("access token", token.AccessToken); err != nil {
		return storedCredential{}, err
	}
	if err := validateOAuthSecret("refresh token", token.RefreshToken); err != nil {
		return storedCredential{}, err
	}
	if !strings.EqualFold(strings.TrimSpace(token.TokenType), "Bearer") {
		return storedCredential{}, fmt.Errorf("identity provider returned unsupported token type")
	}
	if token.ExpiresIn <= 0 || token.ExpiresIn > int64((24*time.Hour)/time.Second) {
		return storedCredential{}, fmt.Errorf("identity provider returned invalid token lifetime")
	}
	return storedCredential{
		Version:      1,
		Kind:         authTypeOAuth2Device,
		AccessToken:  strings.TrimSpace(token.AccessToken),
		RefreshToken: strings.TrimSpace(token.RefreshToken),
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Duration(token.ExpiresIn) * time.Second),
		TokenURL:     client.TokenEndpoint,
		ClientID:     client.ClientID,
		Issuer:       client.Issuer,
	}, nil
}

func encodeStoredCredential(credential storedCredential) (string, error) {
	data, err := json.Marshal(credential)
	if err != nil {
		return "", fmt.Errorf("encode credential: %w", err)
	}
	if len(data) > maxOAuthSecretBytes {
		return "", fmt.Errorf("credential is too large for secure storage")
	}
	return string(data), nil
}

func decodeStoredCredential(raw string) (storedCredential, bool, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "{") {
		return storedCredential{}, false, nil
	}
	var credential storedCredential
	if err := json.Unmarshal([]byte(raw), &credential); err != nil {
		return storedCredential{}, true, fmt.Errorf("stored credential is damaged")
	}
	if credential.Version != 1 || credential.Kind != authTypeOAuth2Device {
		return storedCredential{}, true, fmt.Errorf("stored credential uses an unsupported format")
	}
	if err := validateOAuthSecret("access token", credential.AccessToken); err != nil {
		return storedCredential{}, true, fmt.Errorf("stored credential is damaged")
	}
	if err := validateOAuthSecret("refresh token", credential.RefreshToken); err != nil {
		return storedCredential{}, true, fmt.Errorf("stored credential is damaged")
	}
	if !strings.EqualFold(credential.TokenType, "Bearer") || credential.ExpiresAt.IsZero() {
		return storedCredential{}, true, fmt.Errorf("stored credential is damaged")
	}
	tokenURL, err := safeEndpointURL(credential.TokenURL)
	if err != nil {
		return storedCredential{}, true, fmt.Errorf("stored credential is damaged")
	}
	issuer, err := safeWebURL(credential.Issuer)
	if err != nil {
		return storedCredential{}, true, fmt.Errorf("stored credential is damaged")
	}
	clientID, err := validatePublicValue("client_id", credential.ClientID)
	if err != nil {
		return storedCredential{}, true, fmt.Errorf("stored credential is damaged")
	}
	credential.TokenURL = tokenURL.String()
	credential.Issuer = issuer.String()
	credential.ClientID = clientID
	return credential, true, nil
}

func (authorization *deviceAuthorization) validate() error {
	if err := validateOAuthSecret("device code", authorization.DeviceCode); err != nil {
		return err
	}
	authorization.DeviceCode = strings.TrimSpace(authorization.DeviceCode)
	userCode, err := validatePublicValue("user_code", authorization.UserCode)
	if err != nil || len(userCode) > 128 {
		return fmt.Errorf("identity provider returned invalid user code")
	}
	authorization.UserCode = userCode
	verificationURI, err := safeBrowserURL(authorization.VerificationURI)
	if err != nil {
		return fmt.Errorf("identity provider returned invalid verification URI")
	}
	authorization.VerificationURI = verificationURI.String()
	if authorization.VerificationURIComplete != "" {
		complete, err := safeBrowserURL(authorization.VerificationURIComplete)
		if err != nil || !sameOrigin(verificationURI, complete) {
			return fmt.Errorf("identity provider returned invalid complete verification URI")
		}
		authorization.VerificationURIComplete = complete.String()
	}
	if authorization.ExpiresIn <= 0 || authorization.ExpiresIn > int64((24*time.Hour)/time.Second) {
		return fmt.Errorf("identity provider returned invalid device-code lifetime")
	}
	if authorization.Interval < 0 || authorization.Interval > 60 {
		return fmt.Errorf("identity provider returned invalid polling interval")
	}
	return nil
}

func (r *runner) getJSON(ctx context.Context, rawURL string, expectedOrigin *url.URL, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	client := *r.client.HTTP
	client.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		if !sameOrigin(expectedOrigin, next.URL) {
			return http.ErrUseLastResponse
		}
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if !sameOrigin(expectedOrigin, response.Request.URL) {
		return fmt.Errorf("cross-origin discovery redirect is not allowed")
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned HTTP %d", response.StatusCode)
	}
	return decodeBoundedJSON(response.Body, target)
}

func (r *runner) postFormJSON(ctx context.Context, rawURL string, form url.Values, target any) (int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := *r.client.HTTP
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return response.StatusCode, fmt.Errorf("redirects are not allowed for OAuth token requests")
	}
	if err := decodeBoundedJSON(response.Body, target); err != nil {
		return response.StatusCode, err
	}
	return response.StatusCode, nil
}

func decodeBoundedJSON(reader io.Reader, target any) error {
	data, err := io.ReadAll(io.LimitReader(reader, maxOAuthBodyBytes+1))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if len(data) > maxOAuthBodyBytes {
		return fmt.Errorf("response is too large")
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("response is not valid JSON")
	}
	return nil
}

func targetOrigin(raw string) (*url.URL, error) {
	target, err := safeWebURL(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid context API URL: %w", err)
	}
	return &url.URL{Scheme: target.Scheme, Host: target.Host}, nil
}

func safeWebURL(raw string) (*url.URL, error) {
	return safeAbsoluteWebURL(raw, false)
}

func safeBrowserURL(raw string) (*url.URL, error) {
	return safeAbsoluteWebURL(raw, true)
}

func safeEndpointURL(raw string) (*url.URL, error) {
	return safeAbsoluteWebURL(raw, true)
}

func safeAbsoluteWebURL(raw string, allowQuery bool) (*url.URL, error) {
	value := strings.TrimSpace(raw)
	target, err := url.Parse(value)
	if err != nil || target.Host == "" || target.User != nil || (!allowQuery && target.RawQuery != "") || target.Fragment != "" {
		return nil, fmt.Errorf("expected an absolute HTTP URL without credentials, query, or fragment")
	}
	if target.Scheme == "https" {
		return target, nil
	}
	if target.Scheme == "http" && isLoopbackHost(target.Hostname()) {
		return target, nil
	}
	return nil, fmt.Errorf("HTTPS is required except for loopback development")
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	address := net.ParseIP(strings.TrimSpace(host))
	return address != nil && address.IsLoopback()
}

func sameOrigin(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func validatePublicValue(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 4096 || strings.ContainsAny(value, " \t\r\n\x00") {
		return "", fmt.Errorf("hosted authentication metadata contains invalid %s", name)
	}
	return value, nil
}

func validateScopes(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > 32 {
		return nil, fmt.Errorf("hosted authentication metadata contains invalid scopes")
	}
	scopes := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		scope, err := validatePublicValue("scope", value)
		if err != nil {
			return nil, fmt.Errorf("hosted authentication metadata contains invalid scopes")
		}
		if _, exists := seen[scope]; exists {
			return nil, fmt.Errorf("hosted authentication metadata contains duplicate scopes")
		}
		seen[scope] = struct{}{}
		scopes = append(scopes, scope)
	}
	if _, ok := seen["openid"]; !ok {
		return nil, fmt.Errorf("hosted authentication metadata must request openid")
	}
	if _, ok := seen["offline_access"]; !ok {
		return nil, fmt.Errorf("hosted authentication metadata must request offline_access")
	}
	return scopes, nil
}

func validateOAuthSecret(name, value string) error {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxOAuthSecretBytes || strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("identity provider returned invalid %s", name)
	}
	return nil
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func openSystemBrowser(rawURL string) error {
	if _, err := safeBrowserURL(rawURL); err != nil {
		return err
	}
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", rawURL)
	case "darwin":
		command = exec.Command("open", rawURL)
	default:
		command = exec.Command("xdg-open", rawURL)
	}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
