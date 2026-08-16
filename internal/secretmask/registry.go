package secretmask

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

const (
	ResponseDigestHeader        = "X-Windforce-Secret-Mask-Digest"
	MaxSecretPayloadBytes       = 1 << 20
	MaxSecretJSONLeaves         = 2048
	MaxDynamicPatternBytes      = 64 << 10
	MaxDynamicPatterns          = 4096
	MaxDynamicTotalPatternBytes = 4 << 20
)

var ErrMaskLimit = errors.New("dynamic secret mask limit exceeded")

func Digest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

// Registry is a concurrency-safe, per-Job exact-pattern registry. Dynamic
// registration is atomic: either every required exact/JSON-escaped pattern is
// available to all output paths, or none of the candidate patterns is added.
type Registry struct {
	mu         sync.RWMutex
	patterns   map[string][]byte
	matcher    *multiPatternMatcher
	generation uint64
	totalBytes int
	maxLen     int
}

func NewRegistry(initial []string) *Registry {
	r := &Registry{patterns: map[string][]byte{}, maxLen: 1}
	for _, pattern := range Normalize(initial) {
		r.addTrusted(pattern)
	}
	r.rebuildMatcherLocked()
	return r
}

func (r *Registry) RegisterSecret(value string) error {
	if r == nil {
		return errors.New("secret mask registry is required")
	}
	if len(value) == 0 {
		return nil
	}
	if len(value) > MaxSecretPayloadBytes {
		return fmt.Errorf("secret payload exceeds %d bytes: %w", MaxSecretPayloadBytes, ErrMaskLimit)
	}
	values, err := requiredSecretValues(value)
	if err != nil {
		return err
	}
	candidates := Normalize(values)
	r.mu.Lock()
	defer r.mu.Unlock()
	newCount := 0
	newBytes := 0
	for _, pattern := range candidates {
		if len(pattern) > MaxDynamicPatternBytes {
			return fmt.Errorf("required mask pattern exceeds %d bytes: %w", MaxDynamicPatternBytes, ErrMaskLimit)
		}
		if _, exists := r.patterns[string(pattern)]; exists {
			continue
		}
		newCount++
		newBytes += len(pattern)
	}
	if len(r.patterns)+newCount > MaxDynamicPatterns {
		return fmt.Errorf("dynamic mask patterns exceed %d: %w", MaxDynamicPatterns, ErrMaskLimit)
	}
	if r.totalBytes+newBytes > MaxDynamicTotalPatternBytes {
		return fmt.Errorf("dynamic mask pattern bytes exceed %d: %w", MaxDynamicTotalPatternBytes, ErrMaskLimit)
	}
	for _, pattern := range candidates {
		if _, exists := r.patterns[string(pattern)]; exists {
			continue
		}
		copyPattern := append([]byte(nil), pattern...)
		r.patterns[string(copyPattern)] = copyPattern
		r.totalBytes += len(copyPattern)
		if len(copyPattern) > r.maxLen {
			r.maxLen = len(copyPattern)
		}
	}
	if newCount > 0 {
		r.rebuildMatcherLocked()
	}
	return nil
}

func requiredSecretValues(value string) ([]string, error) {
	var decoded any
	if json.Unmarshal([]byte(value), &decoded) != nil {
		if len(value) > MaxDynamicPatternBytes {
			return nil, fmt.Errorf("non-JSON secret exceeds single-pattern limit %d: %w", MaxDynamicPatternBytes, ErrMaskLimit)
		}
		return []string{value}, nil
	}
	values := []string{}
	leafCount := 0
	var visit func(any) error
	visit = func(item any) error {
		switch typed := item.(type) {
		case string:
			if typed == "" {
				return nil
			}
			leafCount++
			if leafCount > MaxSecretJSONLeaves {
				return fmt.Errorf("secret JSON string leaves exceed %d: %w", MaxSecretJSONLeaves, ErrMaskLimit)
			}
			values = append(values, typed)
		case []any:
			for _, child := range typed {
				if err := visit(child); err != nil {
					return err
				}
			}
		case map[string]any:
			for _, child := range typed {
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(decoded); err != nil {
		return nil, err
	}
	if len(value) <= MaxDynamicPatternBytes {
		values = append(values, value)
	}
	return values, nil
}

func (r *Registry) Bytes(value []byte) []byte {
	matcher, _, _ := r.snapshot()
	masked := append([]byte(nil), value...)
	deltas := make([]int32, len(masked)+1)
	matcher.scan(masked, 0, 0, func(start, end int) { markRange(deltas, start, end) })
	applyRangeMarks(masked, deltas)
	return masked
}

func (r *Registry) String(value string) string {
	return string(r.Bytes([]byte(value)))
}

func (r *Registry) snapshot() (*multiPatternMatcher, int, uint64) {
	if r == nil {
		return newMultiPatternMatcher(nil), 1, 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.matcher, r.maxLen, r.generation
}

func (r *Registry) addTrusted(pattern []byte) {
	if len(pattern) == 0 {
		return
	}
	key := string(pattern)
	if _, exists := r.patterns[key]; exists {
		return
	}
	copyPattern := append([]byte(nil), pattern...)
	r.patterns[key] = copyPattern
	r.totalBytes += len(copyPattern)
	if len(copyPattern) > r.maxLen {
		r.maxLen = len(copyPattern)
	}
}

func (r *Registry) rebuildMatcherLocked() {
	r.matcher = newMultiPatternMatcher(r.patterns)
	r.generation++
}
