package state

import (
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
)

var ErrInsufficientPlacementCapacity = errors.New("insufficient matching placement capacity")

// PlacementPolicyMutationRequest applies one App or Action policy patch only
// when every candidate target has the requested matching live capacity.
type PlacementPolicyMutationRequest struct {
	WorkspaceID          string
	App                  string
	Action               string
	Patch                catalog.RoutingPolicyPatch
	OperationID          string
	ExpectedRevision     int64
	MinimumMatchingSlots int64
	RequestFingerprint   string
	Actor                string
}

type PlacementPolicyMutationResult struct {
	Deployment contract.Deployment
	Policy     catalog.RoutingPolicy
	Check      catalog.RoutingPolicyOperationResult
	Replayed   bool
}

func normalizePlacementPolicyMutationRequest(request PlacementPolicyMutationRequest) (PlacementPolicyMutationRequest, error) {
	request.WorkspaceID = contract.NormalizeWorkspace(request.WorkspaceID)
	request.App = strings.TrimSpace(request.App)
	request.Action = strings.TrimSpace(request.Action)
	request.OperationID = strings.TrimSpace(request.OperationID)
	request.RequestFingerprint = strings.TrimSpace(request.RequestFingerprint)
	request.Actor = strings.TrimSpace(request.Actor)
	request.Patch.Actor = request.Actor
	if request.App == "" || request.ExpectedRevision < 0 || request.MinimumMatchingSlots < 1 ||
		request.OperationID == "" || len(request.OperationID) > 128 || CleanID(request.OperationID) != request.OperationID ||
		request.RequestFingerprint == "" || (!request.Patch.RouteTagSet && !request.Patch.RequiredLabelsSet) {
		return PlacementPolicyMutationRequest{}, ErrInvalidState
	}
	return request, nil
}

func replayPlacementPolicyMutation(policy catalog.RoutingPolicy, request PlacementPolicyMutationRequest) (catalog.RoutingPolicyOperationResult, bool, error) {
	policy = catalog.NormalizeRoutingPolicy(policy)
	if policy.LastOperationID != request.OperationID {
		return catalog.RoutingPolicyOperationResult{}, false, nil
	}
	if policy.LastRequestFingerprint != request.RequestFingerprint || policy.LastOperationResult == nil {
		return catalog.RoutingPolicyOperationResult{}, false, ErrConflict
	}
	return *policy.LastOperationResult, true, nil
}

func observePlacementTargets(
	deployment contract.Deployment,
	actionKey string,
	minimumMatchingSlots int64,
	checkedAt time.Time,
	workers []WorkerRecord,
	credentials map[string]WorkerCredential,
	runStates map[string]WorkerGroupRunState,
) (catalog.RoutingPolicyOperationResult, bool, error) {
	type target struct {
		app     string
		action  string
		tag     string
		labels  []string
		profile contract.ExecutionProfile
	}
	targets := []target{{
		app: deployment.App, tag: contract.EffectiveRouteTagForApp(deployment),
		labels: contract.EffectiveRequiredLabels(deployment, contract.Action{}), profile: deployment.ExecutionProfile,
	}}
	if actionKey != "" {
		action, ok := deployment.Actions[actionKey]
		if !ok {
			return catalog.RoutingPolicyOperationResult{}, false, catalog.ErrActionNotFound
		}
		targets = []target{{
			app: deployment.App, action: actionKey,
			tag:    contract.EffectiveRouteTagForAction(deployment, action),
			labels: contract.EffectiveRequiredLabels(deployment, action), profile: deployment.ExecutionProfile,
		}}
	} else {
		actionKeys := make([]string, 0, len(deployment.Actions))
		for key := range deployment.Actions {
			actionKeys = append(actionKeys, key)
		}
		sort.Strings(actionKeys)
		for _, key := range actionKeys {
			action := deployment.Actions[key]
			targets = append(targets, target{
				app: deployment.App, action: key,
				tag:    contract.EffectiveRouteTagForAction(deployment, action),
				labels: contract.EffectiveRequiredLabels(deployment, action), profile: deployment.ExecutionProfile,
			})
		}
	}

	result := catalog.RoutingPolicyOperationResult{
		CheckedAt: checkedAt.UTC(), MinimumMatchingSlots: minimumMatchingSlots,
		Targets: make([]catalog.RoutingPolicyTargetObservation, 0, len(targets)),
	}
	sufficient := true
	for _, candidate := range targets {
		observation := catalog.RoutingPolicyTargetObservation{
			App: candidate.app, Action: candidate.action, EffectiveTag: candidate.tag,
			EffectiveRequiredLabels: append([]string{}, candidate.labels...), ExecutionProfile: candidate.profile,
		}
		for _, worker := range workers {
			if !placementWorkerEligible(worker, deployment.SourceWorkspace(), checkedAt, credentials, runStates) {
				continue
			}
			tags, labels, err := WorkerClaimSelector(worker)
			if err != nil {
				return catalog.RoutingPolicyOperationResult{}, false, err
			}
			if !selectorAllowed(candidate.tag, candidate.labels, normalizeClaimTags(tags), normalizeClaimTags(labels)) {
				continue
			}
			observation.MatchingWorkers++
			slots := worker.Slots
			if slots < 1 {
				slots = 1
			}
			if observation.MatchingSlots > math.MaxInt64-int64(slots) {
				observation.MatchingSlots = math.MaxInt64
			} else {
				observation.MatchingSlots += int64(slots)
			}
		}
		if observation.MatchingSlots < minimumMatchingSlots {
			sufficient = false
		}
		result.Targets = append(result.Targets, observation)
	}
	return result, sufficient, nil
}

func placementWorkerEligible(
	worker WorkerRecord,
	workspaceID string,
	now time.Time,
	credentials map[string]WorkerCredential,
	runStates map[string]WorkerGroupRunState,
) bool {
	status, err := NormalizeWorkerStatus(worker.Status)
	if err != nil || status != WorkerStatusActive || !worker.Live(now) {
		return false
	}
	if strings.TrimSpace(worker.CredentialID) == "" {
		// A registered static Worker is bound to this same registry selector at
		// claim time. Unregistered compatibility claims have no capacity record.
		return true
	}
	credential, ok := credentials[worker.CredentialID]
	if !ok || credential.Generation != worker.CredentialGeneration || credential.Group != worker.Group ||
		!credential.AllowsNewWork(now) || !WorkspaceAllowed(workspaceID, credential.WorkspaceIDs) {
		return false
	}
	runState, ok := runStates[worker.Group]
	if !ok {
		runState = DefaultWorkerGroupRunState(worker.Group)
	}
	return !runState.Draining()
}

func placementPolicyAuditRecord(deployment contract.Deployment, actionKey string, previous catalog.RoutingPolicy, updated catalog.RoutingPolicy, actor string, now time.Time) catalog.AuditRecord {
	scope := "app"
	if actionKey != "" {
		scope = "action"
	}
	return catalog.PrepareAuditRecord(catalog.AuditRecord{
		Workspace: deployment.SourceWorkspace(), GitSourceID: deployment.SourceGitSourceID(), App: deployment.App,
		Kind: "execution_placement_updated", Detail: catalog.RoutingPolicyMutationDetail(scope, actionKey, previous, updated), Actor: strings.TrimSpace(actor),
	}, now)
}
