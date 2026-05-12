package flecs

import "fmt"

// Final returns the ID of the built-in Final trait entity.
//
// Marking an entity Final prevents it from being used as the target of an IsA
// relationship. Adding MakePair(IsA, target) where target is Final panics at
// AddID time — regardless of whether the source and target are the same entity.
// This matches C EcsFinal enforcement in component_index.c:447-453.
//
// Usage:
//
//	flecs.SetFinal(w, myEntityID)
//	// or bare-tag form:
//	fw.AddID(myEntityID, w.Final())
//
// No built-in entity ships Final by default (matching C bootstrap).
func (w *World) Final() ID { return w.finalID }

// SetFinal marks entityID as Final: any subsequent AddID(src, MakePair(IsA, entityID))
// will panic with a constraint-violated message.
func SetFinal(w *World, entityID ID) {
	applyFinalPolicy(w, entityID)
}

// IsFinal reports whether entityID has been marked Final. Accepts scope so it
// can be called directly from inside a Write block (per Phase 15.8 convention).
func IsFinal(s scope, entityID ID) bool {
	return s.scopeWorld().finalPolicies[ID(entityID.Index())]
}

// applyFinalPolicy writes the Final flag into w.finalPolicies keyed by the
// entity's raw index. Keying by index (not full ID) ensures that comparisons
// via id.Index() always find the entry regardless of generation counter.
// Called by SetFinal and by addIDImmediate when the bare Final tag is added.
func applyFinalPolicy(w *World, entityID ID) {
	if w.finalPolicies == nil {
		w.finalPolicies = make(map[ID]bool)
	}
	w.finalPolicies[ID(entityID.Index())] = true
}

// checkFinal panics if target has the Final trait, preventing (IsA, target)
// from being stored. Fires for all sources, including self-pairs (src == target),
// matching C's unconditional check in component_index.c:447-453.
func checkFinal(w *World, target ID) {
	if w.finalPolicies[ID(target.Index())] {
		panic(fmt.Sprintf(
			"flecs: cannot add (IsA, %v): %v has the Final trait",
			target, target,
		))
	}
}
