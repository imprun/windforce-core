package state

import (
	"sort"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

// QueueDemandSelector describes the worker-side selector to evaluate against
// queued and leased jobs. Key is opaque caller context echoed in the result;
// Core attaches no product meaning to it.
type QueueDemandSelector struct {
	Key         string   `json:"key"`
	WorkspaceID string   `json:"workspace_id"`
	Tags        []string `json:"tags,omitempty"`
	Labels      []string `json:"labels,omitempty"`
}

// QueueDemand is one selector's claimable backlog and active lease view.
// Queued and ExpiredReacquirable are disjoint diagnostics whose sum is
// Eligible.
type QueueDemand struct {
	Selector            QueueDemandSelector `json:"selector"`
	Eligible            int                 `json:"eligible"`
	Queued              int                 `json:"queued"`
	ExpiredReacquirable int                 `json:"expired_reacquirable"`
	Claimed             int                 `json:"claimed"`
	BusyWorkers         int                 `json:"busy_workers"`
	OldestEligibleAt    *time.Time          `json:"oldest_eligible_at,omitempty"`
}

// QueueDemandSnapshot fences every selector result to one authoritative store
// view. StoreEpoch changes when the store is initialized anew; revisions are
// comparable only inside one epoch.
type QueueDemandSnapshot struct {
	StoreEpoch       string        `json:"store_epoch"`
	SnapshotRevision int64         `json:"snapshot_revision"`
	ObservedAt       time.Time     `json:"observed_at"`
	Items            []QueueDemand `json:"items"`
}

func buildQueueDemandSnapshot(storeEpoch string, revision int64, observedAt time.Time, jobs []Job, selectors []QueueDemandSelector) QueueDemandSnapshot {
	return buildQueueDemandSnapshotWithRates(storeEpoch, revision, observedAt, jobs, selectors, nil)
}

func buildQueueDemandSnapshotWithRates(storeEpoch string, revision int64, observedAt time.Time, jobs []Job, selectors []QueueDemandSelector, rateBuckets map[string]ExecutionRateBucket) QueueDemandSnapshot {
	return buildQueueDemandSnapshotWithPolicies(storeEpoch, revision, observedAt, jobs, selectors, rateBuckets, nil)
}

func buildQueueDemandSnapshotWithPolicies(storeEpoch string, revision int64, observedAt time.Time, jobs []Job, selectors []QueueDemandSelector, rateBuckets map[string]ExecutionRateBucket, policies executionLimitPolicyLookup) QueueDemandSnapshot {
	observedAt = observedAt.UTC()
	items := make([]QueueDemand, 0, len(selectors))
	baseRunning := activeRunningByApp(jobs, observedAt)
	baseKeyedRunning := activeRunningByKeyedConcurrency(jobs, observedAt)
	baseRateUsage := currentRateUsage(rateBuckets, observedAt)

	for _, rawSelector := range selectors {
		selector := normalizeQueueDemandSelector(rawSelector)
		allowedTags := normalizeClaimTags(selector.Tags)
		offeredLabels := normalizeClaimTags(selector.Labels)
		busyWorkers := map[string]struct{}{}
		candidates := make([]Job, 0)
		item := QueueDemand{Selector: selector}

		for _, job := range jobs {
			if normalizedJobWorkspace("", job) != selector.WorkspaceID || !claimAllowed(job, allowedTags, offeredLabels) {
				continue
			}
			if activeQueueLease(job, observedAt) {
				item.Claimed++
				if job.LeaseOwner != "" {
					busyWorkers[job.LeaseOwner] = struct{}{}
				}
				continue
			}
			if queueDemandCandidate(job, observedAt) {
				candidates = append(candidates, job)
			}
		}

		sort.Slice(candidates, func(i, j int) bool {
			left, right := candidates[i], candidates[j]
			if left.Priority != right.Priority {
				return left.Priority < right.Priority
			}
			if !left.CreatedAt.Equal(right.CreatedAt) {
				return left.CreatedAt.Before(right.CreatedAt)
			}
			return left.ID < right.ID
		})

		running := cloneDemandCounts(baseRunning)
		keyedRunning := cloneDemandCounts(baseKeyedRunning)
		rateUsage := cloneDemandCounts(baseRateUsage)
		for _, candidate := range candidates {
			appKey := demandAppKey(candidate)
			appLimit, appLimited, appValid := effectiveAppConcurrencyLimit(candidate, policies)
			if !appValid || (appLimited && appKey != "" && running[appKey] >= appLimit) {
				continue
			}
			if demandKeyedConcurrencyReached(candidate, keyedRunning, policies) {
				continue
			}
			if demandRateLimitsReached(candidate, rateUsage, policies) {
				continue
			}
			if appKey != "" {
				running[appKey]++
			}
			for _, limit := range candidate.Payload.ExecutionLimits.Concurrency {
				keyedRunning[keyedConcurrencyCountKey(candidate, limit)]++
			}
			addDemandRateConsumption(candidate, rateUsage)
			item.Eligible++
			if candidate.State == JobQueued {
				item.Queued++
			} else {
				item.ExpiredReacquirable++
			}
			if item.OldestEligibleAt == nil || candidate.CreatedAt.Before(*item.OldestEligibleAt) {
				value := candidate.CreatedAt.UTC()
				item.OldestEligibleAt = &value
			}
		}
		item.BusyWorkers = len(busyWorkers)
		items = append(items, item)
	}

	return QueueDemandSnapshot{
		StoreEpoch:       storeEpoch,
		SnapshotRevision: revision,
		ObservedAt:       observedAt,
		Items:            items,
	}
}

func activeRunningByKeyedConcurrency(jobs []Job, observedAt time.Time) map[string]int {
	counts := map[string]int{}
	for _, job := range jobs {
		if !activeQueueLease(job, observedAt) {
			continue
		}
		for _, limit := range job.Payload.ExecutionLimits.Concurrency {
			if validKeyedConcurrencyPin(limit) {
				counts[keyedConcurrencyCountKey(job, limit)]++
			}
		}
	}
	return counts
}

func demandKeyedConcurrencyReached(candidate Job, running map[string]int, policies executionLimitPolicyLookup) bool {
	for _, limit := range candidate.Payload.ExecutionLimits.Concurrency {
		effectiveLimit, valid := effectiveKeyedConcurrencyLimit(candidate, limit, policies)
		if !validKeyedConcurrencyPin(limit) || !valid || running[keyedConcurrencyCountKey(candidate, limit)] >= effectiveLimit {
			return true
		}
	}
	return false
}

func keyedConcurrencyCountKey(job Job, limit KeyedConcurrencyLimitPin) string {
	return normalizedJobWorkspace("", job) + "\x00" + limit.Scope + "\x00" + limit.PolicyID + "\x00" + limit.ShapeFingerprint + "\x00" + limit.KeyDigest
}

func normalizeQueueDemandSelector(selector QueueDemandSelector) QueueDemandSelector {
	selector.Key = strings.TrimSpace(selector.Key)
	selector.WorkspaceID = contract.NormalizeWorkspace(selector.WorkspaceID)
	selector.Tags = normalizedDemandValues(selector.Tags)
	selector.Labels = normalizedDemandValues(selector.Labels)
	return selector
}

func normalizedDemandValues(values []string) []string {
	set := normalizeClaimTags(values)
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func activeQueueLease(job Job, observedAt time.Time) bool {
	if job.State != JobRunning {
		return false
	}
	return job.LeaseExpiresAt == nil || job.LeaseExpiresAt.After(observedAt)
}

func queueDemandCandidate(job Job, observedAt time.Time) bool {
	if job.CanceledBy != nil {
		return false
	}
	if job.State == JobQueued {
		return true
	}
	return job.State == JobRunning && job.LeaseExpiresAt != nil && !job.LeaseExpiresAt.After(observedAt)
}

func activeRunningByApp(jobs []Job, observedAt time.Time) map[string]int {
	counts := map[string]int{}
	for _, job := range jobs {
		if !activeQueueLease(job, observedAt) {
			continue
		}
		if key := demandAppKey(job); key != "" {
			counts[key]++
		}
	}
	return counts
}

func demandAppKey(job Job) string {
	appKey := jobAppKey(job)
	if appKey == "" {
		return ""
	}
	return normalizedJobWorkspace("", job) + "\x00" + appKey
}

func cloneDemandCounts(source map[string]int) map[string]int {
	clone := make(map[string]int, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
