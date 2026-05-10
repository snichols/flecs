// Package table implements the archetype storage layer for the flecs Go port.
//
// SoA layout: a Table holds one Column per non-tag component type and a
// parallel entity-ID column ([]flecs.ID). Each row corresponds to one entity.
// A Table has a fixed component-id signature for its lifetime; archetype
// migration (add/remove component) is deferred to Phase 2.
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

	"github.com/snichols/flecs"
	"github.com/snichols/flecs/internal/component"
)

// Table is a fixed-signature archetype storage unit. Each unique sorted set of
// component IDs maps to exactly one Table in the World (Phase 1.5+).
type Table struct {
	ids      []flecs.ID       // sorted component-id signature; read-only after New
	columns  []*Column        // columns[i] corresponds to ids[i]; nil for tags (Size==0)
	entities []flecs.ID       // parallel entity-ID column
	colIndex map[flecs.ID]int // O(1) id-to-index lookup; for archetypes >8 components a map wins over binary search
}

// New constructs a Table for the given sorted component-id signature.
// ids must be sorted ascending; New panics if not. len(ids) == len(types).
// Tag components (types[i].Size==0) get a nil column slot.
func New(ids []flecs.ID, types []*component.TypeInfo) *Table {
	if !sort.SliceIsSorted(ids, func(i, j int) bool { return ids[i] < ids[j] }) {
		panic("table: ids must be sorted ascending")
	}
	t := &Table{
		ids:      ids,
		columns:  make([]*Column, len(ids)),
		colIndex: make(map[flecs.ID]int, len(ids)),
	}
	for i, id := range ids {
		t.colIndex[id] = i
		if types[i].Size > 0 {
			t.columns[i] = newColumn(types[i].Type, types[i].Size)
		}
	}
	return t
}

// Type returns the component-id signature. The returned slice is read-only;
// callers must not modify it.
func (t *Table) Type() []flecs.ID { return t.ids }

// Count returns the current number of rows.
func (t *Table) Count() int { return len(t.entities) }

// Entities returns the entity-ID column as a read-only view. The slice is
// invalidated by any Append or RemoveSwap call; do not retain across mutations.
func (t *Table) Entities() []flecs.ID { return t.entities }

// HasComponent reports whether id is in the table's signature.
func (t *Table) HasComponent(id flecs.ID) bool {
	_, ok := t.colIndex[id]
	return ok
}

// ColumnIndex returns the index into the internal ids/columns slices for id,
// or -1 if id is not in the signature. O(1) via precomputed map.
func (t *Table) ColumnIndex(id flecs.ID) int {
	if i, ok := t.colIndex[id]; ok {
		return i
	}
	return -1
}

// Append adds a new zero-initialized row for entity and returns its row index.
// All non-tag component columns are extended by one zero element.
func (t *Table) Append(entity flecs.ID) int {
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
func (t *Table) RemoveSwap(row int) (movedEntity flecs.ID, moved bool) {
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
func (t *Table) Set(row int, id flecs.ID, src unsafe.Pointer) {
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
func (t *Table) Get(row int, id flecs.ID) unsafe.Pointer {
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
