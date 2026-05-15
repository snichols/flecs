package flecs

import (
	"fmt"

	"github.com/snichols/flecs/internal/storage/table"
)

var _ = (*table.Table)(nil) // keep import used for func(t *table.Table) callbacks in compIndex.Each

// SetParentStorage declares that relID uses non-fragmenting parent storage.
//
// When parent storage is active for a relationship:
//   - All entities that share the same non-relationship components and differ only
//     in their (relID, target) pair are stored in ONE archetype table, tagged with
//     the marker (relID, Any) in the signature. The concrete target per entity is
//     stored in a per-row parent column on that table.
//   - Reparenting (changing the target of (relID, *) on an existing entity) is O(1):
//     only the parent column entry is updated; no archetype migration occurs.
//   - All existing APIs (EachChild, ParentOf, GetUp, HasUp, queries, traversal,
//     cleanup policies, observers, snapshots) behave identically to the default
//     fragmenting mode.
//
// Preconditions (panic on violation):
//   - relID must have been marked as a Relationship via SetRelationship.
//   - relID must be Exclusive (one parent per entity). See SetExclusive.
//   - No entities may currently carry a (relID, *) pair. SetParentStorage is
//     fail-loud if the relationship already has live sources.
//
// Default is OFF (standard fragmenting behavior). Opt-in via:
//
//	flecs.SetRelationship(w, myRelID)
//	flecs.SetExclusive(w, myRelID)
//	flecs.SetParentStorage(w, myRelID)
//
// The built-in ChildOf is exclusive and is a relationship; you can enable
// parent storage for it directly:
//
//	flecs.SetParentStorage(w, w.ChildOf())
func SetParentStorage(w *World, relID ID) {
	relKey := ID(relID.Index())
	if !w.relationshipPolicies[relKey] {
		panic(fmt.Sprintf(
			"flecs: SetParentStorage: %v must be declared a Relationship via SetRelationship first",
			relID,
		))
	}
	if !w.exclusivePolicies[relKey] {
		panic(fmt.Sprintf(
			"flecs: SetParentStorage: %v must be declared Exclusive via SetExclusive first (parent storage requires exactly one parent per entity)",
			relID,
		))
	}
	// Fail-loud if any entity currently carries (relID, *).
	// Scan all world tables for any that have a concrete (relID, target) pair in signature.
	relIdx := relID.Index()
	for _, t := range w.tables {
		if t.Count() == 0 {
			continue
		}
		for _, cid := range t.Type() {
			if cid.IsPair() && cid.First().Index() == relIdx {
				panic(fmt.Sprintf(
					"flecs: SetParentStorage: relationship %v already has live entities; SetParentStorage must be called before any (relID, target) pairs are added",
					relID,
				))
			}
		}
	}
	if w.parentStoragePolicies == nil {
		w.parentStoragePolicies = make(map[ID]bool)
	}
	w.parentStoragePolicies[relKey] = true
}

// IsParentStorage reports whether relID uses non-fragmenting parent storage.
// Accepts any scope (World, Reader, or Writer).
func IsParentStorage(s scope, relID ID) bool {
	return s.scopeWorld().parentStoragePolicies[ID(relID.Index())]
}

// parentStorageMarker returns the signature marker ID for relID when parent
// storage is active: (relID, anyID). This marker appears in the archetype
// table signature instead of the concrete (relID, target) pair.
func (w *World) parentStorageMarker(relID ID) ID {
	return MakePair(relID, w.anyID)
}

// isParentStoragePairID reports whether the pair id's relationship has parent
// storage active. id must be a pair (id.IsPair() == true).
func isParentStoragePairID(w *World, id ID) bool {
	return w.parentStoragePolicies[ID(id.First().Index())]
}

