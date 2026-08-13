package state

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
)

const (
	TargetPolicyModeAll        = "all"
	TargetPolicyModeRestricted = "restricted"
)

type TargetPolicy struct {
	Mode           string   `json:"mode" yaml:"mode"`
	AllowedTargets []string `json:"allowed_targets" yaml:"allowedTargets"`
}

type CreateClientRequest struct {
	WorkspaceID      string
	Name             string
	TokenHash        string
	InvocationPolicy *TargetPolicy
	Actor            string
}

type UpdateClientInvocationPolicyRequest struct {
	WorkspaceID        string
	ClientID           string
	Policy             TargetPolicy
	OperationID        string
	ExpectedRevision   int64
	RequestFingerprint string
	Actor              string
}

func NormalizeTargetPolicy(policy TargetPolicy) (TargetPolicy, error) {
	policy.Mode = strings.TrimSpace(policy.Mode)
	switch policy.Mode {
	case TargetPolicyModeAll:
		if len(policy.AllowedTargets) != 0 {
			return TargetPolicy{}, errors.New("all target policy must not include allowed targets")
		}
		return TargetPolicy{Mode: TargetPolicyModeAll, AllowedTargets: []string{}}, nil
	case TargetPolicyModeRestricted:
		targets := make([]string, 0, len(policy.AllowedTargets))
		seen := make(map[string]struct{}, len(policy.AllowedTargets))
		for _, raw := range policy.AllowedTargets {
			target := strings.TrimSpace(raw)
			if !validTargetPolicyTarget(target) {
				return TargetPolicy{}, errors.New("allowed target must be an app or app/action key")
			}
			if _, exists := seen[target]; exists {
				continue
			}
			seen[target] = struct{}{}
			targets = append(targets, target)
		}
		sort.Strings(targets)
		return TargetPolicy{Mode: TargetPolicyModeRestricted, AllowedTargets: targets}, nil
	default:
		return TargetPolicy{}, errors.New("target policy mode must be all or restricted")
	}
}

func EffectiveTargetPolicy(policy TargetPolicy) TargetPolicy {
	if strings.TrimSpace(policy.Mode) == "" {
		return TargetPolicy{Mode: TargetPolicyModeAll, AllowedTargets: []string{}}
	}
	normalized, err := NormalizeTargetPolicy(policy)
	if err != nil {
		return TargetPolicy{Mode: TargetPolicyModeRestricted, AllowedTargets: []string{}}
	}
	return normalized
}

func initialTargetPolicy(policy *TargetPolicy) (TargetPolicy, error) {
	if policy == nil {
		return TargetPolicy{Mode: TargetPolicyModeAll, AllowedTargets: []string{}}, nil
	}
	return NormalizeTargetPolicy(*policy)
}

func (policy TargetPolicy) Allows(app string, action string) bool {
	policy = EffectiveTargetPolicy(policy)
	if policy.Mode == TargetPolicyModeAll {
		return true
	}
	app = strings.TrimSpace(app)
	action = strings.TrimSpace(action)
	if !contract.ValidAppKey(app) {
		return false
	}
	target := app
	if action != "" {
		if !contract.ValidActionKey(action) {
			return false
		}
		target += "/" + action
	}
	for _, candidate := range policy.AllowedTargets {
		if candidate == app || candidate == target || action == "" && strings.HasPrefix(candidate, app+"/") {
			return true
		}
	}
	return false
}

func (client Client) EffectiveInvocationPolicy() TargetPolicy {
	return EffectiveTargetPolicy(client.InvocationPolicy)
}

func validTargetPolicyTarget(target string) bool {
	app, action, hasAction := strings.Cut(target, "/")
	if !contract.ValidAppKey(app) {
		return false
	}
	if !hasAction {
		return true
	}
	return !strings.Contains(action, "/") && contract.ValidActionKey(action)
}

func clientInvocationPolicyDetail(client Client) string {
	policy := client.EffectiveInvocationPolicy()
	detail, _ := json.Marshal(struct {
		Mode           string   `json:"mode"`
		AllowedTargets []string `json:"allowed_targets"`
		Revision       int64    `json:"revision"`
	}{
		Mode:           policy.Mode,
		AllowedTargets: policy.AllowedTargets,
		Revision:       client.InvocationPolicyRevision,
	})
	return string(detail)
}
