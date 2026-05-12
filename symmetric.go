package flecs

// Symmetric returns the ID of the built-in Symmetric trait entity.
//
// Marking a relationship symmetric causes any (R, B) added to entity a to be
// automatically mirrored as (R, A) on entity b. Removal is also mirrored:
// removing (R, B) from a removes (R, A) from b. Useful for inherently
// undirected relations such as Friend, MarriedTo, AlliesWith, or Coplanar.
//
//	flecs.SetSymmetric(w, marriedToID)
//	fw.AddID(bob, flecs.MakePair(marriedToID, alice))
//	// alice now also has (marriedToID, bob)
//
// The bare-tag form is also valid and equivalent to [SetSymmetric]:
//
//	fw.AddID(marriedToID, w.Symmetric())
func (w *World) Symmetric() ID { return w.symmetricID }

// SetSymmetric marks relID as a symmetric relationship: adding (R, B) to
// entity a automatically adds (R, A) to entity b; removing (R, B) from a
// removes (R, A) from b.
//
// Equivalent to fw.AddID(relID, w.Symmetric()).
func SetSymmetric(w *World, relID ID) {
	applySymmetricPolicy(w, relID)
}

// IsSymmetric reports whether relID has been marked symmetric.
func IsSymmetric(w *World, relID ID) bool {
	return w.symmetricPolicies[ID(relID.Index())]
}

// applySymmetricPolicy writes the symmetric flag into w.symmetricPolicies keyed
// by the entity's raw index. Keying by index (not full ID) ensures that pair
// decomposition via id.First() — which strips the generation field — always
// finds the entry regardless of the relationship entity's generation counter.
// Called by SetSymmetric and by addIDImmediate when the bare Symmetric tag is
// added to a relationship entity.
func applySymmetricPolicy(w *World, relID ID) {
	if w.symmetricPolicies == nil {
		w.symmetricPolicies = make(map[ID]bool)
	}
	w.symmetricPolicies[ID(relID.Index())] = true
}
