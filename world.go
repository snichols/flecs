package flecs

import (
	"sort"
	"unsafe"

	"github.com/snichols/flecs/internal/component"
	"github.com/snichols/flecs/internal/storage/componentindex"
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
	index      *entityindex.Index
	registry   *component.Registry
	tables     map[string]*table.Table         // sigKey(sorted []ID) → table
	empty      *table.Table                    // canonical empty-signature table for new entities
	compIndex  *componentindex.Index           // reverse map: component ID → tables containing it
	observers  map[observerKey][]*observerNode // lazily allocated; keyed by (id, event)
	childOfID  ID                              // built-in ChildOf relationship entity (index 1)
	isAID      ID                              // built-in IsA relationship entity (index 2)
	nameID     ID                              // built-in Name component entity (index 3; user entities start at index 4)
	deferDepth int                             // nesting counter; 0 means "apply immediately"
	deferred   []func(w *World)                // queue of buffered operations; flushed when deferDepth reaches 0
}

// New initializes and returns an empty World.
//
// Built-in entity allocation order:
//   - Index 0: null sentinel (never issued by Alloc)
//   - Index 1: ChildOf built-in relationship entity
//   - Index 2: IsA built-in relationship entity
//   - Index 3: Name built-in component entity
//   - Index 4+: user entities (NewEntity)
func New() *World {
	w := &World{
		index:     entityindex.New(),
		registry:  component.NewRegistry(),
		tables:    make(map[string]*table.Table),
		compIndex: componentindex.New(),
	}
	w.empty = table.New([]ID{}, []*component.TypeInfo{})
	w.tables[sigKey(nil)] = w.empty
	for _, id := range w.empty.Type() {
		w.compIndex.Register(id, w.empty)
	}
	// Allocate the built-in ChildOf relationship entity (gets index 1).
	childOf := w.index.Alloc()
	rec := w.index.Get(childOf)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(childOf))
	w.childOfID = childOf
	// Allocate the built-in IsA relationship entity (gets index 2).
	isA := w.index.Alloc()
	rec = w.index.Get(isA)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(isA))
	w.isAID = isA
	// Register the built-in Name component (gets index 3).
	w.nameID = RegisterComponent[Name](w)
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

// deleteOne removes a single entity from its archetype table and frees its ID.
// It is the primitive used by both Delete (non-parent case) and the cascade
// delete orchestrator. Returns true if e was alive.
// Fires OnRemove for each component in e's current table before removal.
func (w *World) deleteOne(e ID) bool {
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	t := rec.Table
	row := int(rec.Row)
	if t != nil {
		for _, id := range t.Type() {
			info, _ := w.registry.LookupByID(id)
			w.fireOnRemove(info, id, e, t.Get(row, id))
		}
		moved, ok := t.RemoveSwap(row)
		if ok {
			movedRec := w.index.Get(moved)
			movedRec.Row = uint32(row)
		}
	}
	return w.index.Free(e)
}

// Delete removes entity e and all entities related to it via (ChildOf, e) pairs,
// recursively. Deletion is post-order: children are deleted before their parents,
// leaves before any internal node.
//
// Returns true if e was alive. Returns false immediately with no cascade if e
// is not alive, preserving Phase 1.5 semantics.
//
// Within a deferred block, the operation is queued if e is currently alive;
// the cascade runs during flush in the order the Delete was queued.
//
// A cycle guard (seen map) prevents infinite loops for self-referential hierarchies.
func (w *World) Delete(e ID) bool {
	if w.deferDepth > 0 {
		if !w.index.IsAlive(e) {
			return false
		}
		w.deferred = append(w.deferred, func(w *World) {
			deleteImmediate(w, e)
		})
		return true
	}
	return deleteImmediate(w, e)
}

func deleteImmediate(w *World, e ID) bool {
	if !w.index.IsAlive(e) {
		return false
	}

	// Collect e and all descendants via iterative DFS with cycle detection.
	stack := []ID{e}
	var toDelete []ID
	seen := make(map[ID]struct{})
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := seen[node]; ok {
			continue
		}
		seen[node] = struct{}{}
		toDelete = append(toDelete, node)
		pairID := MakePair(w.childOfID, node)
		for _, t := range w.compIndex.TablesFor(pairID) {
			// Snapshot the entity list before any deleteOne calls mutate the table.
			entities := append([]ID(nil), t.Entities()...)
			for _, child := range entities {
				if w.index.IsAlive(child) {
					stack = append(stack, child)
				}
			}
		}
	}

	// Delete in post-order: deepest descendants first, root last.
	for i := len(toDelete) - 1; i >= 0; i-- {
		w.deleteOne(toDelete[i])
	}
	return true
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
// If e already has T, the existing value is overwritten in place (fires OnSet).
// Otherwise an archetype migration moves e to the table for its new component set
// (fires OnAdd then OnSet).
//
// Within a deferred block (DeferBegin/DeferEnd or Defer), the operation is
// queued and applied on DeferEnd. Reads (Get/Has/Owns/IsAlive) still see the
// CURRENT state, not the deferred future state.
func Set[T any](w *World, e ID, v T) {
	if w.deferDepth > 0 {
		captured := v
		w.deferred = append(w.deferred, func(w *World) {
			setImmediate[T](w, e, captured)
		})
		return
	}
	setImmediate[T](w, e, v)
}

