package flecs

import "fmt"

// PairIsTag returns the ID of the built-in PairIsTag trait entity.
//
// Marking a relationship with PairIsTag forces every pair (R, T) using that
// relationship to behave as a tag — no component data is allocated per pair,
// even if the target T is a data-bearing component type. This is a defensive
// declaration that prevents accidental promotion of an intended tag-pair into
// a data-pair via SetPair[T] / SetPairByID.
//
// The canonical upstream use case (C EcsPairIsTag, flecs.h:1888-1890):
// a Serializable component that the user also wants to use as a pair
// relationship without (Serializable, Position) accidentally allocating a
// second Position-shaped slot per entity.
//
// Built-in entities bootstrapped with PairIsTag: IsA, ChildOf, DependsOn, SlotOf
// (mirroring C bootstrap.c:1272-1273, 1274, 1283). Flag is not yet ported.
//
// Usage:
//
//	flecs.SetPairIsTag(w, myRelID)
//	// or bare-tag form:
//	fw.AddID(myRelID, w.PairIsTag())
//
//	// Tag-form pair add is still allowed:
//	fw.AddID(e, flecs.MakePair(myRelID, targetID))
//
//	// Value-bearing pair operations now panic:
//	flecs.SetPair[MyType](fw, e, myRelID, targetID, v) // panics
func (w *World) PairIsTag() ID { return w.pairIsTagID }

// SetPairIsTag marks relID as a PairIsTag relationship. Idempotent.
//
// Panics if relID has already been used as the relationship in a data-bearing
// pair: calling SetPairIsTag after RegisterPairData[T](R, T) would leave
// existing storage in an inconsistent state. Mark the relationship before
// using it as a data-pair (mirroring C flecs_assert_relation_unused in
// bootstrap.c:270-290).
//
// Calling SetPairIsTag on a non-component entity (an entity with no associated
// data type) is a no-op — the relationship was already tag-form, and a
// defensive caller may want to declare the intent regardless.
func SetPairIsTag(w *World, relID ID) {
	for _, rid := range w.registry.IDs() {
		if !rid.IsPair() {
			continue
		}
		if rid.First().Index() != relID.Index() {
			continue
		}
		if info, ok := w.registry.LookupByID(rid); ok && info.Size > 0 {
			panic(fmt.Sprintf(
				"flecs: cannot mark %v as PairIsTag: pair (%v, %v) already has data registered",
				relID, relID, rid.Second(),
			))
		}
	}
	applyPairIsTagPolicy(w, relID)
}

// IsPairIsTag reports whether relID has the PairIsTag trait. Accepts scope so
// it can be called directly from inside a Write block (per Phase 15.8 convention).
//
// If relID is a pair ID (IsPair() == true), the relationship side is examined:
// IsPairIsTag(s, MakePair(R, T)) returns the same answer as IsPairIsTag(s, R).
// This matches the ergonomic shape of IsTrait / IsRelationship.
func IsPairIsTag(s scope, relID ID) bool {
	w := s.scopeWorld()
	if relID.IsPair() {
		relID = relID.First()
	}
	return w.pairIsTagPolicies[ID(relID.Index())]
}

// applyPairIsTagPolicy writes the PairIsTag flag keyed by raw index.
// Called by SetPairIsTag and by addIDImmediate when the bare PairIsTag tag is added.
func applyPairIsTagPolicy(w *World, relID ID) {
	if w.pairIsTagPolicies == nil {
		w.pairIsTagPolicies = make(map[ID]bool)
	}
	w.pairIsTagPolicies[ID(relID.Index())] = true
}

// checkPairIsTag panics if rel has the PairIsTag trait, preventing value-bearing
// pair data from being written. The bare-tag AddID path is unaffected — only
// the data-writing paths (SetPair[T], SetPairByID) call this check.
func checkPairIsTag(w *World, rel ID) {
	if w.pairIsTagPolicies[ID(rel.Index())] {
		panic(fmt.Sprintf(
			"flecs: cannot set data on pair (%v, *): %v has the PairIsTag trait",
			rel, rel,
		))
	}
}
