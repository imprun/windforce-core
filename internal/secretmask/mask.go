package secretmask

import (
	"encoding/json"
	"sort"
	"sync"
)

const replacementByte = '*'

func Normalize(values []string) [][]byte {
	unique := map[string]struct{}{}
	for _, value := range values {
		if value == "" {
			continue
		}
		unique[value] = struct{}{}
		encoded, err := json.Marshal(value)
		if err == nil && len(encoded) >= 2 {
			unique[string(encoded[1:len(encoded)-1])] = struct{}{}
		}
	}
	result := make([][]byte, 0, len(unique))
	for value := range unique {
		if value != "" {
			result = append(result, []byte(value))
		}
	}
	sort.Slice(result, func(i, j int) bool { return len(result[i]) > len(result[j]) })
	return result
}

func Bytes(value []byte, secrets []string) []byte {
	return NewRegistry(secrets).Bytes(value)
}

func String(value string, secrets []string) string {
	return string(Bytes([]byte(value), secrets))
}

// Stream delays at most max-secret-length minus one bytes so a secret split
// across arbitrary process log chunks is still masked before emission.
type Stream struct {
	mu         sync.Mutex
	registry   *Registry
	raw        []byte
	deltas     []int32
	state      int32
	scanned    int
	generation uint64
	emit       func([]byte)
}

func NewStream(secrets []string, emit func([]byte)) *Stream {
	return NewRegistryStream(NewRegistry(secrets), emit)

}

func NewRegistryStream(registry *Registry, emit func([]byte)) *Stream {
	if registry == nil {
		registry = NewRegistry(nil)
	}
	return &Stream{registry: registry, emit: emit}
}

func (s *Stream) Write(chunk []byte) {
	if s == nil || len(chunk) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.raw = append(s.raw, chunk...)
	if len(s.deltas) == 0 {
		s.deltas = make([]int32, len(s.raw)+1)
	} else {
		s.deltas = append(s.deltas, make([]int32, len(chunk))...)
	}
	matcher, maxLen, generation := s.registry.snapshot()
	if generation != s.generation {
		s.state, s.scanned, s.generation = 0, 0, generation
	}
	s.state = matcher.scan(s.raw[s.scanned:], s.state, s.scanned, func(start, end int) {
		markRange(s.deltas, start, end)
	})
	s.scanned = len(s.raw)
	keep := maxLen - 1
	if len(s.raw) <= keep {
		return
	}
	s.emitPrefix(len(s.raw) - keep)
}

func (s *Stream) Flush() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	matcher, _, generation := s.registry.snapshot()
	if generation != s.generation {
		s.state, s.scanned, s.generation = 0, 0, generation
	}
	s.state = matcher.scan(s.raw[s.scanned:], s.state, s.scanned, func(start, end int) {
		markRange(s.deltas, start, end)
	})
	s.scanned = len(s.raw)
	s.emitPrefix(len(s.raw))
}

func (s *Stream) emitPrefix(size int) {
	if size <= 0 {
		return
	}
	output := append([]byte(nil), s.raw[:size]...)
	active := applyRangeMarks(output, s.deltas[:size+1])
	if s.emit != nil {
		s.emit(output)
	}
	s.raw = append(s.raw[:0], s.raw[size:]...)
	s.deltas = append(s.deltas[:0], s.deltas[size:]...)
	if len(s.deltas) == 0 {
		s.deltas = []int32{active}
	} else {
		s.deltas[0] += active
	}
	s.scanned -= size
	if s.scanned < 0 {
		s.scanned = 0
	}
}

func markRange(deltas []int32, start, end int) {
	if start < 0 {
		start = 0
	}
	if end > len(deltas)-1 {
		end = len(deltas) - 1
	}
	if start >= end {
		return
	}
	deltas[start]++
	deltas[end]--
}

func applyRangeMarks(value []byte, deltas []int32) int32 {
	var active int32
	for index := range value {
		if index < len(deltas) {
			active += deltas[index]
		}
		if active > 0 {
			value[index] = replacementByte
		}
	}
	return active
}
