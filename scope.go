package flecs

import (
	"errors"
	"fmt"
	"reflect"
	"unsafe"

	"github.com/snichols/flecs/internal/component"
	"github.com/snichols/flecs/internal/storage/table"
)

// ErrExclusiveAccessViolation is returned (as a panic value) when Write is
// called from a goroutine that is not the current exclusive-access owner.
var ErrExclusiveAccessViolation = errors.New("flecs: exclusive access violation: Write called from different goroutine while world is claimed")

// ErrMergeReentry is returned (as a panic value) when Write is called
// re-entrantly from inside a pre- or post-merge hook callback. A merge is
// already in progress; re-entering Write would corrupt the command-queue
// depth tracking.
var ErrMergeReentry = errors.New("flecs: merge reentry: Write called from inside a pre- or post-merge hook")

// Reader is a scoped capability for reading committed world state.
// Obtained via world.Read(fn). Invalid outside the callback.
//
// This type models the read half of C flecs's polymorphic ecs_world_t* pattern.
// In Go we make the capability explicit via Reader/Writer.
type Reader struct {
	world *World
}

// Writer is a scoped capability for reading AND writing.
// Obtained via world.Write(fn), or it.Writer() inside a system fn.
// A Writer is-a Reader: all *Reader operations work on *Writer via embedding.
//
// All structural mutations are queued in a defer scope and flushed when fn returns.
type Writer struct {
	Reader
	stage      *stage // routes mutations to this stage's command queue
	scopeStack []ID   // per-Writer entity scope stack; auto-(ChildOf,top) on NewEntity/RangeNew
}

// scope is satisfied by both *Reader and *Writer, allowing read free-functions
// (Each1, Each2, Get, Has, etc.) to be called directly from inside a Write scope
// without an explicit AsReader() downgrade. Unexported: users pass *Reader or *Writer.
type scope interface {
	scopeWorld() *World
}

func (r *Reader) scopeWorld() *World { return r.world }
func (w *Writer) scopeWorld() *World { return w.world }

// ── Reader methods ────────────────────────────────────────────────────────────

// IsAlive reports whether e is currently alive.
func (r *Reader) IsAlive(e ID) bool {
	return r.world.index.IsAlive(e)
}

// Count returns the number of currently alive entities (including component entities).
func (r *Reader) Count() int {
	return r.world.index.Count()
}

// TablesFor returns a snapshot of all archetype tables that contain componentID.
func (r *Reader) TablesFor(id ID) []*table.Table {
	return r.world.compIndex.TablesFor(id)
}

// EachTableFor calls fn for every archetype table containing componentID.
func (r *Reader) EachTableFor(id ID, fn func(*table.Table) bool) {
	r.world.compIndex.Each(id, fn)
}

// HasID reports whether entity e has the component or tag identified by id —
// locally or via an IsA chain.
//
// Reflexive self-pair extension: if id is a pair (R, e) and R is marked
// [Reflexive], this returns true even when no self-pair is stored. The
// first == second gate is evaluated before the policy map lookup so that
// non-self queries pay zero extra cost (no map access).
func (r *Reader) HasID(e ID, id ID) bool {
	w := r.world
	if id.IsPair() {
		relKey := ID(id.First().Index())
		// Parent-storage pair: consult parent column instead of archetype.
		if w.parentStoragePolicies[relKey] {
			rec := w.index.Get(e)
			if rec == nil || rec.Table == nil {
				return false
			}
			marker := w.parentStorageMarker(id.First())
			if !rec.Table.HasComponent(marker) {
				return false
			}
			if isWildcardID(w, id.Second()) {
				return true
			}
			parent, ok := rec.Table.GetParentEntry(int(rec.Row), relKey)
			return ok && parent.Index() == id.Second().Index()
		}
		// Union pair: consult union store (pairs never appear in the archetype type).
		if w.unionPolicies[relKey] {
			store, ok := w.unionStore[relKey]
			if !ok {
				return false
			}
			pos, has := store.index[ID(e.Index())]
			if !has {
				return false
			}
			termTarget := id.Second()
			if isWildcardID(w, termTarget) {
				return true
			}
			return store.dense[pos].target.Index() == termTarget.Index()
		}
	}
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	if rec.Table != nil && rec.Table.HasComponent(id) {
		return true
	}
	// Reflexive self-pair: if the query is (R, e) and R is reflexive, e
	// implicitly has the pair without a stored record (R(X,X) == true).
	// Gate on the target == entity check before touching the policy map so
	// that non-self HasID calls pay zero extra cost (no map access).
	if id.IsPair() {
		second := id.Second() // target entity index (generation-stripped by MakePair)
		if second == ID(e.Index()) {
			if w.reflexivePolicies[id.First()] {
				return true
			}
		}
	}
	return hasViaIsAPooled(w, e, id)
}

