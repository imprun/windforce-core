package crypto

import "testing"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := DeriveWorkspaceKey("instance-secret", "workspace-a")
	encrypted, err := Encrypt(key, "s3cr3t")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if encrypted == "s3cr3t" {
		t.Fatal("encrypted value must not equal plaintext")
	}
	decrypted, err := Decrypt(key, encrypted)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if decrypted != "s3cr3t" {
		t.Fatalf("decrypted = %q, want s3cr3t", decrypted)
	}
	if _, err := Decrypt(DeriveWorkspaceKey("other-secret", "workspace-a"), encrypted); err == nil {
		t.Fatal("Decrypt accepted a value encrypted with another key")
	}
}

func TestResolveDEKVersionsAndGraceWindow(t *testing.T) {
	current := DeriveKEK("current-secret")
	previous := DeriveKEK("previous-secret")
	dek, err := GenerateDEK()
	if err != nil {
		t.Fatalf("GenerateDEK: %v", err)
	}

	if got, err := ResolveDEK("legacy-derived-key", 0, []string{current, previous}); err != nil || got != "legacy-derived-key" {
		t.Fatalf("legacy resolve = %q, %v", got, err)
	}

	wrappedPrevious, err := WrapDEK(previous, dek)
	if err != nil {
		t.Fatalf("WrapDEK previous: %v", err)
	}
	if got, err := ResolveDEK(wrappedPrevious, 1, []string{current, previous}); err != nil || got != dek {
		t.Fatalf("previous resolve = %q, %v; want %q", got, err, dek)
	}
	if _, err := ResolveDEK(wrappedPrevious, 1, []string{current}); err == nil {
		t.Fatal("ResolveDEK unwrapped previous-key DEK without the previous KEK")
	}
}
