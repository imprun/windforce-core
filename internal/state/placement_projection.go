package state

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
)

const (
	PlacementReasonWorkspaceNotAllowed      = "workspace_not_allowed"
	PlacementReasonDraining                 = "draining"
	PlacementReasonNoLiveCapacity           = "no_live_capacity"
	PlacementReasonMissingTag               = "missing_tag"
	PlacementReasonMissingLabel             = "missing_label"
	PlacementReasonExecutionProfileMismatch = "execution_profile_mismatch"

	unmanagedWorkerGroup = "unmanaged"
)

// PlacementObservationStore is an optional read-only capability. Implementations
// must calculate each response from one state snapshot.
type PlacementObservationStore interface {
	GetWorkerGroupInventory(context.Context, string, bool) (WorkerGroupInventory, error)
	GetPlacementCandidates(context.Context, string, string, string, bool) (PlacementCandidates, error)
}

type WorkerGroupInventory struct {
	Workspace  string                     `json:"workspace"`
	ObservedAt time.Time                  `json:"observed_at"`
	Groups     []WorkerGroupInventoryItem `json:"groups"`
}

type WorkerGroupInventoryItem struct {
	Group                string                      `json:"group"`
	Status               string                      `json:"status"`
	WorkspaceAllowed     bool                        `json:"workspace_allowed"`
	Managed              bool                        `json:"managed"`
	ActiveCredentials    int                         `json:"active_credentials"`
	RunState             string                      `json:"run_state"`
	RunStateRevision     int64                       `json:"run_state_revision"`
	DeadlineAt           *time.Time                  `json:"deadline_at,omitempty"`
	LiveWorkers          int                         `json:"live_workers"`
	UnmanagedLiveWorkers int                         `json:"unmanaged_live_workers"`
	AvailableSlots       int                         `json:"available_slots"`
	ActiveLeases         int                         `json:"active_leases"`
	RunningJobs          int                         `json:"running_jobs"`
	Quiescent            bool                        `json:"quiescent"`
	Tags                 []string                    `json:"tags"`
	Labels               []string                    `json:"labels"`
	ExecutionProfiles    []contract.ExecutionProfile `json:"execution_profiles"`
	EngineVersions       []string                    `json:"engine_versions"`
	BuildRevisions       []string                    `json:"build_revisions"`
	VersionOrBuildDrift  bool                        `json:"version_or_build_drift"`
	LastHeartbeatAt      *time.Time                  `json:"last_heartbeat_at,omitempty"`
}

type PlacementCandidates struct {
	Workspace  string                      `json:"workspace"`
	ObservedAt time.Time                   `json:"observed_at"`
	Targets    []PlacementTargetCandidates `json:"targets"`
}

type PlacementTargetCandidates struct {
	App                     string                          `json:"app"`
	Action                  string                          `json:"action,omitempty"`
	EffectiveTag            string                          `json:"effective_tag"`
	EffectiveRequiredLabels []string                        `json:"effective_required_labels"`
	ExecutionProfile        contract.ExecutionProfile       `json:"execution_profile"`
	MatchingWorkers         int64                           `json:"matching_workers"`
	MatchingSlots           int64                           `json:"matching_slots"`
	Candidates              []WorkerGroupPlacementCandidate `json:"candidates"`
}

type WorkerGroupPlacementCandidate struct {
	Group               string   `json:"group"`
	WorkspaceAllowed    bool     `json:"workspace_allowed"`
	Managed             bool     `json:"managed"`
	RunState            string   `json:"run_state"`
	Eligible            bool     `json:"eligible"`
	MatchingWorkers     int64    `json:"matching_workers"`
	MatchingSlots       int64    `json:"matching_slots"`
	ReasonCodes         []string `json:"reason_codes"`
	VersionOrBuildDrift bool     `json:"version_or_build_drift"`
}

type placementTarget struct {
	app     string
	action  string
	tag     string
	labels  []string
	profile contract.ExecutionProfile
}

func placementTargets(deployment contract.Deployment, actionKey string) ([]placementTarget, error) {
	targets := []placementTarget{{
		app: deployment.App, tag: contract.EffectiveRouteTagForApp(deployment),
		labels: contract.EffectiveRequiredLabels(deployment, contract.Action{}), profile: deployment.ExecutionProfile,
	}}
	if actionKey != "" {
		action, ok := deployment.Actions[actionKey]
		if !ok {
			return nil, catalog.ErrActionNotFound
		}
		return []placementTarget{{
			app: deployment.App, action: actionKey,
			tag:    contract.EffectiveRouteTagForAction(deployment, action),
			labels: contract.EffectiveRequiredLabels(deployment, action), profile: deployment.ExecutionProfile,
		}}, nil
	}
	actionKeys := make([]string, 0, len(deployment.Actions))
	for key := range deployment.Actions {
		actionKeys = append(actionKeys, key)
	}
	sort.Strings(actionKeys)
	for _, key := range actionKeys {
		action := deployment.Actions[key]
		targets = append(targets, placementTarget{
			app: deployment.App, action: key,
			tag:    contract.EffectiveRouteTagForAction(deployment, action),
			labels: contract.EffectiveRequiredLabels(deployment, action), profile: deployment.ExecutionProfile,
		})
	}
	return targets, nil
}