// OwnsID reports whether entity e locally owns the component or tag identified
// by id. Local-only: does not walk the IsA chain.
func (r *Reader) OwnsID(e ID, id ID) bool {
	w := r.world
	if id.IsPair() {
		relKey := ID(id.First().Index())
		// Parent-storage pair: consult parent column instead of archetype.
		if w.parentStoragePolicies[relKey] {
			rec := w.index.Get(e)
			if rec == nil || rec.Table == nil {
				return false
			}
			marker := w.parentStorageMarker(id.First())
			if !rec.Table.HasComponent(marker) {
				return false
			}
			if isWildcardID(w, id.Second()) {
				return true
			}
			parent, ok := rec.Table.GetParentEntry(int(rec.Row), relKey)
			return ok && parent.Index() == id.Second().Index()
		}
		// Union pair: consult union store (pairs never appear in the archetype type).
		if w.unionPolicies[relKey] {
			store, ok := w.unionStore[relKey]
			if !ok {
				return false
			}
			pos, has := store.index[ID(e.Index())]
			if !has {
				return false
			}
			termTarget := id.Second()
			if isWildcardID(w, termTarget) {
				return true
			}
			return store.dense[pos].target.Index() == termTarget.Index()
		}
	}
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	return rec.Table != nil && rec.Table.HasComponent(id)
}

// ParentOf returns the parent of entity e: the target of the first
// (ChildOf, *) pair found in e's archetype signature or parent column.
func (r *Reader) ParentOf(e ID) (ID, bool) {
	return r.world.ParentOf(e)
}

// EachChild calls fn for every direct child of parent.
// If parent has the [OrderedChildren] trait, children are visited in insertion order.
func (r *Reader) EachChild(parent ID, fn func(ID) bool) {
	w := r.world
	if w.orderedChildren != nil {
		if list, ok := w.orderedChildren[ID(parent.Index())]; ok {
			snapshot := append([]ID(nil), list.entries...)
			for _, child := range snapshot {
				if !fn(child) {
					return
				}
			}
			return
		}
	}
	pairID := MakePair(w.childOfID, parent)
	w.compIndex.Each(pairID, func(t *table.Table) bool {
		for _, child := range t.Entities() {
			if !fn(child) {
				return false
			}
		}
		return true
	})
}

// PrefabOf returns the first IsA prefab of entity e.
func (r *Reader) PrefabOf(e ID) (ID, bool) {
	return prefabOfInternal(r.world, e)
}

// EachPrefab calls fn for every direct IsA prefab of entity e.
func (r *Reader) EachPrefab(e ID, fn func(prefab ID) bool) {
	rec := r.world.index.Get(e)
	if rec == nil || rec.Table == nil {
		return
	}
	eachPairTarget(rec.Table.Type(), r.world.isAID.Index(), fn)
}

// Lookup resolves a dot-separated path and returns the entity at the leaf.
func (r *Reader) Lookup(path string) (ID, bool) {
	return r.world.Lookup(path)
}

// LookupChild finds the direct child of parent with the given name.
func (r *Reader) LookupChild(parent ID, name string) (ID, bool) {
	return r.world.LookupChild(parent, name)
}

// PathOf reconstructs e's dot-separated path from the root.
func (r *Reader) PathOf(e ID) string {
	return r.world.PathOf(e)
}

// GetName returns the name of entity e.
func (r *Reader) GetName(e ID) (string, bool) {
	n, ok := Get[Name](r, e)
	if !ok || n.Value == "" {
		return "", false
	}
	return n.Value, true
}

// Components returns a fresh slice of all registered component IDs.
func (r *Reader) Components() []ID {
	return r.world.registry.IDs()
}

// Unit returns the Unit descriptor for unitID, if it is a registered unit entity.
func (r *Reader) Unit(unitID ID) (Unit, bool) {
	u, ok := r.world.unitDefs[unitID]
	return u, ok
}

// ComponentInfo returns metadata for the component identified by id.
func (r *Reader) ComponentInfo(id ID) (ComponentInfo, bool) {
	info, ok := r.world.registry.LookupByID(id)
	if !ok {
		return ComponentInfo{}, false
	}
	return ComponentInfo{
		ID:    id,
		Name:  info.Name,
		Size:  info.Size,
		Align: info.Align,
		Type:  info.Type,
	}, true
}

// EntityComponents returns the component IDs in entity e's current archetype.
func (r *Reader) EntityComponents(e ID) []ID {
	rec := r.world.index.Get(e)
	if rec == nil || rec.Table == nil {
		return nil
	}
	sig := rec.Table.Type()
	if len(sig) == 0 {
		return nil
	}
	out := make([]ID, len(sig))
	copy(out, sig)
	return out
}

// EachEntity iterates all alive entities in allocation order.
// fn returns false to stop iteration early.
func (r *Reader) EachEntity(fn func(e ID) bool) {
	r.world.index.EachID(fn)
}

// AliveEntities collects all alive entities into a fresh slice.
func (r *Reader) AliveEntities() []ID {
	out := make([]ID, 0, r.world.index.Count())
	r.world.index.EachID(func(e ID) bool {
		out = append(out, e)
		return true
	})
	return out
}

// SystemCount returns the number of currently registered (non-closed) systems.
func (r *Reader) SystemCount() int {
	n := 0
	for _, s := range r.world.systems {
		if !s.removed {
			n++
		}
	}
	return n
}

