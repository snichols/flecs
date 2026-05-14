package flecs

import (
	"context"
	"log/slog"
	"unsafe"
)

// EventKind identifies which lifecycle event an observer subscribes to.
type EventKind int

const (
	// EventOnAdd fires when a component is newly added to an entity.
	EventOnAdd EventKind = 1
	// EventOnSet fires when a component value is written (including the initial write after OnAdd).
	EventOnSet EventKind = 2
	// EventOnRemove fires before a component is removed from an entity, including on entity deletion.
	EventOnRemove EventKind = 3
	// EventOnTableCreate fires once per archetype table when the table is first
	// created (first entity migrates into a previously-unseen component signature).
	// Does not fire for the world's initial empty table.
	EventOnTableCreate EventKind = 4
)

// String returns a human-readable name for the event kind.
func (ev EventKind) String() string {
	switch ev {
	case EventOnAdd:
		return "OnAdd"
	case EventOnSet:
		return "OnSet"
	case EventOnRemove:
		return "OnRemove"
	case EventOnTableCreate:
		return "OnTableCreate"
	default:
		return "Unknown"
	}
}

// observerKey is the composite map key for the observer store.
// id is the component/tag entity ID (or a sentinel for table-create/custom events).
// eventEntity is the built-in or user-allocated event entity ID.
type observerKey struct {
	id          ID
	eventEntity ID
}

// eventKindToEntity maps the convenience EventKind enum to the corresponding
// built-in event entity ID. Returns 0 for unknown kinds.
func eventKindToEntity(w *World, ev EventKind) ID {
	switch ev {
	case EventOnAdd:
		return w.eventOnAddID
	case EventOnSet:
		return w.eventOnSetID
	case EventOnRemove:
		return w.eventOnRemoveID
	case EventOnTableCreate:
		return w.eventOnTableCreateID
	default:
		return 0
	}
}

// observerNode holds the internal state of one observer subscription.
// removed is set to true by Unsubscribe; the node is skipped on the next
// dispatch and compacted lazily on the next registration for the same key.
// observer is a back-pointer to the owning Observer handle; used by
// dispatchObservers to consult the enabled flag without a separate map lookup.
type observerNode struct {
	key      observerKey
	callback func(fw *Writer, e ID, ptr unsafe.Pointer)
	observer *Observer
	removed  bool
}

// Observer is an opaque handle returned by Observe[T], ObserveID, Observe2[T],
// or ObserveWithOptions[T]. Call Unsubscribe to stop receiving events.
// Observer is NOT goroutine-safe.
type Observer struct {
	w       *World
	nodes   []*observerNode
	enabled bool // false = skip in dispatchObservers; default true
}

// Unsubscribe removes all subscriptions held by this observer. Idempotent:
// safe to call multiple times. After Unsubscribe returns, this observer will
// not fire again.
//
// If Unsubscribe is called from within a callback during an active dispatch,
// the removal takes effect immediately: observers not yet visited in the
// current dispatch iteration are skipped. Observers that have already fired
// in the current event are unaffected (they have already been called).
func (o *Observer) Unsubscribe() {
	if len(o.nodes) == 0 {
		return
	}
	for _, n := range o.nodes {
		n.removed = true
	}
	if o.w != nil && o.w.logger != nil {
		o.w.logger.LogAttrs(context.Background(), slog.LevelDebug, "observer unsubscribed")
	}
	o.nodes = nil
}

// SetEnabled enables or disables this observer for event dispatch.
// A disabled observer is silently skipped in dispatchObservers but remains
// registered and can be re-enabled at any time. Default is true (enabled).
// Idempotent.
//
// Intended for serial use outside an active dispatch. Toggling from within a
// callback is safe (the change is visible to observers not yet visited in the
// current dispatch iteration; already-fired observers in that dispatch are
// unaffected).
func (o *Observer) SetEnabled(v bool) { o.enabled = v }

// IsEnabled reports whether this observer is currently enabled for dispatch.
func (o *Observer) IsEnabled() bool { return o.enabled }