func buildWorkerGroupInventory(
	workspaceID string,
	includeUnauthorized bool,
	observedAt time.Time,
	workers []WorkerRecord,
	credentials map[string]WorkerCredential,
	runStates map[string]WorkerGroupRunState,
	jobs []Job,
) (WorkerGroupInventory, error) {
	observedAt = observedAt.UTC()
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	groups := placementGroups(workspaceID, includeUnauthorized, observedAt, workers, credentials, runStates)
	items := make([]WorkerGroupInventoryItem, 0, len(groups))
	for _, group := range groups {
		item, err := buildWorkerGroupInventoryItem(
			group, workspaceID, includeUnauthorized, observedAt, workers, credentials, runStates, jobs,
		)
		if err != nil {
			return WorkerGroupInventory{}, err
		}
		if includeUnauthorized || item.WorkspaceAllowed {
			items = append(items, item)
		}
	}
	return WorkerGroupInventory{Workspace: workspaceID, ObservedAt: observedAt, Groups: items}, nil
}

func buildWorkerGroupInventoryItem(
	group string,
	workspaceID string,
	includeUnauthorized bool,
	observedAt time.Time,
	workers []WorkerRecord,
	credentials map[string]WorkerCredential,
	runStates map[string]WorkerGroupRunState,
	jobs []Job,
) (WorkerGroupInventoryItem, error) {
	runState := DefaultWorkerGroupRunState(group)
	if current, ok := runStates[group]; ok {
		runState = current
	}
	observationGroup := group
	if group == unmanagedWorkerGroup {
		// Legacy/static Workers have no persisted group. Keep the public
		// projection named, while preserving the claim-time unattributed lease
		// accounting used for that compatibility pool.
		observationGroup = ""
	}
	observation := buildWorkerGroupObservation(observationGroup, runState, observedAt, workers, jobs)
	item := WorkerGroupInventoryItem{
		Group: group, RunState: runState.State, RunStateRevision: runState.Revision, DeadlineAt: runState.DeadlineAt,
		ActiveLeases: observation.ActiveLeases, RunningJobs: observation.RunningJobs, Quiescent: observation.Quiescent,
		Tags: []string{}, Labels: []string{}, ExecutionProfiles: []contract.ExecutionProfile{},
		EngineVersions: []string{}, BuildRevisions: []string{},
	}

	for _, credential := range credentials {
		if credential.Group != group || !credential.AllowsNewWork(observedAt) {
			continue
		}
		item.Managed = true
		if WorkspaceAllowed(workspaceID, credential.WorkspaceIDs) {
			item.WorkspaceAllowed = true
		}
		if includeUnauthorized || WorkspaceAllowed(workspaceID, credential.WorkspaceIDs) {
			item.ActiveCredentials++
		}
	}

	tagSet := map[string]struct{}{}
	labelSet := map[string]struct{}{}
	profileSet := map[string]contract.ExecutionProfile{}
	versionSet := map[string]struct{}{}
	revisionSet := map[string]struct{}{}
	availableSlots := 0
	for _, worker := range workers {
		if placementWorkerGroup(worker) != group || !placementWorkerGenerallyActive(worker, observedAt, credentials) {
			continue
		}
		if strings.TrimSpace(worker.CredentialID) != "" {
			item.Managed = true
		}
		workspaceEligible := placementWorkerWorkspaceActive(worker, workspaceID, observedAt, credentials)
		if workspaceEligible {
			item.WorkspaceAllowed = true
		}
		if !includeUnauthorized && !workspaceEligible {
			continue
		}
		item.LiveWorkers++
		if strings.TrimSpace(worker.CredentialID) == "" {
			item.UnmanagedLiveWorkers++
		}
		if placementWorkerEligible(worker, workspaceID, observedAt, credentials, runStates) {
			availableSlots += normalizedWorkerSlots(worker)
		}
		if item.LastHeartbeatAt == nil || worker.LastHeartbeatAt.After(*item.LastHeartbeatAt) {
			heartbeat := worker.LastHeartbeatAt.UTC()
			item.LastHeartbeatAt = &heartbeat
		}
		for _, tag := range NormalizeWorkerScope(worker.Tags) {
			tagSet[tag] = struct{}{}
		}
		claimTags, claimLabels, err := WorkerClaimSelector(worker)
		if err != nil {
			return WorkerGroupInventoryItem{}, err
		}
		for _, tag := range claimTags {
			tagSet[tag] = struct{}{}
		}
		for _, label := range claimLabels {
			labelSet[label] = struct{}{}
		}
		for _, profile := range worker.ExecutionProfiles {
			profileSet[profile.Key] = profile
		}
		if value := strings.TrimSpace(worker.EngineVersion); value != "" {
			versionSet[value] = struct{}{}
		}
		if value := strings.TrimSpace(worker.BuildRevision); value != "" {
			revisionSet[value] = struct{}{}
		}
	}
	if item.ActiveLeases >= availableSlots {
		item.AvailableSlots = 0
	} else {
		item.AvailableSlots = availableSlots - item.ActiveLeases
	}
	item.Tags = sortedSet(tagSet)
	item.Labels = sortedSet(labelSet)
	item.EngineVersions = sortedSet(versionSet)
	item.BuildRevisions = sortedSet(revisionSet)
	item.VersionOrBuildDrift = len(item.EngineVersions) > 1 || len(item.BuildRevisions) > 1
	profileKeys := make([]string, 0, len(profileSet))
	for key := range profileSet {
		profileKeys = append(profileKeys, key)
	}
	sort.Strings(profileKeys)
	for _, key := range profileKeys {
		item.ExecutionProfiles = append(item.ExecutionProfiles, profileSet[key])
	}
	switch {
	case runState.Draining():
		item.Status = WorkerGroupDraining
	case item.LiveWorkers == 0:
		item.Status = "offline"
	case item.VersionOrBuildDrift:
		item.Status = "degraded"
	default:
		item.Status = "ready"
	}
	return item, nil
}

