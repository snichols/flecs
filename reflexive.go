package flecs

import "github.com/snichols/flecs/internal/storage/table"

// Reflexive returns the ID of the built-in Reflexive trait entity.
//
// Marking a relationship reflexive asserts that every entity implicitly
// has the relationship to itself: R(X, X) is true for any X, without
// storing an explicit self-pair. The canonical example is [IsA]: a type
// is always considered an instance of itself.
//
// Reflexive affects two parts of the API:
//
//  1. [Reader.HasID] / [HasID]: HasID(e, MakePair(R, e)) returns true when R
//     is reflexive, even if no self-pair is stored. This is a deliberate
//     divergence from C flecs (where ecs_has_id does not consult EcsReflexive;
//     see CHANGELOG v0.39.0 for rationale).
//
//  2. Query matching: a term With(MakePair(R, target)) includes the table of
//     target itself as a match, in addition to tables that directly hold
//     (R, target). Reflexive composes with [Transitive]: a Reflexive+Transitive
//     R yields the starting entity and all ancestors via transitive chain-walking.
//
// Usage:
//
//	flecs.SetReflexive(w, myRelID)
//	// or bare-tag form:
//	fw.AddID(myRelID, w.Reflexive())
//
//	// HasID self-pair now returns true even without a stored pair.
//	r.HasID(a, flecs.MakePair(myRelID, a)) // → true
//
// # Cached query limitation
//
// [CachedQuery] evaluates reflexive self-matches at table-creation time: it
// adds the target entity's table to the match set when the cache is built or
// a new table is created. If the target migrates to a different table after
// the cache is built, the cache will not update automatically. Staleness is
// accepted for this phase; entity-migration cache invalidation is a future
// enhancement.
func (w *World) Reflexive() ID { return w.reflexiveID }

// SetReflexive marks relID as a reflexive relationship: R(X, X) is implicitly
// true for all X. See [World.Reflexive] for full semantics.
//
// Equivalent to fw.AddID(relID, w.Reflexive()).
func SetReflexive(w *World, relID ID) {
	applyReflexivePolicy(w, relID)
}

// IsReflexive reports whether relID has been marked reflexive.
func IsReflexive(w *World, relID ID) bool {
	return w.reflexivePolicies[ID(relID.Index())]
}

// applyReflexivePolicy writes the reflexive flag into w.reflexivePolicies
// keyed by the entity's raw index. Keying by index (not full ID) ensures that
// pair decomposition via id.First() — which strips the generation field —
// always finds the entry regardless of the relationship entity's generation
// counter. Called by SetReflexive and by addIDImmediate when the bare
// Reflexive tag is added to a relationship entity.
func applyReflexivePolicy(w *World, relID ID) {
	if w.reflexivePolicies == nil {
		w.reflexivePolicies = make(map[ID]bool)
	}
	w.reflexivePolicies[ID(relID.Index())] = true
}

// reflexiveTableMatches reports whether the target entity of a reflexive pair
// term lives in table t. When R is reflexive and the query term is (R, target),
// the target entity itself is an implicit match — so the table containing
// target qualifies as a match even if it does not hold (R, target) directly.
func reflexiveTableMatches(w *World, t *table.Table, termID ID) bool {
	target := termID.Second()
	rec := w.index.Get(target)
	if rec == nil || rec.Table == nil {
		return false
	}
	return rec.Table == t
}
