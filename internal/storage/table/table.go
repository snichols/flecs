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
	"reflect"
	"runtime"
	"sort"
	"sync/atomic"
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
	// changeCount is a monotonic counter incremented on every structural change
	// (Append, RemoveSwap) and on column writes (BumpChange). Used by
	// CachedQuery.Changed() for opt-in change detection. Wrapping at max uint64
	// is treated as a change (conservative; practical infinity for any real use).
	changeCount uint64
	// bitsets holds per-component enabled/disabled state for CanToggle components.
	// Each entry is a packed bitset (1 = enabled, 0 = disabled) covering all rows.
	// Absence of an entry means all rows are enabled (lazy allocation: only created
	// on the first DisableRow call). Keyed by component ID as it appears in sig.
	bitsets map[ids.ID][]uint64
	// Reclamation tracking fields (Phase 16.46).
	// emptyTicks counts consecutive Progress ticks where Count() == 0.
	// refCount is held by live queries/observers/iters; reclamation is skipped
	// when refCount > 0. pinned suppresses reclamation unconditionally.
	// dead is set after the table is reclaimed; pointer dereferences panic.
	// reclaimEpoch records the last tick at which the table was reclaim-eligible.
	emptyTicks   uint32
	refCount     int32 // accessed via sync/atomic
	reclaimEpoch uint64
	pinned       bool
	dead         bool
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

// ChangeCount returns the current value of the table's monotonic change counter.
// The counter is incremented by Append, RemoveSwap, and BumpChange. It is used
// by CachedQuery.Changed() to detect whether a table was mutated between calls.
func (t *Table) ChangeCount() uint64 { return t.changeCount }

// Pin prevents this table from being reclaimed by the sweep, regardless of
// emptyTicks or refCount. Idempotent.
func (t *Table) Pin() { t.pinned = true }

// Unpin re-enables reclamation eligibility. Idempotent.
func (t *Table) Unpin() { t.pinned = false }

// IsPinned reports whether the table is pinned against reclamation.
func (t *Table) IsPinned() bool { return t.pinned }

// IncrRef atomically increments the reference count. Called by queries,
// observers, and open iterators that hold a pointer to this table.
func (t *Table) IncrRef() { atomic.AddInt32(&t.refCount, 1) }

// DecrRef atomically decrements the reference count. Panics if the count would
// go negative (indicates a bug in the caller's ref-tracking).
func (t *Table) DecrRef() {
	if atomic.AddInt32(&t.refCount, -1) < 0 {
		panic("table: DecrRef: refCount went negative")
	}
}

// RefCount returns the current reference count. For testing only.
func (t *Table) RefCount() int32 { return atomic.LoadInt32(&t.refCount) }

// EmptyTicks returns the number of consecutive Progress ticks during which
// the table had Count() == 0.
func (t *Table) EmptyTicks() uint32 { return t.emptyTicks }

// IncrEmptyTicks increments emptyTicks by one. Called by the reclamation
// sweep for each table that is empty, not pinned, and has no live refs.
func (t *Table) IncrEmptyTicks() { t.emptyTicks++ }

// ResetEmptyTicks resets emptyTicks to zero. Called when a row is inserted
// (in Append) and on snapshot restore.
func (t *Table) ResetEmptyTicks() { t.emptyTicks = 0 }

// SetReclaimEpoch records the last tick at which this table was
// reclaim-eligible.
func (t *Table) SetReclaimEpoch(epoch uint64) { t.reclaimEpoch = epoch }

// ReclaimEpoch returns the last tick at which this table was reclaim-eligible.
func (t *Table) ReclaimEpoch() uint64 { return t.reclaimEpoch }

// IsDead reports whether this table has been reclaimed. Any access to a dead
// table via a retained *Table pointer is a programming error; callers should
// treat a true return as an internal panic trigger.
func (t *Table) IsDead() bool { return t.dead }

// MarkDead marks the table as reclaimed. After this call, retained *Table
// pointers must not be used to read entity or component data.
func (t *Table) MarkDead() { t.dead = true }

// FreeColumns releases column storage back to the runtime, dropping all row
// data. Called during reclamation after all observers have fired. The table
// pointer itself remains allocated but dead==true; any subsequent data access
// panics on the caller's side via IsDead checks.
func (t *Table) FreeColumns() {
	t.entities = nil
	t.columns = nil
	t.bitsets = nil
	t.addEdges = nil
	t.removeEdges = nil
}

