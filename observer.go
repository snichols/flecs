package flecs

import "unsafe"

// EventKind identifies which lifecycle event an observer subscribes to.
type EventKind int

const (
	// EventOnAdd fires when a component is newly added to an entity.
	EventOnAdd EventKind = 1
	// EventOnSet fires when a component value is written (including the initial write after OnAdd).
	EventOnSet EventKind = 2
	// EventOnRemove fires before a component is removed from an entity, including on entity deletion.
	EventOnRemove EventKind = 3
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
	default:
		return "Unknown"
	}
}

// observerKey is the composite map key for the observer store.
type observerKey struct {
	id    ID
	event EventKind
}

// observerNode holds the internal state of one observer subscription.
// removed is set to true by Unsubscribe; the node is skipped on the next
// dispatch and compacted lazily on the next registration for the same key.
type observerNode struct {
	key      observerKey
	callback func(e ID, ptr unsafe.Pointer)
	removed  bool
}

// Observer is an opaque handle returned by Observe[T], ObserveID, or Observe2[T].
// Call Unsubscribe to stop receiving events. Observer is NOT goroutine-safe.
type Observer struct {
	nodes []*observerNode
}

// Unsubscribe removes all subscriptions held by this observer. Idempotent:
// safe to call multiple times. After Unsubscribe returns, this observer will
// not fire again. If Unsubscribe is called from within a callback during an
// active dispatch, the removal is deferred: all observers that were active
// at the start of that dispatch still fire for the current event.
func (o *Observer) Unsubscribe() {
	for _, n := range o.nodes {
		n.removed = true
	}
	o.nodes = nil
}

// Observe registers fn as an observer for component T on the given event.
// T is auto-registered as a component entity (matching the OnSet[T] convention)
// if it has not already been registered. The observer fires after any hook
// registered for the same (T, event), and after observers registered earlier.
// Returns an *Observer handle; call Unsubscribe to cancel.
func Observe[T any](w *World, event EventKind, fn func(e ID, v *T)) *Observer {
	id := RegisterComponent[T](w)
	callback := func(e ID, ptr unsafe.Pointer) {
		fn(e, (*T)(ptr))
	}
	obs := &Observer{}
	node := w.addObserverNode(id, event, callback)
	obs.nodes = append(obs.nodes, node)
	return obs
}

// ObserveID registers fn as a raw observer for the given id on the given event.
// id may be any entity ID, pair ID, or tag ID; no TypeInfo auto-registration
// is performed. The observer fires whenever that exact id appears in a fire
// helper call (AddID, Set, SetPair, Delete paths).
// Returns an *Observer handle; call Unsubscribe to cancel.
func ObserveID(w *World, id ID, event EventKind, fn func(e ID, ptr unsafe.Pointer)) *Observer {
	obs := &Observer{}
	node := w.addObserverNode(id, event, fn)
	obs.nodes = append(obs.nodes, node)
	return obs
}

// Observe2 registers fn as an observer for component T on multiple event kinds.
// The fn callback receives the EventKind that triggered it, allowing a single
// function to handle Add, Set, and Remove events. Returns a single *Observer
// handle; Unsubscribe cancels all subscriptions.
func Observe2[T any](w *World, events []EventKind, fn func(event EventKind, e ID, v *T)) *Observer {
	id := RegisterComponent[T](w)
	obs := &Observer{}
	for _, ev := range events {
		ev := ev
		callback := func(e ID, ptr unsafe.Pointer) {
			fn(ev, e, (*T)(ptr))
		}
		node := w.addObserverNode(id, ev, callback)
		obs.nodes = append(obs.nodes, node)
	}
	return obs
}

// addObserverNode creates an observerNode for (id, event) and appends it to
// the world's observer map, compacting removed entries lazily before insertion.
func (w *World) addObserverNode(id ID, event EventKind, callback func(e ID, ptr unsafe.Pointer)) *observerNode {
	if w.observers == nil {
		w.observers = make(map[observerKey][]*observerNode)
	}
	key := observerKey{id: id, event: event}
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

// dispatchObservers fires all active observers for (id, event) in registration
// order. The active set is captured at the start of dispatch: observers that
// call Unsubscribe during a callback are marked removed but still fire for
// the current event (deferred-removal contract).
func (w *World) dispatchObservers(id ID, event EventKind, e ID, ptr unsafe.Pointer) {
	if w.observers == nil {
		return
	}
	key := observerKey{id: id, event: event}
	nodes := w.observers[key]
	if len(nodes) == 0 {
		return
	}
	// Snapshot active nodes before any callback can modify the removed flags.
	active := make([]*observerNode, 0, len(nodes))
	for _, n := range nodes {
		if !n.removed {
			active = append(active, n)
		}
	}
	for _, n := range active {
		n.callback(e, ptr)
	}
}
