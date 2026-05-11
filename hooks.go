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
// For zero-size types (tags), v in the callback points to a zero-byte location;
// dereferencing is technically valid in Go but the value is meaningless.
// T need not be registered as a component entity before calling OnAdd; if
// unregistered, its type metadata is auto-registered idempotently.
func OnAdd[T any](w *World, fn func(e ID, v *T)) {
	if !w.inProgress.Load() {
		w.rwmu.Lock()
		w.inProgress.Store(true)
		defer func() {
			w.inProgress.Store(false)
			w.rwmu.Unlock()
		}()
	}
	info := component.Register[T](w.registry)
	if fn == nil {
		info.Hooks.OnAdd = nil
		return
	}
	info.Hooks.OnAdd = func(_ any, e ID, ptr unsafe.Pointer) {
		fn(e, (*T)(ptr))
	}
}

// OnSet registers fn as the OnSet hook for component T in w.
// fn is called every time T's value is written on an entity via Set[T] or
// SetPair[T], including the initial write that follows OnAdd.
// If fn is nil, any existing OnSet hook for T is cleared.
// Calling OnSet[T] twice replaces the prior hook.
//
// For zero-size types (tags), the callback receives a nil pointer; the value
// is meaningless. OnSet does not fire for AddID (which carries no value).
func OnSet[T any](w *World, fn func(e ID, v *T)) {
	if !w.inProgress.Load() {
		w.rwmu.Lock()
		w.inProgress.Store(true)
		defer func() {
			w.inProgress.Store(false)
			w.rwmu.Unlock()
		}()
	}
	info := component.Register[T](w.registry)
	if fn == nil {
		info.Hooks.OnSet = nil
		return
	}
	info.Hooks.OnSet = func(_ any, e ID, ptr unsafe.Pointer) {
		fn(e, (*T)(ptr))
	}
}

// OnRemove registers fn as the OnRemove hook for component T in w.
// fn is called before T is removed from an entity, including when the entity
// is deleted. *v still holds the component value at the time of the call.
// If fn is nil, any existing OnRemove hook for T is cleared.
// Calling OnRemove[T] twice replaces the prior hook.
//
// For zero-size types (tags), v points to a zero-byte location.
//
// Known limitation: if the hook panics, the panic propagates through the
// calling operation and the World is left in a transitional state. Phase 5.3
// deferred commands will offer a safer pattern.
//
// Re-entrancy: read-only World operations (Get, Has, Owns, IsAlive, Count) are
// safe to call from within a hook. Mutating operations (Set, Remove, Delete) on
// the entity currently being observed have undefined behavior; defer them to
// Phase 5.3.
func OnRemove[T any](w *World, fn func(e ID, v *T)) {
	if !w.inProgress.Load() {
		w.rwmu.Lock()
		w.inProgress.Store(true)
		defer func() {
			w.inProgress.Store(false)
			w.rwmu.Unlock()
		}()
	}
	info := component.Register[T](w.registry)
	if fn == nil {
		info.Hooks.OnRemove = nil
		return
	}
	info.Hooks.OnRemove = func(_ any, e ID, ptr unsafe.Pointer) {
		fn(e, (*T)(ptr))
	}
}

// fireOnAdd invokes the OnAdd hook (if set) then dispatches observers for id.
// Dispatch order: hook first, then observers in registration order.
// id must be the component/tag/pair entity ID being added; info may be nil
// (e.g. raw tag IDs with no TypeInfo), in which case only observers fire.
// ptr is a pointer to the newly-added slot; nil for zero-size components.
func (w *World) fireOnAdd(info *component.TypeInfo, id ID, e ID, ptr unsafe.Pointer) {
	if info != nil && info.Hooks.OnAdd != nil {
		// Release write lock so the hook can call world read methods (IsAlive, Get, …)
		// without deadlocking. An inner function with a deferred reacquire ensures the
		// lock is restored even if the hook panics, so the caller's defer-Unlock is safe.
		w.inProgress.Store(false)
		w.rwmu.Unlock()
		func() {
			defer func() {
				w.rwmu.Lock()
				w.inProgress.Store(true)
			}()
			info.Hooks.OnAdd(w, e, ptr)
		}()
	}
	w.dispatchObservers(id, EventOnAdd, e, ptr)
}

// fireOnSet invokes the OnSet hook (if set) then dispatches observers for id.
// Dispatch order: hook first, then observers in registration order.
// id must be the component/pair entity ID being set; info may be nil.
// ptr is a pointer to the component slot after the value was written.
func (w *World) fireOnSet(info *component.TypeInfo, id ID, e ID, ptr unsafe.Pointer) {
	if info != nil && info.Hooks.OnSet != nil {
		w.inProgress.Store(false)
		w.rwmu.Unlock()
		func() {
			defer func() {
				w.rwmu.Lock()
				w.inProgress.Store(true)
			}()
			info.Hooks.OnSet(w, e, ptr)
		}()
	}
	w.dispatchObservers(id, EventOnSet, e, ptr)
}

// fireOnRemove invokes the OnRemove hook (if set) then dispatches observers for id.
// Dispatch order: hook first, then observers in registration order.
// id must be the component/tag/pair entity ID being removed; info may be nil.
// ptr is a pointer to the source slot; the value is still valid at call time.
func (w *World) fireOnRemove(info *component.TypeInfo, id ID, e ID, ptr unsafe.Pointer) {
	if info != nil && info.Hooks.OnRemove != nil {
		w.inProgress.Store(false)
		w.rwmu.Unlock()
		func() {
			defer func() {
				w.rwmu.Lock()
				w.inProgress.Store(true)
			}()
			info.Hooks.OnRemove(w, e, ptr)
		}()
	}
	w.dispatchObservers(id, EventOnRemove, e, ptr)
}
