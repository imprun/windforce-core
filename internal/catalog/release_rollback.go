package catalog

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

var (
	ErrReleaseNotFound      = errors.New("release not found")
	ErrReleaseAlreadyActive = errors.New("release is already active")
	ErrInvalidRollback      = errors.New("invalid release rollback")
)

type ReleaseRollbackRequest struct {
	Workspace    string
	App          string
	ReleaseID    string
	Actor        string
	Reason       string
	RolledBackAt time.Time
}

type ReleaseRollbackResult struct {
	Target            DeploymentHistory
	PreviousReleaseID string
	PreviousCommit    string
	Reason            string
	RolledBackAt      time.Time
	Audit             AuditRecord
}

func FindRelease(snapshot Snapshot, workspace string, app string, releaseID string) (DeploymentHistory, error) {
	workspace = contract.NormalizeWorkspace(workspace)
	app = strings.TrimSpace(app)
	releaseID = strings.TrimSpace(releaseID)
	for _, history := range snapshot.History {
		if contract.NormalizeWorkspace(history.Workspace) == workspace && history.App == app && history.ID == releaseID {
			return history, nil
		}
	}
	return DeploymentHistory{}, ErrReleaseNotFound
}

func PrepareReleaseRollbackRequest(request ReleaseRollbackRequest) (ReleaseRollbackRequest, error) {
	request.Workspace = contract.NormalizeWorkspace(request.Workspace)
	request.App = strings.TrimSpace(request.App)
	request.ReleaseID = strings.TrimSpace(request.ReleaseID)
	request.Actor = strings.TrimSpace(request.Actor)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.App == "" || request.ReleaseID == "" || request.Actor == "" || request.Reason == "" {
		return ReleaseRollbackRequest{}, ErrInvalidRollback
	}
	if request.RolledBackAt.IsZero() {
		request.RolledBackAt = time.Now().UTC()
	} else {
		request.RolledBackAt = request.RolledBackAt.UTC()
	}
	return request, nil
}

func NewReleaseRollbackResult(target DeploymentHistory, previousReleaseID string, previousCommit string, request ReleaseRollbackRequest) ReleaseRollbackResult {
	audit := PrepareAuditRecord(AuditRecord{
		Workspace:   request.Workspace,
		GitSourceID: ReleaseGitSourceID(target),
		App:         request.App,
		Kind:        "release_rolled_back",
		Detail:      fmt.Sprintf("active release %s -> %s; reason: %s", emptyAsNone(previousReleaseID), target.ID, request.Reason),
		Actor:       request.Actor,
		CreatedAt:   request.RolledBackAt,
	}, request.RolledBackAt)
	return ReleaseRollbackResult{
		Target:            target,
		PreviousReleaseID: previousReleaseID,
		PreviousCommit:    previousCommit,
		Reason:            request.Reason,
		RolledBackAt:      request.RolledBackAt,
		Audit:             audit,
	}
}

func ReleaseGitSourceID(history DeploymentHistory) string {
	return firstNonEmpty(history.GitSourceID, history.Deployment.SourceGitSourceID())
}

// ApplyReleaseRollback switches only the active release pointer and its
// deployment snapshot. Synchronized release candidates and immutable release
// history remain unchanged.
func ApplyReleaseRollback(snapshot *Snapshot, request ReleaseRollbackRequest) (ReleaseRollbackResult, error) {
	NormalizeSnapshot(snapshot)
	var err error
	request, err = PrepareReleaseRollbackRequest(request)
	if err != nil {
		return ReleaseRollbackResult{}, err
	}

	target, err := FindRelease(*snapshot, request.Workspace, request.App, request.ReleaseID)
	if err != nil {
		return ReleaseRollbackResult{}, err
	}
	key := DeploymentKey(request.Workspace, request.App)
	previousID := snapshot.ActiveHistoryIDs[key]
	if previousID == target.ID {
		return ReleaseRollbackResult{}, ErrReleaseAlreadyActive
	}
	previousCommit := ""
	if previous, ok := snapshot.Deployments[key]; ok {
		previousCommit = previous.Commit
	}

	snapshot.Deployments[key] = target.Deployment
	snapshot.ActiveHistoryIDs[key] = target.ID
	marker := SourceReleaseMarker{
		Workspace:   request.Workspace,
		GitSourceID: ReleaseGitSourceID(target),
		Commit:      target.Commit,
		ReleasedAt:  request.RolledBackAt,
	}
	snapshot.SourceMarkers[SourceReleaseKey(marker.Workspace, marker.GitSourceID)] = marker
	result := NewReleaseRollbackResult(target, previousID, previousCommit, request)
	snapshot.Audit = append(snapshot.Audit, result.Audit)

	return result, nil
}

func backfillActiveHistoryIDs(snapshot *Snapshot) {
	for key, deployment := range snapshot.Deployments {
		if strings.TrimSpace(snapshot.ActiveHistoryIDs[key]) != "" {
			continue
		}
		for index := len(snapshot.History) - 1; index >= 0; index-- {
			history := snapshot.History[index]
			if DeploymentKey(history.Workspace, history.App) != key {
				continue
			}
			if MatchesPublishedDeployment(deployment, history) {
				snapshot.ActiveHistoryIDs[key] = history.ID
				break
			}
		}
	}
}

func MatchesPublishedDeployment(deployment contract.Deployment, history DeploymentHistory) bool {
	if deployment.DeploymentID != nil && history.DeploymentID != nil {
		return strings.TrimSpace(*deployment.DeploymentID) != "" && *deployment.DeploymentID == *history.DeploymentID
	}
	if deployment.Commit != history.Commit {
		return false
	}
	if deployment.BundleDigest != "" || history.Deployment.BundleDigest != "" {
		return deployment.BundleDigest == history.Deployment.BundleDigest
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func emptyAsNone(value string) string {
	if strings.TrimSpace(value) == "" {
		return "none"
	}
	return strings.TrimSpace(value)
}
