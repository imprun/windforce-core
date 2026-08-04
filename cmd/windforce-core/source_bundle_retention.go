package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/imprun/windforce-core/internal/bundle"
	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
)

const (
	defaultSourceBundleRetentionInterval = time.Hour
	defaultSourceBundleGracePeriod       = 7 * 24 * time.Hour
)

type sourceBundleCatalog interface {
	LoadCatalog(context.Context) (catalog.Snapshot, error)
}

type sourceBundleRetentionPolicy struct {
	GracePeriod time.Duration
	Interval    time.Duration
	DryRun      bool
}

func (p sourceBundleRetentionPolicy) Enabled() bool {
	return p.GracePeriod > 0
}

func runSourceBundleRetentionLoop(ctx context.Context, store *bundle.LocalStore, releaseCatalog sourceBundleCatalog, policy sourceBundleRetentionPolicy) {
	if policy.Interval <= 0 {
		policy.Interval = defaultSourceBundleRetentionInterval
	}
	tick := func() {
		snapshot, err := releaseCatalog.LoadCatalog(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "source bundle retention: load release references: %v\n", err)
			return
		}
		result, err := store.PruneUnreferenced(ctx, bundle.PruneOptions{
			Referenced: sourceBundleReferences(snapshot),
			Before:     time.Now().UTC().Add(-policy.GracePeriod),
			DryRun:     policy.DryRun,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "source bundle retention: prune: %v\n", err)
			return
		}
		if result.Eligible == 0 && result.Invalid == 0 {
			return
		}
		if policy.DryRun {
			fmt.Fprintf(os.Stderr, "source bundle retention: dry-run eligible=%d discovered=%d referenced=%d recent=%d invalid=%d\n", result.Eligible, result.Discovered, result.Referenced, result.Recent, result.Invalid)
			return
		}
		fmt.Fprintf(os.Stderr, "source bundle retention: removed=%d eligible=%d discovered=%d referenced=%d recent=%d invalid=%d\n", result.Removed, result.Eligible, result.Discovered, result.Referenced, result.Recent, result.Invalid)
	}

	tick()
	ticker := time.NewTicker(policy.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick()
		}
	}
}

func sourceBundleReferences(snapshot catalog.Snapshot) []bundle.Reference {
	references := make([]bundle.Reference, 0, len(snapshot.Deployments)+len(snapshot.History)+len(snapshot.Candidates)+len(snapshot.SourceMarkers))
	appendDeployment := func(deployment contract.Deployment) {
		if deployment.Commit == "" || deployment.SourceGitSourceID() == "" {
			return
		}
		references = append(references, bundle.Reference{
			Workspace:   deployment.SourceWorkspace(),
			GitSourceID: deployment.SourceGitSourceID(),
			Commit:      deployment.Commit,
		})
	}

	for _, deployment := range snapshot.Deployments {
		appendDeployment(deployment)
	}
	for _, history := range snapshot.History {
		appendDeployment(history.Deployment)
		if history.Commit != "" && history.GitSourceID != "" {
			references = append(references, bundle.Reference{
				Workspace:   history.Workspace,
				GitSourceID: history.GitSourceID,
				Commit:      history.Commit,
			})
		}
	}
	for _, candidate := range snapshot.Candidates {
		appendDeployment(candidate.Deployment)
	}
	for _, marker := range snapshot.SourceMarkers {
		if marker.Commit == "" || marker.GitSourceID == "" {
			continue
		}
		references = append(references, bundle.Reference{
			Workspace:   marker.Workspace,
			GitSourceID: marker.GitSourceID,
			Commit:      marker.Commit,
		})
	}
	return references
}
