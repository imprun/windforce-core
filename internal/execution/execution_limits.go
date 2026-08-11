package execution

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/state"
)

const maxExecutionLimitKeyComponentBytes = 4096

type executionKeyDigester interface {
	DeriveExecutionKeyDigest(ctx context.Context, workspaceID string, namespace string, keyMaterial []byte) (string, error)
}

type executionLimitInputError struct {
	message string
}

func (e *executionLimitInputError) Error() string { return e.message }

func resolveExecutionLimitPins(ctx context.Context, store Store, workspaceID string, appKey string, actionKey string, deployment contract.Deployment, action contract.Action, input json.RawMessage) (state.ExecutionLimitPins, error) {
	var root any
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return state.ExecutionLimitPins{}, &executionLimitInputError{message: "resolved input is not valid JSON: " + err.Error()}
	}

	appLimits, err := contract.NormalizeExecutionLimits(deployment.ExecutionLimits)
	if err != nil {
		return state.ExecutionLimitPins{}, fmt.Errorf("active release app execution limits: %w", err)
	}
	actionLimits, err := contract.NormalizeExecutionLimits(action.ExecutionLimits)
	if err != nil {
		return state.ExecutionLimitPins{}, fmt.Errorf("active release action execution limits: %w", err)
	}
	if len(appLimits.Concurrency)+len(actionLimits.Concurrency) == 0 {
		return state.ExecutionLimitPins{}, nil
	}
	digester, ok := store.(executionKeyDigester)
	if !ok {
		return state.ExecutionLimitPins{}, errors.New("state store does not support execution-limit key derivation")
	}

	pins := state.ExecutionLimitPins{
		Concurrency: make([]state.KeyedConcurrencyLimitPin, 0, len(appLimits.Concurrency)+len(actionLimits.Concurrency)),
	}
	seen := make(map[string]struct{}, cap(pins.Concurrency))
	appendLimits := func(scope string, limits []contract.KeyedConcurrencyLimit) error {
		for _, limit := range limits {
			identity := scope + ":" + limit.ID
			if _, exists := seen[identity]; exists {
				return fmt.Errorf("active release repeats concurrency limit %q in %s scope", limit.ID, scope)
			}
			seen[identity] = struct{}{}
			material, err := resolveConcurrencyKeyMaterial(root, limit)
			if err != nil {
				return err
			}
			namespace := concurrencyLimitNamespace(appKey, actionKey, scope, limit.ID)
			digest, err := digester.DeriveExecutionKeyDigest(ctx, workspaceID, namespace, material)
			if err != nil {
				return fmt.Errorf("derive concurrency limit %q key digest: %w", limit.ID, err)
			}
			pins.Concurrency = append(pins.Concurrency, state.KeyedConcurrencyLimitPin{
				PolicyID:       limit.ID,
				PolicyRevision: concurrencyLimitRevision(limit),
				Scope:          scope,
				KeyDigest:      digest,
				MaxConcurrent:  limit.MaxConcurrent,
			})
		}
		return nil
	}
	if err := appendLimits(state.ExecutionLimitScopeApp, appLimits.Concurrency); err != nil {
		return state.ExecutionLimitPins{}, err
	}
	if err := appendLimits(state.ExecutionLimitScopeAction, actionLimits.Concurrency); err != nil {
		return state.ExecutionLimitPins{}, err
	}
	if len(pins.Concurrency) == 0 {
		pins.Concurrency = nil
	}
	return pins, nil
}

func resolveConcurrencyKeyMaterial(root any, limit contract.KeyedConcurrencyLimit) ([]byte, error) {
	var material bytes.Buffer
	for _, pointer := range limit.InputPointers {
		value, err := resolveInputJSONPointer(root, pointer)
		if err != nil {
			return nil, &executionLimitInputError{message: fmt.Sprintf("concurrency limit %q: %v", limit.ID, err)}
		}
		canonical, err := canonicalConcurrencyKeyComponent(value)
		if err != nil {
			return nil, &executionLimitInputError{message: fmt.Sprintf("concurrency limit %q input pointer %q: %v", limit.ID, pointer, err)}
		}
		if len(canonical) > maxExecutionLimitKeyComponentBytes {
			return nil, &executionLimitInputError{message: fmt.Sprintf("concurrency limit %q input pointer %q exceeds %d bytes", limit.ID, pointer, maxExecutionLimitKeyComponentBytes)}
		}
		_ = binary.Write(&material, binary.BigEndian, uint32(len(canonical)))
		_, _ = material.Write(canonical)
	}
	return material.Bytes(), nil
}

func resolveInputJSONPointer(root any, pointer string) (any, error) {
	if err := contract.ValidateInputJSONPointer(pointer); err != nil {
		return nil, err
	}
	current := root
	for _, encoded := range strings.Split(pointer[1:], "/") {
		token := strings.ReplaceAll(strings.ReplaceAll(encoded, "~1", "/"), "~0", "~")
		switch value := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = value[token]
			if !ok {
				return nil, fmt.Errorf("input pointer %q does not exist", pointer)
			}
		case []any:
			if token == "-" || (len(token) > 1 && token[0] == '0') {
				return nil, fmt.Errorf("input pointer %q has an invalid array index", pointer)
			}
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(value) {
				return nil, fmt.Errorf("input pointer %q has an out-of-range array index", pointer)
			}
			current = value[index]
		default:
			return nil, fmt.Errorf("input pointer %q traverses a scalar", pointer)
		}
	}
	return current, nil
}

func canonicalConcurrencyKeyComponent(value any) ([]byte, error) {
	switch typed := value.(type) {
	case string:
		encoded, _ := json.Marshal(typed)
		return append([]byte("string:"), encoded...), nil
	case bool:
		if typed {
			return []byte("boolean:true"), nil
		}
		return []byte("boolean:false"), nil
	case json.Number:
		number, ok := new(big.Rat).SetString(typed.String())
		if !ok {
			return nil, errors.New("value is not a valid JSON number")
		}
		return []byte("number:" + number.RatString()), nil
	case nil:
		return nil, errors.New("value must not be null")
	default:
		return nil, errors.New("value must be a string, number, or boolean")
	}
}

func concurrencyLimitNamespace(appKey string, actionKey string, scope string, policyID string) string {
	if scope == state.ExecutionLimitScopeAction {
		return "concurrency/v1/app/" + appKey + "/action/" + actionKey + "/policy/" + policyID
	}
	return "concurrency/v1/app/" + appKey + "/policy/" + policyID
}

func concurrencyLimitRevision(limit contract.KeyedConcurrencyLimit) string {
	encoded, _ := json.Marshal(limit)
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:])
}
