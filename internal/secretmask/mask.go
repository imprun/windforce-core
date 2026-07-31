package secretmask

import (
	"bytes"
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
	patterns := Normalize(secrets)
	masked := append([]byte(nil), value...)
	marks := make([]bool, len(masked))
	markMatches(masked, marks, patterns)
	applyMarks(masked, marks)
	return masked
}

func String(value string, secrets []string) string {
	return string(Bytes([]byte(value), secrets))
}

// Stream delays at most max-secret-length minus one bytes so a secret split
// across arbitrary process log chunks is still masked before emission.
type Stream struct {
	mu       sync.Mutex
	patterns [][]byte
	maxLen   int
	raw      []byte
	marks    []bool
	emit     func([]byte)
}

func NewStream(secrets []string, emit func([]byte)) *Stream {
	patterns := Normalize(secrets)
	maxLen := 1
	for _, pattern := range patterns {
		if len(pattern) > maxLen {
			maxLen = len(pattern)
		}
	}
	return &Stream{patterns: patterns, maxLen: maxLen, emit: emit}
}

func (s *Stream) Write(chunk []byte) {
	if s == nil || len(chunk) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.raw = append(s.raw, chunk...)
	s.marks = append(s.marks, make([]bool, len(chunk))...)
	markMatches(s.raw, s.marks, s.patterns)
	keep := s.maxLen - 1
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
	markMatches(s.raw, s.marks, s.patterns)
	s.emitPrefix(len(s.raw))
}

func (s *Stream) emitPrefix(size int) {
	if size <= 0 {
		return
	}
	output := append([]byte(nil), s.raw[:size]...)
	applyMarks(output, s.marks[:size])
	if s.emit != nil {
		s.emit(output)
	}
	s.raw = append(s.raw[:0], s.raw[size:]...)
	s.marks = append(s.marks[:0], s.marks[size:]...)
}

func markMatches(value []byte, marks []bool, patterns [][]byte) {
	for _, pattern := range patterns {
		if len(pattern) == 0 || len(pattern) > len(value) {
			continue
		}
		for offset := 0; offset <= len(value)-len(pattern); {
			index := bytes.Index(value[offset:], pattern)
			if index < 0 {
				break
			}
			start := offset + index
			for position := start; position < start+len(pattern); position++ {
				marks[position] = true
			}
			offset = start + 1
		}
	}
}

func applyMarks(value []byte, marks []bool) {
	for index := range value {
		if index < len(marks) && marks[index] {
			value[index] = replacementByte
		}
	}
}
