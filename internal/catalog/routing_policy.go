package catalog

import (
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

// RoutingPolicy is workspace-owned operator configuration. It is deliberately
// stored outside release history so publishing or rolling back an App cannot
// replace the operator's routing decision.
type RoutingPolicy struct {
	Workspace              string                         `json:"workspace"`
	App                    string                         `json:"app"`
	RouteTagOverride       *string                        `json:"routeTagOverride,omitempty"`
	RequiredLabelsOverride *[]string                      `json:"requiredLabelsOverride,omitempty"`
	Actions                map[string]ActionRoutingPolicy `json:"actions,omitempty"`
	UpdatedBy              string                         `json:"updatedBy,omitempty"`
	UpdatedAt              time.Time                      `json:"updatedAt,omitempty"`
}

type ActionRoutingPolicy struct {
	RouteTagOverride       *string   `json:"routeTagOverride,omitempty"`
	RequiredLabelsOverride *[]string `json:"requiredLabelsOverride,omitempty"`
	UpdatedBy              string    `json:"updatedBy,omitempty"`
	UpdatedAt              time.Time `json:"updatedAt,omitempty"`
}

type RoutingPolicyPatch struct {
	RouteTagSet            bool
	RouteTagOverride       *string
	RequiredLabelsSet      bool
	RequiredLabelsOverride *[]string
	Actor                  string
}

func NewRoutingPolicy(workspace string, app string) RoutingPolicy {
	return RoutingPolicy{
		Workspace: contract.NormalizeWorkspace(workspace),
		App:       app,
		Actions:   map[string]ActionRoutingPolicy{},
	}
}

func RoutingPolicyKey(workspace string, app string) string {
	return DeploymentKey(workspace, app)
}

func NormalizeRoutingPolicy(policy RoutingPolicy) RoutingPolicy {
	policy.Workspace = contract.NormalizeWorkspace(policy.Workspace)
	policy.App = strings.TrimSpace(policy.App)
	policy.RouteTagOverride = cloneTrimmedStringPtr(policy.RouteTagOverride)
	policy.RequiredLabelsOverride = cloneStringSlicePtrPreserveEmpty(policy.RequiredLabelsOverride)
	if policy.Actions == nil {
		policy.Actions = map[string]ActionRoutingPolicy{}
	}
	for key, action := range policy.Actions {
		action.RouteTagOverride = cloneTrimmedStringPtr(action.RouteTagOverride)
		action.RequiredLabelsOverride = cloneStringSlicePtrPreserveEmpty(action.RequiredLabelsOverride)
		policy.Actions[key] = action
	}
	return policy
}

func ApplyRoutingPolicy(deployment contract.Deployment, policy RoutingPolicy) contract.Deployment {
	policy = NormalizeRoutingPolicy(policy)
	deployment.TagOverride = cloneStringPtr(policy.RouteTagOverride)
	deployment.RequiredLabelsOverride = cloneStringSlicePtrPreserveEmpty(policy.RequiredLabelsOverride)
	actions := make(map[string]contract.Action, len(deployment.Actions))
	for key, action := range deployment.Actions {
		if actionPolicy, ok := policy.Actions[key]; ok {
			action.TagOverride = cloneStringPtr(actionPolicy.RouteTagOverride)
			action.RequiredLabelsOverride = cloneStringSlicePtrPreserveEmpty(actionPolicy.RequiredLabelsOverride)
		} else {
			action.TagOverride = nil
			action.RequiredLabelsOverride = nil
		}
		actions[key] = action
	}
	deployment.Actions = actions
	return deployment
}

// ExtractEmbeddedRoutingPolicy migrates the pre-policy representation where
// route overrides were written into the active Deployment JSON. Existing
// policy values always win over embedded legacy values.
func ExtractEmbeddedRoutingPolicy(deployment *contract.Deployment, policy RoutingPolicy) RoutingPolicy {
	if strings.TrimSpace(policy.Workspace) == "" {
		policy.Workspace = deployment.SourceWorkspace()
	}
	if strings.TrimSpace(policy.App) == "" {
		policy.App = deployment.App
	}
	policy = NormalizeRoutingPolicy(policy)
	if policy.RouteTagOverride == nil && deployment.TagOverride != nil {
		policy.RouteTagOverride = cloneStringPtr(deployment.TagOverride)
	}
	if policy.RequiredLabelsOverride == nil && deployment.RequiredLabelsOverride != nil {
		policy.RequiredLabelsOverride = cloneStringSlicePtrPreserveEmpty(deployment.RequiredLabelsOverride)
	}
	for key, action := range deployment.Actions {
		actionPolicy := policy.Actions[key]
		if actionPolicy.RouteTagOverride == nil && action.TagOverride != nil {
			actionPolicy.RouteTagOverride = cloneStringPtr(action.TagOverride)
		}
		if actionPolicy.RequiredLabelsOverride == nil && action.RequiredLabelsOverride != nil {
			actionPolicy.RequiredLabelsOverride = cloneStringSlicePtrPreserveEmpty(action.RequiredLabelsOverride)
		}
		if actionPolicy.RouteTagOverride != nil || actionPolicy.RequiredLabelsOverride != nil {
			policy.Actions[key] = actionPolicy
		}
	}
	deployment.TagOverride = nil
	deployment.RequiredLabelsOverride = nil
	for key, action := range deployment.Actions {
		action.TagOverride = nil
		action.RequiredLabelsOverride = nil
		deployment.Actions[key] = action
	}
	return policy
}

func RoutingPolicyEmpty(policy RoutingPolicy) bool {
	if policy.RouteTagOverride != nil || policy.RequiredLabelsOverride != nil {
		return false
	}
	for _, action := range policy.Actions {
		if action.RouteTagOverride != nil || action.RequiredLabelsOverride != nil {
			return false
		}
	}
	return true
}

func ApplyRoutingPolicyPatch(policy RoutingPolicy, actionKey string, patch RoutingPolicyPatch, now time.Time) RoutingPolicy {
	policy = NormalizeRoutingPolicy(policy)
	actor := strings.TrimSpace(patch.Actor)
	if actor == "" {
		actor = "system"
	}
	if actionKey == "" {
		if patch.RouteTagSet {
			policy.RouteTagOverride = cloneStringPtr(patch.RouteTagOverride)
		}
		if patch.RequiredLabelsSet {
			policy.RequiredLabelsOverride = cloneStringSlicePtrPreserveEmpty(patch.RequiredLabelsOverride)
		}
		policy.UpdatedBy = actor
		policy.UpdatedAt = now
		return policy
	}
	action := policy.Actions[actionKey]
	if patch.RouteTagSet {
		action.RouteTagOverride = cloneStringPtr(patch.RouteTagOverride)
	}
	if patch.RequiredLabelsSet {
		action.RequiredLabelsOverride = cloneStringSlicePtrPreserveEmpty(patch.RequiredLabelsOverride)
	}
	action.UpdatedBy = actor
	action.UpdatedAt = now
	if action.RouteTagOverride == nil && action.RequiredLabelsOverride == nil {
		delete(policy.Actions, actionKey)
	} else {
		policy.Actions[actionKey] = action
	}
	policy.UpdatedBy = actor
	policy.UpdatedAt = now
	return policy
}

func cloneTrimmedStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

func cloneStringSlicePtrPreserveEmpty(value *[]string) *[]string {
	if value == nil {
		return nil
	}
	cloned := append([]string{}, (*value)...)
	return &cloned
}
