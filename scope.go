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
	stage *stage // routes mutations to this stage's command queue
}

// AsReader returns the embedded *Reader so callers can pass it to read-only
// free functions (e.g. Each1, Get) without opening a separate w.Read scope.
//
//	w.Write(func(fw *flecs.Writer) {
//	    flecs.Each2[Position, Velocity](fw.AsReader(), func(...) { ... })
//	})
func (fw *Writer) AsReader() *Reader { return &fw.Reader }

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
func (r *Reader) HasID(e ID, id ID) bool {
	rec := r.world.index.Get(e)
	if rec == nil {
		return false
	}
	if rec.Table != nil && rec.Table.HasComponent(id) {
		return true
	}
	return hasViaIsA(r.world, e, id, nil)
}

// OwnsID reports whether entity e locally owns the component or tag identified
// by id. Local-only: does not walk the IsA chain.
func (r *Reader) OwnsID(e ID, id ID) bool {
	rec := r.world.index.Get(e)
	if rec == nil {
		return false
	}
	return rec.Table != nil && rec.Table.HasComponent(id)
}

// ParentOf returns the parent of entity e: the target of the first
// (ChildOf, *) pair found in e's archetype signature.
func (r *Reader) ParentOf(e ID) (ID, bool) {
	rec := r.world.index.Get(e)
	if rec == nil || rec.Table == nil {
		return 0, false
	}
	return firstPairTarget(rec.Table.Type(), r.world.childOfID.Index())
}

// EachChild calls fn for every direct child of parent.
func (r *Reader) EachChild(parent ID, fn func(ID) bool) {
	pairID := MakePair(r.world.childOfID, parent)
	r.world.compIndex.Each(pairID, func(t *table.Table) bool {
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

// SystemCountInPhase returns the number of active systems in the given phase.
func (r *Reader) SystemCountInPhase(phase ID) int {
	return r.world.SystemCountInPhase(phase)
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
// and returns its ID.
func (fw *Writer) NewEntity() ID {
	return fw.world.newEntityInternal()
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
func Get[T any](r *Reader, e ID) (T, bool) {
	return getOnWorld[T](r.world, e)
}

// GetRef returns a pointer to component T on entity e. The pointer is only
// valid for the duration of the enclosing Read or Write scope. Returns nil
// if T is not registered, e is not alive, or e does not have T locally.
func GetRef[T any](r *Reader, e ID) *T {
	return getRefOnWorld[T](r.world, e)
}

// Has reports whether entity e has component T — locally or via an IsA chain.
// Auto-registers T so the answer is meaningful.
func Has[T any](r *Reader, e ID) bool {
	return hasOnWorld[T](r.world, e)
}

// Owns reports whether entity e locally owns component T.
// Auto-registers T (matches Has[T] policy).
func Owns[T any](r *Reader, e ID) bool {
	return ownsOnWorld[T](r.world, e)
}

// GetPair returns the value of pair (rel, tgt) on entity e.
func GetPair[T any](r *Reader, e ID, rel ID, tgt ID) (T, bool) {
	return getPairOnWorld[T](r.world, e, rel, tgt)
}

// GetPairRef returns a pointer into the pair (rel, tgt) data slot for entity e.
// The pointer is only valid for the duration of the enclosing scope.
func GetPairRef[T any](r *Reader, e ID, rel ID, tgt ID) *T {
	return getPairRefOnWorld[T](r.world, e, rel, tgt)
}

// HasID reports whether entity e has the component or tag identified by id —
// locally or via an IsA chain.
func HasID(r *Reader, e ID, id ID) bool {
	return r.HasID(e, id)
}

// OwnsID reports whether entity e locally owns the component or tag identified by id.
func OwnsID(r *Reader, e ID, id ID) bool {
	return r.OwnsID(e, id)
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
func GetUp[T any](r *Reader, e ID, rel ID) (T, bool) {
	return getUpInternal[T](r.world, e, rel)
}

// HasUp reports whether e or any ancestor reachable via rel locally owns the
// component identified by id.
func HasUp(r *Reader, e ID, id ID, rel ID) bool {
	return hasUpInternal(r.world, e, id, rel)
}

// TargetUp returns the ID of the first entity in the chain (e or an ancestor
// via rel) that locally owns the component identified by id.
func TargetUp(r *Reader, e ID, id ID, rel ID) (ID, bool) {
	return targetUpInternal(r.world, e, id, rel)
}

// PrefabOf returns the first IsA prefab of entity e.
func PrefabOf(r *Reader, e ID) (ID, bool) {
	return prefabOfInternal(r.world, e)
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
func Each1[A any](r *Reader, fn func(e ID, a *A)) {
	var ids [1]ID
	ids[0] = RegisterComponent[A](r.world)
	toggleA := r.world.canTogglePolicies[ID(ids[0].Index())]
	q := NewQuery(r.world, ids[:]...)
	it := q.Iter()
	for it.Next() {
		if aShared := upPtr[A](r.world, it, ids[0]); aShared != nil {
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
func Each2[A, B any](r *Reader, fn func(e ID, a *A, b *B)) {
	var ids [2]ID
	ids[0] = RegisterComponent[A](r.world)
	ids[1] = RegisterComponent[B](r.world)
	toggleA := r.world.canTogglePolicies[ID(ids[0].Index())]
	toggleB := r.world.canTogglePolicies[ID(ids[1].Index())]
	q := NewQuery(r.world, ids[:]...)
	it := q.Iter()
	for it.Next() {
		aShared := upPtr[A](r.world, it, ids[0])
		bShared := upPtr[B](r.world, it, ids[1])
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
func Each3[A, B, C any](r *Reader, fn func(e ID, a *A, b *B, c *C)) {
	var ids [3]ID
	ids[0] = RegisterComponent[A](r.world)
	ids[1] = RegisterComponent[B](r.world)
	ids[2] = RegisterComponent[C](r.world)
	toggleA := r.world.canTogglePolicies[ID(ids[0].Index())]
	toggleB := r.world.canTogglePolicies[ID(ids[1].Index())]
	toggleC := r.world.canTogglePolicies[ID(ids[2].Index())]
	q := NewQuery(r.world, ids[:]...)
	it := q.Iter()
	for it.Next() {
		aShared := upPtr[A](r.world, it, ids[0])
		bShared := upPtr[B](r.world, it, ids[1])
		cShared := upPtr[C](r.world, it, ids[2])
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
func Each4[A, B, C, D any](r *Reader, fn func(e ID, a *A, b *B, c *C, d *D)) {
	var ids [4]ID
	ids[0] = RegisterComponent[A](r.world)
	ids[1] = RegisterComponent[B](r.world)
	ids[2] = RegisterComponent[C](r.world)
	ids[3] = RegisterComponent[D](r.world)
	toggleA := r.world.canTogglePolicies[ID(ids[0].Index())]
	toggleB := r.world.canTogglePolicies[ID(ids[1].Index())]
	toggleC := r.world.canTogglePolicies[ID(ids[2].Index())]
	toggleD := r.world.canTogglePolicies[ID(ids[3].Index())]
	q := NewQuery(r.world, ids[:]...)
	it := q.Iter()
	for it.Next() {
		aShared := upPtr[A](r.world, it, ids[0])
		bShared := upPtr[B](r.world, it, ids[1])
		cShared := upPtr[C](r.world, it, ids[2])
		dShared := upPtr[D](r.world, it, ids[3])
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