func buildPlacementCandidates(
	workspaceID string,
	actionKey string,
	includeUnauthorized bool,
	observedAt time.Time,
	deployment contract.Deployment,
	workers []WorkerRecord,
	credentials map[string]WorkerCredential,
	runStates map[string]WorkerGroupRunState,
	jobs []Job,
) (PlacementCandidates, error) {
	inventory, err := buildWorkerGroupInventory(
		workspaceID, includeUnauthorized, observedAt, workers, credentials, runStates, jobs,
	)
	if err != nil {
		return PlacementCandidates{}, err
	}
	targets, err := placementTargets(deployment, actionKey)
	if err != nil {
		return PlacementCandidates{}, err
	}
	result := PlacementCandidates{
		Workspace: inventory.Workspace, ObservedAt: inventory.ObservedAt,
		Targets: make([]PlacementTargetCandidates, 0, len(targets)),
	}
	for _, target := range targets {
		view := PlacementTargetCandidates{
			App: target.app, Action: target.action, EffectiveTag: target.tag,
			EffectiveRequiredLabels: append([]string{}, target.labels...), ExecutionProfile: target.profile,
			Candidates: make([]WorkerGroupPlacementCandidate, 0, len(inventory.Groups)),
		}
		for _, group := range inventory.Groups {
			candidate, err := buildWorkerGroupPlacementCandidate(
				target, group, inventory.Workspace, inventory.ObservedAt, workers, credentials, runStates,
			)
			if err != nil {
				return PlacementCandidates{}, err
			}
			view.Candidates = append(view.Candidates, candidate)
			view.MatchingWorkers += candidate.MatchingWorkers
			if view.MatchingSlots > math.MaxInt64-candidate.MatchingSlots {
				view.MatchingSlots = math.MaxInt64
			} else {
				view.MatchingSlots += candidate.MatchingSlots
			}
		}
		result.Targets = append(result.Targets, view)
	}
	return result, nil
}

