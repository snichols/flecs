package flecs

import "fmt"

// clearImmediate removes all components from entity e, leaving it alive in the
// empty archetype. Fires OnRemove for each component. Mirrors ecs_clear from
// the C upstream (src/entity.c:1613-1644).
//
// Pattern mirrors deleteOne (world.go:641-718) without the index.Free call.
func clearImmediate(w *World, e ID) bool {
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	t := rec.Table
	row := int(rec.Row)

	// hasComponents is true when the entity is in a non-empty archetype table.
	// An entity that only has DontFragment components lives in the empty table
	// (DontFragment never causes archetype transitions), so hasComponents may be
	// false even when the sparseHeld cleanup below fires.
	hasComponents := t != nil && len(t.Type()) > 0

	if hasComponents {
		// Fire OnRemove for archetype components. Skip pure Sparse (sparsePolicies &&
		// !dontFragmentPolicies): their data lives in the sparse-set, not the table
		// column; the sparseHeld block below fires OnRemove with the correct pointer.
		for _, id := range t.Type() {
			if !id.IsPair() && w.sparsePolicies[ID(id.Index())] && !w.dontFragmentPolicies[ID(id.Index())] {
				continue
			}
			info, _ := w.registry.LookupByID(id)
			w.fireOnRemove(info, id, e, t.Get(row, id))
		}
		// Swap-remove e from its current table and fix up the moved entity's record.
		moved, ok := t.RemoveSwap(row)
		if ok {
			movedRec := w.index.Get(moved)
			movedRec.Row = uint32(row)
		}
	}

	// Clear any singleton slots held by this entity.
	for compIdx, holder := range w.singletonInstances {
		if holder.Index() == e.Index() {
			delete(w.singletonInstances, compIdx)
		}
	}
	// Clear any writeOnce hasBeenSet slots for this entity.
	for key := range w.writeOnceHasBeenSet {
		if key.entity == e.Index() {
			delete(w.writeOnceHasBeenSet, key)
		}
	}
	// OrderedChildren: remove entity's own list (it was an ordered parent), and
	// remove entity from any other parent's ordered list (it was an ordered child).
	if w.orderedChildren != nil {
		delete(w.orderedChildren, ID(e.Index()))
		for _, list := range w.orderedChildren {
			removeFromOrderedList(list, e)
		}
	}
	// Fire OnRemove for sparse-stored components (Sparse and DontFragment) and
	// remove them from their sparse-sets. sparseHeld gives O(k) cleanup.
	if w.sparseHeld != nil {
		eIdx := uint32(e.Index())
		if held := w.sparseHeld[eIdx]; len(held) > 0 {
			heldSnap := append([]ID(nil), held...)
			for _, cid := range heldSnap {
				key := ID(cid.Index())
				if ss, ok := w.sparseStorage[key]; ok {
					slot, hasSlot := ss.index[e.Index()]
					if hasSlot {
						info, _ := w.registry.LookupByID(cid)
						w.fireOnRemove(info, cid, e, ss.dense[slot].data)
					}
				}
				sparseSetRemove(w, e, cid)
			}
		}
	}
	// Fire OnRemove for union-stored pairs and remove them from the union store.
	unionStoreRemoveEntity(w, e)

	// Place entity in the empty table. Only done when e was actually moved out of
	// a non-empty table; entities already in the empty table stay at their row.
	if hasComponents {
		rec.Table = w.empty
		rec.Row = uint32(w.empty.Append(e))
	}

	return true
}

// Clear removes all components, tags, and pairs from entity e, leaving it alive
// in the empty archetype. OnRemove fires for each component. Does not fire
// OnDelete. Mirrors C ecs_clear (src/entity.c:1613).
//
// Deferred: the clear is queued and executed on scope close. All AddID/Set
// commands queued before the Clear for the same entity are superseded; commands
// queued after the Clear apply on top of the empty archetype.
//
// Returns false if e is not alive.
func Clear(fw *Writer, e ID) bool {
	s := fw.stage
	if s.deferDepth == 0 {
		return clearImmediate(fw.world, e)
	}
	if !fw.world.index.IsAlive(e) {
		return false
	}
	s.queue.append(cmd{kind: cmdClear, entity: e})
	return true
}

// MakeAlive claims a specific entity ID for use in this world. Useful for
// networked scenarios where both peers must share the same entity IDs.
//
// Behaviour:
//   - If the ID is already alive at the same generation: no-op, returns id.
//   - If the slot is free (or was never allocated): the registry generation is
//     bumped to match id.Generation(), the slot is marked alive, and the entity
//     is attached to the empty archetype. Returns id.
//   - If the slot is alive at a different generation: panics with an informative
//     message. Use Delete + MakeAlive to reclaim a live slot.
//
// Panics in a deferred scope (mirrors C's defer assertion on ecs_make_alive).
// Mirrors C ecs_make_alive (src/entity.c:3111).
func MakeAlive(fw *Writer, id ID) ID {
	w := fw.world
	if fw.stage.deferDepth > 0 {
		panic("flecs: MakeAlive: cannot be called in a deferred scope")
	}

	canonical, ok := w.index.MakeAlive(id)
	if !ok {
		panic(fmt.Sprintf(
			"flecs: MakeAlive: entity index %d is alive with generation %d; requested generation %d",
			id.Index(), canonical.Generation(), id.Generation()))
	}

	rec := w.index.Get(canonical)
	if rec.Table == nil {
		// Freshly claimed slot: place entity in the empty archetype.
		rec.Table = w.empty
		rec.Row = uint32(w.empty.Append(canonical))
	}

	return canonical
}

// SetVersion overrides the generation counter on an alive entity. After the
// call, IsAlive(oldID) is false and IsAlive(versionedID) is true.
//
// The requested generation must be ≥ the current generation. A decrease would
// silently invalidate outstanding handles in surprising ways; call Delete +
// MakeAlive to reset to a lower generation deliberately. This is a deliberate
// divergence from C ecs_set_version, which accepts any value.
//
// Panics if the entity index is not alive, if the new generation is strictly
// less than the current generation, or if called in a deferred scope.
// Mirrors C ecs_set_version (src/entity.c:3219).
func SetVersion(fw *Writer, versionedID ID) {
	w := fw.world
	if fw.stage.deferDepth > 0 {
		panic("flecs: SetVersion: cannot be called in a deferred scope")
	}
	rawIdx := versionedID.Index()
	newGen := versionedID.Generation()

	currentID, alive := w.index.GetCurrentByIndex(rawIdx)
	if !alive {
		panic(fmt.Sprintf("flecs: SetVersion: entity index %d is not alive", rawIdx))
	}
	currentGen := currentID.Generation()
	if newGen < currentGen {
		panic(fmt.Sprintf(
			"flecs: SetVersion: requested generation %d is less than current generation %d for entity index %d; use Delete + MakeAlive to set a lower generation",
			newGen, currentGen, rawIdx))
	}
	if newGen == currentGen {
		return // same generation, no-op
	}

	w.index.SetVersion(rawIdx, newGen)

	// Update the entity's entry in its table's entities slice so that the table
	// reflects the new versioned ID. Mirrors C flecs_entity_index_set_version.
	rec := w.index.Get(versionedID)
	if rec != nil && rec.Table != nil {
		rec.Table.Entities()[int(rec.Row)] = versionedID
	}
}
