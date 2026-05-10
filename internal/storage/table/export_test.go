package table

import "github.com/snichols/flecs/internal/ids"

// Test-only inspection helpers for edge cache assertions.
// Named without the "Test" prefix to avoid collision with Go's test-function
// signature requirement (func TestXxx(t *testing.T)).

// EdgeCount returns the total number of cached add+remove edges on t.
func EdgeCount(t *Table) int {
	return len(t.addEdges) + len(t.removeEdges)
}

// AddEdgeFor is a test-only alias for NextOnAdd.
func AddEdgeFor(t *Table, id ids.ID) (*Table, bool) {
	return t.NextOnAdd(id)
}

// RemoveEdgeFor is a test-only alias for NextOnRemove.
func RemoveEdgeFor(t *Table, id ids.ID) (*Table, bool) {
	return t.NextOnRemove(id)
}
