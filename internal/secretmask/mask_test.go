package secretmask

import (
	"bytes"
	"testing"
)

func TestBytesMasksPlainAndJSONEscapedSecret(t *testing.T) {
	secret := "abc\"def"
	got := Bytes([]byte(`{"plain":"abc\"def","log":"abc\"def"}`), []string{secret})
	if bytes.Contains(got, []byte("abc")) || bytes.Contains(got, []byte("def")) {
		t.Fatalf("masked output still contains secret fragments: %s", got)
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
