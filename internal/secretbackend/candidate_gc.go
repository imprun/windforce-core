package secretbackend

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

const (
	DefaultRuntimeCandidateGracePeriod = 24 * time.Hour
	DefaultRuntimeCandidatePageSize    = 100
	DefaultRuntimeCandidateSweepLimit  = 1000
	MaxRuntimeCandidatePageSize        = 1000
)

// RuntimeCandidate is privacy-safe inventory metadata for one object in a
// backend's dedicated runtime-candidate namespace. Version is an opaque
// compare-and-delete token; it must never contain the stored Secret value.
type RuntimeCandidate struct {
	Reference     Reference
	CandidateID   string
	LastTouchedAt time.Time
	Version       string
}

type ListRuntimeCandidatesRequest struct {
	Before time.Time
	Cursor string
	Limit  int
}

type RuntimeCandidatePage struct {
	Candidates []RuntimeCandidate
	NextCursor string
}

// RuntimeCandidateCleaner is an optional capability for side-effecting Secret
// backends. Store and DeleteRuntimeCandidate must be serialized per candidate:
// every Store attempt, including an exact retry, advances the candidate's
// Version/LastTouchedAt without creating a second object, and delete succeeds
// only when the supplied Version is still current. This prevents a sweep from
// deleting a candidate concurrently refreshed by a publisher.
//
// ListRuntimeCandidates is limited to the backend's dedicated runtime
// candidate namespace. Implementations must not perform an unscoped provider
// scan. DeleteRuntimeCandidate returns false when the candidate is missing or
// changed after enumeration.
type RuntimeCandidateCleaner interface {
	ListRuntimeCandidates(ctx context.Context, request ListRuntimeCandidatesRequest) (RuntimeCandidatePage, error)
	DeleteRuntimeCandidate(ctx context.Context, candidate RuntimeCandidate) (bool, error)
}

// RuntimeCandidateLiveReferenceSource supplies the current Core state roots
// for mark-and-sweep. An error must fail closed before backend enumeration.
type RuntimeCandidateLiveReferenceSource interface {
	ListLiveRuntimeSecretCandidateReferences(ctx context.Context) ([]Reference, error)
}

type RuntimeCandidateSweepResult struct {
	Scanned     uint64
	Reclaimed   uint64
	SkippedLive uint64
	Deferred    uint64
	Failed      uint64
}

type RuntimeCandidateCollector struct {
	Backend     RuntimeCandidateCleaner
	Live        RuntimeCandidateLiveReferenceSource
	GracePeriod time.Duration
	PageSize    int
	SweepLimit  int
	Now         func() time.Time
	Metrics     *RuntimeCandidateMetrics
}

func (collector RuntimeCandidateCollector) Sweep(ctx context.Context) (RuntimeCandidateSweepResult, error) {
	var result RuntimeCandidateSweepResult
	if collector.Backend == nil || collector.Live == nil {
		return result, errors.New("runtime Secret candidate collector requires backend and live-reference source")
	}
	gracePeriod := collector.GracePeriod
	if gracePeriod <= 0 {
		return result, errors.New("runtime Secret candidate grace period must be positive")
	}
	pageSize := collector.PageSize
	if pageSize <= 0 {
		pageSize = DefaultRuntimeCandidatePageSize
	}
	if pageSize > MaxRuntimeCandidatePageSize {
		return result, fmt.Errorf("runtime Secret candidate page size must not exceed %d", MaxRuntimeCandidatePageSize)
	}
	sweepLimit := collector.SweepLimit
	if sweepLimit <= 0 {
		sweepLimit = DefaultRuntimeCandidateSweepLimit
	}
	now := time.Now().UTC()
	if collector.Now != nil {
		now = collector.Now().UTC()
	}
	cutoff := now.Add(-gracePeriod)

	liveReferences, err := collector.Live.ListLiveRuntimeSecretCandidateReferences(ctx)
	if err != nil {
		result.Failed++
		collector.Metrics.observe(result)
		return result, errors.New("list live runtime Secret candidate references failed")
	}
	live := make(map[string]struct{}, len(liveReferences))
	for _, reference := range liveReferences {
		live[runtimeCandidateReferenceKey(reference)] = struct{}{}
	}

	var sweepErrors []error
	cursor := ""
	seenCursors := map[string]struct{}{}
	for int(result.Scanned) < sweepLimit {
		limit := min(pageSize, sweepLimit-int(result.Scanned))
		page, listErr := collector.Backend.ListRuntimeCandidates(ctx, ListRuntimeCandidatesRequest{
			Before: cutoff,
			Cursor: cursor,
			Limit:  limit,
		})
		if listErr != nil {
			result.Failed++
			sweepErrors = append(sweepErrors, errors.New("list runtime Secret candidates failed"))
			break
		}
		if len(page.Candidates) > limit {
			result.Failed++
			sweepErrors = append(sweepErrors, errors.New("runtime Secret candidate backend exceeded requested page size"))
			break
		}
		for _, candidate := range page.Candidates {
			result.Scanned++
			if err := validateRuntimeCandidate(candidate); err != nil {
				result.Failed++
				sweepErrors = append(sweepErrors, err)
				continue
			}
			if candidate.LastTouchedAt.After(cutoff) {
				result.Deferred++
				continue
			}
			if _, found := live[runtimeCandidateReferenceKey(candidate.Reference)]; found {
				result.SkippedLive++
				continue
			}
			deleted, deleteErr := collector.Backend.DeleteRuntimeCandidate(ctx, candidate)
			if deleteErr != nil {
				result.Failed++
				sweepErrors = append(sweepErrors, errors.New("delete runtime Secret candidate failed"))
				continue
			}
			if deleted {
				result.Reclaimed++
			} else {
				result.Deferred++
			}
		}
		if strings.TrimSpace(page.NextCursor) == "" {
			break
		}
		if len(page.Candidates) == 0 {
			result.Failed++
			sweepErrors = append(sweepErrors, errors.New("runtime Secret candidate backend returned an empty page with a continuation cursor"))
			break
		}
		if page.NextCursor == cursor {
			result.Failed++
			sweepErrors = append(sweepErrors, errors.New("runtime Secret candidate backend repeated its cursor"))
			break
		}
		if _, found := seenCursors[page.NextCursor]; found {
			result.Failed++
			sweepErrors = append(sweepErrors, errors.New("runtime Secret candidate backend returned a cursor cycle"))
			break
		}
		seenCursors[page.NextCursor] = struct{}{}
		cursor = page.NextCursor
	}
	collector.Metrics.observe(result)
	return result, errors.Join(sweepErrors...)
}

