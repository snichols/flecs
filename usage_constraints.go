package flecs

import "fmt"

// Package-level note: Relationship, Target, and Trait ship together in Phase
// 15.15 (v0.47.0) because Trait only has meaning in combination with
// Relationship — it exempts a pair target from Relationship's "no-tag-as-target"
// check. Splitting them across phases would leave dead code or require an awkward
// two-version dependency dance.
//
// Design decisions baked in here:
//
//  1. Wildcard and Any are NOT bootstrapped with Relationship or Target. Upstream
//     component_index.c:396 has a !ecs_id_is_wildcard(rel) guard inside the
//     Relationship-as-target check, meaning wildcards are exempt without being
//     marked. We replicate that by simply not marking Wildcard/Any, keeping
//     query patterns like (R, *) working without special-casing.
//
//  2. Self-pair (R, R) when R is Relationship-only panics: R is in the target
//     slot of (R, R), so the Relationship-as-target check fires naturally.
//
//  3. These markers are sticky — once set they cannot be removed. This matches
//     the convention established by Final, Exclusive, and every other trait in
//     this codebase (write-once semantics for structural invariants).

// Relationship returns the ID of the built-in Relationship trait entity.
//
// An entity marked Relationship can only appear as the relationship (first
// element) of a pair. Attempting to add it as a plain tag, or as a pair target
// without the Trait exemption, panics at write time.
//
// Usage:
//
//	flecs.SetRelationship(w, myRelID)
//	// or bare-tag form:
//	fw.AddID(myRelID, w.Relationship())
func (w *World) Relationship() ID { return w.relationshipID }

// Target returns the ID of the built-in Target trait entity.
//
// An entity marked Target can only appear as the target (second element) of a
// pair. Attempting to add it as a plain tag, or as the relationship of a pair,
// panics at write time.
//
// Usage:
//
//	flecs.SetTarget(w, myTgtID)
//	// or bare-tag form:
//	fw.AddID(myTgtID, w.Target())
func (w *World) Target() ID { return w.targetID }

// Trait returns the ID of the built-in Trait trait entity.
//
// Marking an entity Trait exempts it from Relationship's "no-tag-as-target"
// check when it appears in the target slot of a pair. This mirrors C
// bootstrap.c:1060-1061 where ChildOf and IsA are both marked Trait, permitting
// patterns like (SomeRel, ChildOf) and (SomeRel, IsA) where a Relationship entity
// appears as a pair target.
//
// Usage:
//
//	flecs.SetTrait(w, myID)
//	// or bare-tag form:
//	fw.AddID(myID, w.Trait())
func (w *World) Trait() ID { return w.traitID }

// SetRelationship marks id as a Relationship-constrained entity. Idempotent.
func SetRelationship(w *World, id ID) {
	applyRelationshipPolicy(w, id)
}

// IsRelationship reports whether id has been marked as a Relationship. Accepts
// scope so it can be called inside both Read and Write blocks.
func IsRelationship(s scope, id ID) bool {
	return s.scopeWorld().relationshipPolicies[ID(id.Index())]
}

// SetTarget marks id as a Target-constrained entity. Idempotent.
func SetTarget(w *World, id ID) {
	applyTargetPolicy(w, id)
}

// IsTarget reports whether id has been marked as a Target. Accepts scope so it
// can be called inside both Read and Write blocks.
func IsTarget(s scope, id ID) bool {
	return s.scopeWorld().targetPolicies[ID(id.Index())]
}

// SetTrait marks id as a Trait entity, exempting it from Relationship's
// "no-tag-as-target" check when it appears in a pair target slot. Idempotent.
func SetTrait(w *World, id ID) {
	applyTraitPolicy(w, id)
}

// IsTrait reports whether id has been marked as a Trait. Accepts scope so it
// can be called inside both Read and Write blocks.
func IsTrait(s scope, id ID) bool {
	return s.scopeWorld().traitPolicies[ID(id.Index())]
}

// applyRelationshipPolicy writes the Relationship flag keyed by raw index.
func applyRelationshipPolicy(w *World, id ID) {
	if w.relationshipPolicies == nil {
		w.relationshipPolicies = make(map[ID]bool)
	}
	w.relationshipPolicies[ID(id.Index())] = true
}

// applyTargetPolicy writes the Target flag keyed by raw index.
func applyTargetPolicy(w *World, id ID) {
	if w.targetPolicies == nil {
		w.targetPolicies = make(map[ID]bool)
	}
	w.targetPolicies[ID(id.Index())] = true
}

// applyTraitPolicy writes the Trait flag keyed by raw index.
func applyTraitPolicy(w *World, id ID) {
	if w.traitPolicies == nil {
		w.traitPolicies = make(map[ID]bool)
	}
	w.traitPolicies[ID(id.Index())] = true
}

// checkUsageConstraints enforces Relationship/Target constraints when id is
// being added to entity e. Called from both the immediate and deferred paths.
//
// Bare-tag add (!id.IsPair()):
//   - Relationship entity used as bare tag → panic.
//   - Target entity used as bare tag → panic.
//
// Pair add (id.IsPair()):
//   - Target entity in relationship slot → panic.
//   - Relationship entity in target slot without Trait exemption → panic.
func checkUsageConstraints(w *World, id ID) {
	if !id.IsPair() {
		if w.relationshipPolicies[ID(id.Index())] {
			panic(fmt.Sprintf(
				"flecs: cannot add '%v' as a bare tag: has the Relationship trait and must be used in a pair as relationship",
				id,
			))
		}
		if w.targetPolicies[ID(id.Index())] {
			panic(fmt.Sprintf(
				"flecs: cannot add '%v' as a bare tag: has the Target trait and must be used in a pair as target",
				id,
			))
		}
		return
	}
	rel := id.First()
	tgt := id.Second()
	if w.targetPolicies[ID(rel.Index())] {
		panic(fmt.Sprintf(
			"flecs: cannot use '%v' as relationship in pair '%v': has the Target trait",
			rel, id,
		))
	}
	if w.relationshipPolicies[ID(tgt.Index())] && !w.traitPolicies[ID(tgt.Index())] {
		panic(fmt.Sprintf(
			"flecs: cannot use '%v' as target in pair '%v': has the Relationship trait (mark target as Trait to exempt)",
			tgt, id,
		))
	}
}
