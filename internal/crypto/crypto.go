// Package crypto provides AES-GCM encryption for secret variable values, keyed
// by a per-workspace key.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

// DeriveWorkspaceKey derives a stable per-workspace key string from the instance
// secret and the workspace id.
func DeriveWorkspaceKey(secret, workspaceID string) string {
	sum := sha256.Sum256([]byte(secret + ":" + workspaceID))
	return base64.RawStdEncoding.EncodeToString(sum[:])
}

func aeadFor(key string) (cipher.AEAD, error) {
	k := sha256.Sum256([]byte(key)) // 32-byte AES-256 key
	block, err := aes.NewCipher(k[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Encrypt returns base64(nonce || ciphertext).
func Encrypt(key, plaintext string) (string, error) {
	aead, err := aeadFor(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt reverses Encrypt.
func Decrypt(key, encoded string) (string, error) {
	aead, err := aeadFor(key)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	ns := aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:ns], raw[ns:]
	pt, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// GenerateDEK returns a fresh random 32-byte data-encryption key, base64-encoded.
func GenerateDEK() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(b), nil
}

// DeriveKEK derives the key-encryption key from the instance secret, in a domain
// separate from the job-token HMAC so the two never share key material.
func DeriveKEK(secret string) string {
	sum := sha256.Sum256([]byte(secret + ":wsdek-kek"))
	return base64.RawStdEncoding.EncodeToString(sum[:])
}

// WrapDEK encrypts a DEK under the KEK; UnwrapDEK reverses it.
func WrapDEK(kek, dek string) (string, error) { return Encrypt(kek, dek) }

func UnwrapDEK(kek, wrapped string) (string, error) { return Decrypt(kek, wrapped) }

// ResolveDEK returns the data-encryption key for a stored workspace_key row.
// kekVersion 0 is a legacy row whose stored value is the key, used as-is; version
// >= 1 means the stored value is a wrapped DEK, unwrapped by trying each
// candidate KEK in turn.
func ResolveDEK(storedKey string, kekVersion int32, keks []string) (string, error) {
	if kekVersion == 0 {
		return storedKey, nil
	}
	var lastErr error
	for _, kek := range keks {
		if kek == "" {
			continue
		}
		dek, err := UnwrapDEK(kek, storedKey)
		if err == nil {
			return dek, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no KEK available to unwrap the workspace DEK")
	}
	return "", lastErr
}

// WrappedDEKVersion is the kek_version stored for a wrapped DEK.
const WrappedDEKVersion int32 = 1

// NewWrappedDEK generates a fresh random DEK and returns it wrapped under the KEK
// derived from secret, with the kek_version to store.
func NewWrappedDEK(secret string) (wrapped string, kekVersion int32, err error) {
	dek, err := GenerateDEK()
	if err != nil {
		return "", 0, err
	}
	w, err := WrapDEK(DeriveKEK(secret), dek)
	if err != nil {
		return "", 0, err
	}
	return w, WrappedDEKVersion, nil
}