// Run sweeps immediately and then on each interval until ctx is cancelled.
// Per-sweep errors are reported and retried; the callback must not log Secret
// values, backend credentials, cursors, versions, or stored backend material.
func (collector RuntimeCandidateCollector) Run(ctx context.Context, interval time.Duration, report func(error)) error {
	if interval <= 0 {
		return errors.New("runtime Secret candidate sweep interval must be positive")
	}
	run := func() {
		if _, err := collector.Sweep(ctx); err != nil && ctx.Err() == nil && report != nil {
			report(err)
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			run()
		}
	}
}

func RuntimeCandidateReference(reference Reference, stored string) (Reference, bool, error) {
	if !strings.HasPrefix(stored, runtimeCandidatePrefix) {
		return Reference{}, false, nil
	}
	opened, _, err := OpenRuntimeCandidate(reference, stored)
	if err != nil {
		return Reference{}, false, err
	}
	return opened, true, nil
}

func runtimeCandidateReferenceKey(reference Reference) string {
	return strings.Join([]string{
		contract.NormalizeWorkspace(reference.WorkspaceID),
		strings.ToLower(strings.TrimSpace(reference.Kind)),
		strings.Trim(strings.TrimSpace(reference.Path), "/"),
	}, "\x00")
}

func validateRuntimeCandidate(candidate RuntimeCandidate) error {
	idBytes, err := hex.DecodeString(strings.TrimSpace(candidate.CandidateID))
	if err != nil || len(idBytes) != 16 {
		return errors.New("runtime Secret candidate inventory returned an invalid candidate ID")
	}
	if strings.TrimSpace(candidate.Reference.WorkspaceID) == "" ||
		strings.ToLower(strings.TrimSpace(candidate.Reference.Kind)) != "variable-app" ||
		strings.TrimSpace(candidate.Reference.Path) == "" ||
		!strings.HasSuffix(strings.ToLower(strings.Trim(candidate.Reference.Path, "/")), "/"+strings.ToLower(candidate.CandidateID)) {
		return errors.New("runtime Secret candidate inventory returned an invalid namespace reference")
	}
	if candidate.LastTouchedAt.IsZero() || strings.TrimSpace(candidate.Version) == "" {
		return errors.New("runtime Secret candidate inventory omitted compare-and-delete metadata")
	}
	return nil
}

type RuntimeCandidateMetrics struct {
	mu       sync.RWMutex
	counters RuntimeCandidateSweepResult
}

func NewRuntimeCandidateMetrics() *RuntimeCandidateMetrics {
	return &RuntimeCandidateMetrics{}
}

func (metrics *RuntimeCandidateMetrics) observe(result RuntimeCandidateSweepResult) {
	if metrics == nil {
		return
	}
	metrics.mu.Lock()
	metrics.counters.Scanned += result.Scanned
	metrics.counters.Reclaimed += result.Reclaimed
	metrics.counters.SkippedLive += result.SkippedLive
	metrics.counters.Deferred += result.Deferred
	metrics.counters.Failed += result.Failed
	metrics.mu.Unlock()
}

func (metrics *RuntimeCandidateMetrics) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if next != nil {
			next.ServeHTTP(response, request)
		} else {
			response.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		}
		_, _ = fmt.Fprint(response, metrics.render())
	})
}

func (metrics *RuntimeCandidateMetrics) render() string {
	if metrics == nil {
		return ""
	}
	metrics.mu.RLock()
	values := map[string]uint64{
		"deferred":     metrics.counters.Deferred,
		"failed":       metrics.counters.Failed,
		"reclaimed":    metrics.counters.Reclaimed,
		"scanned":      metrics.counters.Scanned,
		"skipped_live": metrics.counters.SkippedLive,
	}
	metrics.mu.RUnlock()
	outcomes := make([]string, 0, len(values))
	for outcome := range values {
		outcomes = append(outcomes, outcome)
	}
	sort.Strings(outcomes)
	var output strings.Builder
	output.WriteString("# HELP windforce_runtime_secret_candidate_gc_total Runtime Secret candidate collection outcomes observed by this Core process.\n")
	output.WriteString("# TYPE windforce_runtime_secret_candidate_gc_total counter\n")
	for _, outcome := range outcomes {
		fmt.Fprintf(&output, "windforce_runtime_secret_candidate_gc_total{outcome=%q} %d\n", outcome, values[outcome])
	}
	return output.String()
}
