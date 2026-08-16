package state

import (
	"strconv"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/executionlimit"
)

func validKeyedRatePin(limit KeyedRateLimitPin) bool {
	return strings.TrimSpace(limit.PolicyID) != "" &&
		validExecutionLimitDigest(limit.PolicyRevision, "sha256:") &&
		(limit.ShapeFingerprint == "" || executionlimit.IsFingerprint(limit.ShapeFingerprint)) &&
		(limit.Scope == ExecutionLimitScopeApp || limit.Scope == ExecutionLimitScopeAction) &&
		validExecutionLimitDigest(limit.KeyDigest, "hmac-sha256:") &&
		limit.MaxAttempts > 0 &&
		limit.WindowSeconds >= contract.MinRateWindowSeconds &&
		limit.WindowSeconds <= contract.MaxRateWindowSeconds
}

func rateWindow(now time.Time, windowSeconds int32) (time.Time, time.Time) {
	now = now.UTC()
	seconds := int64(windowSeconds)
	start := time.Unix((now.Unix()/seconds)*seconds, 0).UTC()
	return start, start.Add(time.Duration(windowSeconds) * time.Second)
}

func rateBucketKey(workspaceID string, limit KeyedRateLimitPin) string {
	return contract.NormalizeWorkspace(workspaceID) + "\x00" + limit.Scope + "\x00" + limit.PolicyID + "\x00" + limit.ShapeFingerprint + "\x00" + limit.KeyDigest + "\x00" + strconv.FormatInt(int64(limit.WindowSeconds), 10)
}

func rateLimitsReached(snapshot *Snapshot, candidate Job, now time.Time) bool {
	policies := executionLimitPolicyLookupFromSnapshot(snapshot)
	for _, limit := range candidate.Payload.ExecutionLimits.Rate {
		if !validKeyedRatePin(limit) {
			return true
		}
		effectiveLimit, valid := effectiveKeyedRateLimit(candidate, limit, policies)
		if !valid {
			return true
		}
		start, _ := rateWindow(now, limit.WindowSeconds)
		bucket, ok := snapshot.ExecutionRateBuckets[rateBucketKey(normalizedJobWorkspace("", candidate), limit)]
		if ok && bucket.WindowStart.Equal(start) && bucket.Consumed >= effectiveLimit {
			return true
		}
	}
	return false
}

func consumeRateLimits(snapshot *Snapshot, candidate Job, now time.Time) {
	workspaceID := normalizedJobWorkspace("", candidate)
	for _, limit := range candidate.Payload.ExecutionLimits.Rate {
		start, end := rateWindow(now, limit.WindowSeconds)
		key := rateBucketKey(workspaceID, limit)
		bucket := snapshot.ExecutionRateBuckets[key]
		if !bucket.WindowStart.Equal(start) {
			bucket = ExecutionRateBucket{WindowStart: start, WindowEnd: end}
		}
		bucket.Consumed++
		bucket.WindowEnd = end
		snapshot.ExecutionRateBuckets[key] = bucket
	}
}

func pruneExecutionRateBuckets(snapshot *Snapshot, now time.Time) {
	for key, bucket := range snapshot.ExecutionRateBuckets {
		if bucket.WindowEnd.IsZero() || !bucket.WindowEnd.After(now) {
			delete(snapshot.ExecutionRateBuckets, key)
		}
	}
}

func currentRateUsage(buckets map[string]ExecutionRateBucket, observedAt time.Time) map[string]int {
	usage := make(map[string]int, len(buckets))
	for key, bucket := range buckets {
		if bucket.WindowStart.IsZero() || bucket.WindowEnd.IsZero() || bucket.WindowStart.After(observedAt) || !bucket.WindowEnd.After(observedAt) || bucket.Consumed <= 0 {
			continue
		}
		usage[key] = int(bucket.Consumed)
	}
	return usage
}

func demandRateLimitsReached(candidate Job, usage map[string]int, policies executionLimitPolicyLookup) bool {
	workspaceID := normalizedJobWorkspace("", candidate)
	for _, limit := range candidate.Payload.ExecutionLimits.Rate {
		if !validKeyedRatePin(limit) {
			return true
		}
		effectiveLimit, valid := effectiveKeyedRateLimit(candidate, limit, policies)
		if !valid || usage[rateBucketKey(workspaceID, limit)] >= int(effectiveLimit) {
			return true
		}
	}
	return false
}

func addDemandRateConsumption(candidate Job, usage map[string]int) {
	workspaceID := normalizedJobWorkspace("", candidate)
	for _, limit := range candidate.Payload.ExecutionLimits.Rate {
		usage[rateBucketKey(workspaceID, limit)]++
	}
}
