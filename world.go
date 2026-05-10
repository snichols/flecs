package flecs

import (
	"sort"
	"unsafe"

	"github.com/snichols/flecs/internal/component"
	"github.com/snichols/flecs/internal/storage/entityindex"
	"github.com/snichols/flecs/internal/storage/table"
)

// World is the central ECS object. It owns entities (keyed by ID), component
// metadata (in a Registry), and archetype tables (keyed by sorted component-ID
// signature). Components are first-class entities; each registered component
// type is itself allocated an entity ID.
//
// Archetype storage: entities that share the same component set are grouped into
// a Table, stored in structure-of-arrays columns. Changing the component set of
// an entity triggers an archetype migration: the entity moves to the table whose
// signature matches the new set.
//
// Signature key encoding: a sorted []ID is encoded as raw bytes
// (8 bytes per uint64 ID, host byte-order) for use as a map key. This encoding
// is stable within a single process but not across processes or machines.
//
// *World is NOT goroutine-safe; external synchronization is required.
type World struct {
	index    *entityindex.Index
	registry *component.Registry
	tables   map[string]*table.Table // sigKey(sorted []ID) → table
	empty    *table.Table            // canonical empty-signature table for new entities
}

// New initializes and returns an empty World.
func New() *World {
	w := &World{
		index:    entityindex.New(),
		registry: component.NewRegistry(),
		tables:   make(map[string]*table.Table),
	}
	w.empty = table.New([]ID{}, []*component.TypeInfo{})
	w.tables[sigKey(nil)] = w.empty
	return w
}

// NewEntity allocates a new entity, places it in the empty-signature table,
// and returns its ID.
func (w *World) NewEntity() ID {
	e := w.index.Alloc()
	rec := w.index.Get(e)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(e))
	return e
}

// Delete removes entity e from its archetype table and frees its ID.
// Returns true if e was alive. After deletion, IsAlive(e) is false.
//
// If another entity occupied the last row of e's table, RemoveSwap moves it
// into e's vacated row; Delete updates that entity's Record.Row accordingly.
func (w *World) Delete(e ID) bool {
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	t := rec.Table
	row := int(rec.Row)
	if t != nil {
		moved, ok := t.RemoveSwap(row)
		if ok {
			movedRec := w.index.Get(moved)
			movedRec.Row = uint32(row)
		}
	}
	return w.index.Free(e)
}

// IsAlive reports whether e is currently alive.
func (w *World) IsAlive(e ID) bool { return w.index.IsAlive(e) }

// Count returns the number of currently alive entities (including component entities).
func (w *World) Count() int { return w.index.Count() }

// RegisterComponent registers T as a component-entity in w and returns its ID.
// Idempotent: if T is already registered with a component ID, returns that ID.
// The component itself is an entity, mirroring the flecs convention that
// components are first-class entities.
func RegisterComponent[T any](w *World) ID {
	info, ok := component.LookupByType[T](w.registry)
	if ok && info.Component != 0 {
		return info.Component
	}
	if !ok {
		info = component.Register[T](w.registry)
	}
	id := w.index.Alloc()
	w.registry.AssociateID(info, id)
	return id
}

// Set writes value v as component T on entity e.
// If T is not yet registered, it is auto-registered. Panics if e is not alive.
// If e already has T, the existing value is overwritten in place.
// Otherwise an archetype migration moves e to the table for its new component set.
func Set[T any](w *World, e ID, v T) {
	cid := RegisterComponent[T](w)
	rec := w.index.Get(e)
	if rec == nil {
		panic("flecs: Set called on dead entity")
	}
	t := rec.Table
	if t != nil && t.HasComponent(cid) {
		t.Set(int(rec.Row), cid, unsafe.Pointer(&v))
		return
	}
	w.migrate(e, cid, 0, unsafe.Pointer(&v))
}

// Get returns the value of component T on entity e.
// Returns (zero, false) if T is not registered, e is not alive, or e lacks T.
// Does NOT auto-register T: a Get on an unregistered type is a not-found, not a side effect.
func Get[T any](w *World, e ID) (T, bool) {
	var zero T
	info, ok := component.LookupByType[T](w.registry)
	if !ok || info.Component == 0 {
		return zero, false
	}
	rec := w.index.Get(e)
	if rec == nil {
		return zero, false
	}
	t := rec.Table
	if t == nil || !t.HasComponent(info.Component) {
		return zero, false
	}
	ptr := t.Get(int(rec.Row), info.Component)
	if ptr == nil {
		// Tag component (Size==0): entity has it but there is no data.
		return zero, true
	}
	return *(*T)(ptr), true
}

