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
func walkUpUnlocked(w *World, e ID, rel ID, fn func(ID) bool) (ID, bool) {
	rec := w.index.Get(e)
	if rec == nil {
		return 0, false
	}
	// Depth 0: check e itself — no seen-map allocation.
	if fn(e) {
		return e, true
	}
	// Find the first (rel, target) pair in e's signature.
	relIdx := rel.Index()
	target, ok := firstPairTarget(rec.Table.Type(), relIdx)
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
		curRec := w.index.Get(current)
		if curRec == nil || curRec.Table == nil {
			return 0, false
		}
		next, hasNext := firstPairTarget(curRec.Table.Type(), relIdx)
		if !hasNext {
			return 0, false
		}
		current = next
	}
	// Depth limit exceeded.
	return 0, false
}

// GetUp walks the relationship rel up from e (self-first), returning the value
// of component T from the first entity in the chain that locally owns T.
// Returns (zero, false) if:
//   - T is not registered in the world
//   - e is not alive
//   - no entity in the chain locally owns T
//
// Local ownership means T is present in the entity's own archetype table.
// A parent that inherits T via an IsA chain is NOT considered an owner; use
// [Get] on that parent if IsA-aware lookup is needed.
//
// Self-first: if e itself locally owns T, its value is returned without
// traversing the relationship chain (matches flecs's Self|Up flag).
//
// Cycle detection and depth limiting ([maxTraversalDepth]) are enforced
// internally; malformed graphs terminate cleanly.
func GetUp[T any](w *World, e ID, rel ID) (T, bool) {
	w.rwmu.RLock()
	defer w.rwmu.RUnlock()
	var zero T
	info, ok := component.LookupByType[T](w.registry)
	if !ok || info.Component == 0 {
		return zero, false
	}
	cid := info.Component
	owner, found := walkUpUnlocked(w, e, rel, func(cur ID) bool {
		return ownsIDUnlocked(w, cur, cid)
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

// HasUp reports whether e or any ancestor reachable via rel locally owns the
// component identified by id. Returns false if e is not alive.
//
// Self-first: e itself is checked before walking the relationship chain.
//
// Local ownership only: a parent that inherits id via IsA does not satisfy the
// predicate — only direct table ownership counts.
//
// Cheaper than [GetUp] when the component value is not needed.
func HasUp(w *World, e ID, id ID, rel ID) bool {
	w.rwmu.RLock()
	defer w.rwmu.RUnlock()
	_, found := walkUpUnlocked(w, e, rel, func(cur ID) bool {
		return ownsIDUnlocked(w, cur, id)
	})
	return found
}

// TargetUp returns the ID of the first entity in the chain (e or an ancestor
// via rel) that locally owns the component identified by id. Returns (0, false)
// if no entity in the chain locally owns id, or if e is not alive.
//
// Self-first: if e itself locally owns id, (e, true) is returned.
//
// Useful for asking "which ancestor owns this component?" for debugging or
// attribution.
//
// Local ownership only: a parent that inherits id via IsA does not satisfy the
// predicate.
func TargetUp(w *World, e ID, id ID, rel ID) (ID, bool) {
	w.rwmu.RLock()
	defer w.rwmu.RUnlock()
	return walkUpUnlocked(w, e, rel, func(cur ID) bool {
		return ownsIDUnlocked(w, cur, id)
	})
}
