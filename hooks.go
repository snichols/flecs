package flecs

import (
	"unsafe"

	"github.com/snichols/flecs/internal/component"
)

// OnAdd registers fn as the OnAdd hook for component T in w.
// fn is called once when T is newly added to an entity; it is NOT called on
// subsequent Set[T] calls that overwrite an existing value.
// If fn is nil, any existing OnAdd hook for T is cleared.
// Calling OnAdd[T] twice replaces the prior hook (one hook per type per event).
//
// For zero-size types (tags), v in the callback is the zero value of T;
// the value is meaningless but the hook still fires.
// T need not be registered as a component entity before calling OnAdd; if
// unregistered, its type metadata is auto-registered idempotently.
//
// The *Writer passed to fn is a non-nil Writer scoped to the current world.
// Read operations (Get, Has, IsAlive) are safe to call from within the hook.
func OnAdd[T any](w *World, fn func(fw *Writer, e ID, v T)) {
	w.checkExclusiveAccessWrite()
	info := component.Register[T](w.registry)
	if fn == nil {
		info.Hooks.OnAdd = nil
		return
	}
	info.Hooks.OnAdd = func(world any, e ID, ptr unsafe.Pointer) {
		var v T
		if ptr != nil {
			v = *(*T)(ptr)
		}
		fn(world.(*Writer), e, v)
	}
}

// OnSet registers fn as the OnSet hook for component T in w.
// fn is called every time T's value is written on an entity via Set[T] or
// SetPair[T], including the initial write that follows OnAdd.
// If fn is nil, any existing OnSet hook for T is cleared.
// Calling OnSet[T] twice replaces the prior hook.
//
// For zero-size types (tags), the callback receives the zero value of T.
// OnSet does not fire for AddID (which carries no value).
//
// The *Writer passed to fn is a non-nil Writer scoped to the current world.
func OnSet[T any](w *World, fn func(fw *Writer, e ID, v T)) {
	w.checkExclusiveAccessWrite()
	info := component.Register[T](w.registry)
	if fn == nil {
		info.Hooks.OnSet = nil
		return
	}
	info.Hooks.OnSet = func(world any, e ID, ptr unsafe.Pointer) {
		var v T
		if ptr != nil {
			v = *(*T)(ptr)
		}
		fn(world.(*Writer), e, v)
	}
}

// OnRemove registers fn as the OnRemove hook for component T in w.
// fn is called before T is removed from an entity, including when the entity
// is deleted. v holds the component value at the time of the call.
// If fn is nil, any existing OnRemove hook for T is cleared.
// Calling OnRemove[T] twice replaces the prior hook.
//
// For zero-size types (tags), v is the zero value of T.
//
// The *Writer passed to fn is a non-nil Writer scoped to the current world.
// Read operations (Get, Has, IsAlive) are safe to call from within the hook.
func OnRemove[T any](w *World, fn func(fw *Writer, e ID, v T)) {
	w.checkExclusiveAccessWrite()
	info := component.Register[T](w.registry)
	if fn == nil {
		info.Hooks.OnRemove = nil
		return
	}
	info.Hooks.OnRemove = func(world any, e ID, ptr unsafe.Pointer) {
		var v T
		if ptr != nil {
			v = *(*T)(ptr)
		}
		fn(world.(*Writer), e, v)
	}
}

