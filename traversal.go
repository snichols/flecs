package flecs

import "github.com/snichols/flecs/internal/component"

// maxTraversalDepth is the maximum number of relationship hops past the
// starting entity that walkUp will follow. It is a defence-in-depth guard:
// cycle detection already terminates malformed graphs, but the depth cap
// ensures termination even if cycle detection ever has a latent bug or the
// graph contains a legitimate but pathologically long chain.
const maxTraversalDepth = 64

// walkUp walks the relationship rel up from e, invoking fn at each entity
// starting with e itself (Self+Up semantics). If fn returns true for an entity,
// walkUp stops and returns (entity, true). If no entity in the chain satisfies
// fn, returns (0, false).
//
// Dead target guard: if a target entity in the chain is not alive the walk
// terminates with (0, false), matching the IsA dead-prefab semantic.
//
// Cycle detection: a seen map is allocated lazily only after the first step
// past e (zero allocation when fn matches on e itself). Detected cycles
// terminate cleanly.
//
// Depth limit: at most maxTraversalDepth steps past e are followed. Chains
// deeper than maxTraversalDepth return (0, false) without panicking.
func walkUp(w *World, e ID, rel ID, fn func(ID) bool) (ID, bool) {
	rec := w.index.Get(e)
	if rec == nil {
		return 0, false
	}
	// Depth 0: check e itself — no seen-map allocation.
	if fn(e) {
		return e, true
	}
	// Find the first parent via parent column (parent-storage) or archetype pair.
	target, ok := w.firstParentVia(e, rel)
	if !ok {
		return 0, false
	}
	// Walk up the chain. Allocate seen lazily on the first step past e.
	var seen map[ID]struct{}
	current := target
	for depth := 1; depth <= maxTraversalDepth; depth++ {
		if !w.index.IsAlive(current) {
			return 0, false
		}
		if seen == nil {
			seen = map[ID]struct{}{e: {}}
		}
		if _, visited := seen[current]; visited {
			return 0, false
		}
		seen[current] = struct{}{}
		if fn(current) {
			return current, true
		}
		next, hasNext := w.firstParentVia(current, rel)
		if !hasNext {
			return 0, false
		}
		current = next
	}
	// Depth limit exceeded.
	return 0, false
}

// firstParentVia returns the first parent of entity e via relationship rel.
// When parent-storage is active for rel, reads from the parent column;
// otherwise searches the archetype signature for the first (rel, target) pair.
func (w *World) firstParentVia(e ID, rel ID) (ID, bool) {
	rec := w.index.Get(e)
	if rec == nil || rec.Table == nil {
		return 0, false
	}
	relKey := ID(rel.Index())
	if w.parentStoragePolicies[relKey] {
		marker := w.parentStorageMarker(rel)
		if rec.Table.HasComponent(marker) {
			return rec.Table.GetParentEntry(int(rec.Row), relKey)
		}
		return 0, false
	}
	return firstPairTarget(rec.Table.Type(), rel.Index())
}

// getUpInternal walks the relationship rel up from e (self-first), returning the value
// of component T from the first entity in the chain that locally owns T.
func getUpInternal[T any](w *World, e ID, rel ID) (T, bool) {
	var zero T
	info, ok := component.LookupByType[T](w.registry)
	if !ok || info.Component == 0 {
		return zero, false
	}
	cid := info.Component
	owner, found := walkUp(w, e, rel, func(cur ID) bool {
		rec := w.index.Get(cur)
		if rec == nil {
			return false
		}
		return rec.Table != nil && rec.Table.HasComponent(cid)
	})
	if !found {
		return zero, false
	}
	rec := w.index.Get(owner)
	if rec == nil {
		return zero, false
	}
	ptr := rec.Table.Get(int(rec.Row), cid)
	if ptr == nil {
		return zero, true
	}
	return *(*T)(ptr), true
}

// hasUpInternal reports whether e or any ancestor reachable via rel locally owns the
// component identified by id.
func hasUpInternal(w *World, e ID, id ID, rel ID) bool {
	_, found := walkUp(w, e, rel, func(cur ID) bool {
		rec := w.index.Get(cur)
		if rec == nil {
			return false
		}
		return rec.Table != nil && rec.Table.HasComponent(id)
	})
	return found
}

// targetUpInternal returns the ID of the first entity in the chain (e or an ancestor
// via rel) that locally owns the component identified by id.
func targetUpInternal(w *World, e ID, id ID, rel ID) (ID, bool) {
	return walkUp(w, e, rel, func(cur ID) bool {
		rec := w.index.Get(cur)
		if rec == nil {
			return false
		}
		return rec.Table != nil && rec.Table.HasComponent(id)
	})
}
