package flecs

import "fmt"

// unionEntry is one entity→target mapping held in a unionRelStore.
type unionEntry struct {
	entity ID
	target ID
}

// unionRelStore is the per-relationship side store for Union semantics.
// dense holds entries in insertion order (stable iteration). index maps
// entity-index (as ID(e.Index())) → position in dense for O(1) lookup.
type unionRelStore struct {
	dense []unionEntry
	index map[ID]int // entity-index → position in dense
}

// applyUnionPolicy initializes the union flag and backing store for relID.
// Idempotent. Also enables Exclusive behavior for the relationship.
func applyUnionPolicy(w *World, relID ID) {
	if w.unionPolicies == nil {
		w.unionPolicies = make(map[ID]bool)
	}
	key := ID(relID.Index())
	if w.unionPolicies[key] {
		return // idempotent
	}
	w.unionPolicies[key] = true
	if w.unionStore == nil {
		w.unionStore = make(map[ID]*unionRelStore)
	}
	w.unionStore[key] = &unionRelStore{
		index: make(map[ID]int),
	}
	applyExclusivePolicy(w, relID) // Union implies Exclusive
}

// SetUnion marks relID as a union relationship: at most one target per source
// entity, stored compactly in a per-relationship side store without archetype
// fragmentation. Adding a second target silently replaces the first.
//
// Under the hood, SetUnion is sugar for the DontFragment+Exclusive composition
// used by upstream C flecs (see docs/MigrationGuide.md:492-502 in the upstream
// C flecs repo). The Go implementation uses a dedicated side store rather than
// extending DontFragment to relationship pairs, keeping the component and
// relationship storage paths cleanly separated.
//
// Panics if relID already has the Exclusive trait without Union (use SetUnion
// from the start). SetExclusive on a Union relationship also panics.
//
// Usage:
//
//	var Movement, Standing, Walking flecs.ID
//	w.Write(func(fw *flecs.Writer) {
//	    Movement = fw.NewEntity()
//	    Standing = fw.NewEntity()
//	    Walking  = fw.NewEntity()
//	})
//	flecs.SetUnion(w, Movement)
//
//	var e flecs.ID
//	w.Write(func(fw *flecs.Writer) {
//	    e = fw.NewEntity()
//	    fw.AddID(e, flecs.MakePair(Movement, Standing))
//	    fw.AddID(e, flecs.MakePair(Movement, Walking)) // silently replaces Standing
//	})
//
// The entity's archetype table does NOT change between adds — only the union
// store is updated. This is the defining property of Union vs Exclusive.
func SetUnion(w *World, relID ID) {
	key := ID(relID.Index())
	if w.exclusivePolicies[key] && !w.unionPolicies[key] {
		panic(fmt.Sprintf(
			"flecs: SetUnion: relationship %v is already marked Exclusive; Union subsumes Exclusive — use SetUnion from the start",
			relID,
		))
	}
	applyUnionPolicy(w, relID)
}

// IsUnion reports whether relID has been marked as a union relationship.
// Accepts scope so it can be called inside both Read and Write blocks.
func IsUnion(s scope, relID ID) bool {
	w := s.scopeWorld()
	return w.unionPolicies[ID(relID.Index())]
}

// EachUnion iterates all entities that have an active union target for relID,
// calling fn for each in insertion order. relID must have been marked with
// SetUnion. Stable iteration order: entries appear in the order they were first
// added (insertion order in the dense slice).
func EachUnion(s scope, relID ID, fn func(e ID, target ID)) {
	w := s.scopeWorld()
	store, ok := w.unionStore[ID(relID.Index())]
	if !ok {
		return
	}
	// Snapshot before iteration for mutation safety.
	snap := make([]unionEntry, len(store.dense))
	copy(snap, store.dense)
	for _, entry := range snap {
		fn(entry.entity, entry.target)
	}
}

// checkUnionPair panics if rel has the Union trait, preventing data-bearing
// pair writes. Union pairs are tag-only — the only state is the active target.
func checkUnionPair(w *World, rel ID) {
	if w.unionPolicies[ID(rel.Index())] {
		panic(fmt.Sprintf(
			"flecs: cannot set data on pair (%v, *): %v has the Union trait; union pairs carry no data",
			rel, rel,
		))
	}
}