// SystemCountInPhase returns the number of registered non-closed systems in
// the given phase. Disabled systems are included. Panics if phase is nil.
func (r *Reader) SystemCountInPhase(phase *Phase) int {
	return r.world.SystemCountInPhase(phase)
}

// Phases returns all pipeline phases in topological (execution) order.
// For a world with only the four built-in phases this is:
// PreUpdate, OnFixedUpdate, OnUpdate, PostUpdate.
// Custom phases created with [NewPhase] appear at their DependsOn-derived
// position in the order.
func (r *Reader) Phases() []*Phase {
	w := r.world
	if w.pipelineDirty {
		w.rebuildPipeline()
	}
	out := make([]*Phase, len(w.phaseOrder))
	copy(out, w.phaseOrder)
	return out
}

// SystemsInPhase returns a snapshot of all registered non-closed systems in
// the given phase, in topological (DependsOn) order with registration order as
// the tiebreaker. Disabled systems are included.
// Returns an empty (non-nil) slice when no systems are registered for the phase.
// Panics if phase is nil.
func (r *Reader) SystemsInPhase(phase *Phase) []*System {
	if phase == nil {
		panic("flecs: SystemsInPhase: phase must not be nil")
	}
	w := r.world
	if w.pipelineDirty {
		w.rebuildPipeline()
	}
	out := make([]*System, len(phase.orderedSystems))
	copy(out, phase.orderedSystems)
	return out
}

// EachSystem calls fn for each registered non-closed system in the given phase,
// in topological order. Disabled systems are included. fn returning false halts
// iteration early. Panics if phase is nil.
func (r *Reader) EachSystem(phase *Phase, fn func(*System) bool) {
	if phase == nil {
		panic("flecs: EachSystem: phase must not be nil")
	}
	w := r.world
	if w.pipelineDirty {
		w.rebuildPipeline()
	}
	for _, s := range phase.orderedSystems {
		if !fn(s) {
			return
		}
	}
}

// GetByID reads the value of the component identified by id from entity e.
func (r *Reader) GetByID(e ID, id ID) (any, bool) {
	return r.world.GetByID(e, id)
}

// Stats returns a snapshot of world-level counters.
func (r *Reader) Stats() Stats {
	return r.world.Stats()
}

// ── Writer methods ────────────────────────────────────────────────────────────

// NewEntity allocates a new entity, places it in the empty-signature table,
// and returns its ID. If a scope is active (see WithinScope / PushScope),
// automatically adds (ChildOf, scope) to the new entity.
func (fw *Writer) NewEntity() ID {
	e := fw.world.newEntityInternal()
	if len(fw.scopeStack) > 0 {
		AddID(fw, e, MakePair(fw.world.childOfID, fw.scopeStack[len(fw.scopeStack)-1]))
	}
	return e
}

// AddID adds the component or tag identified by id to entity e.
func (fw *Writer) AddID(e ID, id ID) bool {
	return AddID(fw, e, id)
}

// RemoveID removes the component or tag identified by id from entity e.
func (fw *Writer) RemoveID(e ID, id ID) bool {
	return RemoveID(fw, e, id)
}

// Delete removes entity e and all entities related to it via (ChildOf, e) pairs.
func (fw *Writer) Delete(e ID) bool {
	s := fw.stage
	if s.deferDepth == 0 {
		return deleteImmediate(fw.world, e)
	}
	if !fw.world.index.IsAlive(e) {
		return false
	}
	s.queue.append(cmd{kind: cmdDelete, entity: e})
	return true
}

// Clear removes all components from entity e, leaving it alive. See [Clear].
func (fw *Writer) Clear(e ID) bool { return Clear(fw, e) }

// MakeAlive claims a specific entity ID. See [MakeAlive].
func (fw *Writer) MakeAlive(id ID) ID { return MakeAlive(fw, id) }

// SetVersion overrides the generation counter on an alive entity. See [SetVersion].
func (fw *Writer) SetVersion(versionedID ID) { SetVersion(fw, versionedID) }

// RangeSet constrains the allocator to issue IDs in [min, max). See [RangeSet].
func (fw *Writer) RangeSet(min, max ID) { RangeSet(fw, min, max) }

// RangeClear removes the active range constraint. See [RangeClear].
func (fw *Writer) RangeClear() { RangeClear(fw) }

// RangeNew issues a single entity ID within [min, max). See [RangeNew].
func (fw *Writer) RangeNew(min, max ID) ID { return RangeNew(fw, min, max) }

// SetName sets the Name component on entity e.
func (fw *Writer) SetName(e ID, name string) {
	Set[Name](fw, e, Name{Value: name})
}

