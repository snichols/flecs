package flecs

import "reflect"

// ComponentInfo is a snapshot of a component's metadata at the time of the call.
// It is a value type; mutating the returned struct has no effect on the world.
//
// Type may be nil for pair or tag components that have no associated Go type
// (e.g. a raw entity ID used as a tag, or an auto-registered pair-as-tag). In
// that case Size and Align are zero and Name is "tag".
type ComponentInfo struct {
	// ID is the component entity ID.
	ID ID
	// Name is reflect.Type.String() for the registered Go type
	// (e.g. "flecs.Name", "main.Position").
	// For tag and auto-registered pair components, Name is "tag".
	// For pair components registered via SetPair[T], Name is "pair(<T-name>)".
	Name string
	// Size is unsafe.Sizeof of the component type. Zero for tags and zero-size types.
	Size uintptr
	// Align is unsafe.Alignof of the component type. Zero for tags.
	Align uintptr
	// Type is the reflect.Type of the registered Go type.
	// Nil for tag components and pair-as-tag entries.
	Type reflect.Type
}

// Components returns a fresh slice of all registered component IDs in
// registration order. A component is any entity that has associated TypeInfo in
// the internal registry — including the built-in Name component, components
// registered via RegisterComponent[T], pair data types registered via SetPair[T],
// and raw entity/pair IDs auto-registered via AddID (EnsureID path).
//
// Built-in tag entities (ChildOf, IsA, PreUpdate, OnUpdate, PostUpdate,
// OnFixedUpdate) are NOT components — they have no TypeInfo — and are excluded.
//
// After World.New(), Components() contains exactly one entry: the Name component.
// Mutating the returned slice does not affect the world.
func (w *World) Components() []ID {
	w.rwmu.RLock()
	defer w.rwmu.RUnlock()
	return w.registry.IDs()
}

// ComponentInfo returns metadata for the component identified by id.
// Returns (ComponentInfo{}, false) if id is not registered as a component.
//
// Pair-ID support: if id is a pair (e.g. MakePair(R, T)) look up its per-pair
// TypeInfo. If no TypeInfo is associated with that pair ID, return false.
//
// The EnsureID path auto-registers zero-size tag TypeInfos for raw entity IDs
// and pair IDs used as tags; those entries return (info, true) with Size==0.
func (w *World) ComponentInfo(id ID) (ComponentInfo, bool) {
	w.rwmu.RLock()
	defer w.rwmu.RUnlock()
	return componentInfoUnlocked(w, id)
}

// componentInfoUnlocked is the lock-free body of ComponentInfo.
func componentInfoUnlocked(w *World, id ID) (ComponentInfo, bool) {
	info, ok := w.registry.LookupByID(id)
	if !ok {
		return ComponentInfo{}, false
	}
	var t reflect.Type
	if info.Type != nil {
		t = info.Type
	}
	return ComponentInfo{
		ID:    id,
		Name:  info.Name,
		Size:  info.Size,
		Align: info.Align,
		Type:  t,
	}, true
}

// EntityComponents returns a fresh sorted-ascending slice of the component IDs
// in entity e's current archetype signature.
//
// Returns nil for dead entities or entities in the empty archetype (no components).
// The returned slice is a copy of the table's signature; mutating it does not
// affect the world. Includes pair IDs (e.g. MakePair(ChildOf, parent)).
func (w *World) EntityComponents(e ID) []ID {
	w.rwmu.RLock()
	defer w.rwmu.RUnlock()
	return entityComponentsUnlocked(w, e)
}

// entityComponentsUnlocked is the lock-free body of EntityComponents.
func entityComponentsUnlocked(w *World, e ID) []ID {
	rec := w.index.Get(e)
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

// EachEntity iterates all alive entities in allocation order (dense-vector order
// from the entity index). fn returns false to stop iteration early.
//
// The iteration includes all alive entities: user entities, built-in relationship
// entities (ChildOf, IsA), phase entities (PreUpdate, OnUpdate, PostUpdate,
// OnFixedUpdate), and the Name component entity. Callers that want only user
// entities must filter by comparing against w.Count() or using w.IsAlive.
//
// Behavior is undefined if fn calls Set/Add/Remove/Delete during iteration;
// wrap in Defer for safe mutation.
func (w *World) EachEntity(fn func(e ID) bool) {
	w.rwmu.RLock()
	defer w.rwmu.RUnlock()
	w.index.EachID(fn)
}

// AliveEntities collects all alive entities into a fresh slice and returns it.
// The order matches EachEntity (allocation/dense-vector order). Allocates once.
//
// For hot paths, prefer EachEntity to avoid the allocation.
func (w *World) AliveEntities() []ID {
	w.rwmu.RLock()
	defer w.rwmu.RUnlock()
	out := make([]ID, 0, w.index.Count())
	w.index.EachID(func(e ID) bool {
		out = append(out, e)
		return true
	})
	return out
}
