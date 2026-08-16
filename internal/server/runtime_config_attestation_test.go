package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/imprun/windforce-core/internal/secretmask"
)

func TestValidSecretMaskRegistrationIsBodyAndJobTokenBound(t *testing.T) {
	body := []byte(`{"value":"secret","operationId":"op-1"}`)
	req := httptest.NewRequest("PUT", "/api/w/ws/variables/p/token", nil)
	req.Header.Set("Authorization", "Bearer job-token")
	digest := sha256.Sum256(body)
	mac := hmac.New(sha256.New, []byte("job-token"))
	_, _ = mac.Write(digest[:])
	req.Header.Set(secretMaskRegistrationHeader, hex.EncodeToString(mac.Sum(nil)))
	if !validSecretMaskRegistration(req, body) {
		t.Fatal("valid attestation was rejected")
	}
	if validSecretMaskRegistration(req, []byte(`{"value":"different"}`)) {
		t.Fatal("attestation was accepted for a different body")
	}
	req.Header.Set("Authorization", "Bearer another-job-token")
	if validSecretMaskRegistration(req, body) {
		t.Fatal("attestation was accepted for a different Job token")
	}
}

func TestRuntimeResourceMutationFingerprintIncludesDescription(t *testing.T) {
	first := runtimeResourceMutationFingerprint("session/profile", json.RawMessage(`{"ok":true}`), "profile@1", "first", nil)
	second := runtimeResourceMutationFingerprint("session/profile", json.RawMessage(`{"ok":true}`), "profile@1", "second", nil)
	if first == second {
		t.Fatal("Resource mutation description was omitted from the idempotency fingerprint")
	}
}

func TestAppendSecretMaskDigestsDoesNotExposePlaintextAndDeduplicates(t *testing.T) {
	header := http.Header{}
	appendSecretMaskDigests(header, []string{"session-secret", "session-secret", ""}, true)
	values := header.Values(secretmask.ResponseDigestHeader)
	if len(values) != 1 || values[0] != secretmask.Digest("session-secret") {
		t.Fatalf("digest headers = %#v", values)
	}
	if strings.Contains(strings.Join(values, ","), "session-secret") {
		t.Fatal("Secret plaintext was exposed in the response header")
	}
	appendSecretMaskDigests(header, []string{"disabled-secret"}, false)
	if len(header.Values(secretmask.ResponseDigestHeader)) != 1 {
		t.Fatal("disabled digest emission changed the response header")
	}
}