// BumpChange increments the table's change counter. Called by the World after
// a column write (Set[T], SetByID, SetPair[T], SetPairByID) on an entity that
// already owned the component — structural changes are covered by Append and
// RemoveSwap. Conservative: any column write marks the table dirty for all
// cached queries that contain it (never under-reports, may over-report).
func (t *Table) BumpChange() { t.changeCount++ }

// Append adds a new zero-initialized row for entity and returns its row index.
// All non-tag component columns are extended by one zero element.
// Existing CanToggle bitsets are extended with the new row set to enabled (1).
func (t *Table) Append(entity ids.ID) int {
	t.emptyTicks = 0 // reset reclamation counter on first row insertion
	row := len(t.entities)
	t.entities = append(t.entities, entity)
	for _, col := range t.columns {
		if col != nil {
			col.appendZero()
		}
	}
	// Extend any existing bitsets: new row defaults to enabled (bit = 1).
	for id, bs := range t.bitsets {
		wordIdx := row >> 6
		bitIdx := uint(row) & 63
		if wordIdx >= len(bs) {
			t.bitsets[id] = append(bs, uint64(1)<<bitIdx)
		} else {
			bs[wordIdx] |= uint64(1) << bitIdx
		}
	}
	t.changeCount++
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
	// Update any CanToggle bitsets: swap last row's bit into the removed slot,
	// then shrink. Mirror of how columns use swap-remove.
	newN := n - 1
	newNW := (newN + 63) >> 6
	for id, bs := range t.bitsets {
		lastWordIdx := last >> 6
		lastBitIdx := uint(last) & 63
		if row != last {
			rowWordIdx := row >> 6
			rowBitIdx := uint(row) & 63
			lastBit := (bs[lastWordIdx] >> lastBitIdx) & 1
			if lastBit != 0 {
				bs[rowWordIdx] |= uint64(1) << rowBitIdx
			} else {
				bs[rowWordIdx] &^= uint64(1) << rowBitIdx
			}
		}
		bs[lastWordIdx] &^= uint64(1) << lastBitIdx
		if newNW == 0 {
			delete(t.bitsets, id)
		} else if newNW < len(bs) {
			t.bitsets[id] = bs[:newNW]
		}
	}
	t.changeCount++
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
// Idempotent if dst is the same pointer. Panics if a different, live *Table is
// already cached for (t, +id) — this indicates a correctness bug upstream.
// A stale dead-table entry is silently overwritten (reclamation may invalidate
// cached destinations).
// Not goroutine-safe; matches the Phase 1 single-threaded World invariant.
func (t *Table) CacheAddEdge(id ids.ID, dst *Table) {
	if t.addEdges == nil {
		t.addEdges = make(map[ids.ID]*Table)
	}
	if existing, ok := t.addEdges[id]; ok && existing != dst {
		if !existing.dead {
			panic("table: CacheAddEdge: conflicting destination for same (table, id)")
		}
		// Overwrite stale dead-table entry.
	}
	t.addEdges[id] = dst
}

// ColumnBasePtr returns the base pointer of column id's backing array, the
// element's reflect.Type, and the current row count.
// Returns (nil, nil, 0) for tag columns (Size==0).
//
// The pointer is invalidated by any subsequent Append (may grow the column) or
// RemoveSwap (may reorder rows). Do not retain it across those operations.
//
// Panics if id is not in the table's signature.
func (t *Table) ColumnBasePtr(id ids.ID) (unsafe.Pointer, reflect.Type, int) {
	idx, ok := t.colIndex[id]
	if !ok {
		panic("table: ColumnBasePtr: id not in signature")
	}
	col := t.columns[idx]
	if col == nil {
		return nil, nil, 0 // tag
	}
	n := len(t.entities)
	base, elemType := col.BaseUnsafe()
	runtime.KeepAlive(col)
	return base, elemType, n
}

// ColumnReflectSlice returns the backing slice for the column of id, sliced to
// the current row count. The returned reflect.Value has kind Slice and element
// type equal to the registered Go type for that component.
//
// For tag components (Size==0) the column is nil; the zero reflect.Value is
// returned. Callers must check rv.IsValid() before using it.
//
// The value is invalidated by any subsequent Append (may grow the column) or
// RemoveSwap (may reorder rows). Do not cache across those operations.
//
// Deprecated: use ColumnBasePtr for zero-alloc access. Retained for one cycle.
//
// Panics if id is not in the table's signature.
func (t *Table) ColumnReflectSlice(id ids.ID) reflect.Value {
	idx, ok := t.colIndex[id]
	if !ok {
		panic("table: ColumnReflectSlice: id not in signature")
	}
	col := t.columns[idx]
	if col == nil {
		return reflect.Value{} // tag column: no data
	}
	return col.slice.Slice(0, len(t.entities))
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
		if !existing.dead {
			panic("table: CacheRemoveEdge: conflicting destination for same (table, id)")
		}
		// Overwrite stale dead-table entry.
	}
	t.removeEdges[id] = dst
}

