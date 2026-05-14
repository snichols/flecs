package flecs

import (
	"context"
	"log/slog"
	"unsafe"
)

// ObserverOptions carries optional configuration for [ObserveWithOptions].
// Construct via [WithYieldExisting] or use the zero value for no options.
type ObserverOptions struct {
	yieldExisting bool
}

// WithYieldExisting returns options that retroactively fire the observer for
// every entity that already matches the component at registration time.
//
// Supported events: OnAdd and OnSet. For each such event in the subscription
// list, the sweep delivers one callback invocation per existing matching entity
// with the subscribed event kind. OnRemove events are silently skipped in the
// sweep; registering with yieldExisting=true and only OnRemove events panics.
//
// The sweep walks archetype tables containing the component ID, skipping tables
// that carry the Disabled or Prefab tag (mirrors ordinary query exclusion from
// Phase 16.2). Iteration order is archetype-table order (the order tables were
// registered in compIndex). The sweep is synchronous: ObserveWithOptions
// returns only after all matching entities are visited.
//
// The sweep fires the newly-registered observer's callback directly; it does
// NOT route through dispatchObservers, so peer observers already subscribed to
// the same event are not re-fired.
func WithYieldExisting() ObserverOptions {
	return ObserverOptions{yieldExisting: true}
}

// ObserveWithOptions registers fn as an observer for component T on the given
// events, with additional options (e.g. [WithYieldExisting]). fn receives the
// EventKind that triggered it, enabling a single callback to handle multiple
// event kinds. Returns a single *Observer handle; Unsubscribe cancels all
// subscriptions.
//
// If opts carries [WithYieldExisting], the sweep runs synchronously before
// ObserveWithOptions returns: for each event in {OnAdd, OnSet} that is in the
// events list, all entities already carrying component T (excluding Disabled
// and Prefab) receive a callback invocation. Panics if yieldExisting is true
// and the events list contains only OnRemove.
//
// The *Writer passed to fn is a non-nil Writer scoped to the current world.
func ObserveWithOptions[T any](w *World, opts ObserverOptions, events []EventKind, fn func(fw *Writer, event EventKind, e ID, v T)) *Observer {
	w.checkExclusiveAccessWrite()

	if opts.yieldExisting {
		onlyRemove := true
		for _, ev := range events {
			if ev != EventOnRemove {
				onlyRemove = false
				break
			}
		}
		if onlyRemove {
			panic("flecs: ObserveWithOptions: yieldExisting requires at least one OnAdd or OnSet event; OnRemove-only observers cannot yield existing entities at registration time")
		}
	}

	id := RegisterComponent[T](w)
	obs := &Observer{w: w, enabled: true}

	// sweepCallbacks collects (event, callback) pairs for yield_existing.
	type sweepEntry struct {
		ev       EventKind
		callback func(fw *Writer, e ID, ptr unsafe.Pointer)
	}
	var sweepCallbacks []sweepEntry

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

		if opts.yieldExisting && (ev == EventOnAdd || ev == EventOnSet) {
			sweepCallbacks = append(sweepCallbacks, sweepEntry{ev: ev, callback: callback})
		}

		if w.logger != nil {
			w.logger.LogAttrs(context.Background(), slog.LevelDebug, "observer registered",
				slog.Uint64("id", uint64(id)),
				slog.String("event", ev.String()))
		}
	}

	// Yield existing: fire the callback directly (not via dispatchObservers)
	// for each entity that already carries the component.
	if opts.yieldExisting && obs.enabled && len(sweepCallbacks) > 0 {
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

	return obs
}
