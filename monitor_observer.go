package flecs

import (
	"context"
	"log/slog"

	"github.com/snichols/flecs/internal/storage/table"
)

// monitorObserver holds internal state for one monitor subscription.
//
// Archetype-only monitors (sparseMode=false) use a table-pair check on each
// migrate call: the monitor's terms are evaluated against the entity's previous
// and new archetype tables and the callback fires on entry/exit transitions.
// No per-entity state is stored — O(monitors×terms) per migration.
//
// Sparse-mode monitors (sparseMode=true, triggered by any And/Not term with
// Sparse, DontFragment, or Union flags) use a per-monitor matched set and are
// re-evaluated at each relevant component-change site (add/remove of any
// component referenced by the monitor's terms).
type monitorObserver struct {
	terms        []Term          // sorted: And, Not, Or-groups, Optional
	orGroups     [][]ID          // pre-extracted OR-groups from the sorted terms
	skipDisabled bool            // implicit-skip: exclude tables with the Disabled tag
	skipPrefab   bool            // implicit-skip: exclude tables with the Prefab tag
	termIDs      map[ID]struct{} // IDs in any And/Not/Or term; used for relevance check
	callback     func(fw *Writer, e ID, entered bool)
	observer     *Observer
	matched      map[ID]struct{} // sparse-mode only: entities currently matching this monitor
	sparseMode   bool            // true if any And/Not term has Sparse, DontFragment, or Union
}

// Monitor registers fn as a monitor observer that fires when an entity starts
// or stops matching the multi-term query described by terms.
//
// fn is called with entered=true the first time the entity satisfies all terms,
// and with entered=false when it stops matching. Unlike regular component
// observers, a monitor fires at most once per enter/exit transition.
//
// Canonical uses: alert systems, state machines, tutorial triggers, debug
// counters, and any logic that tracks entry and exit from a query predicate.
//
// terms must include at least one With (TermAnd) term. All constraints from
// NewQueryFromTerms apply: no zero-ID Or terms, no duplicate IDs.
//
// Returns an *Observer handle; call Unsubscribe to stop receiving events.
func Monitor(w *World, terms []Term, fn func(fw *Writer, e ID, entered bool)) *Observer {
	return MonitorWithOptions(w, terms, ObserverOptions{}, fn)
}

// MonitorWithOptions is the options-bearing variant of Monitor.
// WithYieldExisting() sweeps the world at registration time and fires
// fn(fw, e, true) for every entity currently matching the terms, skipping
// Disabled and Prefab tables. The sweep is synchronous: MonitorWithOptions
// returns only after all matching entities are visited.
//
// Returns an *Observer handle; call Unsubscribe to stop receiving events.
func MonitorWithOptions(w *World, terms []Term, opts ObserverOptions, fn func(fw *Writer, e ID, entered bool)) *Observer {
	w.checkExclusiveAccessWrite()

	sorted, _, orGroups, _ := validateAndSortTerms(w, "flecs: Monitor", terms)
	skipDisabled, skipPrefab := computeQuerySkipFlags(w, sorted)

	// Sparse mode: any And/Not term that references a non-archetype component
	// (DontFragment, Union) or a sparse-set-stored component (Sparse flag) forces
	// per-entity matched-set tracking. All-archetype monitors use the faster
	// table-pair check on each migrate call.
	sparseMode := false
	for _, t := range sorted {
		if t.Kind == TermAnd || t.Kind == TermNot {
			if t.Sparse || t.DontFragment || t.Union {
				sparseMode = true
				break
			}
		}
	}

	// Build a set of component IDs referenced by any And/Not/Or term for quick
	// relevance filtering in fireSparseMonitors.
	termIDs := make(map[ID]struct{}, len(sorted))
	for _, t := range sorted {
		if t.Kind == TermAnd || t.Kind == TermNot {
			termIDs[t.ID] = struct{}{}
		}
	}
	for _, g := range orGroups {
		for _, id := range g {
			termIDs[id] = struct{}{}
		}
	}

	obs := &Observer{w: w, enabled: true}
	m := &monitorObserver{
		terms:        sorted,
		orGroups:     orGroups,
		skipDisabled: skipDisabled,
		skipPrefab:   skipPrefab,
		termIDs:      termIDs,
		callback:     fn,
		observer:     obs,
		sparseMode:   sparseMode,
	}
	if sparseMode {
		m.matched = make(map[ID]struct{})
	}

	// A dummy node is added to obs.nodes so that Unsubscribe() correctly sets
	// obs.nodes = nil, which fireArchetypeMonitors / fireSparseMonitors use to
	// detect unsubscribed monitors (len(m.observer.nodes) == 0).
	node := &observerNode{observer: obs}
	obs.nodes = append(obs.nodes, node)

	if w.monitors == nil {
		w.monitors = make([]*monitorObserver, 0, 4)
	}
	// Compact dead (unsubscribed) monitors lazily on each new registration.
	if len(w.monitors) > 0 {
		live := w.monitors[:0]
		for _, existing := range w.monitors {
			if len(existing.observer.nodes) > 0 {
				live = append(live, existing)
			}
		}
		w.monitors = live
	}
	w.monitors = append(w.monitors, m)

	if opts.yieldExisting {
		monitorSweepExisting(w, m)
	}

	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "monitor registered",
			slog.Int("terms", len(sorted)),
			slog.Bool("sparse_mode", sparseMode))
	}
	return obs
}

