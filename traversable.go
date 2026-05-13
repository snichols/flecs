package flecs

// Traversable returns the ID of the built-in Traversable trait entity.
//
// Marking a relationship Traversable formally permits its use as the traversal
// relationship in query terms with Up, SelfUp, or Cascade modifiers. Attempting
// to use a non-traversable relationship for traversal panics at query construction
// time (NewQueryFromTerms, NewCachedQueryFromTerms).
//
// Adding Traversable to a relationship also implies Acyclic, mirroring C
// bootstrap.c:1295-1296. Write-time cycle rejection (via the Acyclic implication)
// applies to all traversable relationships.
//
// ChildOf and IsA are bootstrapped Traversable at world creation; existing queries
// that traverse these built-in relationships continue to work without change.
//
// Transitive relationships are automatically marked Traversable (mirroring C
// bootstrap.c:1299), so SetTransitive(w, R) makes R usable with .Up(R) without
// a separate SetTraversable call.
//
// Usage:
//
//	flecs.SetTraversable(w, myRelID)
//	// or bare-tag form:
//	fw.AddID(myRelID, w.Traversable())
func (w *World) Traversable() ID { return w.traversableID }

// SetTraversable marks relID as a traversable relationship, permitting its use
// in query terms with Up, SelfUp, or Cascade modifiers. Also marks relID as
// Acyclic (Traversable implies Acyclic, mirroring C bootstrap.c:1295-1296).
// Idempotent.
func SetTraversable(w *World, relID ID) {
	applyTraversablePolicy(w, relID)
}

// IsTraversable reports whether relID has been marked as traversable. Accepts
// scope so it can be called inside both Read and Write blocks (per Phase 15.8
// convention).
func IsTraversable(s scope, relID ID) bool {
	return s.scopeWorld().traversablePolicies[ID(relID.Index())]
}

// applyTraversablePolicy writes the traversable flag into w.traversablePolicies
// keyed by the entity's raw index, then implies Acyclic (matching C
// bootstrap.c:1295-1296). Idempotent.
func applyTraversablePolicy(w *World, relID ID) {
	if w.traversablePolicies == nil {
		w.traversablePolicies = make(map[ID]bool)
	}
	w.traversablePolicies[ID(relID.Index())] = true
	// Traversable implies Acyclic (C bootstrap.c:1295-1296).
	applyAcyclicPolicy(w, relID)
}
