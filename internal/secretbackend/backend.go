package secretbackend

import (
	"context"
	"fmt"
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
	wfcrypto "github.com/imprun/windforce-core/internal/crypto"
)

type Reference struct {
	WorkspaceID string
	Kind        string
	Path        string
}

type Backend interface {
	Store(ctx context.Context, reference Reference, plaintext string) (string, error)
	Resolve(ctx context.Context, reference Reference, stored string) (string, error)
}

type WorkspaceKeyProvider interface {
	GetWorkspaceKeyVersioned(ctx context.Context, workspaceID string) (string, int32, error)
}

type Database struct {
	keys     WorkspaceKeyProvider
	current  string
	previous string
}

const boundCiphertextPrefix = "wfsec:v1:"

func NewDatabase(keys WorkspaceKeyProvider, current string, previous string) *Database {
	return &Database{
		keys:     keys,
		current:  strings.TrimSpace(current),
		previous: strings.TrimSpace(previous),
	}
}

func (b *Database) Store(ctx context.Context, reference Reference, plaintext string) (string, error) {
	dek, _, err := b.workspaceDEK(ctx, reference.WorkspaceID)
	if err != nil {
		return "", fmt.Errorf("resolve workspace data-encryption key: %w", err)
	}
	encrypted, err := wfcrypto.EncryptWithAAD(dek, plaintext, referenceAAD(reference))
	if err != nil {
		return "", fmt.Errorf("encrypt %s %q: %w", reference.Kind, reference.Path, err)
	}
	return boundCiphertextPrefix + encrypted, nil
}

func (b *Database) Resolve(ctx context.Context, reference Reference, stored string) (string, error) {
	dek, legacyDerived, err := b.workspaceDEK(ctx, reference.WorkspaceID)
	if err != nil {
		return "", fmt.Errorf("resolve workspace data-encryption key: %w", err)
	}
	plaintext, err := decryptStored(dek, reference, stored)
	if err == nil {
		return plaintext, nil
	}
	if legacyDerived && b.previous != "" {
		legacyDEK := wfcrypto.DeriveWorkspaceKey(b.previous, contract.NormalizeWorkspace(reference.WorkspaceID))
		if previousPlaintext, previousErr := decryptStored(legacyDEK, reference, stored); previousErr == nil {
			return previousPlaintext, nil
		}
	}
	return "", fmt.Errorf("decrypt %s %q: %w", reference.Kind, reference.Path, err)
}

func decryptStored(dek string, reference Reference, stored string) (string, error) {
	if strings.HasPrefix(stored, boundCiphertextPrefix) {
		return wfcrypto.DecryptWithAAD(
			dek,
			strings.TrimPrefix(stored, boundCiphertextPrefix),
			referenceAAD(reference),
		)
	}
	// Compatibility for values stored before record-bound ciphertext v1.
	return wfcrypto.Decrypt(dek, stored)
}

func referenceAAD(reference Reference) []byte {
	return []byte(strings.Join([]string{
		"windforce-secret-v1",
		contract.NormalizeWorkspace(reference.WorkspaceID),
		strings.ToLower(strings.TrimSpace(reference.Kind)),
		strings.TrimSpace(reference.Path),
	}, "\x00"))
}

func (b *Database) workspaceDEK(ctx context.Context, workspaceID string) (string, bool, error) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	if b.keys != nil {
		stored, version, err := b.keys.GetWorkspaceKeyVersioned(ctx, workspaceID)
		if err != nil {
			return "", false, err
		}
		if stored != "" {
			dek, err := wfcrypto.ResolveDEK(stored, version, b.keks())
			return dek, false, err
		}
	}
	return wfcrypto.DeriveWorkspaceKey(b.current, workspaceID), true, nil
}

func (b *Database) keks() []string {
	keks := []string{wfcrypto.DeriveKEK(b.current)}
	if b.previous != "" {
		keks = append(keks, wfcrypto.DeriveKEK(b.previous))
	}
	return keks
}
