package flecs

import (
	"errors"

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
	return fw.world.Delete(e)
}

// SetName sets the Name component on entity e.
func (fw *Writer) SetName(e ID, name string) {
	fw.world.SetName(e, name)
}

// SetByID writes value v as the component identified by id on entity e.
func (fw *Writer) SetByID(e ID, id ID, v any) {
	fw.world.SetByID(e, id, v)
}

// SetPairByID sets the pair (rel, tgt) on entity e with the dynamic value v.
func (fw *Writer) SetPairByID(e, rel, tgt ID, v any) {
	fw.world.SetPairByID(e, rel, tgt, v)
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
	setOnWorld[T](fw.world, e, v)
}

// Remove removes component T from entity e.
func Remove[T any](fw *Writer, e ID) bool {
	return removeOnWorld[T](fw.world, e)
}

// SetPair sets the pair (rel, tgt) on entity e with typed data value v.
func SetPair[T any](fw *Writer, e ID, rel ID, tgt ID, v T) {
	setPairOnWorld[T](fw.world, e, rel, tgt, v)
}

// AddID adds the component or tag identified by id to entity e.
func AddID(fw *Writer, e ID, e2 ID) bool {
	return addIDOnWorld(fw.world, e, e2)
}

// RemoveID removes the component or tag identified by id from entity e.
func RemoveID(fw *Writer, e ID, id ID) bool {
	return removeIDOnWorld(fw.world, e, id)
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

// Each1 calls fn once for every entity that has component A.
func Each1[A any](r *Reader, fn func(e ID, a *A)) {
	var ids [1]ID
	ids[0] = RegisterComponent[A](r.world)
	q := NewQuery(r.world, ids[:]...)
	it := q.Iter()
	for it.Next() {
		colA := Field[A](it, ids[0])
		for i, e := range it.Entities() {
			fn(e, &colA[i])
		}
	}
}

// Each2 calls fn once for every entity that has all of components A and B.
func Each2[A, B any](r *Reader, fn func(e ID, a *A, b *B)) {
	var ids [2]ID
	ids[0] = RegisterComponent[A](r.world)
	ids[1] = RegisterComponent[B](r.world)
	q := NewQuery(r.world, ids[:]...)
	it := q.Iter()
	for it.Next() {
		colA := Field[A](it, ids[0])
		colB := Field[B](it, ids[1])
		for i, e := range it.Entities() {
			fn(e, &colA[i], &colB[i])
		}
	}
}

// Each3 calls fn once for every entity that has all of components A, B, and C.
func Each3[A, B, C any](r *Reader, fn func(e ID, a *A, b *B, c *C)) {
	var ids [3]ID
	ids[0] = RegisterComponent[A](r.world)
	ids[1] = RegisterComponent[B](r.world)
	ids[2] = RegisterComponent[C](r.world)
	q := NewQuery(r.world, ids[:]...)
	it := q.Iter()
	for it.Next() {
		colA := Field[A](it, ids[0])
		colB := Field[B](it, ids[1])
		colC := Field[C](it, ids[2])
		for i, e := range it.Entities() {
			fn(e, &colA[i], &colB[i], &colC[i])
		}
	}
}

// Each4 calls fn once for every entity that has all of components A, B, C, and D.
func Each4[A, B, C, D any](r *Reader, fn func(e ID, a *A, b *B, c *C, d *D)) {
	var ids [4]ID
	ids[0] = RegisterComponent[A](r.world)
	ids[1] = RegisterComponent[B](r.world)
	ids[2] = RegisterComponent[C](r.world)
	ids[3] = RegisterComponent[D](r.world)
	q := NewQuery(r.world, ids[:]...)
	it := q.Iter()
	for it.Next() {
		colA := Field[A](it, ids[0])
		colB := Field[B](it, ids[1])
		colC := Field[C](it, ids[2])
		colD := Field[D](it, ids[3])
		for i, e := range it.Entities() {
			fn(e, &colA[i], &colB[i], &colC[i], &colD[i])
		}
	}
}
