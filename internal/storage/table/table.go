// Package table implements the archetype storage layer for the flecs Go port.
//
// SoA layout: a Table holds one Column per non-tag component type and a
// parallel entity-ID column ([]ids.ID). Each row corresponds to one entity.
// A Table has a fixed component-id signature for its lifetime; archetype
// migration (add/remove component) happens in the World layer.
//
// Swap-remove model: Table.RemoveSwap moves the last row into the vacated slot
// and decrements the row count. The caller must update the moved entity's
// Record.Row to the freed row index. This O(1) removal avoids element shifts.
//
// The column layout mirrors ecs_table_t / ecs_data_t from the C upstream:
// flecs/src/storage/table.h lines 98-168.
//
// Tables are NOT goroutine-safe; external synchronization is required.
package table

import (
	"sort"
	"unsafe"

	"github.com/snichols/flecs/internal/component"
	"github.com/snichols/flecs/internal/ids"
)

// Table is a fixed-signature archetype storage unit. Each unique sorted set of
// component IDs maps to exactly one Table in the World (Phase 1.5+).
type Table struct {
	sig      []ids.ID       // sorted component-id signature; read-only after New
	columns  []*Column      // columns[i] corresponds to sig[i]; nil for tags (Size==0)
	entities []ids.ID       // parallel entity-ID column
	colIndex map[ids.ID]int // O(1) id-to-index lookup
	// addEdges and removeEdges are lazily allocated on first CacheAddEdge /
	// CacheRemoveEdge call. Tables that are never migrated through pay no
	// map-header overhead.
	addEdges    map[ids.ID]*Table // (this, +id) → destination table
	removeEdges map[ids.ID]*Table // (this, -id) → destination table
}

// New constructs a Table for the given sorted component-id signature.
// sig must be sorted ascending; New panics if not. len(sig) == len(types).
// Tag components (types[i].Size==0) get a nil column slot.
func New(sig []ids.ID, types []*component.TypeInfo) *Table {
	if !sort.SliceIsSorted(sig, func(i, j int) bool { return sig[i] < sig[j] }) {
		panic("table: ids must be sorted ascending")
	}
	t := &Table{
		sig:      sig,
		columns:  make([]*Column, len(sig)),
		colIndex: make(map[ids.ID]int, len(sig)),
	}
	for i, id := range sig {
		t.colIndex[id] = i
		if types[i].Size > 0 {
			t.columns[i] = newColumn(types[i].Type, types[i].Size)
		}
	}
	return t
}

// Type returns the component-id signature. The returned slice is read-only;
// callers must not modify it.
func (t *Table) Type() []ids.ID { return t.sig }

// Count returns the current number of rows.
func (t *Table) Count() int { return len(t.entities) }

// Entities returns the entity-ID column as a read-only view. The slice is
// invalidated by any Append or RemoveSwap call; do not retain across mutations.
func (t *Table) Entities() []ids.ID { return t.entities }

// HasComponent reports whether id is in the table's signature.
func (t *Table) HasComponent(id ids.ID) bool {
	_, ok := t.colIndex[id]
	return ok
}

// ColumnIndex returns the index into the internal sig/columns slices for id,
// or -1 if id is not in the signature. O(1) via precomputed map.
func (t *Table) ColumnIndex(id ids.ID) int {
	if i, ok := t.colIndex[id]; ok {
		return i
	}
	return -1
}

// Append adds a new zero-initialized row for entity and returns its row index.
// All non-tag component columns are extended by one zero element.
func (t *Table) Append(entity ids.ID) int {
	row := len(t.entities)
	t.entities = append(t.entities, entity)
	for _, col := range t.columns {
		if col != nil {
			col.appendZero()
		}
	}
	return row
}

