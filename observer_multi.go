package flecs

import (
	"context"
	"log/slog"
	"unsafe"
)

// ObserveQuery registers fn as a multi-term observer for a single event.
// terms[0] must be TermAnd — it is the trigger component (the dispatch key).
// terms[1:] are filter terms evaluated against the affected entity at fire time.
// The callback receives a raw pointer to the trigger component's storage; may be
// nil for zero-size components or tags.
//
// Short-circuit: the first failing filter term causes the callback to be skipped.
// Terms are snapshotted at registration; mutations to the original slice have no effect.
//
// Panics if len(terms) == 0 or terms[0].Kind != TermAnd.
//
// Returns an *Observer handle; call Unsubscribe to cancel.
func ObserveQuery(w *World, event EventKind, terms []Term, fn func(fw *Writer, e ID, ptr unsafe.Pointer)) *Observer {
	validateMultiTermTrigger("flecs: ObserveQuery", terms)
	triggerID := terms[0].ID
	sorted, _, orGroups, _ := validateAndSortTerms(w, "flecs: ObserveQuery", terms)
	skipDisabled, skipPrefab := computeQuerySkipFlags(w, sorted)
	return observeQueryCore(w, ObserverOptions{}, triggerID, []EventKind{event}, sorted, orGroups,
		skipDisabled, skipPrefab,
		func(fw *Writer, _ EventKind, e ID, ptr unsafe.Pointer) { fn(fw, e, ptr) })
}

// ObserveQueryID registers fn as a multi-term observer using an explicit trigger ID
// rather than deriving it from terms[0]. This variant is useful for raw-ID or pair-ID
// triggers that are inconvenient to express as a Term.
//
// triggerID is the dispatch key (what component/pair triggers the event).
// filterTerms are evaluated against the affected entity at fire time; they need not
// include a TermAnd for the trigger (it is implied by the event having fired).
// Pass nil or an empty slice for a single-term observer.
//
// Panics if triggerID == 0.
//
// Returns an *Observer handle; call Unsubscribe to cancel.
func ObserveQueryID(w *World, triggerID ID, event EventKind, filterTerms []Term, fn func(fw *Writer, e ID, ptr unsafe.Pointer)) *Observer {
	if triggerID == 0 {
		panic("flecs: ObserveQueryID: triggerID must be non-zero")
	}
	sorted, orGroups, skipDisabled, skipPrefab := validateMultiTermFilter(w, "flecs: ObserveQueryID", triggerID, filterTerms)
	return observeQueryCore(w, ObserverOptions{}, triggerID, []EventKind{event}, sorted, orGroups,
		skipDisabled, skipPrefab,
		func(fw *Writer, _ EventKind, e ID, ptr unsafe.Pointer) { fn(fw, e, ptr) })
}

// ObserveQueryEvents registers fn as a multi-term observer for multiple event kinds.
// terms[0] must be TermAnd (the trigger); terms[1:] are filter terms.
// fn receives the EventKind that triggered it.
//
// Panics if len(terms) == 0 or terms[0].Kind != TermAnd.
//
// Returns a single *Observer handle; Unsubscribe cancels all event subscriptions.
func ObserveQueryEvents(w *World, events []EventKind, terms []Term, fn func(fw *Writer, event EventKind, e ID, ptr unsafe.Pointer)) *Observer {
	return ObserveQueryWithOptions(w, ObserverOptions{}, events, terms, fn)
}

// ObserveQueryWithOptions is the options-bearing variant of ObserveQueryEvents.
// terms[0] must be TermAnd (the trigger); terms[1:] are filter terms.
// fn receives the EventKind that triggered it.
//
// Supported options:
//   - [WithYieldExisting]: sweeps the world at registration and fires fn once per
//     entity already matching the full term set, for each OnAdd/OnSet event in events.
//     Panics if events contains only OnRemove.
//   - [WithSource]: restricts the observer to a specific source entity; the
//     fixed-source check runs before the multi-term filter (dispatch table key).
//
// Panics if len(terms) == 0 or terms[0].Kind != TermAnd.
//
// Returns a single *Observer handle; Unsubscribe cancels all event subscriptions.
func ObserveQueryWithOptions(w *World, opts ObserverOptions, events []EventKind, terms []Term, fn func(fw *Writer, event EventKind, e ID, ptr unsafe.Pointer)) *Observer {
	validateMultiTermTrigger("flecs: ObserveQueryWithOptions", terms)
	triggerID := terms[0].ID
	sorted, _, orGroups, _ := validateAndSortTerms(w, "flecs: ObserveQueryWithOptions", terms)
	skipDisabled, skipPrefab := computeQuerySkipFlags(w, sorted)
	return observeQueryCore(w, opts, triggerID, events, sorted, orGroups, skipDisabled, skipPrefab, fn)
}

// validateMultiTermTrigger checks the common preconditions for ObserveQuery*.
func validateMultiTermTrigger(caller string, terms []Term) {
	if len(terms) == 0 {
		panic(caller + ": at least one term is required")
	}
	if terms[0].Kind != TermAnd {
		panic(caller + ": first term must be TermAnd (the trigger component)")
	}
}