func setImmediate[T any](w *World, e ID, v T) {
	cid := RegisterComponent[T](w)
	rec := w.index.Get(e)
	if rec == nil {
		panic("flecs: Set called on dead entity")
	}
	t := rec.Table
	if t != nil && t.HasComponent(cid) {
		t.Set(int(rec.Row), cid, unsafe.Pointer(&v))
		info, _ := component.LookupByType[T](w.registry)
		w.fireOnSet(info, cid, e, t.Get(int(rec.Row), cid))
		return
	}
	w.migrate(e, cid, 0, unsafe.Pointer(&v))
	// OnAdd fired inside migrate; fire OnSet now that the slot is written.
	rec = w.index.Get(e)
	info, _ := component.LookupByType[T](w.registry)
	w.fireOnSet(info, cid, e, rec.Table.Get(int(rec.Row), cid))
}

// Get returns the value of component T on entity e. Checks the entity's own
// table first (Owns semantics); on a miss, walks the IsA chain transitively.
// Returns (zero, false) if T is not registered, e is not alive, or no IsA
// path yields T. Does NOT auto-register T.
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
	if t != nil && t.HasComponent(info.Component) {
		ptr := t.Get(int(rec.Row), info.Component)
		if ptr == nil {
			return zero, true
		}
		return *(*T)(ptr), true
	}
	// Local miss: walk the IsA chain.
	seen := map[ID]struct{}{e: {}}
	return getViaIsA[T](w, e, info.Component, seen)
}

// Has reports whether entity e has component T — locally or via an IsA chain.
// Auto-registers T so the answer is meaningful; an unregistered type yields false.
func Has[T any](w *World, e ID) bool {
	cid := RegisterComponent[T](w)
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	t := rec.Table
	if t != nil && t.HasComponent(cid) {
		return true
	}
	seen := map[ID]struct{}{e: {}}
	return hasViaIsA(w, e, cid, seen)
}

// Owns reports whether entity e locally owns component T — T is present in
// e's own archetype table rather than inherited via an IsA chain.
// Auto-registers T (matches Has[T] policy). Returns false if e is not alive.
func Owns[T any](w *World, e ID) bool {
	cid := RegisterComponent[T](w)
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	return rec.Table != nil && rec.Table.HasComponent(cid)
}

// Remove removes component T from entity e.
// Returns true if T was present and has been removed, false if e was dead or
// lacked T. If removal empties the component set, e moves to the empty table.
//
// Within a deferred block, the operation is queued; returns true if T is
// currently present on e (at queue time).
func Remove[T any](w *World, e ID) bool {
	if w.deferDepth > 0 {
		info, ok := component.LookupByType[T](w.registry)
		if !ok || info.Component == 0 {
			return false
		}
		rec := w.index.Get(e)
		if rec == nil || rec.Table == nil || !rec.Table.HasComponent(info.Component) {
			return false
		}
		w.deferred = append(w.deferred, func(w *World) {
			removeImmediate[T](w, e)
		})
		return true
	}
	return removeImmediate[T](w, e)
}

func removeImmediate[T any](w *World, e ID) bool {
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
	// For single-component transitions, consult the edge cache first to avoid
	// the sigKey encode + map lookup on repeated migrations of the same shape.
	var newTable *table.Table

	switch {
	case removeID != 0 && addID == 0 && oldTable != nil:
		if dst, ok := oldTable.NextOnRemove(removeID); ok {
			newTable = dst
		}
	case addID != 0 && removeID == 0 && oldTable != nil:
		if dst, ok := oldTable.NextOnAdd(addID); ok {
			newTable = dst
		}
	}

	if newTable == nil {
		key := sigKey(newSig)
		var exists bool
		newTable, exists = w.tables[key]
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
			for _, id := range newTable.Type() {
				w.compIndex.Register(id, newTable)
			}
		}
		// Cache the result for future single-component transitions from oldTable.
		if oldTable != nil {
			switch {
			case removeID != 0 && addID == 0:
				oldTable.CacheRemoveEdge(removeID, newTable)
			case addID != 0 && removeID == 0:
				oldTable.CacheAddEdge(addID, newTable)
			}
		}
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

	// Fire OnAdd for the newly-added component — destination slot is fully written.
	if addID != 0 {
		addInfo, _ := w.registry.LookupByID(addID)
		w.fireOnAdd(addInfo, addID, e, newTable.Get(newRow, addID))
	}

	// Fire OnRemove for the removed component — source slot still intact.
	if removeID != 0 && oldTable != nil {
		remInfo, _ := w.registry.LookupByID(removeID)
		w.fireOnRemove(remInfo, removeID, e, oldTable.Get(oldRow, removeID))
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

// TablesFor returns a snapshot of all archetype tables that contain
// componentID, in registration order. Returns an empty (non-nil) slice when no
// tables are registered for componentID.
func (w *World) TablesFor(componentID ID) []*table.Table {
	return w.compIndex.TablesFor(componentID)
}

// EachTableFor calls fn for every archetype table containing componentID, in
// registration order. fn returns false to stop iteration early. No allocation
// is performed; this is the hot path for Phase 3 query iteration.
func (w *World) EachTableFor(componentID ID, fn func(*table.Table) bool) {
	w.compIndex.Each(componentID, fn)
}

// eachAlive calls fn for every currently alive entity, in dense order.
// Callbacks must not call Alloc or Free (i.e. NewEntity, Delete) during iteration.
func (w *World) eachAlive(fn func(ID)) {
	w.index.Each(func(id ID, _ *entityindex.Record) {
		fn(id)
	})
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