// SetByID writes value v as the component identified by id on entity e.
func (fw *Writer) SetByID(e ID, id ID, v any) {
	s := fw.stage
	if s.deferDepth == 0 {
		setByIDImmediate(fw.world, e, id, v)
		return
	}
	info, ok := fw.world.registry.LookupByID(id)
	if !ok {
		panic(fmt.Sprintf("flecs: SetByID: component id %d is not registered", uint64(id)))
	}
	if reflect.TypeOf(v) != info.Type {
		panic(fmt.Sprintf("flecs: SetByID: type mismatch for component %s (id=%d); expected %s, got %s",
			info.Name, uint64(id), info.Type, reflect.TypeOf(v)))
	}
	if info.Size > 0 {
		pv := reflect.New(info.Type)
		pv.Elem().Set(reflect.ValueOf(v))
		off, buf := s.queue.arena.alloc(int(info.Size), int(info.Align))
		copy(buf, unsafe.Slice((*byte)(pv.UnsafePointer()), info.Size))
		s.queue.append(cmd{kind: cmdSetByID, entity: e, id: id,
			valueOff: off, valueSize: uint32(info.Size)})
	} else {
		s.queue.append(cmd{kind: cmdSetByID, entity: e, id: id})
	}
}

// SetPairByID sets the pair (rel, tgt) on entity e with the dynamic value v.
func (fw *Writer) SetPairByID(e, rel, tgt ID, v any) {
	if v == nil {
		panic("flecs: SetPairByID: v must not be nil")
	}
	checkPairIsTag(fw.world, rel)
	checkUnionPair(fw.world, rel)
	pairID := MakePair(rel, tgt)
	vType := reflect.TypeOf(v)
	if existing, ok := fw.world.registry.LookupByID(pairID); ok {
		if existing.Type != vType {
			panic(fmt.Sprintf("flecs: SetPairByID: pair (rel=%d, tgt=%d) is already registered with type %s, cannot set with type %s",
				uint64(rel), uint64(tgt), existing.Type, vType))
		}
	} else {
		component.RegisterPairDataByType(fw.world.registry, pairID, vType)
	}
	fw.SetByID(e, pairID, v)
}

// ── Free functions (Reader-based reads) ──────────────────────────────────────

// Get returns the value of component T on entity e.
// Checks the entity's own table first; on a miss, walks the IsA chain.
// Does NOT auto-register T.
func Get[T any](s scope, e ID) (T, bool) {
	return getOnWorld[T](s.scopeWorld(), e)
}

// GetRef returns a pointer to component T on entity e. The pointer is only
// valid for the duration of the enclosing Read or Write scope. Returns nil
// if T is not registered, e is not alive, or e does not have T locally.
func GetRef[T any](s scope, e ID) *T {
	return getRefOnWorld[T](s.scopeWorld(), e)
}

// Has reports whether entity e has component T — locally or via an IsA chain.
// Auto-registers T so the answer is meaningful.
func Has[T any](s scope, e ID) bool {
	return hasOnWorld[T](s.scopeWorld(), e)
}

// Owns reports whether entity e locally owns component T.
// Auto-registers T (matches Has[T] policy).
func Owns[T any](s scope, e ID) bool {
	return ownsOnWorld[T](s.scopeWorld(), e)
}

// GetPairTarget returns the target of the first (rel, *) pair on entity e —
// the * part — in O(1) without a name lookup. Returns (0, false) if e is not
// alive or has no (rel, *) pair.
//
// Primary use case: resolving prefab slots created by [SlotOf]. After
// instantiating a prefab that has a child c with (SlotOf, prefab), the runtime
// adds (c, instanceChild) to the instance root. GetPairTarget retrieves that
// instance child:
//
//	instTurret, ok := flecs.GetPairTarget(r, inst, turret) // → copied turret child
func GetPairTarget(s scope, e ID, rel ID) (ID, bool) {
	w := s.scopeWorld()
	rec := w.index.Get(e)
	if rec == nil || rec.Table == nil {
		return 0, false
	}
	return firstPairTarget(rec.Table.Type(), rel.Index())
}

// GetPair returns the value of pair (rel, tgt) on entity e.
func GetPair[T any](s scope, e ID, rel ID, tgt ID) (T, bool) {
	return getPairOnWorld[T](s.scopeWorld(), e, rel, tgt)
}

// GetPairRef returns a pointer into the pair (rel, tgt) data slot for entity e.
// The pointer is only valid for the duration of the enclosing scope.
func GetPairRef[T any](s scope, e ID, rel ID, tgt ID) *T {
	return getPairRefOnWorld[T](s.scopeWorld(), e, rel, tgt)
}

// HasID reports whether entity e has the component or tag identified by id —
// locally or via an IsA chain.
func HasID(s scope, e ID, id ID) bool {
	w := s.scopeWorld()
	// Sparse or DontFragment: consult sparse-set index; DontFragment components are NOT
	// in the entity's archetype type. Sparse-only components are in the archetype but
	// the sparse-set index is always authoritative for presence.
	if !id.IsPair() {
		key := ID(id.Index())
		if w.sparsePolicies[key] || w.dontFragmentPolicies[key] {
			ss, ok := w.sparseStorage[key]
			if !ok {
				return false
			}
			_, has := ss.index[e.Index()]
			return has
		}
	}
	if id.IsPair() {
		relKey := ID(id.First().Index())
		// Parent-storage pair: consult parent column instead of archetype.
		if w.parentStoragePolicies[relKey] {
			rec := w.index.Get(e)
			if rec == nil || rec.Table == nil {
				return false
			}
			marker := w.parentStorageMarker(id.First())
			if !rec.Table.HasComponent(marker) {
				return false
			}
			if isWildcardID(w, id.Second()) {
				return true
			}
			parent, ok := rec.Table.GetParentEntry(int(rec.Row), relKey)
			return ok && parent.Index() == id.Second().Index()
		}
		// Union pair: consult union store (pairs never appear in the archetype type).
		if w.unionPolicies[relKey] {
			store, ok := w.unionStore[relKey]
			if !ok {
				return false
			}
			pos, has := store.index[ID(e.Index())]
			if !has {
				return false
			}
			termTarget := id.Second()
			if isWildcardID(w, termTarget) {
				return true // entity has any active target
			}
			return store.dense[pos].target.Index() == termTarget.Index()
		}
	}
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	if rec.Table != nil && rec.Table.HasComponent(id) {
		return true
	}
	if id.IsPair() {
		second := id.Second()
		if second == ID(e.Index()) {
			if w.reflexivePolicies[id.First()] {
				return true
			}
		}
	}
	return hasViaIsAPooled(w, e, id)
}

