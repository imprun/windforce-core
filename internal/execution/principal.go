package execution

import (
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/state"
)

type PrincipalKind string

const (
	PrincipalOperator PrincipalKind = "operator"
	PrincipalClient   PrincipalKind = "client"
	PrincipalService  PrincipalKind = "service"
	PrincipalTrigger  PrincipalKind = "trigger"
)

type Scope string

const (
	ScopeRunsCreate                 Scope = "runs:create"
	ScopeRunsReadOwn                Scope = "runs:read:own"
	ScopeRunsReadAny                Scope = "runs:read:any"
	ScopeRunsCancelOwn              Scope = "runs:cancel:own"
	ScopeRunsCancelAny              Scope = "runs:cancel:any"
	ScopeAppsRead                   Scope = "apps:read"
	ScopeHumanTasksRead             Scope = "human_tasks:read"
	ScopeHumanTasksDecide           Scope = "human_tasks:decide"
	ScopeOpaqueHTTPProjectionsRead  Scope = "opaque-http-projections:read"
	ScopeOpaqueHTTPProjectionsWrite Scope = "opaque-http-projections:write"
)

var ValidScopes = []Scope{
	ScopeRunsCreate,
	ScopeRunsReadOwn,
	ScopeRunsReadAny,
	ScopeRunsCancelOwn,
	ScopeRunsCancelAny,
	ScopeAppsRead,
	ScopeHumanTasksRead,
	ScopeHumanTasksDecide,
	ScopeOpaqueHTTPProjectionsRead,
	ScopeOpaqueHTTPProjectionsWrite,
}

type Principal struct {
	Kind           PrincipalKind
	ID             string
	Workspace      string
	Subject        string
	Scopes         []Scope
	AllowedTargets []string
	TargetPolicy   state.TargetPolicy
}

func (p Principal) Normalized() Principal {
	p.Workspace = contract.NormalizeWorkspace(p.Workspace)
	p.ID = strings.TrimSpace(p.ID)
	p.Subject = strings.TrimSpace(p.Subject)
	if p.Subject == "" {
		p.Subject = string(p.Kind) + ":" + p.ID
	}
	seenScopes := map[Scope]struct{}{}
	scopes := make([]Scope, 0, len(p.Scopes))
	for _, scope := range p.Scopes {
		if _, seen := seenScopes[scope]; seen {
			continue
		}
		seenScopes[scope] = struct{}{}
		scopes = append(scopes, scope)
	}
	p.Scopes = scopes
	targets := make([]string, 0, len(p.AllowedTargets))
	seenTargets := map[string]struct{}{}
	for _, target := range p.AllowedTargets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		if _, seen := seenTargets[target]; seen {
			continue
		}
		seenTargets[target] = struct{}{}
		targets = append(targets, target)
	}
	p.AllowedTargets = targets
	if p.Kind == PrincipalClient {
		p.TargetPolicy = state.EffectiveTargetPolicy(p.TargetPolicy)
	}
	return p
}

func (p Principal) HasScope(scope Scope) bool {
	if p.Kind == PrincipalOperator {
		return true
	}
	for _, candidate := range p.Scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}

func (p Principal) AllowsTarget(app string, action string) bool {
	if p.Kind == PrincipalOperator {
		return true
	}
	if p.Kind == PrincipalClient {
		return p.TargetPolicy.Allows(app, action)
	}
	if len(p.AllowedTargets) == 0 {
		return true
	}
	target := strings.TrimSpace(app) + "/" + strings.TrimSpace(action)
	for _, candidate := range p.AllowedTargets {
		if candidate == app || candidate == target || action == "" && strings.HasPrefix(candidate, app+"/") {
			return true
		}
	}
	return false
}

func (p Principal) Owns(run state.Run) bool {
	if p.Kind == PrincipalOperator {
		return true
	}
	if run.PrincipalKind != "" || run.PrincipalID != "" {
		return run.PrincipalKind == string(p.Kind) && run.PrincipalID == p.ID
	}
	return p.Kind == PrincipalClient && run.ClientID != "" && run.ClientID == p.ID
}

func (p Principal) IdempotencyScope() string {
	return string(p.Kind) + ":" + p.ID
}

func DefaultClientPrincipal(workspace string, client state.Client) Principal {
	return Principal{
		Kind:      PrincipalClient,
		ID:        client.ID,
		Workspace: workspace,
		Subject:   "client:" + client.ID,
		Scopes: []Scope{
			ScopeRunsCreate,
			ScopeRunsReadOwn,
			ScopeRunsCancelOwn,
			ScopeAppsRead,
		},
		TargetPolicy: client.EffectiveInvocationPolicy(),
	}.Normalized()
}

func ServicePrincipal(workspace string, principal state.ServicePrincipal) Principal {
	scopes := make([]Scope, 0, len(principal.Scopes))
	for _, scope := range principal.Scopes {
		scopes = append(scopes, Scope(scope))
	}
	return Principal{
		Kind:           PrincipalService,
		ID:             principal.ID,
		Workspace:      workspace,
		Subject:        "service:" + principal.ID,
		Scopes:         scopes,
		AllowedTargets: principal.AllowedTargets,
	}.Normalized()
}

func OperatorPrincipal(workspace string, subject string) Principal {
	return Principal{
		Kind:      PrincipalOperator,
		ID:        strings.TrimSpace(subject),
		Workspace: workspace,
		Subject:   subject,
	}.Normalized()
}

func TriggerPrincipal(workspace string, triggerID string, app string, action string) Principal {
	return Principal{
		Kind:           PrincipalTrigger,
		ID:             strings.TrimSpace(triggerID),
		Workspace:      workspace,
		Subject:        "trigger:" + strings.TrimSpace(triggerID),
		Scopes:         []Scope{ScopeRunsCreate, ScopeRunsReadOwn},
		AllowedTargets: []string{strings.TrimSpace(app) + "/" + strings.TrimSpace(action)},
	}.Normalized()
}

func ValidScopeSet(scopes []string) bool {
	valid := map[string]struct{}{}
	for _, scope := range ValidScopes {
		valid[string(scope)] = struct{}{}
	}
	for _, scope := range scopes {
		if _, ok := valid[strings.TrimSpace(scope)]; !ok {
			return false
		}
	}
	return true
}