// isUnionTermID reports whether a query term with the given id refers to a
// union pair (relationship is Union). Returns false for non-pair IDs.
func isUnionTermID(w *World, id ID) bool {
	if !id.IsPair() {
		return false
	}
	return w.unionPolicies[ID(id.First().Index())]
}

// unionStoreSet inserts or updates the active target for entity e under relID.
// Fires OnRemove for the prior target (if any, and if different) and OnAdd for
// the new target. Returns true if the state changed (new or replaced target);
// false if the same target was already set (idempotent no-op).
func unionStoreSet(w *World, relKey ID, e ID, relID ID, targetID ID) bool {
	store := w.unionStore[relKey]
	entityKey := ID(e.Index())

	newPairID := MakePair(relID, targetID)
	w.registry.EnsureID(newPairID)

	if pos, has := store.index[entityKey]; has {
		oldTarget := store.dense[pos].target
		if oldTarget.Index() == targetID.Index() {
			return false // idempotent
		}
		// Fire OnRemove for old pair before updating.
		oldPairID := MakePair(relID, oldTarget)
		w.registry.EnsureID(oldPairID)
		oldInfo, _ := w.registry.LookupByID(oldPairID)
		w.fireOnRemove(oldInfo, oldPairID, e, nil)
		store.dense[pos].target = targetID
		// Fire sparse monitors for old pair AFTER the store is updated so that
		// entityMatchesMonitorExcluding sees oldPairID as no longer active.
		if len(w.monitors) > 0 {
			w.fireSparseMonitors(e, oldPairID, 0)
		}
	} else {
		store.index[entityKey] = len(store.dense)
		store.dense = append(store.dense, unionEntry{entity: e, target: targetID})
	}

	// Fire OnAdd for new pair.
	newInfo, _ := w.registry.LookupByID(newPairID)
	w.fireOnAdd(newInfo, newPairID, e, nil)
	// Fire sparse monitors for new pair AFTER it is in the store.
	if len(w.monitors) > 0 {
		w.fireSparseMonitors(e, newPairID, 0)
	}
	return true
}

// unionStoreRemove removes the active union target for entity e under the given
// store. Uses swap-and-truncate to maintain a compact dense slice. Returns the
// removed target and true if found, or zero and false if entity had no target.
func unionStoreRemove(store *unionRelStore, e ID) (ID, bool) {
	entityKey := ID(e.Index())
	slot, has := store.index[entityKey]
	if !has {
		return 0, false
	}
	target := store.dense[slot].target
	last := len(store.dense) - 1
	if slot != last {
		lastEntry := store.dense[last]
		store.dense[slot] = lastEntry
		store.index[ID(lastEntry.entity.Index())] = slot
	}
	store.dense = store.dense[:last]
	delete(store.index, entityKey)
	return target, true
}

// unionStoreRemoveEntity removes entity e from every union store (called on
// entity delete). Fires OnRemove for each active union pair.
func unionStoreRemoveEntity(w *World, e ID) {
	if w.unionStore == nil {
		return
	}
	for relKey, store := range w.unionStore {
		entityKey := ID(e.Index())
		slot, has := store.index[entityKey]
		if !has {
			continue
		}
		oldTarget := store.dense[slot].target
		// Reconstruct relationship entity ID from relKey.
		// relKey is ID(relID.Index()); we look up the live entity record to get the full ID.
		relRec := w.index.Get(ID(relKey))
		var relID ID
		if relRec != nil {
			relID = ID(relKey) // use index-only form; generation already stripped for policy keys
		} else {
			relID = ID(relKey)
		}
		oldPairID := MakePair(relID, oldTarget)
		w.registry.EnsureID(oldPairID)
		oldInfo, _ := w.registry.LookupByID(oldPairID)
		w.fireOnRemove(oldInfo, oldPairID, e, nil)
		unionStoreRemove(store, e)
	}
}