// OwnsID reports whether entity e locally owns the component or tag identified by id.
func OwnsID(s scope, e ID, id ID) bool {
	w := s.scopeWorld()
	if !id.IsPair() {
		key := ID(id.Index())
		if w.sparsePolicies[key] || w.dontFragmentPolicies[key] {
			ss, ok := w.sparseStorage[key]
			if !ok {
				return false
			}
			_, has := ss.index[e.Index()]
			return has
		}
	}
	if id.IsPair() {
		relKey := ID(id.First().Index())
		// Parent-storage pair: consult parent column instead of archetype.
		if w.parentStoragePolicies[relKey] {
			rec := w.index.Get(e)
			if rec == nil || rec.Table == nil {
				return false
			}
			marker := w.parentStorageMarker(id.First())
			if !rec.Table.HasComponent(marker) {
				return false
			}
			if isWildcardID(w, id.Second()) {
				return true
			}
			parent, ok := rec.Table.GetParentEntry(int(rec.Row), relKey)
			return ok && parent.Index() == id.Second().Index()
		}
		// Union pair: consult union store (pairs never appear in the archetype type).
		if w.unionPolicies[relKey] {
			store, ok := w.unionStore[relKey]
			if !ok {
				return false
			}
			pos, has := store.index[ID(e.Index())]
			if !has {
				return false
			}
			termTarget := id.Second()
			if isWildcardID(w, termTarget) {
				return true
			}
			return store.dense[pos].target.Index() == termTarget.Index()
		}
	}
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	return rec.Table != nil && rec.Table.HasComponent(id)
}

// Singleton returns a pointer to component T on the entity currently holding
// it as a singleton, plus true. Returns (nil, false) if T is not registered,
// not marked Singleton, or no entity holds it.
// The pointer is only valid for the duration of the enclosing scope.
func Singleton[T any](s scope) (*T, bool) {
	w := s.scopeWorld()
	cid := RegisterComponent[T](w)
	if !w.singletonPolicies[ID(cid.Index())] {
		return nil, false
	}
	holder, ok := w.singletonInstances[ID(cid.Index())]
	if !ok {
		return nil, false
	}
	ptr := GetRef[T](s, holder)
	if ptr == nil {
		return nil, false
	}
	return ptr, true
}

// WriteSingleton writes value v as component T on the explicit holding entity e.
// Registers T if not yet registered, marks T as Singleton (idempotent), then
// calls Set[T](fw, e, v). Panics if a different entity already holds T.
func WriteSingleton[T any](fw *Writer, e ID, v T) {
	cid := RegisterComponent[T](fw.world)
	applySingletonPolicy(fw.world, cid)
	Set[T](fw, e, v)
}

// ── Free functions (Writer-based writes) ─────────────────────────────────────

// Set writes value v as component T on entity e.
// If T is not yet registered, it is auto-registered.
func Set[T any](fw *Writer, e ID, v T) {
	s := fw.stage
	if s.deferDepth == 0 {
		setImmediate[T](fw.world, e, v)
		return
	}
	cid := RegisterComponent[T](fw.world)
	info, _ := component.LookupByType[T](fw.world.registry)
	if info.Size > 0 {
		off, buf := s.queue.arena.alloc(int(info.Size), int(info.Align))
		copy(buf, unsafe.Slice((*byte)(unsafe.Pointer(&v)), info.Size))
		s.queue.append(cmd{kind: cmdSetByID, entity: e, id: cid,
			valueOff: off, valueSize: uint32(info.Size)})
	} else {
		s.queue.append(cmd{kind: cmdSetByID, entity: e, id: cid})
	}
}

// Remove removes component T from entity e.
func Remove[T any](fw *Writer, e ID) bool {
	s := fw.stage
	if s.deferDepth == 0 {
		return removeImmediate[T](fw.world, e)
	}
	info, ok := component.LookupByType[T](fw.world.registry)
	if !ok || info.Component == 0 {
		return false
	}
	// Check sparse storage first (Sparse or DontFragment), then archetype.
	key := ID(info.Component.Index())
	if fw.world.sparsePolicies[key] || fw.world.dontFragmentPolicies[key] {
		ss, ok := fw.world.sparseStorage[key]
		if !ok {
			return false
		}
		if _, has := ss.index[e.Index()]; !has {
			return false
		}
		s.queue.append(cmd{kind: cmdRemoveID, entity: e, id: info.Component})
		return true
	}
	rec := fw.world.index.Get(e)
	if rec == nil || rec.Table == nil || !rec.Table.HasComponent(info.Component) {
		return false
	}
	s.queue.append(cmd{kind: cmdRemoveID, entity: e, id: info.Component})
	return true
}

