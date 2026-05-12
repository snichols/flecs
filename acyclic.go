package flecs

import "fmt"

// Acyclic returns the ID of the built-in Acyclic trait entity.
//
// Marking a relationship acyclic prevents cycles from being stored: adding
// (e, R, target) is rejected at AddID time if target can already reach e via
// the same relationship. This is a deliberate divergence from C flecs, which
// guards cycles at lookup/traversal time using ECS_MAX_RECURSION; see
// CHANGELOG v0.41.0 for rationale.
//
// ChildOf is bootstrapped as acyclic, preventing infinite-loop cycles in
// EachChild and related hierarchy operations.
//
// Self-pairs (a, R, a) are allowed — Acyclic does not reject them. Use
// [World.Reflexive] if you want self-pairs to be implicitly true.
//
// Usage:
//
//	flecs.SetAcyclic(w, myRelID)
//	// or bare-tag form:
//	fw.AddID(myRelID, w.Acyclic())
func (w *World) Acyclic() ID { return w.acyclicID }

// SetAcyclic marks relID as an acyclic relationship: adding a pair (e, R, target)
// will panic if target can transitively reach e via R. See [World.Acyclic] for
// full semantics.
//
// Equivalent to fw.AddID(relID, w.Acyclic()).
func SetAcyclic(w *World, relID ID) {
	applyAcyclicPolicy(w, relID)
}

// IsAcyclic reports whether relID has been marked acyclic.
func IsAcyclic(w *World, relID ID) bool {
	return w.acyclicPolicies[ID(relID.Index())]
}

// applyAcyclicPolicy writes the acyclic flag into w.acyclicPolicies keyed by
// the entity's raw index. Keying by index (not full ID) ensures that pair
// decomposition via id.First() — which strips the generation field — always
// finds the entry regardless of the relationship entity's generation counter.
// Called by SetAcyclic and by addIDImmediate when the bare Acyclic tag is
// added to a relationship entity.
func applyAcyclicPolicy(w *World, relID ID) {
	if w.acyclicPolicies == nil {
		w.acyclicPolicies = make(map[ID]bool)
	}
	w.acyclicPolicies[ID(relID.Index())] = true
}

// checkAcyclic panics if adding (e, rel, target) would create a cycle in an
// acyclic relationship. It reuses walkUp from traversal.go, which has its own
// maxTraversalDepth guard and cycle-detection seen-map, so existing cycles in
// previously stored data cannot cause infinite walks.
//
// Self-pairs (e == target) are allowed: Acyclic does not reject them.
func checkAcyclic(w *World, e, rel, target ID) {
	if e == target {
		return // self-pairs are allowed on acyclic relationships
	}
	// Walk from target upward via rel. If e is reachable, adding (e, rel, target)
	// would form a cycle.
	_, found := walkUp(w, target, rel, func(cur ID) bool {
		return cur == e
	})
	if found {
		panic(fmt.Sprintf(
			"flecs: acyclic cycle detected: entity %v already reachable from %v via relationship %v; cannot add pair (%v, %v, %v)",
			e, target, rel, e, rel, target,
		))
	}
}