// addParentStoragePair handles adding (relID, target) when parent storage is
// active for relID. Either reparents in place (O(1)) or migrates the marker.
func (w *World) addParentStoragePair(e ID, relID ID, target ID) bool {
	marker := w.parentStorageMarker(relID)
	relKey := ID(relID.Index())

	rec := w.index.Get(e)
	if rec == nil {
		panic("flecs: AddID called on dead entity")
	}

	// Case 1: entity already has the marker — reparent in place (O(1)).
	if rec.Table != nil && rec.Table.HasComponent(marker) {
		oldTarget, hadOldTarget := rec.Table.GetParentEntry(int(rec.Row), relKey)
		if hadOldTarget && oldTarget.Index() == target.Index() {
			return false // already has this parent
		}
		// Fire OnRemove for the old concrete pair.
		if hadOldTarget {
			oldPair := MakePair(relID, oldTarget)
			w.registry.EnsureID(oldPair)
			oldInfo, _ := w.registry.LookupByID(oldPair)
			w.fireOnRemove(oldInfo, oldPair, e, nil)
			// OrderedChildren: remove from old parent's list.
			if relID.Index() == w.childOfID.Index() && w.orderedChildren != nil {
				if list, ok := w.orderedChildren[ID(oldTarget.Index())]; ok {
					removeFromOrderedList(list, e)
				}
			}
		}
		// Update the parent column in place.
		rec.Table.SetParentEntry(int(rec.Row), relKey, target)
		rec.Table.BumpChange()
		// Fire OnAdd for the new concrete pair.
		newPair := MakePair(relID, target)
		w.registry.EnsureID(newPair)
		newInfo, _ := w.registry.LookupByID(newPair)
		w.fireOnAdd(newInfo, newPair, e, nil)
		// OrderedChildren: add to new parent's list.
		if relID.Index() == w.childOfID.Index() && w.orderedChildren != nil {
			if list, ok := w.orderedChildren[ID(target.Index())]; ok {
				list.entries = append(list.entries, e)
			}
		}
		// Archetype monitors: treat reparent as a structural change.
		if len(w.monitors) > 0 {
			w.fireSparseMonitors(e, newPair, 0)
		}
		// With co-adds.
		applyWithCoAdds(w, e, newPair)
		return true
	}

	// Case 2: entity doesn't have the marker yet — migrate to add it.
	// If the entity has an old fragmented pair for this rel (shouldn't happen
	// after SetParentStorage, but be defensive), remove it first.
	if rec.Table != nil {
		relIdx := uint32(relID.Index())
		for _, sigID := range rec.Table.Type() {
			if sigID.IsPair() && uint32(sigID.First().Index()) == relIdx && sigID != marker {
				// Old fragmented pair found — remove it before adding marker.
				w.migrate(e, 0, sigID, nil)
				break
			}
		}
	}

	// Register the marker and migrate to add it (archetype-only, no hook fire).
	w.registry.EnsureID(marker)
	w.migrateArchetypeOnly(e, marker, 0)

	// Write the target to the new parent column.
	rec = w.index.Get(e)
	rec.Table.EnsureParentCol(relKey)
	rec.Table.SetParentEntry(int(rec.Row), relKey, target)
	rec.Table.BumpChange()

	// Fire OnAdd for the concrete pair.
	newPair := MakePair(relID, target)
	w.registry.EnsureID(newPair)
	newInfo, _ := w.registry.LookupByID(newPair)
	w.fireOnAdd(newInfo, newPair, e, nil)

	// OrderedChildren: add to parent's list.
	if relID.Index() == w.childOfID.Index() && w.orderedChildren != nil {
		if list, ok := w.orderedChildren[ID(target.Index())]; ok {
			list.entries = append(list.entries, e)
		}
	}

	// Archetype monitors.
	if len(w.monitors) > 0 {
		w.fireArchetypeMonitors(e, nil, rec.Table)
		w.fireSparseMonitors(e, newPair, 0)
	}

	// With co-adds.
	applyWithCoAdds(w, e, newPair)
	return true
}

// removeParentStoragePair handles removing (relID, target) or (relID, wildcard)
// when parent storage is active for relID.
func (w *World) removeParentStoragePair(e ID, relID ID, targetOrWildcard ID) bool {
	marker := w.parentStorageMarker(relID)
	relKey := ID(relID.Index())

	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	if rec.Table == nil || !rec.Table.HasComponent(marker) {
		return false // entity has no parent relationship for this rel
	}
	oldTarget, hadOldTarget := rec.Table.GetParentEntry(int(rec.Row), relKey)
	if !hadOldTarget {
		return false
	}
	// Specific target: only remove if it matches.
	if !isWildcardID(w, targetOrWildcard) && oldTarget.Index() != targetOrWildcard.Index() {
		return false
	}

	// Fire OnRemove for the old concrete pair (before migration).
	oldPair := MakePair(relID, oldTarget)
	w.registry.EnsureID(oldPair)
	oldInfo, _ := w.registry.LookupByID(oldPair)
	w.fireOnRemove(oldInfo, oldPair, e, nil)

	// OrderedChildren: remove from parent's list.
	if relID.Index() == w.childOfID.Index() && w.orderedChildren != nil {
		if list, ok := w.orderedChildren[ID(oldTarget.Index())]; ok {
			removeFromOrderedList(list, e)
		}
	}

	// Zero the parent column entry before migration (RemoveSwap will clean it up).
	rec.Table.SetParentEntry(int(rec.Row), relKey, 0)

	// Migrate to remove the marker from the archetype.
	oldTable := rec.Table
	w.migrateArchetypeOnly(e, 0, marker)

	// Archetype monitors.
	if len(w.monitors) > 0 {
		rec2 := w.index.Get(e)
		w.fireArchetypeMonitors(e, oldTable, rec2.Table)
		w.fireSparseMonitors(e, oldPair, 0)
	}

	// Propagation cache invalidation for IsA.
	if relID.Index() == w.isAID.Index() {
		w.invalidateInheritorCache()
	}
	// Symmetric mirror.
	if w.symmetricPolicies[relID] {
		w.removeParentStoragePair(oldTarget, relID, e)
	}
	return true
}