func buildWorkerGroupPlacementCandidate(
	target placementTarget,
	group WorkerGroupInventoryItem,
	workspaceID string,
	observedAt time.Time,
	workers []WorkerRecord,
	credentials map[string]WorkerCredential,
	runStates map[string]WorkerGroupRunState,
) (WorkerGroupPlacementCandidate, error) {
	result := WorkerGroupPlacementCandidate{
		Group: group.Group, WorkspaceAllowed: group.WorkspaceAllowed, Managed: group.Managed,
		RunState: group.RunState, ReasonCodes: []string{}, VersionOrBuildDrift: group.VersionOrBuildDrift,
	}
	if !group.WorkspaceAllowed {
		result.ReasonCodes = append(result.ReasonCodes, PlacementReasonWorkspaceNotAllowed)
		return result, nil
	}
	profileLabel := ""
	if strings.TrimSpace(target.profile.Key) != "" {
		label, err := contract.ExecutionProfileLabel(target.profile)
		if err != nil {
			return WorkerGroupPlacementCandidate{}, err
		}
		profileLabel = label
	}
	baseWorkers := 0
	runStateWorkers := 0
	drainingWorkers := 0
	tagMatches := 0
	profileMatches := 0
	for _, worker := range workers {
		if placementWorkerGroup(worker) != group.Group ||
			!placementWorkerWorkspaceActive(worker, workspaceID, observedAt, credentials) {
			continue
		}
		baseWorkers++
		if !placementWorkerEligible(worker, workspaceID, observedAt, credentials, runStates) {
			drainingWorkers++
			continue
		}
		runStateWorkers++
		tags, labels, err := WorkerClaimSelector(worker)
		if err != nil {
			return WorkerGroupPlacementCandidate{}, err
		}
		tagSet := normalizeClaimTags(tags)
		labelSet := normalizeClaimTags(labels)
		if len(tagSet) > 0 {
			if _, ok := tagSet[target.tag]; !ok {
				continue
			}
		}
		tagMatches++
		if profileLabel != "" {
			if _, ok := labelSet[profileLabel]; !ok {
				continue
			}
		}
		profileMatches++
		if !selectorAllowed(target.tag, target.labels, tagSet, labelSet) {
			continue
		}
		result.MatchingWorkers++
		slots := int64(normalizedWorkerSlots(worker))
		if result.MatchingSlots > math.MaxInt64-slots {
			result.MatchingSlots = math.MaxInt64
		} else {
			result.MatchingSlots += slots
		}
	}
	result.Eligible = result.MatchingWorkers > 0
	if result.Eligible {
		return result, nil
	}
	if baseWorkers == 0 {
		result.ReasonCodes = append(result.ReasonCodes, PlacementReasonNoLiveCapacity)
		return result, nil
	}
	if drainingWorkers > 0 {
		result.ReasonCodes = appendReasonCode(result.ReasonCodes, PlacementReasonDraining)
	}
	if runStateWorkers == 0 {
		return result, nil
	}
	if tagMatches == 0 {
		result.ReasonCodes = appendReasonCode(result.ReasonCodes, PlacementReasonMissingTag)
		return result, nil
	}
	if profileLabel != "" && profileMatches == 0 {
		result.ReasonCodes = appendReasonCode(result.ReasonCodes, PlacementReasonExecutionProfileMismatch)
		return result, nil
	}
	result.ReasonCodes = appendReasonCode(result.ReasonCodes, PlacementReasonMissingLabel)
	return result, nil
}

func placementGroups(
	workspaceID string,
	includeUnauthorized bool,
	now time.Time,
	workers []WorkerRecord,
	credentials map[string]WorkerCredential,
	runStates map[string]WorkerGroupRunState,
) []string {
	groups := map[string]struct{}{}
	for _, credential := range credentials {
		if !credential.AllowsNewWork(now) ||
			(!includeUnauthorized && !WorkspaceAllowed(workspaceID, credential.WorkspaceIDs)) {
			continue
		}
		groups[credential.Group] = struct{}{}
	}
	for _, worker := range workers {
		if !placementWorkerGenerallyActive(worker, now, credentials) {
			continue
		}
		if includeUnauthorized || placementWorkerWorkspaceActive(worker, workspaceID, now, credentials) {
			groups[placementWorkerGroup(worker)] = struct{}{}
		}
	}
	if includeUnauthorized {
		for group := range runStates {
			groups[group] = struct{}{}
		}
	}
	return sortedSet(groups)
}

func placementWorkerGenerallyActive(worker WorkerRecord, now time.Time, credentials map[string]WorkerCredential) bool {
	status, err := NormalizeWorkerStatus(worker.Status)
	if err != nil || status != WorkerStatusActive || !worker.Live(now) {
		return false
	}
	if strings.TrimSpace(worker.CredentialID) == "" {
		return true
	}
	credential, ok := credentials[worker.CredentialID]
	return ok && credential.Generation == worker.CredentialGeneration && credential.Group == worker.Group && credential.AllowsNewWork(now)
}

func placementWorkerWorkspaceActive(
	worker WorkerRecord,
	workspaceID string,
	now time.Time,
	credentials map[string]WorkerCredential,
) bool {
	if !placementWorkerGenerallyActive(worker, now, credentials) {
		return false
	}
	if strings.TrimSpace(worker.CredentialID) == "" {
		return true
	}
	return WorkspaceAllowed(workspaceID, credentials[worker.CredentialID].WorkspaceIDs)
}

func placementWorkerGroup(worker WorkerRecord) string {
	if group := strings.TrimSpace(worker.Group); group != "" {
		return group
	}
	return unmanagedWorkerGroup
}

func normalizedWorkerSlots(worker WorkerRecord) int {
	if worker.Slots < 1 {
		return 1
	}
	return worker.Slots
}

func sortedSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func appendReasonCode(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}
