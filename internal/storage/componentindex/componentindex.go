// Package componentindex implements the reverse mapping from component ID to the
// set of archetype tables that contain that component. It is the Go port of the
// table-list portion of ecs_component_record_t from
// flecs/src/storage/component_index.{h,c}.
//
// This index is a secondary, read-optimised view; the authoritative table
// registry is World.tables. Both are written at the same call sites.
//
// NOT goroutine-safe. The caller must ensure single-threaded access.
package componentindex

import (
	"github.com/snichols/flecs/internal/ids"
	"github.com/snichols/flecs/internal/storage/table"
)

// Index is the component index: a reverse map from component ID to the ordered
// list of tables that contain that component.
//
// Insertion order within each list is preserved and documented as part of the
// API contract: TablesFor returns tables in registration order.
type Index struct {
	m map[ids.ID][]*table.Table
}

// New returns an initialised, empty Index.
func New() *Index {
	return &Index{m: make(map[ids.ID][]*table.Table)}
}

// Register records that t contains componentID. It is idempotent: calling
// Register with the same (componentID, t) pair more than once does not produce
// duplicate entries.
func (idx *Index) Register(componentID ids.ID, t *table.Table) {
	list := idx.m[componentID]
	for _, existing := range list {
		if existing == t {
			return
		}
	}
	idx.m[componentID] = append(list, t)
}

// TablesFor returns a snapshot (copy) of the tables containing componentID, in
// registration order. Returns an empty (non-nil) slice when componentID is
// unknown. Callers may mutate the returned slice without affecting the index.
func (idx *Index) TablesFor(componentID ids.ID) []*table.Table {
	list := idx.m[componentID]
	out := make([]*table.Table, len(list))
	copy(out, list)
	return out
}

// Each calls fn for every table containing componentID, in registration order.
// fn returns false to stop iteration early.
//
// This is the hot path for queries — no allocation is performed.
// Caller must NOT call Register during iteration; doing so may invalidate the
// underlying slice and produce undefined behaviour.
func (idx *Index) Each(componentID ids.ID, fn func(*table.Table) bool) {
	for _, t := range idx.m[componentID] {
		if !fn(t) {
			return
		}
	}
}

// Count returns the number of tables containing componentID. O(1).
func (idx *Index) Count(componentID ids.ID) int {
	return len(idx.m[componentID])
}

// CountComponents returns the total number of distinct component IDs known to
// the index. O(1).
func (idx *Index) CountComponents() int {
	return len(idx.m)
}
