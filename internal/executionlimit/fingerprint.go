package executionlimit

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
)

const (
	FingerprintVersion = "v1"
	FingerprintPrefix  = "elfp:" + FingerprintVersion + ":sha256:"

	KindConcurrency = "concurrency"
	KindRate        = "rate"

	ScopeApp    = "app"
	ScopeAction = "action"

	ImplicitAppConcurrencyPolicyID = "app-concurrency"
)

// Shape is the release-owned identity of an execution limit. Capacity values
// are deliberately absent: changing a ceiling must not create a new shape.
type Shape struct {
	WorkspaceID   string
	AppKey        string
	ActionKey     string
	Scope         string
	PolicyID      string
	Kind          string
	InputPointers []string
	WindowSeconds int32
}

// Fingerprint returns a versioned SHA-256 over a length-framed canonical
// tuple. Length framing prevents delimiter ambiguity and keeps the value safe
// to compare across Local, PostgreSQL, admission, and control-plane paths.
func Fingerprint(shape Shape) (string, error) {
	normalized, err := normalizeShape(shape)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	writeFrame(hash, "execution-limit-shape")
	writeFrame(hash, FingerprintVersion)
	writeFrame(hash, normalized.WorkspaceID)
	writeFrame(hash, normalized.AppKey)
	writeFrame(hash, normalized.ActionKey)
	writeFrame(hash, normalized.Scope)
	writeFrame(hash, normalized.PolicyID)
	writeFrame(hash, normalized.Kind)
	for _, pointer := range normalized.InputPointers {
		writeFrame(hash, pointer)
	}
	if normalized.Kind == KindRate {
		var window [4]byte
		binary.BigEndian.PutUint32(window[:], uint32(normalized.WindowSeconds))
		_, _ = hash.Write(window[:])
	}
	return FingerprintPrefix + hex.EncodeToString(hash.Sum(nil)), nil
}

func AppConcurrencyFingerprint(workspaceID string, appKey string) (string, error) {
	return Fingerprint(Shape{
		WorkspaceID: workspaceID,
		AppKey:      appKey,
		Scope:       ScopeApp,
		PolicyID:    ImplicitAppConcurrencyPolicyID,
		Kind:        KindConcurrency,
	})
}

func IsFingerprint(value string) bool {
	if !strings.HasPrefix(value, FingerprintPrefix) {
		return false
	}
	digest := strings.TrimPrefix(value, FingerprintPrefix)
	if len(digest) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}

func normalizeShape(shape Shape) (Shape, error) {
	shape.WorkspaceID = contract.NormalizeWorkspace(shape.WorkspaceID)
	shape.AppKey = strings.TrimSpace(shape.AppKey)
	shape.ActionKey = strings.TrimSpace(shape.ActionKey)
	shape.Scope = strings.TrimSpace(shape.Scope)
	shape.PolicyID = strings.TrimSpace(shape.PolicyID)
	shape.Kind = strings.TrimSpace(shape.Kind)
	if shape.AppKey == "" || shape.PolicyID == "" {
		return Shape{}, fmt.Errorf("execution limit shape requires app and policy id")
	}
	if shape.Scope != ScopeApp && shape.Scope != ScopeAction {
		return Shape{}, fmt.Errorf("execution limit scope %q is invalid", shape.Scope)
	}
	if shape.Scope == ScopeAction && shape.ActionKey == "" {
		return Shape{}, fmt.Errorf("action-scoped execution limit requires an action")
	}
	if shape.Scope == ScopeApp {
		shape.ActionKey = ""
	}
	if shape.Kind != KindConcurrency && shape.Kind != KindRate {
		return Shape{}, fmt.Errorf("execution limit kind %q is invalid", shape.Kind)
	}
	if shape.Kind == KindConcurrency && shape.WindowSeconds != 0 {
		return Shape{}, fmt.Errorf("concurrency execution limit cannot declare a rate window")
	}
	if shape.Kind == KindRate && shape.WindowSeconds <= 0 {
		return Shape{}, fmt.Errorf("rate execution limit requires a positive window")
	}
	if len(shape.InputPointers) > contract.MaxExecutionInputPointers {
		return Shape{}, fmt.Errorf("execution limit shape declares too many input pointers")
	}
	shape.InputPointers = append([]string(nil), shape.InputPointers...)
	for _, pointer := range shape.InputPointers {
		if err := contract.ValidateInputJSONPointer(pointer); err != nil {
			return Shape{}, fmt.Errorf("execution limit shape input pointer: %w", err)
		}
	}
	return shape, nil
}

type byteWriter interface {
	Write([]byte) (int, error)
}

func writeFrame(writer byteWriter, value string) {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(value)))
	_, _ = writer.Write(size[:])
	_, _ = writer.Write([]byte(value))
}