// SetPair sets the pair (rel, tgt) on entity e with typed data value v.
func SetPair[T any](fw *Writer, e ID, rel ID, tgt ID, v T) {
	s := fw.stage
	if s.deferDepth == 0 {
		setPairImmediate[T](fw.world, e, rel, tgt, v)
		return
	}
	checkPairIsTag(fw.world, rel)
	checkUnionPair(fw.world, rel)
	pairID := MakePair(rel, tgt)
	pairInfo := component.RegisterPairData[T](fw.world.registry, pairID)
	if pairInfo.Size > 0 {
		off, buf := s.queue.arena.alloc(int(pairInfo.Size), int(pairInfo.Align))
		copy(buf, unsafe.Slice((*byte)(unsafe.Pointer(&v)), pairInfo.Size))
		s.queue.append(cmd{kind: cmdSetPair, entity: e, id: pairID,
			valueOff: off, valueSize: uint32(pairInfo.Size)})
	} else {
		s.queue.append(cmd{kind: cmdSetPair, entity: e, id: pairID})
	}
}

// AddID adds the component or tag identified by id to entity e.
func AddID(fw *Writer, e ID, e2 ID) bool {
	s := fw.stage
	if s.deferDepth == 0 {
		return addIDImmediate(fw.world, e, e2)
	}
	rec := fw.world.index.Get(e)
	if rec == nil {
		panic("flecs: AddID called on dead entity")
	}
	if rec.Table != nil && rec.Table.HasComponent(e2) {
		return false
	}
	fw.world.registry.EnsureID(e2)
	s.queue.append(cmd{kind: cmdAddID, entity: e, id: e2})
	return true
}

// RemoveID removes the component or tag identified by id from entity e.
func RemoveID(fw *Writer, e ID, id ID) bool {
	s := fw.stage
	if s.deferDepth == 0 {
		return removeIDImmediate(fw.world, e, id)
	}
	// Check sparse storage first, then archetype.
	if !id.IsPair() && fw.world.sparsePolicies[ID(id.Index())] {
		ss, ok := fw.world.sparseStorage[ID(id.Index())]
		if !ok {
			return false
		}
		if _, has := ss.index[e.Index()]; !has {
			return false
		}
		s.queue.append(cmd{kind: cmdRemoveID, entity: e, id: id})
		return true
	}
	// Parent-storage pair: check parent column rather than archetype signature.
	if id.IsPair() && fw.world.parentStoragePolicies[ID(id.First().Index())] {
		rec := fw.world.index.Get(e)
		if rec == nil || rec.Table == nil {
			return false
		}
		marker := fw.world.parentStorageMarker(id.First())
		if !rec.Table.HasComponent(marker) {
			return false
		}
		if !isWildcardID(fw.world, id.Second()) {
			relKey := ID(id.First().Index())
			cur, ok := rec.Table.GetParentEntry(int(rec.Row), relKey)
			if !ok || cur.Index() != id.Second().Index() {
				return false
			}
		}
		s.queue.append(cmd{kind: cmdRemoveID, entity: e, id: id})
		return true
	}
	// Union pair: check union store (pairs never appear in the archetype).
	if id.IsPair() && fw.world.unionPolicies[ID(id.First().Index())] {
		relKey := ID(id.First().Index())
		store, ok := fw.world.unionStore[relKey]
		if !ok {
			return false
		}
		entityKey := ID(e.Index())
		pos, has := store.index[entityKey]
		if !has {
			return false
		}
		currentTarget := store.dense[pos].target
		termTarget := id.Second()
		if !isWildcardID(fw.world, termTarget) && currentTarget.Index() != termTarget.Index() {
			return false // different specific target → no-op
		}
		s.queue.append(cmd{kind: cmdRemoveID, entity: e, id: id})
		return true
	}
	rec := fw.world.index.Get(e)
	if rec == nil || rec.Table == nil || !rec.Table.HasComponent(id) {
		return false
	}
	s.queue.append(cmd{kind: cmdRemoveID, entity: e, id: id})
	return true
}

// ── Traversal free functions on Reader ───────────────────────────────────────

// GetUp walks the relationship rel up from e (self-first), returning the value
// of component T from the first entity in the chain that locally owns T.
func GetUp[T any](s scope, e ID, rel ID) (T, bool) {
	return getUpInternal[T](s.scopeWorld(), e, rel)
}

// HasUp reports whether e or any ancestor reachable via rel locally owns the
// component identified by id.
func HasUp(s scope, e ID, id ID, rel ID) bool {
	return hasUpInternal(s.scopeWorld(), e, id, rel)
}

