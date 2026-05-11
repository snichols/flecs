package flecs

import (
	"context"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	index            *entityindex.Index
	registry         *component.Registry
	tables           map[string]*table.Table         // sigKey(sorted []ID) → table
	empty            *table.Table                    // canonical empty-signature table for new entities
	compIndex        *componentindex.Index           // reverse map: component ID → tables containing it
	observers        map[observerKey][]*observerNode // lazily allocated; keyed by (id, event)
	cachedQueries    []*CachedQuery                  // lazily allocated; notified on new table creation
	systems          []*System                       // lazily allocated; compacted in NewSystem
	childOfID        ID                              // built-in ChildOf relationship entity (index 1)
	isAID            ID                              // built-in IsA relationship entity (index 2)
	nameID           ID                              // built-in Name component entity (index 3)
	preUpdateID      ID                              // built-in PreUpdate phase entity (index 4)
	onUpdateID       ID                              // built-in OnUpdate phase entity (index 5)
	postUpdateID     ID                              // built-in PostUpdate phase entity (index 6)
	onFixedUpdateID  ID                              // built-in OnFixedUpdate phase entity (index 7; first user entity at index 8)
	exclusiveAccess  atomic.Uint64                   //nolint:unused // 0=unclaimed, goroutineID=owned, ^0=write-locked; see exclusive_access.go
	exclusiveThread  string                          //nolint:unused // human-readable label for the owner goroutine; set by ExclusiveAccessBegin
	readonly         atomic.Bool                     // when true, mutators enqueue instead of mutate
	deferMu          sync.Mutex                      // guards deferDepth and deferred; never held during system fn invocation
	deferDepth       int                             // nesting counter; 0 means "apply immediately"
	deferred         []func(w *World)                // queue of buffered operations; flushed when deferDepth reaches 0
	workerCount      int                             // number of persistent goroutines in the worker pool; 0 = serial
	workerCh         chan func()                     // job channel; nil when workerCount == 0
	inProgress       bool                            // true while Progress is executing
	time             float32                         // total accumulated simulation time
	frameCount       uint64                          // number of Progress calls
	fixedTimestep    float32                         // fixed step size; 0 means disabled
	fixedAccumulator float32                         // internal accumulator for fixed-step dispatch
	lastFramePhases  [4]PhaseStats                   // per-phase timing from the most recent Progress call
	logger           *slog.Logger                    // optional structured logger; nil means no logging
}

// New initializes and returns an empty World.
//
// Built-in entity allocation order:
//   - Index 0: null sentinel (never issued by Alloc)
//   - Index 1: ChildOf built-in relationship entity
//   - Index 2: IsA built-in relationship entity
//   - Index 3: Name built-in component entity
//   - Index 4: PreUpdate built-in pipeline phase entity
//   - Index 5: OnUpdate built-in pipeline phase entity
//   - Index 6: PostUpdate built-in pipeline phase entity
//   - Index 7: OnFixedUpdate built-in pipeline phase entity
//   - Index 8+: user entities (NewEntity)
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
	w.notifyTableCreated(w.empty) // no-op: cachedQueries is nil at construction
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
	// Allocate the built-in PreUpdate phase entity (gets index 4).
	preUpdate := w.index.Alloc()
	rec = w.index.Get(preUpdate)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(preUpdate))
	w.preUpdateID = preUpdate
	// Allocate the built-in OnUpdate phase entity (gets index 5).
	onUpdate := w.index.Alloc()
	rec = w.index.Get(onUpdate)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(onUpdate))
	w.onUpdateID = onUpdate
	// Allocate the built-in PostUpdate phase entity (gets index 6).
	postUpdate := w.index.Alloc()
	rec = w.index.Get(postUpdate)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(postUpdate))
	w.postUpdateID = postUpdate
	// Allocate the built-in OnFixedUpdate phase entity (gets index 7).
	onFixedUpdate := w.index.Alloc()
	rec = w.index.Get(onFixedUpdate)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(onFixedUpdate))
	w.onFixedUpdateID = onFixedUpdate
	return w
}

// PreUpdate returns the ID of the built-in PreUpdate pipeline phase entity.
// Systems in this phase run first in each Progress call.
func (w *World) PreUpdate() ID { return w.preUpdateID }

// OnUpdate returns the ID of the built-in OnUpdate pipeline phase entity.
// Systems in this phase run second in each Progress call. NewSystem defaults to this phase.
func (w *World) OnUpdate() ID { return w.onUpdateID }

// PostUpdate returns the ID of the built-in PostUpdate pipeline phase entity.
// Systems in this phase run last in each Progress call (after OnFixedUpdate).
func (w *World) PostUpdate() ID { return w.postUpdateID }

// OnFixedUpdate returns the ID of the built-in OnFixedUpdate pipeline phase entity.
// Systems in this phase are dispatched via a fixed-step accumulator loop inside
// each Progress call and always receive the fixed timestep as dt.
// Use SetFixedTimestep to configure the step; a zero step disables this phase.
func (w *World) OnFixedUpdate() ID { return w.onFixedUpdateID }

