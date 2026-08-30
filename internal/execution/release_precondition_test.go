package execution

import (
	"testing"

	"github.com/imprun/windforce-core/internal/contract"
)

func TestActiveReleasePreconditionMatchesEverySuppliedPin(t *testing.T) {
	t.Parallel()

	deploymentID := "deployment-v1"
	deployment := contract.Deployment{
		DeploymentID: &deploymentID,
		Commit:       "commit-v1",
		BundleDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	exact, err := (ActiveReleasePrecondition{
		DeploymentID: " deployment-v1 ",
		Commit:       " commit-v1 ",
		BundleDigest: " sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ",
	}).normalized()
	if err != nil {
		t.Fatalf("normalize precondition: %v", err)
	}
	if !exact.matches(deployment) {
		t.Fatal("exact Release precondition did not match")
	}

	for _, mutate := range []func(*ActiveReleasePrecondition){
		func(pin *ActiveReleasePrecondition) { pin.DeploymentID = "deployment-v2" },
		func(pin *ActiveReleasePrecondition) { pin.Commit = "commit-v2" },
		func(pin *ActiveReleasePrecondition) {
			pin.BundleDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		},
	} {
		pin := exact
		mutate(&pin)
		if pin.matches(deployment) {
			t.Fatalf("mismatched Release precondition unexpectedly matched: %+v", pin)
		}
	}
}

func TestActiveReleasePreconditionRequiresCommitAndBundleDigest(t *testing.T) {
	t.Parallel()

	for _, pin := range []ActiveReleasePrecondition{
		{},
		{Commit: "commit-v1"},
		{BundleDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	} {
		if _, err := pin.normalized(); err == nil {
			t.Fatalf("incomplete Release precondition unexpectedly accepted: %+v", pin)
		}
	}
}
