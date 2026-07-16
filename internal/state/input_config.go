package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrInvalidInputConfig = errors.New("invalid input config")

type ConfigLayer struct {
	Config     map[string]json.RawMessage
	LockedKeys []string
}

type LockedKeysError struct {
	Keys []string
}

func (e *LockedKeysError) Error() string {
	return "request set locked input keys: " + strings.Join(e.Keys, ", ")
}

func ApplyInputOverlay(request map[string]json.RawMessage, layers []ConfigLayer) (map[string]json.RawMessage, []string) {
	effective := make(map[string]json.RawMessage, len(request))
	locked := map[string]struct{}{}
	for _, layer := range layers {
		for key, value := range layer.Config {
			effective[key] = cloneRaw(value)
		}
		for _, key := range layer.LockedKeys {
			locked[key] = struct{}{}
		}
	}
	rejected := []string{}
	for key, value := range request {
		if _, ok := locked[key]; ok {
			rejected = append(rejected, key)
			continue
		}
		effective[key] = cloneRaw(value)
	}
	sort.Strings(rejected)
	return effective, rejected
}

func resolveInputConfigs(request json.RawMessage, configs []InputConfig) (json.RawMessage, error) {
	if len(configs) == 0 {
		return cloneRaw(request), nil
	}
	var requestObject map[string]json.RawMessage
	if err := json.Unmarshal(canonicalJSONInput(request), &requestObject); err != nil || requestObject == nil {
		return nil, fmt.Errorf("input must be a JSON object when input settings apply")
	}
	sort.SliceStable(configs, func(i, j int) bool {
		return inputConfigRank(configs[i]) < inputConfigRank(configs[j])
	})
	layers := make([]ConfigLayer, 0, len(configs))
	for _, config := range configs {
		var values map[string]json.RawMessage
		if err := json.Unmarshal(canonicalJSONInput(config.Config), &values); err != nil || values == nil {
			return nil, fmt.Errorf("input settings for %s/%s are not a JSON object", config.AppKey, config.ActionKey)
		}
		layers = append(layers, ConfigLayer{Config: values, LockedKeys: append([]string(nil), config.LockedKeys...)})
	}
	effective, rejected := ApplyInputOverlay(requestObject, layers)
	if len(rejected) > 0 {
		return nil, &LockedKeysError{Keys: rejected}
	}
	return json.Marshal(effective)
}

func inputConfigRank(config InputConfig) int {
	rank := 0
	if config.ActionKey != "" {
		rank++
	}
	if config.ClientID != "" {
		rank += 2
	}
	return rank
}

func inputConfigKey(appKey string, actionKey string, clientID string) string {
	return appKey + "\x00" + actionKey + "\x00" + clientID
}

type inputConfigAuditChanges struct {
	Added    []string `json:"added,omitempty"`
	Updated  []string `json:"updated,omitempty"`
	Removed  []string `json:"removed,omitempty"`
	Locked   []string `json:"locked,omitempty"`
	Unlocked []string `json:"unlocked,omitempty"`
}

func inputConfigAuditDetail(previous *InputConfig, next *InputConfig) string {
	previousValues := inputConfigValues(previous)
	nextValues := inputConfigValues(next)
	changes := inputConfigAuditChanges{
		Added:    changedInputKeys(previousValues, nextValues, "added"),
		Updated:  changedInputKeys(previousValues, nextValues, "updated"),
		Removed:  changedInputKeys(previousValues, nextValues, "removed"),
		Locked:   changedStringSet(inputConfigLockedKeys(previous), inputConfigLockedKeys(next)),
		Unlocked: changedStringSet(inputConfigLockedKeys(next), inputConfigLockedKeys(previous)),
	}
	payload, _ := json.Marshal(changes)
	return string(payload)
}

func inputConfigValues(config *InputConfig) map[string]json.RawMessage {
	values := map[string]json.RawMessage{}
	if config != nil {
		_ = json.Unmarshal(config.Config, &values)
	}
	return values
}

func inputConfigLockedKeys(config *InputConfig) []string {
	if config == nil {
		return nil
	}
	return config.LockedKeys
}

func changedInputKeys(previous map[string]json.RawMessage, next map[string]json.RawMessage, kind string) []string {
	keys := []string{}
	for key, nextValue := range next {
		previousValue, exists := previous[key]
		if kind == "added" && !exists {
			keys = append(keys, key)
		}
		if kind == "updated" && exists && !bytes.Equal(previousValue, nextValue) {
			keys = append(keys, key)
		}
	}
	if kind == "removed" {
		for key := range previous {
			if _, exists := next[key]; !exists {
				keys = append(keys, key)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

func changedStringSet(previous []string, next []string) []string {
	existing := make(map[string]struct{}, len(previous))
	for _, value := range previous {
		existing[value] = struct{}{}
	}
	changed := []string{}
	for _, value := range next {
		if _, exists := existing[value]; !exists {
			changed = append(changed, value)
		}
	}
	sort.Strings(changed)
	return changed
}

func findInputConfig(configs []InputConfig, actionKey string, clientID string) *InputConfig {
	for i := range configs {
		if configs[i].ActionKey == actionKey && configs[i].ClientID == clientID {
			copy := configs[i]
			return &copy
		}
	}
	return nil
}

func normalizedLockedKeys(config map[string]json.RawMessage, keys []string) ([]string, error) {
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(keys))
	for _, raw := range keys {
		key := strings.TrimSpace(raw)
		if key == "" {
			return nil, fmt.Errorf("%w: locked key must not be empty", ErrInvalidInputConfig)
		}
		if _, ok := config[key]; !ok {
			return nil, fmt.Errorf("%w: locked key %q is not present in config", ErrInvalidInputConfig, key)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, key)
	}
	sort.Strings(normalized)
	return normalized, nil
}