// Time returns the total simulated time accumulated across all Progress calls.
func (w *World) Time() float32 { return w.time }

// FrameCount returns the number of Progress calls made on this world.
func (w *World) FrameCount() uint64 { return w.frameCount }

// FixedTimestep returns the current fixed step size. Zero means disabled.
func (w *World) FixedTimestep() float32 { return w.fixedTimestep }

// SetFixedTimestep sets the fixed step size used by the OnFixedUpdate accumulator.
// A step of 0 disables OnFixedUpdate dispatch entirely. Panics if step < 0.
//
// Spiral-of-death risk: if the fixed step is smaller than the average frame dt,
// each Progress call must run multiple fixed iterations. If fixed systems are
// slow, the total wall time per frame grows, causing more iterations next frame,
// creating a positive-feedback loop that stalls the simulation. No guard is
// provided; callers must ensure the fixed step is reachable within one frame.
func (w *World) SetFixedTimestep(step float32) {
	if step < 0 {
		panic("flecs: SetFixedTimestep: step must be >= 0")
	}
	w.fixedTimestep = step
}

// SetWorkerCount sets the number of worker goroutines in the persistent pool
// used for parallel system dispatch. Zero (the default) disables parallel
// dispatch; all systems run serially on the calling goroutine.
//
// When n > 0, a buffered channel of size 2*n is created and n goroutines are
// started. Systems flagged with SetParallel(true) whose write sets are pairwise
// disjoint will be dispatched as concurrent jobs within each phase.
//
// Changing n between Progress calls tears down the old pool (goroutines exit
// when the old channel is drained and closed) and starts a new pool.
//
// Calling SetWorkerCount during an active Progress call is a no-op.
//
// Panics if n < 0.
func (w *World) SetWorkerCount(n int) {
	if n < 0 {
		panic("flecs: SetWorkerCount: n must be >= 0")
	}
	if w.inProgress {
		return
	}
	if w.workerCh != nil {
		close(w.workerCh)
		w.workerCh = nil
	}
	w.workerCount = n
	if n > 0 {
		ch := make(chan func(), 2*n)
		w.workerCh = ch
		for range n {
			go func() {
				for fn := range ch {
					fn()
				}
			}()
		}
	}
}

// WorkerCount returns the current number of worker goroutines. Zero means
// serial dispatch (the default).
func (w *World) WorkerCount() int { return w.workerCount }

// NewEntity allocates a new entity, places it in the empty-signature table,
// and returns its ID.
func (w *World) NewEntity() ID {
	w.checkExclusiveAccessWrite()
	e := w.index.Alloc()
	rec := w.index.Get(e)
	rec.Table = w.empty
	rec.Row = uint32(w.empty.Append(e))
	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "entity created",
			slog.Uint64("id", uint64(e)))
	}
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
	freed := w.index.Free(e)
	if freed && w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "entity deleted",
			slog.Uint64("id", uint64(e)))
	}
	return freed
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
	w.checkExclusiveAccessWrite()
	w.deferMu.Lock()
	if w.deferDepth > 0 || w.readonly.Load() {
		if !w.index.IsAlive(e) {
			w.deferMu.Unlock()
			return false
		}
		w.deferred = append(w.deferred, func(w *World) {
			deleteImmediate(w, e)
		})
		w.deferMu.Unlock()
		return true
	}
	w.deferMu.Unlock()
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
func (w *World) IsAlive(e ID) bool {
	w.checkExclusiveAccessRead()
	return w.index.IsAlive(e)
}

// Count returns the number of currently alive entities (including component entities).
func (w *World) Count() int {
	w.checkExclusiveAccessRead()
	return w.index.Count()
}

// RegisterComponent registers T as a component-entity in w and returns its ID.
// Idempotent: if T is already registered with a component ID, returns that ID.
// The component itself is an entity, mirroring the flecs convention that
// components are first-class entities.
func RegisterComponent[T any](w *World) ID {
	w.checkExclusiveAccessWrite()
	info, ok := component.LookupByType[T](w.registry)
	if ok && info.Component != 0 {
		return info.Component
	}
	if !ok {
		info = component.Register[T](w.registry)
	}
	id := w.index.Alloc()
	w.registry.AssociateID(info, id)
	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "component registered",
			slog.String("name", info.Name),
			slog.Uint64("id", uint64(id)),
			slog.Uint64("size", uint64(info.Size)))
	}
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
	w.checkExclusiveAccessWrite()
	w.deferMu.Lock()
	if w.deferDepth > 0 || w.readonly.Load() {
		captured := v
		w.deferred = append(w.deferred, func(w *World) {
			setImmediate[T](w, e, captured)
		})
		w.deferMu.Unlock()
		return
	}
	w.deferMu.Unlock()
	setImmediate[T](w, e, v)
}

func setImmediate[T any](w *World, e ID, v T) {
	cid := RegisterComponent[T](w)
	info, _ := component.LookupByType[T](w.registry)
	setImmediateByPtr(w, e, cid, unsafe.Pointer(&v), info)
}

