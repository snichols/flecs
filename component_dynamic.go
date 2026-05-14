package flecs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"unsafe"

	"github.com/snichols/flecs/internal/storage/table"
)

// dynamicMarshalHooks holds optional custom marshal/unmarshal hooks for a dynamic component.
// When registered via RegisterDynamicComponentWithMarshaler, these override the default
// base64-encoded bytes representation used by MarshalJSON / UnmarshalJSON.
type dynamicMarshalHooks struct {
	marshal   func(ptr unsafe.Pointer) (json.RawMessage, error)
	unmarshal func(data json.RawMessage, ptr unsafe.Pointer) error
}

// RegisterDynamicComponent allocates a new component entity in w whose layout is
// determined at runtime by size and alignment rather than a Go type.
//
// Dynamic components store their data as opaque bytes. They route through the
// same archetype / sparse / DontFragment machinery as typed components.
// Typed helpers (Get[T], Each[T], Field[T], OnAdd[T]) are unusable for dynamic
// components; use GetIDPtr, SetIDPtr, and EachByID for raw-pointer access.
//
// size must be a multiple of alignment so that every row in a column is correctly
// aligned. Panics on name collision with a previously-registered dynamic component.
func RegisterDynamicComponent(fw *Writer, name string, size, alignment uintptr) ID {
	w := fw.world
	w.checkExclusiveAccessWrite()
	info := w.registry.RegisterDynamicByName(name, size, alignment)
	id := w.index.Alloc()
	w.registry.AssociateID(info, id)
	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "dynamic component registered",
			slog.String("name", name),
			slog.Uint64("id", uint64(id)),
			slog.Uint64("size", uint64(size)))
	}
	return id
}

// RegisterDynamicComponentWithMarshaler is like RegisterDynamicComponent but registers
// custom JSON marshal/unmarshal functions for the component.
//
// marshal receives a pointer to the component value and must return valid JSON bytes.
// unmarshal receives JSON bytes and a pointer to write the decoded value into.
// Both functions are called by MarshalJSON and UnmarshalJSON.
func RegisterDynamicComponentWithMarshaler(
	fw *Writer, name string, size, alignment uintptr,
	marshal func(ptr unsafe.Pointer) (json.RawMessage, error),
	unmarshal func(data json.RawMessage, ptr unsafe.Pointer) error,
) ID {
	id := RegisterDynamicComponent(fw, name, size, alignment)
	w := fw.world
	if w.dynamicMarshalers == nil {
		w.dynamicMarshalers = make(map[ID]dynamicMarshalHooks)
	}
	w.dynamicMarshalers[id] = dynamicMarshalHooks{marshal: marshal, unmarshal: unmarshal}
	return id
}

// GetIDPtr returns a raw pointer to the component's live storage slot for entity e,
// or nil if e does not hold componentID or componentID is not registered.
//
// Pointer lifetime: the returned pointer is valid until the next archetype migration
// on e (e.g., adding or removing any component). Migration reallocates the backing
// column and renders the pointer stale. Always re-obtain via a fresh GetIDPtr call
// after any structural change to the entity.
//
// Works for both dynamic and typed component IDs.
func GetIDPtr(s scope, e ID, componentID ID) unsafe.Pointer {
	w := s.scopeWorld()
	w.checkExclusiveAccessRead()
	info, ok := w.registry.LookupByID(componentID)
	if !ok || info.Size == 0 {
		return nil
	}
	if !componentID.IsPair() {
		iIdx := ID(componentID.Index())
		if w.sparsePolicies[iIdx] || w.dontFragmentPolicies[iIdx] {
			return sparseSetGet(w, e, componentID)
		}
	}
	rec := w.index.Get(e)
	if rec == nil || rec.Table == nil {
		return nil
	}
	if !rec.Table.HasComponent(componentID) {
		return nil
	}
	return rec.Table.Get(int(rec.Row), componentID)
}

// SetIDPtr copies size bytes from src into the component slot for entity e.
// Fires OnAdd / OnSet / OnReplace exactly like typed Set.
//
// src must point to exactly size bytes where size was specified at registration.
// A size mismatch corrupts column memory; the caller is responsible for size
// discipline at both registration and access sites.
//
// When the world is deferred (inside DeferBegin/DeferEnd), the operation is
// queued and applied on flush, matching Set[T] and SetByID semantics.
//
// Works for both dynamic and typed component IDs.
func SetIDPtr(fw *Writer, e ID, componentID ID, src unsafe.Pointer) {
	w := fw.world
	w.checkExclusiveAccessWrite()
	info, ok := w.registry.LookupByID(componentID)
	if !ok {
		panic(fmt.Sprintf("flecs: SetIDPtr: component id %d is not registered", uint64(componentID)))
	}
	s := fw.stage
	if s.deferDepth > 0 && info.Size > 0 && src != nil {
		off, buf := s.queue.arena.alloc(int(info.Size), int(info.Align))
		copy(buf, unsafe.Slice((*byte)(src), info.Size))
		s.queue.append(cmd{kind: cmdSetByID, entity: e, id: componentID,
			valueOff: off, valueSize: uint32(info.Size)})
		return
	}
	setImmediateByPtr(w, e, componentID, src, info)
}