// TargetUp returns the ID of the first entity in the chain (e or an ancestor
// via rel) that locally owns the component identified by id.
func TargetUp(s scope, e ID, id ID, rel ID) (ID, bool) {
	return targetUpInternal(s.scopeWorld(), e, id, rel)
}

// PrefabOf returns the first IsA prefab of entity e.
func PrefabOf(s scope, e ID) (ID, bool) {
	return prefabOfInternal(s.scopeWorld(), e)
}

// ── Each free functions on Reader ─────────────────────────────────────────────

// upPtr resolves a component pointer for id in the current iterator table.
// When the term was matched via an ancestor (upSources[id] != 0), it returns a
// pointer into the ancestor's component slot — the same pointer for every row in
// this table (C flecs semantics: all inheritors share the prefab's slot; mutating
// through the pointer affects the prefab and all entities that inherit from it).
// When self-matched (upSources[id] == 0 or no up-sources), returns nil so the
// caller falls back to a per-row Field[T] slice.
func upPtr[T any](w *World, it *QueryIter, id ID) *T {
	src := it.upSources[id]
	if src == 0 {
		return nil
	}
	rec := w.index.Get(src)
	if rec == nil || rec.Table == nil {
		return nil
	}
	ptr := rec.Table.Get(int(rec.Row), id)
	if ptr == nil {
		// Tag component: no data backing store. Return pointer to a zero value
		// so the callback receives a valid (if meaningless) pointer, consistent
		// with how Field[T] handles local tags (returns a zero-value slice).
		var zero T
		return &zero
	}
	return (*T)(ptr)
}

// Each1 calls fn once for every entity that has component A.
//
// If A is marked inheritable via [SetInheritable], the query is automatically
// promoted to Self|Up(IsA): entities that own A locally AND entities that
// inherit A from a prefab via IsA are both matched. When the term is resolved
// via an ancestor (Up path), the same prefab pointer is passed for every entity
// in the matched table — mutating through the pointer affects the prefab and all
// entities that inherit from it.
func Each1[A any](s scope, fn func(e ID, a *A)) {
	w := s.scopeWorld()
	var ids [1]ID
	ids[0] = RegisterComponent[A](w)
	toggleA := w.canTogglePolicies[ID(ids[0].Index())]
	q := NewQuery(w, ids[:]...)
	it := q.Iter()
	for it.Next() {
		if aShared := upPtr[A](w, it, ids[0]); aShared != nil {
			for _, e := range it.Entities() {
				fn(e, aShared)
			}
		} else {
			colA := Field[A](it, ids[0])
			for i, e := range it.Entities() {
				if toggleA && !it.current.IsRowEnabled(ids[0], i) {
					continue
				}
				fn(e, &colA[i])
			}
		}
	}
}

// Each2 calls fn once for every entity that has all of components A and B.
//
// If any component is marked inheritable, its query term is auto-promoted to
// Self|Up(IsA). For each matched table, components resolved via an ancestor
// yield the same prefab pointer for every row (see [Each1] doc for details).
func Each2[A, B any](s scope, fn func(e ID, a *A, b *B)) {
	w := s.scopeWorld()
	var ids [2]ID
	ids[0] = RegisterComponent[A](w)
	ids[1] = RegisterComponent[B](w)
	toggleA := w.canTogglePolicies[ID(ids[0].Index())]
	toggleB := w.canTogglePolicies[ID(ids[1].Index())]
	q := NewQuery(w, ids[:]...)
	it := q.Iter()
	for it.Next() {
		aShared := upPtr[A](w, it, ids[0])
		bShared := upPtr[B](w, it, ids[1])
		if aShared == nil && bShared == nil {
			colA := Field[A](it, ids[0])
			colB := Field[B](it, ids[1])
			for i, e := range it.Entities() {
				if (toggleA && !it.current.IsRowEnabled(ids[0], i)) ||
					(toggleB && !it.current.IsRowEnabled(ids[1], i)) {
					continue
				}
				fn(e, &colA[i], &colB[i])
			}
			continue
		}
		var colA []A
		if aShared == nil {
			colA = Field[A](it, ids[0])
		}
		var colB []B
		if bShared == nil {
			colB = Field[B](it, ids[1])
		}
		for i, e := range it.Entities() {
			if (toggleA && aShared == nil && !it.current.IsRowEnabled(ids[0], i)) ||
				(toggleB && bShared == nil && !it.current.IsRowEnabled(ids[1], i)) {
				continue
			}
			a := aShared
			if a == nil {
				a = &colA[i]
			}
			b := bShared
			if b == nil {
				b = &colB[i]
			}
			fn(e, a, b)
		}
	}
}