// OnReplace registers fn as the OnReplace hook for component T in w.
// fn is called when a Set call overwrites an existing component value;
// it does NOT fire on the first Set (which calls OnAdd then OnSet).
// fn receives both the previous and the incoming value, by value, before
// the slot is overwritten.
//
// Mirrors C flecs ti->hooks.on_replace. Dispatch order on overwrite:
// OnReplace -> column write -> OnSet. OnSet still fires after OnReplace.
//
// Calling OnReplace[T] twice replaces the prior hook. Passing fn=nil clears.
func OnReplace[T any](w *World, fn func(fw *Writer, e ID, old, new T)) {
	w.checkExclusiveAccessWrite()
	info := component.Register[T](w.registry)
	if fn == nil {
		info.Hooks.OnReplace = nil
		return
	}
	info.Hooks.OnReplace = func(world any, e ID, oldPtr, newPtr unsafe.Pointer) {
		var oldV, newV T
		if oldPtr != nil {
			oldV = *(*T)(oldPtr)
		}
		if newPtr != nil {
			newV = *(*T)(newPtr)
		}
		fn(world.(*Writer), e, oldV, newV)
	}
}

// OnReplaceID is the untyped variant of OnReplace for dynamic or runtime-registered
// components. The handler receives raw pointers; both are valid only for the duration
// of the call. Mirrors the shape of ObserveID's untyped-pointer payload.
func OnReplaceID(w *World, componentID ID, fn func(fw *Writer, e ID, oldPtr, newPtr unsafe.Pointer)) {
	w.checkExclusiveAccessWrite()
	info, ok := w.registry.LookupByID(componentID)
	if !ok {
		panic("flecs: OnReplaceID called with unregistered component ID")
	}
	if fn == nil {
		info.Hooks.OnReplace = nil
		return
	}
	info.Hooks.OnReplace = func(world any, e ID, oldPtr, newPtr unsafe.Pointer) {
		fn(world.(*Writer), e, oldPtr, newPtr)
	}
}

// fireOnReplace invokes the OnReplace hook (if set) for id on entity e.
// oldPtr points to the current slot value; newPtr to the incoming value.
// Both are valid only for the duration of the call. No observer dispatch:
// OnReplace has no observer event in upstream C flecs.
func (w *World) fireOnReplace(info *component.TypeInfo, id ID, e ID, oldPtr, newPtr unsafe.Pointer) {
	if info != nil && info.Hooks.OnReplace != nil {
		info.Hooks.OnReplace(&w.writeCapability, e, oldPtr, newPtr)
	}
}

// fireOnAdd invokes the OnAdd hook (if set) then dispatches observers for id.
// Dispatch order: hook first, then observers in registration order.
// id must be the component/tag/pair entity ID being added; info may be nil
// (e.g. raw tag IDs with no TypeInfo), in which case only observers fire.
// ptr is a pointer to the newly-added slot; nil for zero-size components.
func (w *World) fireOnAdd(info *component.TypeInfo, id ID, e ID, ptr unsafe.Pointer) {
	if info != nil && info.Hooks.OnAdd != nil {
		info.Hooks.OnAdd(&w.writeCapability, e, ptr)
	}
	w.dispatchObservers(id, EventOnAdd, e, ptr)
}

// fireOnSet invokes the OnSet hook (if set) then dispatches observers for id.
// Dispatch order: hook first, then observers in registration order.
// id must be the component/pair entity ID being set; info may be nil.
// ptr is a pointer to the component slot after the value was written.
func (w *World) fireOnSet(info *component.TypeInfo, id ID, e ID, ptr unsafe.Pointer) {
	if info != nil && info.Hooks.OnSet != nil {
		info.Hooks.OnSet(&w.writeCapability, e, ptr)
	}
	w.dispatchObservers(id, EventOnSet, e, ptr)
}

// fireOnRemove invokes the OnRemove hook (if set) then dispatches observers for id.
// Dispatch order: hook first, then observers in registration order.
// id must be the component/tag/pair entity ID being removed; info may be nil.
// ptr is a pointer to the source slot; the value is still valid at call time.
func (w *World) fireOnRemove(info *component.TypeInfo, id ID, e ID, ptr unsafe.Pointer) {
	if info != nil && info.Hooks.OnRemove != nil {
		info.Hooks.OnRemove(&w.writeCapability, e, ptr)
	}
	w.dispatchObservers(id, EventOnRemove, e, ptr)
}
