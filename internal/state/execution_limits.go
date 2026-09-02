package state

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

const executionKeyDigestDomain = "windforce.execution-limit-key.v1"

// DeriveExecutionKeyDigest returns a stable workspace-scoped HMAC. Callers
// persist only the returned digest, never namespace material or raw key bytes.
func (s *LocalStore) DeriveExecutionKeyDigest(ctx context.Context, workspaceID string, namespace string, keyMaterial []byte) (string, error) {
	return deriveExecutionKeyDigest(ctx, s, inputCryptoConfig{SecretKey: s.SecretKey, SecretKeyPrevious: s.SecretKeyPrevious}, workspaceID, namespace, keyMaterial)
}

// DeriveExecutionKeyDigest returns the same digest contract as LocalStore.
func (s *PostgresStore) DeriveExecutionKeyDigest(ctx context.Context, workspaceID string, namespace string, keyMaterial []byte) (string, error) {
	return deriveExecutionKeyDigest(ctx, s, inputCryptoConfig{SecretKey: s.SecretKey, SecretKeyPrevious: s.SecretKeyPrevious}, workspaceID, namespace, keyMaterial)
}

func deriveExecutionKeyDigest(ctx context.Context, provider inputWorkspaceKeyProvider, config inputCryptoConfig, workspaceID string, namespace string, keyMaterial []byte) (string, error) {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return "", errors.New("execution limit namespace is empty")
	}
	if len(keyMaterial) == 0 {
		return "", errors.New("execution limit key material is empty")
	}
	storedKey, _, err := provider.GetWorkspaceKeyVersioned(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	if storedKey == "" && strings.TrimSpace(config.SecretKey) == "" {
		return "", errors.New("execution limit key derivation requires SECRET_KEY")
	}
	key, _, err := resolveInputDEK(ctx, provider, config, workspaceID)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(executionKeyDigestDomain))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(namespace))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(keyMaterial)
	return "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil)), nil
}
