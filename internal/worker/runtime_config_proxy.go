package worker

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/secretmask"
	"github.com/imprun/windforce-core/internal/state"
)

const (
	secretMaskRegistrationHeader = "X-Windforce-Secret-Mask-Registered"
	maxRuntimeConfigJSONBody     = state.RuntimeConfigMaxValueBytes*6 + 64<<10
)

type runtimeConfigProxy struct {
	server *http.Server
	ln     net.Listener
}

func startRuntimeConfigProxy(ctx context.Context, coreBaseURL, coreToken string, registry *secretmask.Registry, access contract.RuntimeAccess) (string, string, func(), error) {
	base, err := url.Parse(strings.TrimSpace(coreBaseURL))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", "", nil, errors.New("valid Core callback URL is required")
	}
	if strings.TrimSpace(coreToken) == "" {
		return "", "", nil, errors.New("Core Job callback token is required")
	}
	localToken, err := randomProxyToken()
	if err != nil {
		return "", "", nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", nil, err
	}
	p := &runtimeConfigProxy{ln: ln}
	p.server = &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !bearerEquals(r.Header.Get("Authorization"), localToken) {
				http.Error(w, "invalid Worker callback capability", http.StatusUnauthorized)
				return
			}
			target := *base
			target.Path = strings.TrimRight(base.Path, "/") + r.URL.Path
			target.RawQuery = r.URL.RawQuery

			var body io.Reader = r.Body
			var raw []byte
			secretWrite := r.Method == http.MethodPut && pinnedSecretVariablePath(access, r.URL.Path)
			if secretWrite {
				var readErr error
				raw, readErr = io.ReadAll(io.LimitReader(r.Body, maxRuntimeConfigJSONBody+1))
				if readErr != nil || len(raw) > maxRuntimeConfigJSONBody {
					http.Error(w, "runtime Variable body exceeds limit", http.StatusRequestEntityTooLarge)
					return
				}
				var request struct {
					Value string `json:"value"`
				}
				if json.Unmarshal(raw, &request) != nil {
					http.Error(w, "valid runtime Variable write body required", http.StatusBadRequest)
					return
				}
				if err := registry.RegisterSecret(request.Value); err != nil {
					http.Error(w, "Secret mask registration limit exceeded", http.StatusUnprocessableEntity)
					return
				}
				body = bytes.NewReader(raw)
			}

			out, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), body)
			if err != nil {
				http.Error(w, "could not prepare Core callback", http.StatusBadGateway)
				return
			}
			copyProxyHeaders(out.Header, r.Header)
			out.Header.Set("Authorization", "Bearer "+coreToken)
			if secretWrite {
				out.Header.Set(secretMaskRegistrationHeader, secretMaskAttestation(coreToken, raw))
			}
			response, err := http.DefaultClient.Do(out)
			if err != nil {
				http.Error(w, "Core callback failed", http.StatusBadGateway)
				return
			}
			defer response.Body.Close()
			responseBody := io.Reader(response.Body)
			if digests := response.Header.Values(secretmask.ResponseDigestHeader); len(digests) > 0 {
				rawResponse, readErr := io.ReadAll(io.LimitReader(response.Body, maxRuntimeConfigJSONBody+1))
				if readErr != nil || len(rawResponse) > maxRuntimeConfigJSONBody {
					http.Error(w, "runtime configuration response exceeds limit", http.StatusBadGateway)
					return
				}
				if registerErr := registerResponseSecrets(registry, rawResponse, digests); registerErr != nil {
					http.Error(w, "invalid Secret-bearing Core response", http.StatusBadGateway)
					return
				}
				responseBody = bytes.NewReader(rawResponse)
			}
			copyProxyHeaders(w.Header(), response.Header)
			w.WriteHeader(response.StatusCode)
			_, _ = io.Copy(w, responseBody)
		}),
	}
	go func() {
		<-ctx.Done()
		_ = p.server.Close()
	}()
	go func() { _ = p.server.Serve(ln) }()
	closeFn := func() { _ = p.server.Close() }
	return "http://" + ln.Addr().String(), localToken, closeFn, nil
}

func pinnedSecretVariablePath(access contract.RuntimeAccess, requestPath string) bool {
	const marker = "/variables/p/"
	index := strings.Index(requestPath, marker)
	if index < 0 {
		return false
	}
	path, err := url.PathUnescape(strings.TrimPrefix(requestPath[index:], marker))
	if err != nil {
		return false
	}
	for _, target := range access.WriteVariables {
		if target.Scope == contract.RuntimeConfigScopeApp && target.Storage == contract.RuntimeVariableStorageSecret && target.Path == path {
			return true
		}
	}
	return false
}

func randomProxyToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func bearerEquals(header, expected string) bool {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}
	actual := parts[1]
	return len(actual) == len(expected) && subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

func secretMaskAttestation(coreToken string, body []byte) string {
	digest := sha256.Sum256(body)
	mac := hmac.New(sha256.New, []byte(coreToken))
	_, _ = mac.Write(digest[:])
	return hex.EncodeToString(mac.Sum(nil))
}

func registerResponseSecrets(registry *secretmask.Registry, body []byte, digests []string) error {
	wanted := make(map[string]struct{}, len(digests))
	for _, values := range digests {
		for _, digest := range strings.Split(values, ",") {
			digest = strings.TrimSpace(digest)
			if digest != "" {
				wanted[digest] = struct{}{}
			}
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return err
	}
	return visitStringLeaves(value, func(candidate string) error {
		if _, ok := wanted[secretmask.Digest(candidate)]; !ok {
			return nil
		}
		return registry.RegisterSecret(candidate)
	})
}

func visitStringLeaves(value any, visit func(string) error) error {
	switch current := value.(type) {
	case string:
		return visit(current)
	case []any:
		for _, item := range current {
			if err := visitStringLeaves(item, visit); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, item := range current {
			if err := visitStringLeaves(item, visit); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyProxyHeaders(dst, src http.Header) {
	for key, values := range src {
		if strings.EqualFold(key, "Authorization") || strings.EqualFold(key, secretMaskRegistrationHeader) || strings.EqualFold(key, secretmask.ResponseDigestHeader) || strings.EqualFold(key, "Host") {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}