// validateMultiTermFilter validates and sorts filterTerms for ObserveQueryID.
// It synthesises a trigger term so that validateAndSortTerms (which requires >= 1
// TermAnd) can be reused. The returned sortedTerms include the trigger term.
func validateMultiTermFilter(w *World, caller string, triggerID ID, filterTerms []Term) (sortedTerms []Term, orGroups [][]ID, skipDisabled, skipPrefab bool) {
	allTerms := make([]Term, 0, 1+len(filterTerms))
	allTerms = append(allTerms, With(triggerID))
	allTerms = append(allTerms, filterTerms...)
	var andIDs []ID
	sortedTerms, andIDs, orGroups, _ = validateAndSortTerms(w, caller, allTerms)
	_ = andIDs
	skipDisabled, skipPrefab = computeQuerySkipFlags(w, sortedTerms)
	return
}

// observeQueryCore is the shared implementation for all ObserveQuery* variants.
// triggerID is the dispatch key registered in the observer table.
// sortedTerms are the full validated and sorted terms (trigger included) stored in
// the node's filter snapshot; the trigger term is always satisfied at dispatch time
// so including it is a no-op but keeps validation uniform.
func observeQueryCore(
	w *World,
	opts ObserverOptions,
	triggerID ID,
	events []EventKind,
	sortedTerms []Term,
	orGroups [][]ID,
	skipDisabled, skipPrefab bool,
	fn func(fw *Writer, event EventKind, e ID, ptr unsafe.Pointer),
) *Observer {
	w.checkExclusiveAccessWrite()

	if len(events) == 0 {
		panic("flecs: multi-term observer: at least one event is required")
	}

	if opts.source != 0 {
		for _, ev := range events {
			if ev == EventOnTableCreate || ev == EventOnTableEmpty || ev == EventOnTableFill {
				panic("flecs: ObserveQueryWithOptions: WithSource is not compatible with table events; tables have no source entity semantics")
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
			panic("flecs: ObserveQueryWithOptions: yieldExisting requires at least one OnAdd or OnSet event; OnRemove-only observers cannot yield existing entities at registration time")
		}
	}

	// Build the filter snapshot. The snapshot is shared across all event nodes for
	// the same observer (events for a single observer all share the same terms).
	var mf *multiTermFilter
	if len(sortedTerms) > 0 {
		sparseMode := false
		for _, t := range sortedTerms {
			if t.Kind == TermAnd || t.Kind == TermNot {
				if t.Sparse || t.DontFragment || t.Union {
					sparseMode = true
					break
				}
			}
		}
		mf = &multiTermFilter{
			filterTerms:    sortedTerms,
			filterOrGroups: orGroups,
			skipDisabled:   skipDisabled,
			skipPrefab:     skipPrefab,
			sparseMode:     sparseMode,
		}
	}

	obs := &Observer{w: w, enabled: true}

	var sweepCallbacks []func(fw *Writer, e ID, ptr unsafe.Pointer)

	for _, ev := range events {
		ev := ev
		callback := func(fw *Writer, e ID, ptr unsafe.Pointer) { fn(fw, ev, e, ptr) }
		node := w.addObserverNode(triggerID, eventKindToEntity(w, ev), opts.source, callback)
		node.observer = obs
		node.multiFilter = mf
		obs.nodes = append(obs.nodes, node)
		if opts.yieldExisting && (ev == EventOnAdd || ev == EventOnSet) {
			sweepCallbacks = append(sweepCallbacks, callback)
		}
		if w.logger != nil {
			w.logger.LogAttrs(context.Background(), slog.LevelDebug, "multi-term observer registered",
				slog.Uint64("trigger", uint64(triggerID)),
				slog.String("event", ev.String()),
				slog.Int("terms", len(sortedTerms)))
		}
	}

	if opts.yieldExisting && obs.enabled && len(sweepCallbacks) > 0 {
		multiTermYieldExisting(w, opts, triggerID, mf, sweepCallbacks)
	}

	return obs
}

// multiTermYieldExisting fires sweep callbacks for entities already matching
// the full multi-term filter at registration time.
func multiTermYieldExisting(
	w *World,
	opts ObserverOptions,
	triggerID ID,
	mf *multiTermFilter,
	sweepCallbacks []func(fw *Writer, e ID, ptr unsafe.Pointer),
) {
	if opts.source != 0 {
		// Fixed-source: single entity check, O(1).
		ptr, has, skip := entityRawPtrForYield(w, opts.source, triggerID)
		if !has || skip {
			return
		}
		if mf != nil && !entityMatchesTerms(w, mf.filterTerms, mf.filterOrGroups, opts.source) {
			return
		}
		for _, sc := range sweepCallbacks {
			sc(&w.writeCapability, opts.source, ptr)
		}
		return
	}

	// Any-entity sweep: walk archetype tables containing the trigger component.
	// mf is always non-nil here (observeQueryCore always sets it from sortedTerms which
	// always has at least the trigger term); the nil-check is a safety guard only.
	tables := w.compIndex.TablesFor(triggerID)
	for _, t := range tables {
		if mf != nil && !termsMatchTable(w, mf.filterTerms, mf.filterOrGroups, mf.skipDisabled, mf.skipPrefab, t) {
			continue
		}
		if t.HasComponent(w.disabledID) || t.HasComponent(w.prefabID) {
			continue
		}
		entities := t.Entities()
		for i, e := range entities {
			if mf != nil && mf.sparseMode && !entityMatchesTerms(w, mf.filterTerms, mf.filterOrGroups, e) {
				continue
			}
			ptr := t.Get(i, triggerID)
			for _, sc := range sweepCallbacks {
				sc(&w.writeCapability, e, ptr)
			}
		}
	}
}
