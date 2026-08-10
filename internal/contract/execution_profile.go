package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

const (
	ExecutionProfileVersion = "execution-profile-v1"

	ExecutionRuntimeBun    = "bun"
	ExecutionRuntimePython = "python"
	ExecutionRuntimeGo     = "go"

	executionProfileLabelPrefix = ReservedLabelPrefix + "execution-profile-"
)

// ExecutionProfile is the immutable worker target selected when a Release is
// published. It intentionally describes execution compatibility, not the host
// distribution or the windforce-core build version.
type ExecutionProfile struct {
	Version    string `json:"version"`
	Key        string `json:"key"`
	ID         string `json:"id,omitempty"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Runtime    string `json:"runtime"`
	RuntimeABI string `json:"runtimeAbi"`
	Libc       string `json:"libc"`
}

// NormalizeExecutionRuntime maps the source-manifest vocabulary to the
// launcher/runtime vocabulary. TypeScript is executed by Bun.
func NormalizeExecutionRuntime(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", ScriptLangTypeScript, ExecutionRuntimeBun:
		return ExecutionRuntimeBun, nil
	case ScriptLangPython:
		return ExecutionRuntimePython, nil
	case ScriptLangGo:
		return ExecutionRuntimeGo, nil
	default:
		return "", fmt.Errorf("unsupported execution runtime %q", value)
	}
}

// NewExecutionProfile constructs the canonical compatibility key.
func NewExecutionProfile(id string, osName string, arch string, runtimeName string, runtimeABI string, libc string) (ExecutionProfile, error) {
	runtimeName, err := NormalizeExecutionRuntime(runtimeName)
	if err != nil {
		return ExecutionProfile{}, err
	}
	profile := ExecutionProfile{
		Version:    ExecutionProfileVersion,
		ID:         strings.TrimSpace(id),
		OS:         strings.ToLower(strings.TrimSpace(osName)),
		Arch:       strings.ToLower(strings.TrimSpace(arch)),
		Runtime:    runtimeName,
		RuntimeABI: strings.TrimSpace(runtimeABI),
		Libc:       strings.ToLower(strings.TrimSpace(libc)),
	}
	if profile.OS == "" || profile.Arch == "" || profile.RuntimeABI == "" || profile.Libc == "" {
		return ExecutionProfile{}, fmt.Errorf("execution profile requires os, arch, runtime ABI, and libc identity")
	}
	if len(profile.ID) > 512 || len(profile.OS) > 32 || len(profile.Arch) > 64 || len(profile.RuntimeABI) > 256 || len(profile.Libc) > 64 {
		return ExecutionProfile{}, fmt.Errorf("execution profile field exceeds its size limit")
	}
	profile.Key = executionProfileKey(profile)
	return profile, nil
}

// ValidateExecutionProfile rejects hand-edited or stale profile metadata.
func ValidateExecutionProfile(profile ExecutionProfile) error {
	if profile.Version != ExecutionProfileVersion {
		return fmt.Errorf("unsupported execution profile version %q", profile.Version)
	}
	normalized, err := NewExecutionProfile(profile.ID, profile.OS, profile.Arch, profile.Runtime, profile.RuntimeABI, profile.Libc)
	if err != nil {
		return err
	}
	if profile != normalized {
		return fmt.Errorf("execution profile is not canonical")
	}
	return nil
}

// ExecutionProfilesCompatible uses the canonical key as the scheduler and
// launcher compatibility contract. A configured immutable image/profile ID is
// part of the key, but never substitutes for the runtime and platform fields.
func ExecutionProfilesCompatible(required ExecutionProfile, offered ExecutionProfile) bool {
	return ValidateExecutionProfile(required) == nil &&
		ValidateExecutionProfile(offered) == nil &&
		required.Key == offered.Key
}

func AnyExecutionProfileCompatible(required ExecutionProfile, offered []ExecutionProfile) bool {
	if required == (ExecutionProfile{}) {
		return true
	}
	for _, profile := range offered {
		if ExecutionProfilesCompatible(required, profile) {
			return true
		}
	}
	return false
}

func NormalizeExecutionProfiles(profiles []ExecutionProfile) ([]ExecutionProfile, error) {
	unique := make(map[string]ExecutionProfile, len(profiles))
	for _, profile := range profiles {
		if err := ValidateExecutionProfile(profile); err != nil {
			return nil, err
		}
		unique[profile.Key] = profile
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]ExecutionProfile, 0, len(keys))
	for _, key := range keys {
		out = append(out, unique[key])
	}
	return out, nil
}

// ExecutionProfileLabel compiles a profile into the existing atomic worker
// label scheduler. The sys/ namespace cannot be authored by app manifests.
func ExecutionProfileLabel(profile ExecutionProfile) (string, error) {
	if err := ValidateExecutionProfile(profile); err != nil {
		return "", err
	}
	return executionProfileLabelPrefix + profile.Key[:24], nil
}

func ExecutionProfileLabels(profiles []ExecutionProfile) ([]string, error) {
	labels := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		label, err := ExecutionProfileLabel(profile)
		if err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}
	return NormalizeLabels(labels, true)
}

// WithExecutionProfileLabel appends the engine-owned placement constraint.
// It does not consume one of the manifest's user-label slots.
func WithExecutionProfileLabel(labels []string, profile ExecutionProfile) ([]string, error) {
	profileLabel, err := ExecutionProfileLabel(profile)
	if err != nil {
		return nil, err
	}
	out := append([]string(nil), labels...)
	for _, label := range out {
		if label == profileLabel {
			return out, nil
		}
	}
	out = append(out, profileLabel)
	sort.Strings(out)
	return out, nil
}

// WithExecutionProfileLabels merges engine-owned profile labels after the
// operator/user label set has already passed its independent size limit.
func WithExecutionProfileLabels(labels []string, profiles []ExecutionProfile) ([]string, error) {
	out := append([]string(nil), labels...)
	seen := make(map[string]struct{}, len(out)+len(profiles))
	for _, label := range out {
		seen[label] = struct{}{}
	}
	profileLabels, err := ExecutionProfileLabels(profiles)
	if err != nil {
		return nil, err
	}
	for _, label := range profileLabels {
		if _, ok := seen[label]; !ok {
			out = append(out, label)
			seen[label] = struct{}{}
		}
	}
	sort.Strings(out)
	return out, nil
}

func executionProfileKey(profile ExecutionProfile) string {
	canonical := strings.Join([]string{
		ExecutionProfileVersion,
		profile.ID,
		profile.OS,
		profile.Arch,
		profile.Runtime,
		profile.RuntimeABI,
		profile.Libc,
	}, "\x00")
	digest := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(digest[:])
}