// RemoveSwap removes row using a swap with the last row. If row was not the
// last, the previously-last entity moves into row; movedEntity is its ID and
// moved is true. The caller must update that entity's Record.Row to row.
// If row was the last row, returns (0, false). Panics if row is out of range.
func (t *Table) RemoveSwap(row int) (movedEntity ids.ID, moved bool) {
	n := len(t.entities)
	if row < 0 || row >= n {
		panic("table: RemoveSwap row out of range")
	}
	last := n - 1
	if row != last {
		movedEntity = t.entities[last]
		moved = true
		t.entities[row] = t.entities[last]
	}
	t.entities[last] = 0
	t.entities = t.entities[:last]
	for _, col := range t.columns {
		if col != nil {
			col.removeSwap(row)
		}
	}
	return movedEntity, moved
}

// Set copies the component value at src into the column for id at row.
// Panics if id is not in the signature or row is out of range.
// For tag components (Size==0), Set is a no-op.
// src must point to at least TypeInfo.Size bytes of the component's type.
func (t *Table) Set(row int, id ids.ID, src unsafe.Pointer) {
	idx, ok := t.colIndex[id]
	if !ok {
		panic("table: Set: id not in signature")
	}
	if row < 0 || row >= len(t.entities) {
		panic("table: Set: row out of range")
	}
	col := t.columns[idx]
	if col == nil {
		return // tag: no-op
	}
	col.Set(row, src)
}

// Get returns a pointer to the component slot for id at row.
// Panics if id is not in the signature or row is out of range.
// For tag components (Size==0), returns nil.
// The pointer is stable until the next Append (may grow columns) or
// RemoveSwap (may move data). Do not cache across those operations.
func (t *Table) Get(row int, id ids.ID) unsafe.Pointer {
	idx, ok := t.colIndex[id]
	if !ok {
		panic("table: Get: id not in signature")
	}
	if row < 0 || row >= len(t.entities) {
		panic("table: Get: row out of range")
	}
	col := t.columns[idx]
	if col == nil {
		return nil // tag
	}
	return col.PtrAt(row)
}

// NextOnAdd returns the cached destination table for adding id to this table's
// signature, and true if the edge is cached. Returns (nil, false) on a miss.
// Not goroutine-safe; matches the Phase 1 single-threaded World invariant.
func (t *Table) NextOnAdd(id ids.ID) (*Table, bool) {
	if t.addEdges == nil {
		return nil, false
	}
	dst, ok := t.addEdges[id]
	return dst, ok
}

// NextOnRemove returns the cached destination table for removing id from this
// table's signature, and true if the edge is cached. Returns (nil, false) on a miss.
// Not goroutine-safe; matches the Phase 1 single-threaded World invariant.
func (t *Table) NextOnRemove(id ids.ID) (*Table, bool) {
	if t.removeEdges == nil {
		return nil, false
	}
	dst, ok := t.removeEdges[id]
	return dst, ok
}

// CacheAddEdge records that adding id to this table's signature leads to dst.
// Idempotent if dst is the same pointer. Panics if a different *Table is already
// cached for (t, +id) — this indicates a correctness bug upstream.
// Not goroutine-safe; matches the Phase 1 single-threaded World invariant.
func (t *Table) CacheAddEdge(id ids.ID, dst *Table) {
	if t.addEdges == nil {
		t.addEdges = make(map[ids.ID]*Table)
	}
	if existing, ok := t.addEdges[id]; ok && existing != dst {
		panic("table: CacheAddEdge: conflicting destination for same (table, id)")
	}
	t.addEdges[id] = dst
}

// CacheRemoveEdge records that removing id from this table's signature leads to dst.
// Idempotent if dst is the same pointer. Panics if a different *Table is already
// cached for (t, -id) — this indicates a correctness bug upstream.
// Not goroutine-safe; matches the Phase 1 single-threaded World invariant.
func (t *Table) CacheRemoveEdge(id ids.ID, dst *Table) {
	if t.removeEdges == nil {
		t.removeEdges = make(map[ids.ID]*Table)
	}
	if existing, ok := t.removeEdges[id]; ok && existing != dst {
		panic("table: CacheRemoveEdge: conflicting destination for same (table, id)")
	}
	t.removeEdges[id] = dst
}
