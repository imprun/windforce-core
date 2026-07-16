package webhook

import (
	"encoding/json"
	"os"
	"testing"
)

type signatureFixture struct {
	Secret    string          `json:"secret"`
	Timestamp string          `json:"timestamp"`
	Body      json.RawMessage `json:"body"`
	Signature string          `json:"signature"`
}

func TestSignatureGoldenFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/signature.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture signatureFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if got := Sign(fixture.Secret, fixture.Timestamp, fixture.Body); got != fixture.Signature {
		t.Fatalf("signature = %q, want %q", got, fixture.Signature)
	}
	if !VerifySignature(fixture.Secret, fixture.Timestamp, fixture.Body, fixture.Signature) {
		t.Fatal("fixture signature did not verify")
	}
	tampered := append([]byte(nil), fixture.Body...)
	tampered[len(tampered)-1] ^= 1
	if VerifySignature(fixture.Secret, fixture.Timestamp, tampered, fixture.Signature) {
		t.Fatal("tampered body verified")
	}
}
