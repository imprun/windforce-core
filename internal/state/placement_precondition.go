package state

import (
	"errors"
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
	projection, err := buildPlacementCandidates(
		deployment.SourceWorkspace(), actionKey, true, checkedAt, deployment, workers, credentials, runStates, nil,
	)
	if err != nil {
		return catalog.RoutingPolicyOperationResult{}, false, err
	}
	result := catalog.RoutingPolicyOperationResult{
		CheckedAt: checkedAt.UTC(), MinimumMatchingSlots: minimumMatchingSlots,
		Targets: make([]catalog.RoutingPolicyTargetObservation, 0, len(projection.Targets)),
	}
	sufficient := true
	for _, candidate := range projection.Targets {
		observation := catalog.RoutingPolicyTargetObservation{
			App: candidate.App, Action: candidate.Action, EffectiveTag: candidate.EffectiveTag,
			EffectiveRequiredLabels: append([]string{}, candidate.EffectiveRequiredLabels...),
			ExecutionProfile:        candidate.ExecutionProfile,
			MatchingWorkers:         candidate.MatchingWorkers,
			MatchingSlots:           candidate.MatchingSlots,
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
	if !placementWorkerWorkspaceActive(worker, workspaceID, now, credentials) {
		return false
	}
	if strings.TrimSpace(worker.CredentialID) == "" {
		// A registered static Worker is bound to this same registry selector at
		// claim time. Unregistered compatibility claims have no capacity record.
		return true
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