// Has reports whether entity e has component T.
// Auto-registers T so the answer is meaningful; an unregistered type yields false.
func Has[T any](w *World, e ID) bool {
	cid := RegisterComponent[T](w)
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	t := rec.Table
	return t != nil && t.HasComponent(cid)
}

// Remove removes component T from entity e.
// Returns true if T was present and has been removed, false if e was dead or
// lacked T. If removal empties the component set, e moves to the empty table.
func Remove[T any](w *World, e ID) bool {
	info, ok := component.LookupByType[T](w.registry)
	if !ok || info.Component == 0 {
		return false
	}
	cid := info.Component
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	t := rec.Table
	if t == nil || !t.HasComponent(cid) {
		return false
	}
	w.migrate(e, 0, cid, nil)
	return true
}

// migrate moves entity e to the archetype table for the new component set
// computed as: currentSet ∪ {addID} \ {removeID}.
//
// Either addID or removeID may be 0 (no-op for that side). copyValue, if non-nil,
// is the value to write for addID in the destination table; the old table's value
// for addID (if any) is NOT carried over — copyValue always wins.
//
// Row-tracking invariant: after RemoveSwap on the source table, the entity that
// was in the last row is now at the vacated row; its Record.Row is updated to
// reflect the new position. Failure to maintain this invariant causes subsequent
// Get/Set/Has/Remove operations to read/write the wrong row.
func (w *World) migrate(e ID, addID, removeID ID, copyValue unsafe.Pointer) {
	rec := w.index.Get(e)
	oldTable := rec.Table
	oldRow := int(rec.Row)

	// Compute new signature: current ∪ {addID} \ {removeID}.
	// Never mutate the slice returned by Type().
	var oldSig []ID
	if oldTable != nil {
		oldSig = oldTable.Type()
	}

	newSig := make([]ID, 0, len(oldSig)+1)
	for _, id := range oldSig {
		if id == removeID {
			continue
		}
		newSig = append(newSig, id)
	}
	if addID != 0 {
		// Insert addID in sorted position to maintain the sorted-ascending invariant.
		pos := sort.Search(len(newSig), func(i int) bool { return newSig[i] >= addID })
		newSig = append(newSig, 0)
		copy(newSig[pos+1:], newSig[pos:])
		newSig[pos] = addID
	}

	// Look up or create the destination table.
	key := sigKey(newSig)
	newTable, exists := w.tables[key]
	if !exists {
		types := make([]*component.TypeInfo, len(newSig))
		for i, id := range newSig {
			info, ok := w.registry.LookupByID(id)
			if !ok {
				panic("flecs: migrate: component ID not registered")
			}
			types[i] = info
		}
		newTable = table.New(newSig, types)
		w.tables[key] = newTable
	}

	// Append a new zero-initialized row for e in the destination table.
	newRow := newTable.Append(e)

	// Carry component data from the old table to the new one.
	// Skip the removed component (not present in new table) and the added
	// component (its value comes from copyValue, not from the source).
	if oldTable != nil {
		for _, id := range oldSig {
			if id == removeID || id == addID {
				continue
			}
			if !newTable.HasComponent(id) {
				continue
			}
			ptr := oldTable.Get(oldRow, id)
			if ptr != nil {
				newTable.Set(newRow, id, ptr)
			}
		}
	}

	// Write the new component value, if any.
	if addID != 0 && copyValue != nil {
		newTable.Set(newRow, addID, copyValue)
	}

	// Remove e from the old table using swap-remove.
	// If another entity was in the last row it has been moved to oldRow;
	// update its record so future operations find it at the correct position.
	if oldTable != nil {
		moved, ok := oldTable.RemoveSwap(oldRow)
		if ok {
			movedRec := w.index.Get(moved)
			movedRec.Row = uint32(oldRow)
		}
	}

	// Update the migrating entity's record to point at the new location.
	rec.Table = newTable
	rec.Row = uint32(newRow)
}

// sigKey encodes a sorted []ID as a string map key.
// Each ID is stored as 8 raw bytes (host byte-order). The empty signature
// encodes as the empty string "".
func sigKey(sig []ID) string {
	if len(sig) == 0 {
		return ""
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(&sig[0])), len(sig)*8))
}