// Get returns the value of component T on entity e. Checks the entity's own
// table first (Owns semantics); on a miss, walks the IsA chain transitively.
// Returns (zero, false) if T is not registered, e is not alive, or no IsA
// path yields T. Does NOT auto-register T.
func Get[T any](w *World, e ID) (T, bool) {
	w.checkExclusiveAccessRead()
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
	// Local miss: walk the IsA chain. seen is allocated lazily inside getViaIsA
	// only when an IsA pair is found, avoiding a map allocation in the common case.
	return getViaIsA[T](w, e, info.Component, nil)
}

// Has reports whether entity e has component T — locally or via an IsA chain.
// Auto-registers T so the answer is meaningful; an unregistered type yields false.
func Has[T any](w *World, e ID) bool {
	w.checkExclusiveAccessRead()
	cid := RegisterComponent[T](w)
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	t := rec.Table
	if t != nil && t.HasComponent(cid) {
		return true
	}
	// seen is allocated lazily inside hasViaIsA when an IsA pair is found.
	return hasViaIsA(w, e, cid, nil)
}

// Owns reports whether entity e locally owns component T — T is present in
// e's own archetype table rather than inherited via an IsA chain.
// Auto-registers T (matches Has[T] policy). Returns false if e is not alive.
func Owns[T any](w *World, e ID) bool {
	w.checkExclusiveAccessRead()
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
	w.checkExclusiveAccessWrite()
	w.deferMu.Lock()
	if w.deferDepth > 0 || w.readonly.Load() {
		info, ok := component.LookupByType[T](w.registry)
		if !ok || info.Component == 0 {
			w.deferMu.Unlock()
			return false
		}
		rec := w.index.Get(e)
		if rec == nil || rec.Table == nil || !rec.Table.HasComponent(info.Component) {
			w.deferMu.Unlock()
			return false
		}
		w.deferred = append(w.deferred, func(w *World) {
			removeImmediate[T](w, e)
		})
		w.deferMu.Unlock()
		return true
	}
	w.deferMu.Unlock()
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

	var oldSig []ID
	if oldTable != nil {
		oldSig = oldTable.Type()
	}

	// Consult the edge cache BEFORE computing newSig so that cache hits pay
	// zero allocation cost (no make([]ID, ...) on the fast path).
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
		// Cache miss: compute the new sorted signature and look up (or create)
		// the destination table. Never mutate the slice returned by Type().
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
			// Notify cached queries that a new table is available. Fires once
			// per newly-created table, after full registration. Hot path: do
			// NOT compact w.cachedQueries here.
			w.notifyTableCreated(newTable)
		}
		// Cache the edge for future single-component transitions from oldTable.
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
	w.checkExclusiveAccessRead()
	return w.compIndex.TablesFor(componentID)
}

// EachTableFor calls fn for every archetype table containing componentID, in
// registration order. fn returns false to stop iteration early. No allocation
// is performed; this is the hot path for Phase 3 query iteration.
func (w *World) EachTableFor(componentID ID, fn func(*table.Table) bool) {
	w.checkExclusiveAccessRead()
	w.compIndex.Each(componentID, fn)
}

// eachAlive calls fn for every currently alive entity, in dense order.
// Callbacks must not call Alloc or Free (i.e. NewEntity, Delete) during iteration.
func (w *World) eachAlive(fn func(ID)) {
	w.index.Each(func(id ID, _ *entityindex.Record) {
		fn(id)
	})
}

// notifyTableCreated walks w.cachedQueries and calls tryMatchTable on each
// active (non-removed) entry. Called once per newly-created table, after the
// table is fully registered in w.tables and w.compIndex.
//
// Compaction of removed entries is intentionally skipped here (hot path);
// it happens lazily in NewCachedQuery. Defensively handles nil cachedQueries.
func (w *World) notifyTableCreated(t *table.Table) {
	for _, cq := range w.cachedQueries {
		if !cq.removed {
			cq.tryMatchTable(t)
		}
	}
	if w.logger != nil {
		sig := t.Type()
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "table created",
			slog.Int("signature_len", len(sig)),
			slog.String("signature", formatSig(sig)))
	}
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

// SetLogger installs l as the structured logger for lifecycle events.
// Passing nil disables logging. When set, the logger receives DEBUG-level
// records for the following lifecycle events: entity created, entity deleted,
// component registered, table created, system added, system closed, observer
// registered, observer unsubscribed, snapshot serialized, snapshot loaded.
//
// No logs fire on hot paths: Set/Get/Has/Each/Progress and similar read or
// per-frame operations are intentionally excluded for performance.
//
// Note: lifecycle events that occur inside World.New (empty table creation,
// built-in entity allocation) are not logged because SetLogger cannot be
// called until after New() returns.
func (w *World) SetLogger(l *slog.Logger) { w.logger = l }

// Logger returns the current logger, or nil if none is installed.
func (w *World) Logger() *slog.Logger { return w.logger }

// formatSig returns a space-separated string of decimal component IDs for use
// as the "signature" attribute on "table created" log records.
func formatSig(sig []ID) string {
	if len(sig) == 0 {
		return ""
	}
	var b strings.Builder
	for i, id := range sig {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(strconv.FormatUint(uint64(id), 10))
	}
	return b.String()
}
