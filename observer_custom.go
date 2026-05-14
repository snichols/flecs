package flecs

import (
	"context"
	"fmt"
	"log/slog"
	"unsafe"
)

// RegisterEvent allocates a new entity to serve as a custom event identifier.
// The entity is tagged with the built-in Event tag for discriminability
// (HasID(eventID, w.Event()) returns true).
//
// Custom event entities may be named, tagged, queried, and deleted like any
// other entity. Deleting the event entity unsubscribes all observers for it:
// subsequent Emit calls for the deleted event are no-ops.
//
// Emit must be called from within a Write scope. fw is the active Writer.
func RegisterEvent(fw *Writer, name string) ID {
	fw.world.checkExclusiveAccessWrite()
	e := fw.world.newEntityInternal()
	if name != "" {
		setOnWorld[Name](fw.world, e, Name{Value: name})
	}
	// Add the built-in Event tag immediately (not deferred) so the entity is
	// recognisable as an event entity before any observer is subscribed.
	addIDImmediate(fw.world, e, fw.world.eventTagID)
	if fw.world.logger != nil {
		fw.world.logger.LogAttrs(context.Background(), slog.LevelDebug, "event registered",
			slog.Uint64("id", uint64(e)),
			slog.String("name", name))
	}
	return e
}

// Emit fires eventID for entity with an opaque payload. All active observers
// subscribed via ObserveEvent or ObserveEventTyped for this eventID receive the
// callback synchronously, in registration order.
//
// Emit must be called from within a Write scope (fw is the active Writer).
// Re-entrant emit — emitting from within an observer callback — is safe and
// fires synchronously (same ordering guarantees as existing observer dispatch).
//
// The payload is shallow-copied at the API boundary: each observer receives its
// own copy of the interface value. Mutations to the interface's inner pointee
// are visible to subsequent observers; mutations to the interface value itself
// are not.
//
// If eventID has been deleted, Emit is a no-op.
func Emit(fw *Writer, eventID ID, entity ID, payload interface{}) {
	fw.world.checkExclusiveAccessWrite()
	// Shallow copy: each observer gets its own interface value.
	payloadCopy := payload
	var ptr unsafe.Pointer
	if payloadCopy != nil {
		ptr = unsafe.Pointer(&payloadCopy)
	}
	fw.world.dispatchObservers(eventID, eventID, entity, ptr)
}

// EmitTyped is the type-safe variant of Emit. The payload is wrapped in an
// interface{} and routed through the same dispatch as Emit.
func EmitTyped[T any](fw *Writer, eventID ID, entity ID, payload T) {
	Emit(fw, eventID, entity, payload)
}

// ObserveEvent subscribes fn to custom event eventID. fn is called
// synchronously whenever Emit(fw, eventID, entity, payload) is called. The
// entity argument is the entity passed to Emit; payload is the value passed to
// Emit (shallow-copied at the dispatch boundary).
//
// If opts carries WithYieldExisting(), it is silently ignored for custom events
// (there is no "currently matching" concept for an arbitrary event). The
// observer registers successfully and will fire for future Emit calls.
//
// Returns an *Observer handle; call Unsubscribe to stop receiving events.
func ObserveEvent(w *World, eventID ID, fn func(fw *Writer, e ID, payload interface{})) *Observer {
	w.checkExclusiveAccessWrite()
	callback := func(fw *Writer, e ID, ptr unsafe.Pointer) {
		var payload interface{}
		if ptr != nil {
			payload = *(*interface{})(ptr)
		}
		fn(fw, e, payload)
	}
	obs := &Observer{w: w, enabled: true}
	node := w.addObserverNode(eventID, eventID, callback)
	node.observer = obs
	obs.nodes = append(obs.nodes, node)
	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "custom event observer registered",
			slog.Uint64("eventID", uint64(eventID)))
	}
	return obs
}

// ObserveEventTyped is the typed-payload variant of ObserveEvent. fn receives
// the payload type-asserted to T. If the payload is not assignable to T, the
// callback panics with a descriptive message.
//
// Returns an *Observer handle; call Unsubscribe to stop receiving events.
func ObserveEventTyped[T any](w *World, eventID ID, fn func(fw *Writer, e ID, payload T)) *Observer {
	return ObserveEvent(w, eventID, func(fw *Writer, e ID, payload interface{}) {
		v, ok := payload.(T)
		if !ok {
			panic(fmt.Sprintf("flecs: ObserveEventTyped: payload type mismatch: expected %T, got %T", *new(T), payload))
		}
		fn(fw, e, v)
	})
}
