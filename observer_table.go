package flecs

import (
	"context"
	"log/slog"
	"sort"
	"unsafe"
)

// tableCreateSentinelID is the observer-map key used for EventOnTableCreate
// subscriptions. Table-create observers are untyped (no component filter), so
// there is no real component ID to key on. ID(0) is safe because valid entity
// and component IDs start at 1 in Go-flecs.
const tableCreateSentinelID ID = 0

// OnTableCreate registers fn as an observer that fires once per archetype table
// when the table is first created. An archetype table is created the first time
// any entity migrates into a previously-unseen component signature.
//
// Does not fire for the world's initial empty table, matching upstream's is_root
// suppression (table.c:1278).
//
// Unlike OnAdd[T]/OnSet[T], OnTableCreate is untyped: it fires for every new
// archetype regardless of which components it contains. The handler reads the
// table's full signature via t.Type() and the current row count via t.Count().
// Mutations to the world must go through fw (deferred).
//
// Returns an *Observer handle; call Unsubscribe to stop receiving events.
func OnTableCreate(w *World, fn func(fw *Writer, t *Table)) *Observer {
	return OnTableCreateWithOptions(w, ObserverOptions{}, fn)
}

// OnTableCreateWithOptions is OnTableCreate with additional options such as
// WithYieldExisting.
//
// If opts carries WithYieldExisting(), fn is called synchronously at
// registration time for every existing table (excluding the empty root table),
// in sorted-signature order (deterministic within a single run). Newly created
// tables continue to fire fn via the normal notifyTableCreated path.
func OnTableCreateWithOptions(w *World, opts ObserverOptions, fn func(fw *Writer, t *Table)) *Observer {
	w.checkExclusiveAccessWrite()

	callback := func(fw *Writer, _ ID, ptr unsafe.Pointer) {
		fn(fw, (*Table)(ptr))
	}

	obs := &Observer{w: w, enabled: true}
	node := w.addObserverNode(tableCreateSentinelID, w.eventOnTableCreateID, 0, callback)
	node.observer = obs
	obs.nodes = append(obs.nodes, node)

	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "observer registered",
			slog.String("event", EventOnTableCreate.String()))
	}

	if opts.yieldExisting && obs.enabled {
		// Collect and sort keys for deterministic iteration order.
		keys := make([]string, 0, len(w.tables))
		for k := range w.tables {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			t := w.tables[k]
			if len(t.Type()) == 0 {
				continue // skip the empty root table
			}
			fn(&w.writeCapability, t)
		}
	}

	return obs
}
