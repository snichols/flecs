package flecs

import (
	"unsafe"

	"github.com/snichols/flecs/internal/storage/table"
)

// propagateEvent fires eventEntity for each transitive inheritor of sourceEntity,
// subject to the DontInherit and override (local-copy) gates. Called after the
// local dispatchObservers for the same (componentID, eventEntity).
//
// Propagation is IsA-only and downward (source → inheritors). Monitors do not
// receive propagated fires; propagation is a property of the inheritance relationship,
// not of the observer. Future phases may generalise to other Traversable relationships.
func (w *World) propagateEvent(componentID, eventEntity, sourceEntity ID, ptr unsafe.Pointer) {
	// DontInherit gate: if the component is marked DontInherit, inheritors don't
	// see it via the IsA chain, so propagating its events would be misleading.
	if w.instantiatePolicies[componentID]&policyOnInstantiateDontInherit != 0 {
		return
	}
	for _, inh := range w.inheritorsBFS(sourceEntity) {
		if !w.index.IsAlive(inh) {
			continue
		}
		rec := w.index.Get(inh)
		// Override gate: if the inheritor owns its own copy of the component, its
		// local value masks the inherited one — skip, mirroring upstream observable.c:1083.
		if rec != nil && rec.Table != nil && rec.Table.HasComponent(componentID) {
			continue
		}
		w.dispatchObserversForPropagation(componentID, eventEntity, inh, ptr)
	}
}

// dispatchObserversForPropagation is the propagation-aware variant of
// dispatchObservers. For multi-term observers it skips the trigger component
// term in the filter (because the inheritor has the component via IsA
// inheritance, not as a locally-owned archetype column). Single-term and
// any-entity observers fire unconditionally (subject to enabled flag).
func (w *World) dispatchObserversForPropagation(componentID, eventEntity, inh ID, ptr unsafe.Pointer) {
	if w.observers == nil {
		return
	}
	key := observerKey{id: componentID, eventEntity: eventEntity}
	bucket := w.observers[key]
	if bucket == nil {
		return
	}
	for _, n := range bucket.anyEntity {
		if n.removed || (n.observer != nil && !n.observer.enabled) {
			continue
		}
		if n.multiFilter != nil && inh != 0 &&
			!entityMatchesTermsForPropagation(w, n.multiFilter.filterTerms, n.multiFilter.filterOrGroups, inh, componentID) {
			continue
		}
		n.callback(&w.writeCapability, inh, ptr)
	}
	if bucket.fixedSource != nil {
		for _, n := range bucket.fixedSource[inh] {
			if n.removed || (n.observer != nil && !n.observer.enabled) {
				continue
			}
			if n.multiFilter != nil && inh != 0 &&
				!entityMatchesTermsForPropagation(w, n.multiFilter.filterTerms, n.multiFilter.filterOrGroups, inh, componentID) {
				continue
			}
			n.callback(&w.writeCapability, inh, ptr)
		}
	}
}

// entityMatchesTermsForPropagation is like entityMatchesTerms but treats any
// term whose ID equals inheritedID (the propagated trigger component) as
// automatically satisfied. This reflects that the inheritor has the component
// via the IsA chain, even though it is absent from its own archetype table.
func entityMatchesTermsForPropagation(w *World, terms []Term, orGroups [][]ID, e ID, inheritedID ID) bool {
	rec := w.index.Get(e)
	for _, term := range terms {
		switch term.Kind {
		case TermAnd:
			// Trigger component: always satisfied for propagated events.
			if term.ID == inheritedID {
				continue
			}
			if isWildcardTerm(w, term.ID) {
				if rec == nil || rec.Table == nil || !tableHasWildcardMatch(w, rec.Table, term.ID) {
					return false
				}
			} else if !entityHasComponentForMonitor(w, e, term) {
				return false
			}
		case TermNot:
			if term.ID == inheritedID {
				// Inherited component is present — NOT term fails.
				return false
			}
			if isWildcardTerm(w, term.ID) {
				if rec != nil && rec.Table != nil && tableHasWildcardMatch(w, rec.Table, term.ID) {
					return false
				}
			} else if entityHasComponentForMonitor(w, e, term) {
				return false
			}
		}
	}
	// OR-groups: at least one ID per group must be present.
	for _, group := range orGroups {
		matched := false
		for _, id := range group {
			if id == inheritedID {
				matched = true
				break
			}
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

// propagateReplaceHook calls fn for each transitive inheritor of sourceEntity
// that does not own its own copy of componentID. Used by fireOnReplace to
// propagate the OnReplace hook call to inheritors with the same old/new ptrs.
func (w *World) propagateReplaceHook(componentID, sourceEntity ID, fn func(inh ID)) {
	if w.instantiatePolicies[componentID]&policyOnInstantiateDontInherit != 0 {
		return
	}
	for _, inh := range w.inheritorsBFS(sourceEntity) {
		if !w.index.IsAlive(inh) {
			continue
		}
		rec := w.index.Get(inh)
		if rec != nil && rec.Table != nil && rec.Table.HasComponent(componentID) {
			continue
		}
		fn(inh)
	}
}

// inheritorsBFS returns the BFS-ordered slice of all direct and transitive
// inheritors of prefab. The result is cached; the entire cache is cleared
// whenever any (IsA, *) structural change occurs (add or remove of an IsA pair),
// which ensures correctness for multi-level chains.
//
// The returned slice is owned by the cache; callers must not mutate it.
func (w *World) inheritorsBFS(prefab ID) []ID {
	if w.inheritorCache != nil {
		if cached, ok := w.inheritorCache[prefab]; ok {
			return cached
		}
	}
	result := buildInheritorsBFS(w, prefab)
	if w.inheritorCache == nil {
		w.inheritorCache = make(map[ID][]ID)
	}
	w.inheritorCache[prefab] = result
	return result
}

// buildInheritorsBFS performs a BFS starting from prefab, collecting all entities
// that transitively have (IsA, prefab) — i.e. all entities that inherit from prefab
// directly or via an intermediate prefab. The result excludes prefab itself.
// A visited set prevents infinite loops on self-cycles or mutual cycles.
func buildInheritorsBFS(w *World, prefab ID) []ID {
	var result []ID
	visited := map[ID]struct{}{prefab: {}}
	queue := []ID{prefab}
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		pairID := MakePair(w.isAID, curr)
		w.compIndex.Each(pairID, func(t *table.Table) bool {
			for _, inh := range t.Entities() {
				if _, seen := visited[inh]; seen {
					continue
				}
				visited[inh] = struct{}{}
				result = append(result, inh)
				queue = append(queue, inh)
			}
			return true
		})
	}
	return result
}

// invalidateInheritorCache clears the entire inheritor cache. Clearing all
// entries is necessary because adding (IsA, B) to C also stales the cache for
// any ancestor of B (e.g. P where B IsA P), not just B's own entry.
// Called whenever any (IsA, *) pair is added or removed from any entity.
func (w *World) invalidateInheritorCache() {
	w.inheritorCache = nil
}