// Each3 calls fn once for every entity that has all of components A, B, and C.
//
// If any component is marked inheritable, its query term is auto-promoted to
// Self|Up(IsA). For each matched table, components resolved via an ancestor
// yield the same prefab pointer for every row (see [Each1] doc for details).
func Each3[A, B, C any](s scope, fn func(e ID, a *A, b *B, c *C)) {
	w := s.scopeWorld()
	var ids [3]ID
	ids[0] = RegisterComponent[A](w)
	ids[1] = RegisterComponent[B](w)
	ids[2] = RegisterComponent[C](w)
	toggleA := w.canTogglePolicies[ID(ids[0].Index())]
	toggleB := w.canTogglePolicies[ID(ids[1].Index())]
	toggleC := w.canTogglePolicies[ID(ids[2].Index())]
	q := NewQuery(w, ids[:]...)
	it := q.Iter()
	for it.Next() {
		aShared := upPtr[A](w, it, ids[0])
		bShared := upPtr[B](w, it, ids[1])
		cShared := upPtr[C](w, it, ids[2])
		if aShared == nil && bShared == nil && cShared == nil {
			colA := Field[A](it, ids[0])
			colB := Field[B](it, ids[1])
			colC := Field[C](it, ids[2])
			for i, e := range it.Entities() {
				if (toggleA && !it.current.IsRowEnabled(ids[0], i)) ||
					(toggleB && !it.current.IsRowEnabled(ids[1], i)) ||
					(toggleC && !it.current.IsRowEnabled(ids[2], i)) {
					continue
				}
				fn(e, &colA[i], &colB[i], &colC[i])
			}
			continue
		}
		var colA []A
		if aShared == nil {
			colA = Field[A](it, ids[0])
		}
		var colB []B
		if bShared == nil {
			colB = Field[B](it, ids[1])
		}
		var colC []C
		if cShared == nil {
			colC = Field[C](it, ids[2])
		}
		for i, e := range it.Entities() {
			if (toggleA && aShared == nil && !it.current.IsRowEnabled(ids[0], i)) ||
				(toggleB && bShared == nil && !it.current.IsRowEnabled(ids[1], i)) ||
				(toggleC && cShared == nil && !it.current.IsRowEnabled(ids[2], i)) {
				continue
			}
			a := aShared
			if a == nil {
				a = &colA[i]
			}
			b := bShared
			if b == nil {
				b = &colB[i]
			}
			c := cShared
			if c == nil {
				c = &colC[i]
			}
			fn(e, a, b, c)
		}
	}
}

// Each4 calls fn once for every entity that has all of components A, B, C, and D.
//
// If any component is marked inheritable, its query term is auto-promoted to
// Self|Up(IsA). For each matched table, components resolved via an ancestor
// yield the same prefab pointer for every row (see [Each1] doc for details).
func Each4[A, B, C, D any](s scope, fn func(e ID, a *A, b *B, c *C, d *D)) {
	w := s.scopeWorld()
	var ids [4]ID
	ids[0] = RegisterComponent[A](w)
	ids[1] = RegisterComponent[B](w)
	ids[2] = RegisterComponent[C](w)
	ids[3] = RegisterComponent[D](w)
	toggleA := w.canTogglePolicies[ID(ids[0].Index())]
	toggleB := w.canTogglePolicies[ID(ids[1].Index())]
	toggleC := w.canTogglePolicies[ID(ids[2].Index())]
	toggleD := w.canTogglePolicies[ID(ids[3].Index())]
	q := NewQuery(w, ids[:]...)
	it := q.Iter()
	for it.Next() {
		aShared := upPtr[A](w, it, ids[0])
		bShared := upPtr[B](w, it, ids[1])
		cShared := upPtr[C](w, it, ids[2])
		dShared := upPtr[D](w, it, ids[3])
		if aShared == nil && bShared == nil && cShared == nil && dShared == nil {
			colA := Field[A](it, ids[0])
			colB := Field[B](it, ids[1])
			colC := Field[C](it, ids[2])
			colD := Field[D](it, ids[3])
			for i, e := range it.Entities() {
				if (toggleA && !it.current.IsRowEnabled(ids[0], i)) ||
					(toggleB && !it.current.IsRowEnabled(ids[1], i)) ||
					(toggleC && !it.current.IsRowEnabled(ids[2], i)) ||
					(toggleD && !it.current.IsRowEnabled(ids[3], i)) {
					continue
				}
				fn(e, &colA[i], &colB[i], &colC[i], &colD[i])
			}
			continue
		}
		var colA []A
		if aShared == nil {
			colA = Field[A](it, ids[0])
		}
		var colB []B
		if bShared == nil {
			colB = Field[B](it, ids[1])
		}
		var colC []C
		if cShared == nil {
			colC = Field[C](it, ids[2])
		}
		var colD []D
		if dShared == nil {
			colD = Field[D](it, ids[3])
		}
		for i, e := range it.Entities() {
			if (toggleA && aShared == nil && !it.current.IsRowEnabled(ids[0], i)) ||
				(toggleB && bShared == nil && !it.current.IsRowEnabled(ids[1], i)) ||
				(toggleC && cShared == nil && !it.current.IsRowEnabled(ids[2], i)) ||
				(toggleD && dShared == nil && !it.current.IsRowEnabled(ids[3], i)) {
				continue
			}
			a := aShared
			if a == nil {
				a = &colA[i]
			}
			b := bShared
			if b == nil {
				b = &colB[i]
			}
			c := cShared
			if c == nil {
				c = &colC[i]
			}
			d := dShared
			if d == nil {
				d = &colD[i]
			}
			fn(e, a, b, c, d)
		}
	}
}
