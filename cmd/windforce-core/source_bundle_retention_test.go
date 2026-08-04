package main

import (
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
)

func TestSourceBundleReferencesCoverReleaseState(t *testing.T) {
	deployment := func(workspace string, sourceID string, commit string) contract.Deployment {
		return contract.Deployment{
			App:         "app-" + commit,
			Commit:      commit,
			GitSourceID: sourceID,
			Workspace:   workspace,
		}
	}
	snapshot := catalog.NewSnapshot()
	snapshot.Deployments["ws-a/active"] = deployment("ws-a", "source-a", "active")
	snapshot.History = append(snapshot.History, catalog.DeploymentHistory{
		Workspace:   "ws-a",
		GitSourceID: "source-a",
		Commit:      "history",
	})
	snapshot.Candidates["ws-a/source-a"] = catalog.ReleaseCandidate{
		Deployment: deployment("ws-a", "source-a", "candidate"),
		SyncedAt:   time.Now().UTC(),
	}
	snapshot.SourceMarkers["ws-a/source-a"] = catalog.SourceReleaseMarker{
		Workspace:   "ws-a",
		GitSourceID: "source-a",
		Commit:      "marker",
	}

	references := sourceBundleReferences(snapshot)
	want := map[string]bool{"active": false, "history": false, "candidate": false, "marker": false}
	for _, reference := range references {
		if _, ok := want[reference.Commit]; ok {
			want[reference.Commit] = true
		}
	}
	for commit, found := range want {
		if !found {
			t.Fatalf("reference %q not discovered: %+v", commit, references)
		}
	}
}
