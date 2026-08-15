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
	GetExecutionDemand(context.Context, string, string, string, bool) (ExecutionDemand, error)
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
	TotalSlots           int                         `json:"total_slots"`
	OccupiedSlots        int                         `json:"occupied_slots"`
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
	OccupiedSlots       int64    `json:"occupied_slots"`
	AvailableSlots      int64    `json:"available_slots"`
	Saturated           bool     `json:"saturated"`
	ReasonCodes         []string `json:"reason_codes"`
	VersionOrBuildDrift bool     `json:"version_or_build_drift"`
}

type ExecutionDemand struct {
	Workspace      string                  `json:"workspace"`
	ObservedAt     time.Time               `json:"observed_at"`
	QueuedJobs     int64                   `json:"queued_jobs"`
	OldestQueuedAt *time.Time              `json:"oldest_queued_at,omitempty"`
	Targets        []ExecutionDemandTarget `json:"targets"`
}

type ExecutionDemandTarget struct {
	App                     string                          `json:"app"`
	Action                  string                          `json:"action"`
	EffectiveTag            string                          `json:"effective_tag"`
	EffectiveRequiredLabels []string                        `json:"effective_required_labels"`
	ExecutionProfile        contract.ExecutionProfile       `json:"execution_profile"`
	QueuedJobs              int64                           `json:"queued_jobs"`
	OldestQueuedAt          *time.Time                      `json:"oldest_queued_at,omitempty"`
	MatchingWorkers         int64                           `json:"matching_workers"`
	TotalSlots              int64                           `json:"total_slots"`
	OccupiedSlots           int64                           `json:"occupied_slots"`
	AvailableSlots          int64                           `json:"available_slots"`
	Saturated               bool                            `json:"saturated"`
	Candidates              []WorkerGroupPlacementCandidate `json:"candidates"`
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
	activeLeasesByWorker := activeLeaseCountsByWorker(observedAt, jobs)
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
			slots := normalizedWorkerSlots(worker)
			item.TotalSlots = saturatingAddInt(item.TotalSlots, slots)
			occupied := activeLeasesByWorker[worker.ID]
			if occupied > int64(slots) {
				occupied = int64(slots)
			}
			item.OccupiedSlots = saturatingAddInt(item.OccupiedSlots, int(occupied))
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
	if item.OccupiedSlots >= item.TotalSlots {
		item.AvailableSlots = 0
	} else {
		item.AvailableSlots = item.TotalSlots - item.OccupiedSlots
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
	activeLeases := activeLeaseCountsByWorker(inventory.ObservedAt, jobs)
	for _, target := range targets {
		view := PlacementTargetCandidates{
			App: target.app, Action: target.action, EffectiveTag: target.tag,
			EffectiveRequiredLabels: append([]string{}, target.labels...), ExecutionProfile: target.profile,
			Candidates: make([]WorkerGroupPlacementCandidate, 0, len(inventory.Groups)),
		}
		for _, group := range inventory.Groups {
			candidate, err := buildWorkerGroupPlacementCandidate(
				target, group, inventory.Workspace, inventory.ObservedAt, workers, credentials, runStates, activeLeases,
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
	activeLeases map[string]int64,
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
		occupied := activeLeases[worker.ID]
		if occupied > slots {
			occupied = slots
		}
		if result.OccupiedSlots > math.MaxInt64-occupied {
			result.OccupiedSlots = math.MaxInt64
		} else {
			result.OccupiedSlots += occupied
		}
	}
	if result.OccupiedSlots >= result.MatchingSlots {
		result.AvailableSlots = 0
	} else {
		result.AvailableSlots = result.MatchingSlots - result.OccupiedSlots
	}
	result.Saturated = result.MatchingSlots > 0 && result.AvailableSlots == 0
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

type executionDemandKey struct {
	app     string
	action  string
	tag     string
	labels  string
	profile contract.ExecutionProfile
}

func buildExecutionDemand(
	workspaceID string,
	appFilter string,
	actionFilter string,
	includeUnauthorized bool,
	observedAt time.Time,
	workers []WorkerRecord,
	credentials map[string]WorkerCredential,
	runStates map[string]WorkerGroupRunState,
	jobs []Job,
) (ExecutionDemand, error) {
	observedAt = observedAt.UTC()
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	result := ExecutionDemand{
		Workspace: workspaceID, ObservedAt: observedAt, Targets: []ExecutionDemandTarget{},
	}
	inventory, err := buildWorkerGroupInventory(
		workspaceID, includeUnauthorized, observedAt, workers, credentials, runStates, jobs,
	)
	if err != nil {
		return ExecutionDemand{}, err
	}
	targets := map[executionDemandKey]*ExecutionDemandTarget{}
	for _, job := range jobs {
		if job.State != JobQueued || normalizedJobWorkspace("", job) != workspaceID {
			continue
		}
		app := jobAppKey(job)
		action := strings.TrimSpace(job.Payload.Action)
		if appFilter != "" && app != appFilter || actionFilter != "" && action != actionFilter {
			continue
		}
		labels := sortedSet(normalizeClaimTags(jobRequiredLabels(job)))
		profile := jobExecutionProfile(job)
		key := executionDemandKey{
			app: app, action: action, tag: jobTag(job), labels: strings.Join(labels, "\x1f"), profile: profile,
		}
		target := targets[key]
		if target == nil {
			target = &ExecutionDemandTarget{
				App: app, Action: action, EffectiveTag: key.tag,
				EffectiveRequiredLabels: labels, ExecutionProfile: profile,
				Candidates: []WorkerGroupPlacementCandidate{},
			}
			targets[key] = target
		}
		target.QueuedJobs++
		result.QueuedJobs++
		queuedAt := job.UpdatedAt.UTC()
		if queuedAt.IsZero() {
			queuedAt = job.CreatedAt.UTC()
		}
		setOldestTime(&target.OldestQueuedAt, queuedAt)
		setOldestTime(&result.OldestQueuedAt, queuedAt)
	}
	keys := make([]executionDemandKey, 0, len(targets))
	for key := range targets {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := keys[i], keys[j]
		if left.app != right.app {
			return left.app < right.app
		}
		if left.action != right.action {
			return left.action < right.action
		}
		if left.tag != right.tag {
			return left.tag < right.tag
		}
		if left.labels != right.labels {
			return left.labels < right.labels
		}
		return executionProfileSortKey(left.profile) < executionProfileSortKey(right.profile)
	})
	activeLeases := activeLeaseCountsByWorker(observedAt, jobs)
	for _, key := range keys {
		target := targets[key]
		candidateTarget := placementTarget{
			app: target.App, action: target.Action, tag: target.EffectiveTag,
			labels: target.EffectiveRequiredLabels, profile: target.ExecutionProfile,
		}
		for _, group := range inventory.Groups {
			candidate, err := buildWorkerGroupPlacementCandidate(
				candidateTarget, group, workspaceID, observedAt, workers, credentials, runStates, activeLeases,
			)
			if err != nil {
				return ExecutionDemand{}, err
			}
			target.Candidates = append(target.Candidates, candidate)
			target.MatchingWorkers += candidate.MatchingWorkers
			target.TotalSlots = saturatingAddInt64(target.TotalSlots, candidate.MatchingSlots)
			target.OccupiedSlots = saturatingAddInt64(target.OccupiedSlots, candidate.OccupiedSlots)
			target.AvailableSlots = saturatingAddInt64(target.AvailableSlots, candidate.AvailableSlots)
		}
		target.Saturated = target.TotalSlots > 0 && target.AvailableSlots == 0
		result.Targets = append(result.Targets, *target)
	}
	return result, nil
}

func jobExecutionProfile(job Job) contract.ExecutionProfile {
	if strings.TrimSpace(job.Payload.ExecutionProfile.Key) != "" {
		return job.Payload.ExecutionProfile
	}
	return job.Payload.PinnedDeployment().ExecutionProfile
}

func activeLeaseCountsByWorker(observedAt time.Time, jobs []Job) map[string]int64 {
	counts := map[string]int64{}
	for _, job := range jobs {
		workerID := strings.TrimSpace(job.LeaseOwner)
		if workerID == "" || !activeQueueLease(job, observedAt) {
			continue
		}
		counts[workerID] = saturatingAddInt64(counts[workerID], 1)
	}
	return counts
}

func setOldestTime(current **time.Time, candidate time.Time) {
	if candidate.IsZero() || (*current != nil && !candidate.Before(**current)) {
		return
	}
	value := candidate.UTC()
	*current = &value
}

func executionProfileSortKey(profile contract.ExecutionProfile) string {
	return strings.Join([]string{
		profile.Key, profile.Version, profile.ID, profile.OS, profile.Arch,
		profile.Runtime, profile.RuntimeABI, profile.Libc,
	}, "\x1f")
}

func saturatingAddInt64(current int64, addition int64) int64 {
	if addition > 0 && current > math.MaxInt64-addition {
		return math.MaxInt64
	}
	return current + addition
}

func saturatingAddInt(current int, addition int) int {
	if addition > 0 && current > math.MaxInt-addition {
		return math.MaxInt
	}
	return current + addition
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
