package flecs

// Exclusive returns the ID of the built-in Exclusive trait entity.
//
// Marking a relationship exclusive constrains it to at most one target per
// source entity. Adding a second target automatically replaces the first:
//
//	flecs.SetExclusive(w, marriedToID)
//	fw.AddID(bob, flecs.MakePair(marriedToID, alice)) // bob married alice
//	fw.AddID(bob, flecs.MakePair(marriedToID, carol)) // replaces alice; bob now married to carol
//
// The built-in ChildOf, OnDelete, OnDeleteTarget, and OnInstantiate
// relationships are exclusive by default. IsA is not exclusive.
//
// The bare-tag form is also valid and equivalent:
//
//	fw.AddID(marriedToID, w.Exclusive())
func (w *World) Exclusive() ID { return w.exclusiveID }

// SetExclusive marks relID as an exclusive relationship: at most one target per
// source entity is allowed. Adding a second target replaces the first.
//
// Equivalent to fw.AddID(relID, w.Exclusive()).
func SetExclusive(w *World, relID ID) {
	applyExclusivePolicy(w, relID)
}

// IsExclusive reports whether relID has been marked exclusive.
func IsExclusive(w *World, relID ID) bool {
	return w.exclusivePolicies[ID(relID.Index())]
}

// applyExclusivePolicy writes the exclusive flag into w.exclusivePolicies keyed
// by the entity's raw index. Keying by index (not full ID) ensures that pair
// decomposition via id.First() — which strips the generation field — always
// finds the entry regardless of the relationship entity's generation counter.
// Called by SetExclusive and by addIDImmediate when the bare Exclusive tag is
// added to a relationship entity.
func applyExclusivePolicy(w *World, relID ID) {
	if w.exclusivePolicies == nil {
		w.exclusivePolicies = make(map[ID]bool)
	}
	w.exclusivePolicies[ID(relID.Index())] = true
}
