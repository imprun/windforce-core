package secretmask

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestBytesMasksPlainAndJSONEscapedSecret(t *testing.T) {
	secret := "abc\"def"
	got := Bytes([]byte(`{"plain":"abc\"def","log":"abc\"def"}`), []string{secret})
	if bytes.Contains(got, []byte("abc")) || bytes.Contains(got, []byte("def")) {
		t.Fatalf("masked output still contains secret fragments: %s", got)
	}
}

func TestRegistryMasksDynamicallyRegisteredJSONLeavesAcrossChunks(t *testing.T) {
	registry := NewRegistry([]string{"initial-secret"})
	var output bytes.Buffer
	stream := NewRegistryStream(registry, func(chunk []byte) { _, _ = output.Write(chunk) })
	stream.Write([]byte("before initial-secret\n"))
	if err := registry.RegisterSecret(`{"cookies":[{"name":"sid","value":"short"}],"origin":"https://example.test"}`); err != nil {
		t.Fatal(err)
	}
	stream.Write([]byte("cookie=sho"))
	stream.Write([]byte("rt origin=https://example.test json=\"short\""))
	stream.Flush()
	got := output.String()
	for _, secret := range []string{"initial-secret", "short", "https://example.test"} {
		if strings.Contains(got, secret) {
			t.Fatalf("output contains %q: %q", secret, got)
		}
	}
}

func TestRegistryRejectsRequiredPatternSetAtomically(t *testing.T) {
	registry := NewRegistry(nil)
	oversized := strings.Repeat("x", MaxDynamicPatternBytes+1)
	if err := registry.RegisterSecret(oversized); !errors.Is(err, ErrMaskLimit) {
		t.Fatalf("RegisterSecret error = %v", err)
	}
	if got := registry.String("prefix " + oversized + " suffix"); got != "prefix "+oversized+" suffix" {
		t.Fatalf("registry partially masked rejected secret")
	}
}

func TestStreamMasksSecretAcrossChunks(t *testing.T) {
	var output bytes.Buffer
	stream := NewStream([]string{"cross-boundary-secret"}, func(chunk []byte) {
		_, _ = output.Write(chunk)
	})
	stream.Write([]byte("before cross-bound"))
	stream.Write([]byte("ary-secret after"))
	stream.Flush()
	if bytes.Contains(output.Bytes(), []byte("cross-boundary-secret")) {
		t.Fatalf("stream exposed secret: %q", output.String())
	}
	if !bytes.Contains(output.Bytes(), []byte("before ")) || !bytes.Contains(output.Bytes(), []byte(" after")) {
		t.Fatalf("stream damaged surrounding output: %q", output.String())
	}
}

func TestRegistryMasksRepresentativePlaywrightStorageState(t *testing.T) {
	storageState := `{"cookies":[{"name":"session","value":"session-cookie-42","domain":"example.test","path":"/"}],"origins":[{"origin":"https://example.test","localStorage":[{"name":"accessToken","value":"browser-access-token-42"},{"name":"refreshToken","value":"browser-refresh-token-42"}]}]}`
	registry := NewRegistry(nil)
	if err := registry.RegisterSecret(storageState); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	stream := NewRegistryStream(registry, func(chunk []byte) { _, _ = output.Write(chunk) })
	for _, chunk := range []string{
		"cookie=session-coo",
		"kie-42 access=browser-access-",
		"token-42 refresh=browser-refresh-token-42 origin=https://example.test",
	} {
		stream.Write([]byte(chunk))
	}
	stream.Flush()
	got := output.String()
	for _, secret := range []string{"session-cookie-42", "browser-access-token-42", "browser-refresh-token-42", "https://example.test"} {
		if strings.Contains(got, secret) {
			t.Fatalf("Playwright storageState leaf %q leaked in %q", secret, got)
		}
	}
}

func TestMultiPatternMatcherMatchesNaiveMaskForOverlaps(t *testing.T) {
	secrets := []string{"a", "aba", "bab", `quote"value`, "suffix"}
	input := []byte(`ababa quote\"value and suffixsuffix`)
	want := naiveMask(input, Normalize(secrets))
	if got := Bytes(input, secrets); !bytes.Equal(got, want) {
		t.Fatalf("multi-pattern mask = %q, want %q", got, want)
	}
}

func naiveMask(input []byte, patterns [][]byte) []byte {
	masked := append([]byte(nil), input...)
	marks := make([]bool, len(input))
	for _, pattern := range patterns {
		for offset := 0; len(pattern) > 0 && offset <= len(input)-len(pattern); {
			index := bytes.Index(input[offset:], pattern)
			if index < 0 {
				break
			}
			start := offset + index
			for position := start; position < start+len(pattern); position++ {
				marks[position] = true
			}
			offset = start + 1
		}
	}
	for index, marked := range marks {
		if marked {
			masked[index] = replacementByte
		}
	}
	return masked
}

func BenchmarkRegistryWorstCase4096PatternsFourMiBLog(b *testing.B) {
	registry := NewRegistry(nil)
	for batch := 0; batch < 4; batch++ {
		values := make([]string, 1024)
		for index := range values {
			prefix := fmt.Sprintf("%04d-%04d-", batch, index)
			values[index] = prefix + strings.Repeat(string(rune('a'+batch)), 1000-len(prefix))
		}
		payload, err := json.Marshal(values)
		if err != nil {
			b.Fatal(err)
		}
		if err := registry.RegisterSecret(string(payload)); err != nil {
			b.Fatal(err)
		}
	}
	log := bytes.Repeat([]byte("ordinary-log-byte "), (4<<20)/len("ordinary-log-byte ")+1)
	log = log[:4<<20]
	b.SetBytes(int64(len(log)))
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		_ = registry.Bytes(log)
	}
}
