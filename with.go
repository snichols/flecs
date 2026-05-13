package flecs

import (
	"fmt"
	"strings"
)

// With returns the ID of the built-in With trait entity (index 32).
//
// With ensures that whenever a tagged entity X is added to a target entity E,
// a second entity Y is also automatically added to E. The relationship is
// registered as a pair on the source: (With, Y) on X means "adding X co-adds Y."
//
// Two forms of auto-add:
//
//  1. Bare add: AddID(e, X) where X has (With, Y) → also adds Y to E.
//  2. Pair-with-target add: AddID(e, MakePair(R, T)) where R has (With, S) →
//     also adds (S, T) to E. The co-add inherits the same target.
//
// Chained With cascades transitively: if A has (With, B) and B has (With, C),
// adding A to E adds A, B, and C.
//
// Cycle detection: if a With chain would revisit a source entity that is
// currently being expanded (e.g., SetWith(A, B) + SetWith(B, A)), AddID panics
// with a message naming the cycle path (A → B → A). This matches C upstream's
// Acyclic bootstrap of EcsWith (bootstrap.c:1317) and makes programmer errors
// visible rather than silently short-circuiting via HasComponent dedup.
//
// IsA interaction: With fires on direct add only. Inheriting a component via IsA
// does NOT re-trigger With for the inheritor. Matches C semantics: the expansion
// runs only inside flecs_find_table_with (direct id-add), not on IsA chain walks.
//
// Exclusive interaction: With is one-way add-only. Replacing (R, T1) with (R, T2)
// (via Exclusive) co-adds (S, T2) but does NOT remove the previously co-added
// (S, T1). Manual cleanup is the user's responsibility.
//
// Storage: co-add lists are derived from existing pair storage by scanning the
// source entity's archetype for (With, *) pairs — no separate side-map. Single
// source of truth; automatic JSON round-trip via existing pair marshalling.
func (w *World) With() ID { return w.withID }

// SetWith registers that whenever source is added to an entity, coAdd is also
// automatically added. Idempotent: calling SetWith(w, source, coAdd) a second
// time with the same arguments is a no-op (HasComponent early-return).
//
// A source may have multiple co-adds: SetWith(A, B) and SetWith(A, C) cause
// adding A to also co-add both B and C.
//
// The co-add list is stored as (With, coAdd) pairs on source's archetype. No
// removal API is provided — With is sticky, symmetric with other trait markers.
func SetWith(w *World, source ID, coAdd ID) {
	addIDOnWorld(w, source, MakePair(w.withID, coAdd))
}

// HasWith returns all co-add IDs registered on source via SetWith. Accepts scope
// so it can be called inside both Read and Write blocks (per Phase 15.8 convention).
//
// The returned slice is derived from source's current archetype by scanning for
// (With, *) pairs. Order is not guaranteed. Returns nil if source has no With registrations.
func HasWith(s scope, source ID) []ID {
	w := s.scopeWorld()
	if w.withID == 0 {
		return nil
	}
	rec := w.index.Get(source)
	if rec == nil || rec.Table == nil {
		return nil
	}
	withIdx := uint32(w.withID.Index())
	var result []ID
	for _, id := range rec.Table.Type() {
		if id.IsPair() && uint32(id.First()) == withIdx {
			result = append(result, id.Second())
		}
	}
	return result
}