// monitorSweepExisting fires fn(fw, e, true) for every entity currently
// matching the monitor. Skips Disabled and Prefab tables, matching the
// yield_existing behavior of ObserveWithOptions.
func monitorSweepExisting(w *World, m *monitorObserver) {
	// Find the first TermAnd term that exists in the archetype tables (not
	// DontFragment, not Union) to use as the iteration seed.
	var seedID ID
	for _, t := range m.terms {
		if t.Kind != TermAnd {
			continue
		}
		if t.DontFragment || t.Union {
			continue
		}
		seedID = t.ID
		break
	}
	if seedID == 0 {
		return // all terms are DontFragment/Union; no archetype seed available
	}

	tables := w.compIndex.TablesFor(seedID)
	for _, t := range tables {
		if !monitorMatchesTable(w, m, t) {
			continue
		}
		for _, e := range t.Entities() {
			if m.sparseMode && !entityMatchesMonitorExcluding(w, m, e, 0) {
				continue
			}
			if m.sparseMode {
				m.matched[e] = struct{}{}
			}
			m.callback(&w.writeCapability, e, true)
		}
	}
}

// monitorMatchesTable returns true if archetype table t satisfies all of the
// monitor's non-sparse terms: TermAnd IDs must be present; TermNot IDs must be
// absent; OR-groups must have at least one ID present. DontFragment and Union
// terms are skipped (they never appear in archetype type signatures).
// Implicit Disabled/Prefab exclusion is applied when skipDisabled/skipPrefab is set.
func monitorMatchesTable(w *World, m *monitorObserver, t *table.Table) bool {
	if t == nil {
		return false
	}
	if m.skipDisabled && t.HasComponent(w.disabledID) {
		return false
	}
	if m.skipPrefab && t.HasComponent(w.prefabID) {
		return false
	}
	for _, term := range m.terms {
		switch term.Kind {
		case TermAnd:
			if term.DontFragment || term.Union {
				continue // not in archetype; skip for table-level check
			}
			if !t.HasComponent(term.ID) {
				return false
			}
		case TermNot:
			if term.DontFragment || term.Union {
				continue // not in archetype
			}
			if t.HasComponent(term.ID) {
				return false
			}
		}
	}
	for _, group := range m.orGroups {
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

// termsMatchTable returns true if archetype table t satisfies the given terms and OR-groups.
// DontFragment and Union terms are skipped (they never appear in the archetype type).
// Implicit Disabled/Prefab exclusion is applied when skipDisabled/skipPrefab is set.
// This is a generalized form of monitorMatchesTable that operates on raw terms rather
// than a monitorObserver struct.
func termsMatchTable(w *World, terms []Term, orGroups [][]ID, skipDisabled, skipPrefab bool, t *table.Table) bool {
	if t == nil {
		return false
	}
	if skipDisabled && t.HasComponent(w.disabledID) {
		return false
	}
	if skipPrefab && t.HasComponent(w.prefabID) {
		return false
	}
	for _, term := range terms {
		switch term.Kind {
		case TermAnd:
			if term.DontFragment || term.Union {
				continue // not in archetype; skip for table-level check
			}
			if isWildcardTerm(w, term.ID) {
				if !tableHasWildcardMatch(w, t, term.ID) {
					return false
				}
			} else if !t.HasComponent(term.ID) {
				return false
			}
		case TermNot:
			if term.DontFragment || term.Union {
				continue // not in archetype
			}
			if isWildcardTerm(w, term.ID) {
				if tableHasWildcardMatch(w, t, term.ID) {
					return false
				}
			} else if t.HasComponent(term.ID) {
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

// entityMatchesTerms evaluates the full term set for entity e.
// This is a generalized form of entityMatchesMonitorExcluding without the excludeID
// complication; use it where events fire after storage changes (no in-flight removal).
// Wildcard pair terms (e.g. With(MakePair(R, Wildcard))) are handled via
// tableHasWildcardMatch on the entity's archetype table.
func entityMatchesTerms(w *World, terms []Term, orGroups [][]ID, e ID) bool {
	rec := w.index.Get(e)
	for _, term := range terms {
		switch term.Kind {
		case TermAnd:
			if isWildcardTerm(w, term.ID) {
				if rec == nil || rec.Table == nil || !tableHasWildcardMatch(w, rec.Table, term.ID) {
					return false
				}
			} else if !entityHasComponentForMonitor(w, e, term) {
				return false
			}
		case TermNot:
			if isWildcardTerm(w, term.ID) {
				if rec != nil && rec.Table != nil && tableHasWildcardMatch(w, rec.Table, term.ID) {
					return false
				}
			} else if entityHasComponentForMonitor(w, e, term) {
				return false
			}
			// TermOptional: no effect on matching.
		}
	}
	// OR-groups: at least one ID per group must be present.
	for _, group := range orGroups {
		matched := false
		for _, id := range group {
			if rec != nil && rec.Table != nil && rec.Table.HasComponent(id) {
				matched = true
				break
			}
			iIdx := ID(id.Index())
			if ss, ok := w.sparseStorage[iIdx]; ok {
				if _, has := ss.index[e.Index()]; has {
					matched = true
					break
				}
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// entityHasComponentForMonitor reports whether entity e currently has the
// component described by term, consulting the appropriate storage:
//   - Union pair: union store (active target must match term's second ID)
//   - DontFragment: per-component sparse-set index
//   - Sparse-only or regular: entity's archetype table (rec.Table)
func entityHasComponentForMonitor(w *World, e ID, term Term) bool {
	if term.Union {
		relIdx := ID(term.ID.First().Index())
		us, ok := w.unionStore[relIdx]
		if !ok {
			return false
		}
		entityKey := ID(e.Index())
		pos, has := us.index[entityKey]
		if !has {
			return false
		}
		return us.dense[pos].target.Index() == term.ID.Second().Index()
	}
	if term.DontFragment {
		iIdx := ID(term.ID.Index())
		ss, ok := w.sparseStorage[iIdx]
		if !ok {
			return false
		}
		_, has := ss.index[e.Index()]
		return has
	}
	// Sparse-only or regular archetype component: check the entity's table.
	rec := w.index.Get(e)
	if rec == nil || rec.Table == nil {
		return false
	}
	return rec.Table.HasComponent(term.ID)
}

// entityMatchesMonitorExcluding evaluates the full monitor term set for entity e,
// treating excludeID as absent. This is used on the OnRemove path where the
// component is still present in storage at call time.
// Pass excludeID=0 for normal (no-exclusion) evaluation.
func entityMatchesMonitorExcluding(w *World, m *monitorObserver, e ID, excludeID ID) bool {
	for _, term := range m.terms {
		switch term.Kind {
		case TermAnd:
			if excludeID != 0 && term.ID == excludeID {
				return false // component being removed → treat as absent
			}
			if !entityHasComponentForMonitor(w, e, term) {
				return false
			}
		case TermNot:
			if excludeID != 0 && term.ID == excludeID {
				continue // being removed → now absent → TermNot satisfied
			}
			if entityHasComponentForMonitor(w, e, term) {
				return false
			}
			// TermOptional: no effect on matching.
		}
	}
	// OR-groups: at least one ID per group must be present.
	rec := w.index.Get(e)
	for _, group := range m.orGroups {
		matched := false
		for _, id := range group {
			if excludeID != 0 && id == excludeID {
				continue
			}
			// Check archetype (covers both regular and Sparse-only IDs).
			if rec != nil && rec.Table != nil && rec.Table.HasComponent(id) {
				matched = true
				break
			}
			// Check sparse-set (DontFragment components are not in the archetype type).
			iIdx := ID(id.Index())
			if ss, ok := w.sparseStorage[iIdx]; ok {
				if _, has := ss.index[e.Index()]; has {
					matched = true
					break
				}
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// fireArchetypeMonitors fires all archetype-only (non-sparse-mode) monitors for
// entity e that transitioned from prevTable to nextTable. Called at the end of
// migrate() after the entity's record is updated to nextTable.
//
// For each monitor: if prevTable matched and nextTable does not → entered=false.
// If nextTable matches and prevTable does not → entered=true. No fire if both
// tables have the same match result for a given monitor.
func (w *World) fireArchetypeMonitors(e ID, prevTable, nextTable *table.Table) {
	for _, m := range w.monitors {
		if len(m.observer.nodes) == 0 || !m.observer.enabled {
			continue
		}
		if m.sparseMode {
			continue // handled by fireSparseMonitors
		}
		prevMatch := monitorMatchesTable(w, m, prevTable)
		nextMatch := monitorMatchesTable(w, m, nextTable)
		if prevMatch == nextMatch {
			continue
		}
		m.callback(&w.writeCapability, e, nextMatch)
	}
}

// fireSparseMonitors fires all sparse-mode monitors for entity e when component
// changedID changes state. Only monitors with a term referencing changedID are
// evaluated.
//
// Call this AFTER the component-change operation takes effect in storage,
// so that entityMatchesMonitorExcluding sees the updated state. The sole
// exception is Union pair removal via removeIDImmediate, which also fires AFTER
// the store is updated — pass excludeID=0 and ensure the store is already updated.
//
// For both entry and exit: compares the entity's new match state against its
// previous state (tracked in m.matched) and fires the callback on transitions.
func (w *World) fireSparseMonitors(e ID, changedID ID, excludeID ID) {
	for _, m := range w.monitors {
		if len(m.observer.nodes) == 0 || !m.observer.enabled {
			continue
		}
		if !m.sparseMode {
			continue
		}
		if _, relevant := m.termIDs[changedID]; !relevant {
			continue
		}
		_, prevMatched := m.matched[e]
		nowMatched := entityMatchesMonitorExcluding(w, m, e, excludeID)
		if prevMatched == nowMatched {
			continue
		}
		if nowMatched {
			m.matched[e] = struct{}{}
		} else {
			delete(m.matched, e)
		}
		m.callback(&w.writeCapability, e, nowMatched)
	}
}

// fireMonitorsOnDelete fires exit events (entered=false) for all monitors that
// currently match entity e, then removes e from all sparse-mode matched sets.
// Called at the start of entity deletion (deleteOne) and entity clearing
// (clearImmediate), BEFORE any component removal, so that monitor callbacks
// can still read the entity's components.
func (w *World) fireMonitorsOnDelete(e ID, currentTable *table.Table) {
	for _, m := range w.monitors {
		if len(m.observer.nodes) == 0 || !m.observer.enabled {
			continue
		}
		if m.sparseMode {
			if _, was := m.matched[e]; was {
				delete(m.matched, e)
				m.callback(&w.writeCapability, e, false)
			}
		} else {
			if monitorMatchesTable(w, m, currentTable) {
				m.callback(&w.writeCapability, e, false)
			}
		}
	}
}
