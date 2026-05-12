package flecs

import "fmt"

// OneOf returns the ID of the built-in OneOf trait entity.
//
// OneOf constrains a relationship's target to entities that are direct children
// of a specified parent entity. This enables enum-style relationships where
// valid targets are known children of a parent entity.
//
// Two forms:
//   - Self-tag: fw.AddID(R, w.OneOf()) — target must be a direct child of R.
//   - Pair: fw.AddID(R, MakePair(w.OneOf(), P)) — target must be a direct child of P.
//
// Both forms are equivalent to SetOneOf(w, R, R) and SetOneOf(w, R, P) respectively.
//
// No built-in relationship ships OneOf by default (matching C bootstrap).
func (w *World) OneOf() ID { return w.oneOfID }

// SetOneOf constrains relID's targets to direct children of parentID.
// Passing parentID == relID encodes the self-tag form (target must be a direct
// child of relID itself). Equivalent to fw.AddID(relID, w.OneOf()) for the
// self-tag form, or fw.AddID(relID, MakePair(w.OneOf(), parentID)) for the pair form.
func SetOneOf(w *World, relID, parentID ID) {
	applyOneOfPolicy(w, relID, parentID)
}

// IsOneOf reports whether relID has a OneOf constraint and returns the required
// parent entity. Accepts scope so it can be called inside both Read and Write
// blocks (per Phase 15.8 convention).
func IsOneOf(s scope, relID ID) (parent ID, ok bool) {
	parent, ok = s.scopeWorld().oneOfPolicies[ID(relID.Index())]
	return
}

// applyOneOfPolicy writes the OneOf constraint into w.oneOfPolicies keyed by the
// relationship's raw index. The parent is stored as its raw index (generation
// stripped) for consistent comparison with pair-encoded target IDs, which also
// carry only the lower 32-bit index.
// Called by SetOneOf and by addIDImmediate when the bare OneOf tag or
// (OneOf, parent) pair is added to a relationship entity.
func applyOneOfPolicy(w *World, relID, parentID ID) {
	if w.oneOfPolicies == nil {
		w.oneOfPolicies = make(map[ID]ID)
	}
	w.oneOfPolicies[ID(relID.Index())] = ID(parentID.Index())
}

// checkOneOf panics if target violates relID's OneOf constraint.
// Wildcard and Any targets are exempt, consistent with C ecs_id_is_wildcard skip
// in component_index.c:416. The check is a direct (ChildOf, parent) lookup on
// target — no transitive ancestor traversal, matching C semantics.
func checkOneOf(w *World, relID, target ID) {
	parent, ok := w.oneOfPolicies[ID(relID.Index())]
	if !ok {
		return
	}
	// Wildcard and Any targets are exempt.
	if target.Index() == w.wildcardID.Index() || target.Index() == w.anyID.Index() {
		return
	}
	rec := w.index.Get(target)
	if rec != nil && rec.Table != nil {
		actualParent, hasParent := firstPairTarget(rec.Table.Type(), w.childOfID.Index())
		if hasParent && actualParent == parent {
			return
		}
	}
	panic(fmt.Sprintf(
		"flecs: cannot add (%v, %v): %v is not a direct child of %v (OneOf constraint on %v)",
		relID, target, target, parent, relID,
	))
}
