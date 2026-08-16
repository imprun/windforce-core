package secretmask

// multiPatternMatcher is a compact Aho-Corasick matcher. Edges are stored in
// one sparse adjacency array so the 4 MiB dynamic-pattern budget does not
// allocate a map (or a 256-entry transition table) for every trie node.
type multiPatternMatcher struct {
	nodes []matcherNode
	edges []matcherEdge
}

type matcherNode struct {
	firstEdge  int32
	fail       int32
	terminal   int32
	maxMatched int32
}

type matcherEdge struct {
	to   int32
	next int32
	char byte
}

func newMultiPatternMatcher(patterns map[string][]byte) *multiPatternMatcher {
	m := &multiPatternMatcher{nodes: []matcherNode{{firstEdge: -1}}}
	for _, pattern := range patterns {
		if len(pattern) == 0 {
			continue
		}
		state := int32(0)
		for _, value := range pattern {
			next := m.transition(state, value)
			if next < 0 {
				next = int32(len(m.nodes))
				m.nodes = append(m.nodes, matcherNode{firstEdge: -1})
				m.edges = append(m.edges, matcherEdge{to: next, next: m.nodes[state].firstEdge, char: value})
				m.nodes[state].firstEdge = int32(len(m.edges) - 1)
			}
			state = next
		}
		if length := int32(len(pattern)); length > m.nodes[state].terminal {
			m.nodes[state].terminal = length
		}
	}
	m.buildFailureLinks()
	return m
}

func (m *multiPatternMatcher) transition(state int32, value byte) int32 {
	if m == nil || state < 0 || int(state) >= len(m.nodes) {
		return -1
	}
	for edge := m.nodes[state].firstEdge; edge >= 0; edge = m.edges[edge].next {
		if m.edges[edge].char == value {
			return m.edges[edge].to
		}
	}
	return -1
}

func (m *multiPatternMatcher) buildFailureLinks() {
	if m == nil || len(m.nodes) == 0 {
		return
	}
	queue := make([]int32, 0, len(m.nodes)-1)
	m.nodes[0].maxMatched = m.nodes[0].terminal
	for edge := m.nodes[0].firstEdge; edge >= 0; edge = m.edges[edge].next {
		child := m.edges[edge].to
		m.nodes[child].fail = 0
		m.nodes[child].maxMatched = max(m.nodes[child].terminal, m.nodes[0].maxMatched)
		queue = append(queue, child)
	}
	for head := 0; head < len(queue); head++ {
		parent := queue[head]
		for edge := m.nodes[parent].firstEdge; edge >= 0; edge = m.edges[edge].next {
			child, value := m.edges[edge].to, m.edges[edge].char
			fallback := m.nodes[parent].fail
			next := m.transition(fallback, value)
			for next < 0 && fallback != 0 {
				fallback = m.nodes[fallback].fail
				next = m.transition(fallback, value)
			}
			if next >= 0 && next != child {
				m.nodes[child].fail = next
			}
			failureMatch := m.nodes[m.nodes[child].fail].maxMatched
			m.nodes[child].maxMatched = max(m.nodes[child].terminal, failureMatch)
			queue = append(queue, child)
		}
	}
}

// scan visits the input once (plus amortized failure-link traversal). For all
// patterns ending at one byte, marking only the longest interval is sufficient
// because it contains every shorter suffix match.
func (m *multiPatternMatcher) scan(value []byte, state int32, base int, mark func(start, end int)) int32 {
	if m == nil || len(m.nodes) == 0 {
		return 0
	}
	for index, current := range value {
		next := m.transition(state, current)
		for next < 0 && state != 0 {
			state = m.nodes[state].fail
			next = m.transition(state, current)
		}
		if next >= 0 {
			state = next
		} else {
			state = 0
		}
		if length := int(m.nodes[state].maxMatched); length > 0 {
			end := base + index + 1
			mark(end-length, end)
		}
	}
	return state
}
