package flecs

import "github.com/snichols/flecs/internal/storage/table"

// Transitive returns the ID of the built-in Transitive trait entity.
//
// Marking a relationship transitive enables automatic chain-walking at query
// time: if entity a has (R, B) and entity b has (R, C), a query for (R, C)
// also matches a. Formally: aRb ∧ bRc ⇒ aRc. The canonical motivating example
// is spatial containment (LocatedIn).
//
// Unlike [Symmetric], Transitive does NOT mirror pairs at write time. Chain
// walking is lazy — it happens only when a query term for (R, C) is evaluated.
// This avoids O(n²) pair writes for a chain of length n.
//
// Usage:
//
//	flecs.SetTransitive(w, locatedInID)
//	// or bare-tag form:
//	fw.AddID(locatedInID, w.Transitive())
//
//	// Query for (LocatedIn, USA) matches Manhattan even though only NewYork
//	// has (LocatedIn, USA) directly and Manhattan has (LocatedIn, NewYork).
//	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(locatedInID, usaID)))
//
// Cycle detection and a depth limit ([maxTraversalDepth]) prevent infinite
// loops. Self-relationship (R, a) on a does not auto-match queries unless the
// entity also reaches the target via an explicit chain — Transitive does not
// imply Reflexive (a separate unported trait).
//
// # Cached query staleness
//
// [CachedQuery] evaluates transitive chains at construction and on every new
// table creation. It does NOT re-evaluate when an intermediate entity's pairs
// mutate after the cache is built. Staleness is accepted for this phase;
// pair-mutation cache invalidation is a future enhancement.
func (w *World) Transitive() ID { return w.transitiveID }

// SetTransitive marks relID as a transitive relationship: when evaluating a
// query term (R, C), entities that hold (R, B) are also matched if B (or any
// entity reachable from B via R chains) holds (R, C).
//
// Equivalent to fw.AddID(relID, w.Transitive()).
func SetTransitive(w *World, relID ID) {
	applyTransitivePolicy(w, relID)
}

// IsTransitive reports whether relID has been marked transitive.
func IsTransitive(w *World, relID ID) bool {
	return w.transitivePolicies[ID(relID.Index())]
}

// applyTransitivePolicy writes the transitive flag into w.transitivePolicies
// keyed by the entity's raw index. Keying by index (not full ID) ensures that
// pair decomposition via id.First() — which strips the generation field —
// always finds the entry regardless of the relationship entity's generation
// counter. Called by SetTransitive and by addIDImmediate when the bare
// Transitive tag is added to a relationship entity.
func applyTransitivePolicy(w *World, relID ID) {
	if w.transitivePolicies == nil {
		w.transitivePolicies = make(map[ID]bool)
	}
	w.transitivePolicies[ID(relID.Index())] = true
}

// transitiveWalk performs a DFS from start, following all (rel, X) pairs on
// each visited entity's archetype table. Returns true if target is reachable.
// visited prevents revisiting entities (cycle detection); depth caps the walk
// at maxTraversalDepth to bound runaway chains.
func transitiveWalk(w *World, start ID, rel ID, target ID, visited map[ID]struct{}, depth int) bool {
	if depth > maxTraversalDepth {
		return false
	}
	if _, seen := visited[start]; seen {
		return false
	}
	visited[start] = struct{}{}
	if !w.index.IsAlive(start) {
		return false
	}
	rec := w.index.Get(start)
	if rec == nil || rec.Table == nil {
		return false
	}
	relIdx := uint32(rel.Index())
	for _, sigID := range rec.Table.Type() {
		if !sigID.IsPair() || uint32(sigID.First()) != relIdx {
			continue
		}
		next := sigID.Second()
		if next == target {
			return true
		}
		if transitiveWalk(w, next, rel, target, visited, depth+1) {
			return true
		}
	}
	return false
}

// transitiveTableMatches reports whether any entity in table t transitively
// holds (rel, target) via the chain of (rel, *) pairs. It is called when
// t does not directly contain termID (MakePair(rel, target)) but the
// relationship is flagged transitive. The visited set is shared across all
// starting nodes in the table to avoid redundant work and handle cycles.
func transitiveTableMatches(w *World, t *table.Table, termID ID) bool {
	rel := termID.First()
	target := termID.Second()
	relIdx := uint32(rel.Index())
	visited := map[ID]struct{}{}
	for _, sigID := range t.Type() {
		if !sigID.IsPair() || uint32(sigID.First()) != relIdx {
			continue
		}
		start := sigID.Second()
		if transitiveWalk(w, start, rel, target, visited, 0) {
			return true
		}
	}
	return false
}