// Observe registers fn as an observer for component T on the given event.
// T is auto-registered as a component entity (matching the OnSet[T] convention)
// if it has not already been registered. The observer fires after any hook
// registered for the same (T, event), and after observers registered earlier.
// Returns an *Observer handle; call Unsubscribe to cancel.
//
// The *Writer passed to fn is a non-nil Writer scoped to the current world.
func Observe[T any](w *World, event EventKind, fn func(fw *Writer, e ID, v T)) *Observer {
	w.checkExclusiveAccessWrite()
	id := RegisterComponent[T](w)
	callback := func(fw *Writer, e ID, ptr unsafe.Pointer) {
		var v T
		if ptr != nil {
			v = *(*T)(ptr)
		}
		fn(fw, e, v)
	}
	obs := &Observer{w: w, enabled: true}
	node := w.addObserverNode(id, eventKindToEntity(w, event), callback)
	node.observer = obs
	obs.nodes = append(obs.nodes, node)
	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "observer registered",
			slog.Uint64("id", uint64(id)),
			slog.String("event", event.String()))
	}
	return obs
}

// ObserveID registers fn as a raw observer for the given id on the given event.
// id may be any entity ID, pair ID, or tag ID; no TypeInfo auto-registration
// is performed. The observer fires whenever that exact id appears in a fire
// helper call (AddID, Set, SetPair, Delete paths).
// Returns an *Observer handle; call Unsubscribe to cancel.
//
// The *Writer passed to fn is a non-nil Writer scoped to the current world.
func ObserveID(w *World, id ID, event EventKind, fn func(fw *Writer, e ID, ptr unsafe.Pointer)) *Observer {
	w.checkExclusiveAccessWrite()
	obs := &Observer{w: w, enabled: true}
	node := w.addObserverNode(id, eventKindToEntity(w, event), fn)
	node.observer = obs
	obs.nodes = append(obs.nodes, node)
	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "observer registered",
			slog.Uint64("id", uint64(id)),
			slog.String("event", event.String()))
	}
	return obs
}

// Observe2 registers fn as an observer for component T on multiple event kinds.
// The fn callback receives the EventKind that triggered it, allowing a single
// function to handle Add, Set, and Remove events. Returns a single *Observer
// handle; Unsubscribe cancels all subscriptions.
//
// The *Writer passed to fn is a non-nil Writer scoped to the current world.
func Observe2[T any](w *World, events []EventKind, fn func(fw *Writer, event EventKind, e ID, v T)) *Observer {
	w.checkExclusiveAccessWrite()
	id := RegisterComponent[T](w)
	obs := &Observer{w: w, enabled: true}
	for _, ev := range events {
		ev := ev
		callback := func(fw *Writer, e ID, ptr unsafe.Pointer) {
			var v T
			if ptr != nil {
				v = *(*T)(ptr)
			}
			fn(fw, ev, e, v)
		}
		node := w.addObserverNode(id, eventKindToEntity(w, ev), callback)
		node.observer = obs
		obs.nodes = append(obs.nodes, node)
		if w.logger != nil {
			w.logger.LogAttrs(context.Background(), slog.LevelDebug, "observer registered",
				slog.Uint64("id", uint64(id)),
				slog.String("event", ev.String()))
		}
	}
	return obs
}

// addObserverNode creates an observerNode for (id, eventEntity) and appends it to
// the world's observer map, compacting removed entries lazily before insertion.
func (w *World) addObserverNode(id ID, eventEntity ID, callback func(fw *Writer, e ID, ptr unsafe.Pointer)) *observerNode {
	if w.observers == nil {
		w.observers = make(map[observerKey][]*observerNode)
	}
	key := observerKey{id: id, eventEntity: eventEntity}
	// Compact removed nodes lazily on each new registration for this key.
	if existing := w.observers[key]; len(existing) > 0 {
		live := existing[:0]
		for _, n := range existing {
			if !n.removed {
				live = append(live, n)
			}
		}
		w.observers[key] = live
	}
	node := &observerNode{key: key, callback: callback}
	w.observers[key] = append(w.observers[key], node)
	return node
}

// dispatchObservers fires all active observers for (id, eventEntity) in registration
// order. Observers with removed=true are skipped. An observer that calls
// Unsubscribe during its callback takes effect immediately: not-yet-visited
// observers in the same dispatch are skipped; already-fired observers are
// unaffected.
func (w *World) dispatchObservers(id ID, eventEntity ID, e ID, ptr unsafe.Pointer) {
	if w.observers == nil {
		return
	}
	key := observerKey{id: id, eventEntity: eventEntity}
	nodes := w.observers[key]
	for _, n := range nodes {
		if n.removed || (n.observer != nil && !n.observer.enabled) {
			continue
		}
		n.callback(&w.writeCapability, e, ptr)
	}
}
