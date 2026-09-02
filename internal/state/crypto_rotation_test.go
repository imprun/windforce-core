package state

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/imprun/windforce-core/internal/contract"
	wfcrypto "github.com/imprun/windforce-core/internal/crypto"
)

type rotationWorkspaceKeyProvider struct {
	key     string
	version int32
}

func (p rotationWorkspaceKeyProvider) GetWorkspaceKeyVersioned(context.Context, string) (string, int32, error) {
	return p.key, p.version, nil
}

func TestDecryptInputAtRestUsesPreviousDerivedKeyWhenProviderHasNoWorkspaceKey(t *testing.T) {
	workspaceID := contract.DefaultWorkspace
	previousSecret := "previous-input-secret"
	currentSecret := "current-input-secret"
	legacyDEK := wfcrypto.DeriveWorkspaceKey(previousSecret, workspaceID)
	encrypted, err := wfcrypto.WrapEnc(legacyDEK, []byte(`{"legacy":true}`))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := decryptInputAtRest(context.Background(), rotationWorkspaceKeyProvider{}, inputCryptoConfig{
		SecretKey: currentSecret, SecretKeyPrevious: previousSecret,
	}, workspaceID, json.RawMessage(encrypted))
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != `{"legacy":true}` {
		t.Fatalf("decrypted input = %s", plain)
	}
}

func TestDecryptInputAtRestDoesNotUseDerivedFallbackWhenWorkspaceKeyExists(t *testing.T) {
	workspaceID := contract.DefaultWorkspace
	previousSecret := "previous-input-secret"
	legacyDEK := wfcrypto.DeriveWorkspaceKey(previousSecret, workspaceID)
	encrypted, err := wfcrypto.WrapEnc(legacyDEK, []byte(`{"legacy":true}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = decryptInputAtRest(context.Background(), rotationWorkspaceKeyProvider{
		key: "not-a-wrapped-key", version: wfcrypto.WrappedDEKVersion,
	}, inputCryptoConfig{SecretKey: "current-input-secret", SecretKeyPrevious: previousSecret}, workspaceID, json.RawMessage(encrypted))
	if err == nil {
		t.Fatal("derived-key fallback bypassed an existing workspace key")
	}
}
