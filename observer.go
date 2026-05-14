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
	// EventMonitor is the built-in event kind for monitor observers. It is not used
	// for direct observer dispatch; it identifies the built-in EventMonitor entity
	// returned by World.EventMonitor() and marks the monitor subscription kind in
	// the EventKind enum for symmetry with other built-in event entities.
	EventMonitor EventKind = 5
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
	case EventMonitor:
		return "Monitor"
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

// observerBucket holds all observers for a single (component, event) pair.
//
// anyEntity holds observers that fire for every entity — the common case.
// fixedSource maps a specific source entity to observers that fire only when
// the event lands on that entity. fixedSource is nil until the first fixed-source
// observer is registered for this bucket, so any-entity-only dispatch pays zero
// overhead for the fixed-source path.
//
// Dispatch order: anyEntity observers fire before fixedSource observers for the
// same (component, event) key. Within each list, registration order is preserved.
type observerBucket struct {
	anyEntity   []*observerNode
	fixedSource map[ID][]*observerNode
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
	case EventMonitor:
		return w.eventMonitorID
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
	node := w.addObserverNode(id, eventKindToEntity(w, event), 0, callback)
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
	node := w.addObserverNode(id, eventKindToEntity(w, event), 0, fn)
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
		node := w.addObserverNode(id, eventKindToEntity(w, ev), 0, callback)
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

// ObserveIDWithOptions registers fn as a raw-ID observer for id on the given events,
// with additional options. This is the options-bearing variant of ObserveID; it
// supports WithSource(e) to restrict the observer to a specific source entity and
// WithYieldExisting() to retroactively fire for entities already carrying id.
//
// Panics if opts.source is set and any event in events is EventOnTableCreate
// (tables have no source entity semantics).
//
// Fixed-source observers registered here fire only when the event's entity
// matches the source. Any-entity observers (source == 0) preserve today's
// behaviour: every entity's event fires the callback.
func ObserveIDWithOptions(w *World, id ID, opts ObserverOptions, events []EventKind, fn func(fw *Writer, e ID, ptr unsafe.Pointer)) *Observer {
	w.checkExclusiveAccessWrite()

	if opts.source != 0 {
		for _, ev := range events {
			if ev == EventOnTableCreate {
				panic("flecs: ObserveIDWithOptions: WithSource is not compatible with EventOnTableCreate; tables have no source entity semantics")
			}
		}
	}

	if opts.yieldExisting {
		onlyRemove := true
		for _, ev := range events {
			if ev != EventOnRemove {
				onlyRemove = false
				break
			}
		}
		if onlyRemove {
			panic("flecs: ObserveIDWithOptions: yieldExisting requires at least one OnAdd or OnSet event; OnRemove-only observers cannot yield existing entities at registration time")
		}
	}

	obs := &Observer{w: w, enabled: true}

	type sweepEntry struct {
		ev       EventKind
		callback func(fw *Writer, e ID, ptr unsafe.Pointer)
	}
	var sweepCallbacks []sweepEntry

	for _, ev := range events {
		ev := ev
		node := w.addObserverNode(id, eventKindToEntity(w, ev), opts.source, fn)
		node.observer = obs
		obs.nodes = append(obs.nodes, node)
		if opts.yieldExisting && (ev == EventOnAdd || ev == EventOnSet) {
			sweepCallbacks = append(sweepCallbacks, sweepEntry{ev: ev, callback: fn})
		}
		if w.logger != nil {
			w.logger.LogAttrs(context.Background(), slog.LevelDebug, "observer registered",
				slog.Uint64("id", uint64(id)),
				slog.String("event", ev.String()))
		}
	}

	if opts.yieldExisting && obs.enabled && len(sweepCallbacks) > 0 {
		if opts.source != 0 {
			ptr, has, skip := entityRawPtrForYield(w, opts.source, id)
			if has && !skip {
				for _, sc := range sweepCallbacks {
					sc.callback(&w.writeCapability, opts.source, ptr)
				}
			}
		} else {
			tables := w.compIndex.TablesFor(id)
			for _, sc := range sweepCallbacks {
				for _, t := range tables {
					if t.HasComponent(w.disabledID) || t.HasComponent(w.prefabID) {
						continue
					}
					entities := t.Entities()
					for i, e := range entities {
						ptr := t.Get(i, id)
						sc.callback(&w.writeCapability, e, ptr)
					}
				}
			}
		}
	}

	return obs
}

// entityRawPtrForYield looks up component id on entity e and returns
// (ptr, has, skipDisabledPrefab). has is false when the entity is absent or does
// not own id. skipDisabledPrefab is true when the entity's archetype table carries
// the Disabled or Prefab tag — callers should not fire the yield callback in that
// case. ptr may be nil for zero-size / tag components even when has is true.
func entityRawPtrForYield(w *World, e ID, id ID) (ptr unsafe.Pointer, has, skip bool) {
	if !id.IsPair() {
		iIdx := ID(id.Index())
		if w.sparsePolicies[iIdx] || w.dontFragmentPolicies[iIdx] {
			p := sparseSetGet(w, e, id)
			if p == nil {
				return nil, false, false
			}
			// Sparse component found; check disabled/prefab via the archetype table.
			rec := w.index.Get(e)
			if rec != nil && rec.Table != nil {
				skip = rec.Table.HasComponent(w.disabledID) || rec.Table.HasComponent(w.prefabID)
			}
			return p, true, skip
		}
	}
	rec := w.index.Get(e)
	if rec == nil || rec.Table == nil {
		return nil, false, false
	}
	if !rec.Table.HasComponent(id) {
		return nil, false, false
	}
	skip = rec.Table.HasComponent(w.disabledID) || rec.Table.HasComponent(w.prefabID)
	return rec.Table.Get(int(rec.Row), id), true, skip
}

// addObserverNode creates an observerNode for (id, eventEntity) and appends it to
// the world's observer map, compacting removed entries lazily before insertion.
// source == 0 registers an any-entity observer (fires for every entity);
// source != 0 registers a fixed-source observer (fires only when the event's
// entity matches source).
func (w *World) addObserverNode(id ID, eventEntity ID, source ID, callback func(fw *Writer, e ID, ptr unsafe.Pointer)) *observerNode {
	if w.observers == nil {
		w.observers = make(map[observerKey]*observerBucket)
	}
	key := observerKey{id: id, eventEntity: eventEntity}
	bucket := w.observers[key]
	if bucket == nil {
		bucket = &observerBucket{}
		w.observers[key] = bucket
	}
	node := &observerNode{key: key, callback: callback}
	if source == 0 {
		// Any-entity: compact removed nodes lazily, then append.
		live := bucket.anyEntity[:0]
		for _, n := range bucket.anyEntity {
			if !n.removed {
				live = append(live, n)
			}
		}
		bucket.anyEntity = append(live, node)
	} else {
		// Fixed-source: allocate the per-source map on first use.
		if bucket.fixedSource == nil {
			bucket.fixedSource = make(map[ID][]*observerNode)
		}
		existing := bucket.fixedSource[source]
		live := existing[:0]
		for _, n := range existing {
			if !n.removed {
				live = append(live, n)
			}
		}
		bucket.fixedSource[source] = append(live, node)
	}
	return node
}

// dispatchObservers fires all active observers for (id, eventEntity, e) in
// registration order. Any-entity observers fire first, then fixed-source
// observers for entity e (if any are registered). Observers with removed=true
// or disabled are skipped. An observer that calls Unsubscribe during its
// callback takes effect immediately for not-yet-visited observers.
func (w *World) dispatchObservers(id ID, eventEntity ID, e ID, ptr unsafe.Pointer) {
	if w.observers == nil {
		return
	}
	key := observerKey{id: id, eventEntity: eventEntity}
	bucket := w.observers[key]
	if bucket == nil {
		return
	}
	for _, n := range bucket.anyEntity {
		if n.removed || (n.observer != nil && !n.observer.enabled) {
			continue
		}
		n.callback(&w.writeCapability, e, ptr)
	}
	if bucket.fixedSource != nil {
		for _, n := range bucket.fixedSource[e] {
			if n.removed || (n.observer != nil && !n.observer.enabled) {
				continue
			}
			n.callback(&w.writeCapability, e, ptr)
		}
	}
}