// applyWithCoAdds fires auto-add co-registrations on id after id has been
// successfully added to entity e. It scans id's source entity (id itself for
// bare-tag adds, id.First() for pair adds) for (With, Y) pairs and calls
// addIDImmediate for each Y (or (Y, id.Second()) for pair-form adds).
//
// Cycle detection: withExpandStack on the World tracks which source entities
// are currently being expanded. If a co-add Y matches an entity already in the
// stack (meaning Y's expansion is a parent call in the current recursion), a
// cycle is detected and the function panics with a clear path message. This
// explicit panic is chosen over silent dedup (HasComponent early-return) to make
// programmer errors like SetWith(A,B)+SetWith(B,A) immediately visible.
//
// Ordering: the originating add's table transition lands first (w.migrate returns
// before applyWithCoAdds is called), then each co-add fires as its own independent
// addIDImmediate call (its own migration, its own OnAdd hook fire). This matches
// the order documented in the issue and how the symmetric mirror works today.
func applyWithCoAdds(w *World, e, id ID) {
	if w.withID == 0 {
		return
	}
	var srcID ID
	if id.IsPair() {
		srcID = id.First()
	} else {
		srcID = id
	}
	srcRec := w.index.Get(srcID)
	if srcRec == nil || srcRec.Table == nil {
		return
	}
	withIdx := uint32(w.withID.Index())

	// Quick scan to see if srcID has any (With, *) pairs at all.
	hasAny := false
	for _, tid := range srcRec.Table.Type() {
		if tid.IsPair() && uint32(tid.First()) == withIdx {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return
	}

	// Push id onto the expansion stack (tracks which sources are currently
	// being expanded). Defer pops so the stack is restored even on panic.
	w.withExpandStack = append(w.withExpandStack, id)
	defer func() { w.withExpandStack = w.withExpandStack[:len(w.withExpandStack)-1] }()

	for _, tid := range srcRec.Table.Type() {
		if !tid.IsPair() || uint32(tid.First()) != withIdx {
			continue
		}
		coAddBase := tid.Second()
		var coAdd ID
		if id.IsPair() {
			// Pair-form co-add: preserve the originating target. Mirrors C
			// table_graph.c:1029-1033 where o (target) is re-attached to ra.
			coAdd = MakePair(coAddBase, id.Second())
		} else {
			coAdd = coAddBase
		}
		// Cycle detection: if coAdd is already in the expansion stack, we have
		// a mutual cycle (e.g., A→B→A). Panic with a clear path message so the
		// programmer sees both ends of the cycle.
		for _, stackID := range w.withExpandStack {
			if stackID == coAdd {
				panic(withCycleMsg(w.withExpandStack, coAdd))
			}
		}
		// Fire the co-add as an independent addIDImmediate call (own migration,
		// own OnAdd hook). addIDImmediate will itself call applyWithCoAdds for
		// coAdd, pushing coAdd onto the stack and expanding its co-adds.
		addIDImmediate(w, e, coAdd)
	}
}

// expandWithIntoScratch inserts With co-adds for id into the running sorted ID
// slice dst (used by the deferred coalesce path in batchForEntity). The onStack
// parameter tracks the current expansion path for cycle detection — call with
// onStack = []ID{} (or nil) from top level; the function manages it internally.
// Cycles panic with a clear path message, consistent with the immediate path.
// Already-present IDs (in dst) short-circuit the recursion to avoid redundant
// re-expansion of diamond patterns.
func expandWithIntoScratch(w *World, id ID, dst *[]ID) {
	expandWithIntoScratchHelper(w, id, dst, []ID{id})
}

func expandWithIntoScratchHelper(w *World, id ID, dst *[]ID, onStack []ID) {
	if w.withID == 0 {
		return
	}
	var srcID ID
	if id.IsPair() {
		srcID = id.First()
	} else {
		srcID = id
	}
	srcRec := w.index.Get(srcID)
	if srcRec == nil || srcRec.Table == nil {
		return
	}
	withIdx := uint32(w.withID.Index())
	for _, tid := range srcRec.Table.Type() {
		if !tid.IsPair() || uint32(tid.First()) != withIdx {
			continue
		}
		coAddBase := tid.Second()
		var coAdd ID
		if id.IsPair() {
			coAdd = MakePair(coAddBase, id.Second())
		} else {
			coAdd = coAddBase
		}
		// Cycle detection: if coAdd is in the current expansion path, panic.
		for _, sid := range onStack {
			if sid == coAdd {
				panic(withCycleMsg(onStack, coAdd))
			}
		}
		// Check if coAdd is already in dst. If so, its co-adds have already been
		// expanded (by a prior recursion or a prior cmdAddID). Skip re-expansion
		// to avoid redundant work on diamond patterns (A→C and B→C).
		alreadyPresent := false
		for _, existing := range *dst {
			if existing == coAdd {
				alreadyPresent = true
				break
			}
		}
		*dst = sortedIDInsert(*dst, coAdd)
		if !alreadyPresent {
			// Recurse: expand coAdd's own co-adds.
			expandWithIntoScratchHelper(w, coAdd, dst, append(onStack, coAdd))
		}
	}
}

// withCycleMsg builds a human-readable panic message for a With cycle.
// stack contains the IDs currently being expanded (innermost is last);
// coAdd is the ID that was found already in the stack.
func withCycleMsg(stack []ID, coAdd ID) string {
	// Find the first occurrence of coAdd in the stack to show the cycle start.
	startIdx := 0
	for i, sid := range stack {
		if sid == coAdd {
			startIdx = i
			break
		}
	}
	parts := make([]string, 0, len(stack)-startIdx+1)
	for _, sid := range stack[startIdx:] {
		parts = append(parts, fmt.Sprintf("%v", sid))
	}
	parts = append(parts, fmt.Sprintf("%v", coAdd))
	return "flecs: With cycle detected: " + strings.Join(parts, " → ")
}
