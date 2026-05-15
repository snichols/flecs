package flecs

import (
	"context"
	"log/slog"
	"sort"
	"unsafe"

	"github.com/snichols/flecs/internal/storage/table"
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

// OnTableEmpty registers fn as an observer that fires when a table's row count
// transitions from 1 → 0 (the last entity is removed from the table).
//
// The handler receives the table that became empty. Mutations to the world must
// go through fw (deferred). Returns an *Observer handle; call Unsubscribe to stop.
func OnTableEmpty(w *World, fn func(fw *Writer, t *Table)) *Observer {
	return OnTableEmptyWithOptions(w, ObserverOptions{}, fn)
}

// OnTableEmptyWithOptions is OnTableEmpty with additional options such as
// WithYieldExisting or WithQuery.
//
// If opts carries WithYieldExisting(), fn is called synchronously at
// registration time for every currently-empty table (including the root empty
// table if it matches), in sorted-signature order.
//
// If opts carries WithQuery(terms...), fn fires only for tables whose component
// signature matches all specified terms.
func OnTableEmptyWithOptions(w *World, opts ObserverOptions, fn func(fw *Writer, t *Table)) *Observer {
	w.checkExclusiveAccessWrite()

	callback := func(fw *Writer, _ ID, ptr unsafe.Pointer) {
		fn(fw, (*Table)(ptr))
	}

	obs := &Observer{w: w, enabled: true}
	node := w.addObserverNode(tableCreateSentinelID, w.eventOnTableEmptyID, 0, callback)
	node.observer = obs
	if len(opts.filterTerms) > 0 {
		node.multiFilter = &multiTermFilter{
			filterTerms: append([]Term(nil), opts.filterTerms...),
		}
	}
	obs.nodes = append(obs.nodes, node)

	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "observer registered",
			slog.String("event", EventOnTableEmpty.String()))
	}

	if opts.yieldExisting && obs.enabled {
		keys := make([]string, 0, len(w.tables))
		for k := range w.tables {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			t := w.tables[k]
			if t.Count() == 0 {
				if node.multiFilter != nil && !tableMatchesTerms(t, node.multiFilter.filterTerms, node.multiFilter.filterOrGroups) {
					continue
				}
				fn(&w.writeCapability, t)
			}
		}
	}

	return obs
}

// OnTableFill registers fn as an observer that fires when a table's row count
// transitions from 0 → 1 (the first entity is added to the table).
//
// The handler receives the table that became non-empty. Mutations to the world
// must go through fw (deferred). Returns an *Observer handle; call Unsubscribe
// to stop.
func OnTableFill(w *World, fn func(fw *Writer, t *Table)) *Observer {
	return OnTableFillWithOptions(w, ObserverOptions{}, fn)
}

// OnTableFillWithOptions is OnTableFill with additional options such as
// WithYieldExisting or WithQuery.
//
// If opts carries WithYieldExisting(), fn is called synchronously at
// registration time for every currently non-empty table (excluding the empty
// root table), in sorted-signature order.
//
// If opts carries WithQuery(terms...), fn fires only for tables whose component
// signature matches all specified terms.
func OnTableFillWithOptions(w *World, opts ObserverOptions, fn func(fw *Writer, t *Table)) *Observer {
	w.checkExclusiveAccessWrite()

	callback := func(fw *Writer, _ ID, ptr unsafe.Pointer) {
		fn(fw, (*Table)(ptr))
	}

	obs := &Observer{w: w, enabled: true}
	node := w.addObserverNode(tableCreateSentinelID, w.eventOnTableFillID, 0, callback)
	node.observer = obs
	if len(opts.filterTerms) > 0 {
		node.multiFilter = &multiTermFilter{
			filterTerms: append([]Term(nil), opts.filterTerms...),
		}
	}
	obs.nodes = append(obs.nodes, node)

	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "observer registered",
			slog.String("event", EventOnTableFill.String()))
	}

	if opts.yieldExisting && obs.enabled {
		keys := make([]string, 0, len(w.tables))
		for k := range w.tables {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			t := w.tables[k]
			// Skip the root empty table (no component signature) — it is never
			// "filled" by user code since built-in entities already occupy it.
			if len(t.Type()) == 0 {
				continue
			}
			if t.Count() > 0 {
				if node.multiFilter != nil && !tableMatchesTerms(t, node.multiFilter.filterTerms, node.multiFilter.filterOrGroups) {
					continue
				}
				fn(&w.writeCapability, t)
			}
		}
	}

	return obs
}

// dispatchTableObservers fires all active observers for (id, eventEntity) in
// registration order, evaluating multi-term filters against the table's signature
// rather than an entity. Used for OnTableEmpty and OnTableFill events.
func (w *World) dispatchTableObservers(id ID, eventEntity ID, t *table.Table) {
	if w.observers == nil {
		return
	}
	key := observerKey{id: id, eventEntity: eventEntity}
	bucket := w.observers[key]
	if bucket == nil {
		return
	}
	ptr := unsafe.Pointer(t)
	for _, n := range bucket.anyEntity {
		if n.removed || (n.observer != nil && !n.observer.enabled) {
			continue
		}
		if n.multiFilter != nil && !tableMatchesTerms(t, n.multiFilter.filterTerms, n.multiFilter.filterOrGroups) {
			continue
		}
		n.callback(&w.writeCapability, 0, ptr)
	}
}

// tableMatchesTerms evaluates filter terms against a table's component signature.
// Used for OnTableEmpty / OnTableFill multi-term filtering.
// TermAnd requires the component to be present; TermNot requires it to be absent.
// Other term kinds are ignored (sparse/union components never appear in a table signature).
func tableMatchesTerms(t *table.Table, terms []Term, orGroups [][]ID) bool {
	for _, term := range terms {
		switch term.Kind {
		case TermAnd:
			if !t.HasComponent(term.ID) {
				return false
			}
		case TermNot:
			if t.HasComponent(term.ID) {
				return false
			}
		}
	}
	for _, group := range orGroups {
		matched := false
		for _, id := range group {
			if t.HasComponent(id) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
