package controlcli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestErrorJSONRedactsCredentialFieldsAndTokenPatterns(t *testing.T) {
	input := []byte(`{
		"error":"request used Bearer header-secret",
		"access_token":"access-secret",
		"nested":{"password":"password-secret","detail":"rejected wfw_engine-secret"},
		"token_id":"safe-token-id"
	}`)
	output := string(redactErrorJSON(input))
	for _, secret := range []string{"header-secret", "access-secret", "password-secret", "wfw_engine-secret"} {
		if strings.Contains(output, secret) {
			t.Fatalf("redacted output contains %q: %s", secret, output)
		}
	}
	if !strings.Contains(output, `"token_id":"safe-token-id"`) ||
		strings.Count(output, redactedValue) < 4 {
		t.Fatalf("redacted output = %s", output)
	}
}

func TestWriteErrorRedactsBearerAndEngineCredentials(t *testing.T) {
	var output bytes.Buffer
	writeError(&output, errors.New("Authorization: Bearer access-secret; token=wfk_client-secret"))
	if strings.Contains(output.String(), "access-secret") ||
		strings.Contains(output.String(), "wfk_client-secret") ||
		!strings.Contains(output.String(), redactedValue) {
		t.Fatalf("output = %q", output.String())
	}
}
