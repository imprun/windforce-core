package execution

import (
	"fmt"
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
)

// ActiveReleasePrecondition fences an Admission request to the immutable
// execution identity resolved by a trusted caller. DeploymentID is optional;
// Commit and BundleDigest are always required.
type ActiveReleasePrecondition struct {
	DeploymentID string
	Commit       string
	BundleDigest string
}

func (p ActiveReleasePrecondition) normalized() (ActiveReleasePrecondition, error) {
	p.DeploymentID = strings.TrimSpace(p.DeploymentID)
	p.Commit = strings.TrimSpace(p.Commit)
	p.BundleDigest = strings.TrimSpace(p.BundleDigest)
	if p.Commit == "" || p.BundleDigest == "" {
		return ActiveReleasePrecondition{}, fmt.Errorf("expected release commit and bundle digest are required")
	}
	return p, nil
}

func (p ActiveReleasePrecondition) matches(deployment contract.Deployment) bool {
	if strings.TrimSpace(deployment.Commit) != p.Commit || strings.TrimSpace(deployment.BundleDigest) != p.BundleDigest {
		return false
	}
	if p.DeploymentID == "" {
		return true
	}
	return deployment.DeploymentID != nil && strings.TrimSpace(*deployment.DeploymentID) == p.DeploymentID
}