// DisableRow marks row as disabled for component id. Creates the bitset lazily
// on the first call (initialising all existing rows to enabled). No-op if row
// is already disabled.
//
// Panics if row is out of range.
func (t *Table) DisableRow(id ids.ID, row int) {
	if row < 0 || row >= len(t.entities) {
		panic("table: DisableRow row out of range")
	}
	if t.bitsets == nil {
		t.bitsets = make(map[ids.ID][]uint64)
	}
	bs, ok := t.bitsets[id]
	if !ok {
		n := len(t.entities)
		nw := (n + 63) >> 6
		bs = make([]uint64, nw)
		for i := range bs {
			bs[i] = ^uint64(0)
		}
		if n%64 != 0 {
			bs[nw-1] = (uint64(1) << uint(n%64)) - 1
		}
		t.bitsets[id] = bs
	}
	bs[row>>6] &^= uint64(1) << (uint(row) & 63)
}

// EnableRow marks row as enabled for component id. If no bitset exists for id
// the row is already considered enabled (default), so this is a no-op.
//
// Panics if row is out of range.
func (t *Table) EnableRow(id ids.ID, row int) {
	if row < 0 || row >= len(t.entities) {
		panic("table: EnableRow row out of range")
	}
	if t.bitsets == nil {
		return
	}
	bs, ok := t.bitsets[id]
	if !ok {
		return
	}
	bs[row>>6] |= uint64(1) << (uint(row) & 63)
}

// GetBitsetsCopy returns a deep copy of the CanToggle bitset map.
// Returns nil if no bitsets have been allocated.
func (t *Table) GetBitsetsCopy() map[ids.ID][]uint64 {
	if len(t.bitsets) == 0 {
		return nil
	}
	out := make(map[ids.ID][]uint64, len(t.bitsets))
	for id, words := range t.bitsets {
		cp := make([]uint64, len(words))
		copy(cp, words)
		out[id] = cp
	}
	return out
}

// SetBitsets directly replaces the table's bitset map with a deep copy of bs.
// Intended for snapshot restore only; normal code should use DisableRow/EnableRow.
func (t *Table) SetBitsets(bs map[ids.ID][]uint64) {
	if len(bs) == 0 {
		t.bitsets = nil
		return
	}
	t.bitsets = make(map[ids.ID][]uint64, len(bs))
	for id, words := range bs {
		cp := make([]uint64, len(words))
		copy(cp, words)
		t.bitsets[id] = cp
	}
}

// PruneUserEntities removes all entity rows whose raw index is >= firstUser
// from the entity column. Returns the removed entity IDs. Valid only for tables
// with no columns (i.e. the empty/root archetype table). The caller must update
// entity-index Record.Row for any remaining entities whose row position changed.
func (t *Table) PruneUserEntities(firstUser uint32) []ids.ID {
	var removed []ids.ID
	write := 0
	for _, e := range t.entities {
		if e.Index() >= firstUser {
			removed = append(removed, e)
		} else {
			t.entities[write] = e
			write++
		}
	}
	for i := write; i < len(t.entities); i++ {
		t.entities[i] = 0
	}
	t.entities = t.entities[:write]
	t.changeCount++
	return removed
}

// IsRowEnabled reports whether row is enabled for component id.
// Returns true when no bitset exists for id (all rows enabled by default).
//
// Panics if row is out of range.
func (t *Table) IsRowEnabled(id ids.ID, row int) bool {
	if row < 0 || row >= len(t.entities) {
		panic("table: IsRowEnabled row out of range")
	}
	if t.bitsets == nil {
		return true
	}
	bs, ok := t.bitsets[id]
	if !ok {
		return true
	}
	return (bs[row>>6]>>(uint(row)&63))&1 != 0
}