// EachByID iterates all entities that hold componentID, calling fn with the entity
// ID and a raw pointer to its component value. Iteration order is undefined across
// archetype tables.
//
// The pointer passed to fn is valid only for the duration of the call; do not
// retain it past fn's return. For tag components (size == 0) the pointer is nil.
//
// Works for both dynamic and typed component IDs.
func EachByID(s scope, componentID ID, fn func(e ID, ptr unsafe.Pointer)) {
	w := s.scopeWorld()
	w.checkExclusiveAccessRead()
	_, ok := w.registry.LookupByID(componentID)
	if !ok {
		return
	}
	iIdx := ID(componentID.Index())
	if !componentID.IsPair() {
		if w.sparsePolicies[iIdx] || w.dontFragmentPolicies[iIdx] {
			ss, ssOK := w.sparseStorage[iIdx]
			if !ssOK {
				return
			}
			for _, entry := range ss.dense {
				fn(entry.entity, entry.data)
			}
			return
		}
	}
	w.compIndex.Each(componentID, func(t *table.Table) bool {
		entities := t.Entities()
		for i, eid := range entities {
			if w.index.IsAlive(eid) {
				fn(eid, t.Get(i, componentID))
			}
		}
		return true
	})
}

// OnAddByID registers fn as the OnAdd hook for the component identified by componentID.
// fn is called once when the component is newly added to an entity.
// Suitable for dynamic components where the Go type is unknown at compile time.
// If fn is nil, any existing OnAdd hook for componentID is cleared.
func OnAddByID(w *World, componentID ID, fn func(fw *Writer, e ID, ptr unsafe.Pointer)) {
	w.checkExclusiveAccessWrite()
	info, ok := w.registry.LookupByID(componentID)
	if !ok {
		panic(fmt.Sprintf("flecs: OnAddByID: component id %d is not registered", uint64(componentID)))
	}
	if fn == nil {
		info.Hooks.OnAdd = nil
		return
	}
	info.Hooks.OnAdd = func(world any, e ID, ptr unsafe.Pointer) {
		fn(world.(*Writer), e, ptr)
	}
}

// OnSetByID registers fn as the OnSet hook for the component identified by componentID.
// fn is called every time the component's value is written via SetIDPtr or SetByID.
// Suitable for dynamic components where the Go type is unknown at compile time.
// If fn is nil, any existing OnSet hook for componentID is cleared.
func OnSetByID(w *World, componentID ID, fn func(fw *Writer, e ID, ptr unsafe.Pointer)) {
	w.checkExclusiveAccessWrite()
	info, ok := w.registry.LookupByID(componentID)
	if !ok {
		panic(fmt.Sprintf("flecs: OnSetByID: component id %d is not registered", uint64(componentID)))
	}
	if fn == nil {
		info.Hooks.OnSet = nil
		return
	}
	info.Hooks.OnSet = func(world any, e ID, ptr unsafe.Pointer) {
		fn(world.(*Writer), e, ptr)
	}
}

// OnRemoveByID registers fn as the OnRemove hook for the component identified by componentID.
// fn is called before the component is removed from an entity, including on entity delete.
// Suitable for dynamic components where the Go type is unknown at compile time.
// If fn is nil, any existing OnRemove hook for componentID is cleared.
func OnRemoveByID(w *World, componentID ID, fn func(fw *Writer, e ID, ptr unsafe.Pointer)) {
	w.checkExclusiveAccessWrite()
	info, ok := w.registry.LookupByID(componentID)
	if !ok {
		panic(fmt.Sprintf("flecs: OnRemoveByID: component id %d is not registered", uint64(componentID)))
	}
	if fn == nil {
		info.Hooks.OnRemove = nil
		return
	}
	info.Hooks.OnRemove = func(world any, e ID, ptr unsafe.Pointer) {
		fn(world.(*Writer), e, ptr)
	}
}

// getIDPtrRaw returns a raw pointer to the component slot for id on entity e,
// bypassing all access checks. Used internally by marshal.
func getIDPtrRaw(w *World, e ID, componentID ID) unsafe.Pointer {
	info, ok := w.registry.LookupByID(componentID)
	if !ok || info.Size == 0 {
		return nil
	}
	if !componentID.IsPair() {
		iIdx := ID(componentID.Index())
		if w.sparsePolicies[iIdx] || w.dontFragmentPolicies[iIdx] {
			return sparseSetGet(w, e, componentID)
		}
	}
	rec := w.index.Get(e)
	if rec == nil || rec.Table == nil {
		return nil
	}
	if !rec.Table.HasComponent(componentID) {
		return nil
	}
	return rec.Table.Get(int(rec.Row), componentID)
}
