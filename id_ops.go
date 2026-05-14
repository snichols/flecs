package flecs

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/snichols/flecs/internal/component"
)

// addIDOnWorld adds the component or tag identified by id to entity e.
// Internal helper used by Writer.AddID and scope.AddID.
func addIDOnWorld(w *World, e ID, e2 ID) bool {
	s0 := w.stages[0]
	if s0.deferDepth > 0 {
		rec := w.index.Get(e)
		if rec == nil {
			panic("flecs: AddID called on dead entity")
		}
		// Data-bearing sparse/DontFragment components cannot be added as a bare tag.
		if !e2.IsPair() {
			eIdx := ID(e2.Index())
			if w.sparsePolicies[eIdx] {
				panic(fmt.Sprintf("flecs: AddID: cannot add Sparse component %v as a tag; use Set with a value", e2))
			}
			if w.dontFragmentPolicies[eIdx] {
				panic(fmt.Sprintf("flecs: AddID: cannot add DontFragment component %v as a tag; use Set with a value", e2))
			}
		}
		// Union pair: check union store for idempotency (pair never appears in archetype).
		if e2.IsPair() {
			relKey := ID(e2.First().Index())
			if w.unionPolicies[relKey] {
				if store, ok := w.unionStore[relKey]; ok {
					entityKey := ID(e.Index())
					if pos, has := store.index[entityKey]; has {
						if store.dense[pos].target.Index() == e2.Second().Index() {
							return false // already has this exact target
						}
					}
				}
				w.registry.EnsureID(e2)
				s0.queue.append(cmd{kind: cmdAddID, entity: e, id: e2})
				return true
			}
		}
		if rec.Table != nil && rec.Table.HasComponent(e2) {
			return false
		}
		w.registry.EnsureID(e2)
		s0.queue.append(cmd{kind: cmdAddID, entity: e, id: e2})
		return true
	}
	return addIDImmediate(w, e, e2)
}

func addIDImmediate(w *World, e ID, id ID) bool {
	rec := w.index.Get(e)
	if rec == nil {
		panic("flecs: AddID called on dead entity")
	}
	if rec.Table != nil && rec.Table.HasComponent(id) {
		return false
	}
	w.registry.EnsureID(id)
	// Data-bearing sparse/DontFragment components cannot be added as a bare tag.
	if !id.IsPair() {
		iIdx := ID(id.Index())
		if w.sparsePolicies[iIdx] {
			panic(fmt.Sprintf("flecs: AddID: cannot add Sparse component %v as a tag; use Set with a value", id))
		}
		if w.dontFragmentPolicies[iIdx] {
			panic(fmt.Sprintf("flecs: AddID: cannot add DontFragment component %v as a tag; use Set with a value", id))
		}
	}
	// When a cleanup-trait pair is added to an entity, translate the pair into
	// an entry in the world's cleanupPolicies map. This makes the pair-add path
	// fw.AddID(relID, MakePair(w.OnDeleteTarget(), w.DeleteAction())) equivalent
	// to SetCleanupPolicy(w, relID, w.OnDeleteTarget(), w.DeleteAction()).
	// Mirrors the C bootstrap.c:294-309 flecs_register_on_delete observer approach.
	if id.IsPair() {
		firstIdx := id.First().Index()
		if firstIdx == w.onDeleteID.Index() || firstIdx == w.onDeleteTargetID.Index() {
			var trait ID
			if firstIdx == w.onDeleteID.Index() {
				trait = w.onDeleteID
			} else {
				trait = w.onDeleteTargetID
			}
			actionIdx := id.Second().Index()
			var action ID
			switch actionIdx {
			case w.deleteActionID.Index():
				action = w.deleteActionID
			case w.panicActionID.Index():
				action = w.panicActionID
			default:
				action = w.removeActionID
			}
			applyCleanupPolicy(w, e, trait, action)
		}
		// When a (OnInstantiate, action) pair is added to a component entity,
		// translate it into a per-component policy flag. Mirrors C bootstrap.c:311-317
		// flecs_register_on_instantiate observer pattern.
		if firstIdx == w.onInstantiateID.Index() {
			applyInstantiatePolicy(w, e, id.Second())
		}
		// When a (OneOf, parent) pair is added to a relationship entity, register
		// the OneOf constraint with the specified parent. Pair form allows
		// fw.AddID(relID, MakePair(w.OneOf(), parentID)) to configure the constraint.
		if firstIdx == w.oneOfID.Index() {
			applyOneOfPolicy(w, e, id.Second())
		}
	} else if id.Index() == w.exclusiveID.Index() {
		// Bare Exclusive tag added to entity e: mark e's relationship as exclusive.
		// This is the fw.AddID(myRel, w.Exclusive()) form, mirroring C's
		// ecs_add_id(world, MyRel, EcsExclusive).
		applyExclusivePolicy(w, e)
	} else if id.Index() == w.canToggleID.Index() {
		// Bare CanToggle tag added to entity e: mark e's component as toggleable.
		// This is the fw.AddID(posID, w.CanToggle()) form, mirroring C's
		// ecs_add_id(world, ecs_id(Position), EcsCanToggle).
		applyCanTogglePolicy(w, e)
	} else if id.Index() == w.symmetricID.Index() {
		// Bare Symmetric tag added to entity e: mark e's relationship as symmetric.
		// This is the fw.AddID(relID, w.Symmetric()) form, mirroring C's
		// ecs_add_id(world, MyRel, EcsSymmetric).
		applySymmetricPolicy(w, e)
	} else if id.Index() == w.transitiveID.Index() {
		// Bare Transitive tag added to entity e: mark e's relationship as transitive.
		// This is the fw.AddID(relID, w.Transitive()) form, mirroring C's
		// ecs_add_id(world, MyRel, EcsTransitive).
		applyTransitivePolicy(w, e)
	} else if id.Index() == w.reflexiveID.Index() {
		// Bare Reflexive tag added to entity e: mark e's relationship as reflexive.
		// This is the fw.AddID(relID, w.Reflexive()) form, mirroring C's
		// ecs_add_id(world, MyRel, EcsReflexive).
		applyReflexivePolicy(w, e)
	} else if id.Index() == w.acyclicID.Index() {
		// Bare Acyclic tag added to entity e: mark e's relationship as acyclic.
		// This is the fw.AddID(relID, w.Acyclic()) form, mirroring C's
		// ecs_add_id(world, MyRel, EcsAcyclic).
		applyAcyclicPolicy(w, e)
	} else if id.Index() == w.finalID.Index() {
		// Bare Final tag added to entity e: mark e as final so it cannot be
		// used as an IsA target. This is the fw.AddID(entityID, w.Final()) form,
		// mirroring C's ecs_add_id(world, e, EcsFinal).
		applyFinalPolicy(w, e)
	} else if id.Index() == w.oneOfID.Index() {
		// Bare OneOf tag added to relationship entity e: self-tag form — targets
		// must be direct children of e itself. This is the fw.AddID(relID, w.OneOf())
		// form, mirroring C's ecs_add_id(world, MyRel, EcsOneOf).
		applyOneOfPolicy(w, e, e)
	} else if id.Index() == w.singletonID.Index() {
		// Bare Singleton tag added to component entity e: mark e as a singleton.
		// This is the fw.AddID(componentID, w.Singleton()) form.
		applySingletonPolicy(w, e)
	} else if id.Index() == w.writeOnceID.Index() {
		// Bare WriteOnce tag added to component entity e: mark e as write-once.
		// This is the fw.AddID(componentID, w.WriteOnce()) form.
		applyWriteOncePolicy(w, e)
	} else if id.Index() == w.traversableID.Index() {
		// Bare Traversable tag added to entity e: mark e as a traversable relationship.
		// This is the fw.AddID(relID, w.Traversable()) form, mirroring C's
		// ecs_add_id(world, MyRel, EcsTraversable).
		applyTraversablePolicy(w, e)
	} else if id.Index() == w.relationshipID.Index() {
		// Bare Relationship tag: mark e as a Relationship-constrained entity.
		applyRelationshipPolicy(w, e)
	} else if id.Index() == w.targetID.Index() {
		// Bare Target tag: mark e as a Target-constrained entity.
		applyTargetPolicy(w, e)
	} else if id.Index() == w.traitID.Index() {
		// Bare Trait tag: mark e as a Trait entity (exempts it from Relationship's
		// no-tag-as-target check when appearing in a pair target slot).
		applyTraitPolicy(w, e)
	} else if id.Index() == w.pairIsTagID.Index() {
		// Bare PairIsTag tag: mark e's relationship so all (e, *) pairs are tag-only.
		// This is the fw.AddID(relID, w.PairIsTag()) form, mirroring C's
		// ecs_add_id(world, MyRel, EcsPairIsTag).
		applyPairIsTagPolicy(w, e)
	} else if id.Index() == w.withID.Index() {
		// Bare With tag added to entity e: no immediate policy to apply — With's
		// co-add metadata lives in (With, Y) pairs on e, not in a side-map.
		// The presence of the bare With tag on e itself is allowed for completeness
		// but the semantics are driven by pair-form (With, coAdd) storage.
		_ = e
	} else if w.orderedChildrenID != 0 && id.Index() == w.orderedChildrenID.Index() {
		// Bare OrderedChildren tag added to entity e: initialize the per-parent
		// ordered list and snapshot any existing (ChildOf, e) children into it.
		// This is the fw.AddID(parentID, w.OrderedChildren()) form, mirroring C's
		// flecs_register_ordered_children observer in src/bootstrap.c:604-630.
		applyOrderedChildrenPolicy(w, e)
	} else if w.sparseID != 0 && id.Index() == w.sparseID.Index() {
		// Bare Sparse tag added to component entity e: mark e as Sparse-stored.
		// This is the fw.AddID(componentID, w.Sparse()) form.
		applySparsePolicy(w, e)
	} else if w.dontFragmentID != 0 && id.Index() == w.dontFragmentID.Index() {
		// Bare DontFragment tag added to component entity e: mark e as DontFragment.
		// This is the fw.AddID(componentID, w.DontFragment()) form, mirroring C's
		// ecs_add_id(world, ecs_id(Position), EcsDontFragment).
		applyDontFragmentPolicy(w, e)
	}
	// Union pair handling: at-most-one-target without archetype transition.
	// Must come before the Exclusive enforcement block (union implies exclusive,
	// but uses the union store rather than archetype migration).
	if id.IsPair() {
		relKey := ID(id.First().Index())
		if w.unionPolicies[relKey] {
			// OneOf constraint: validate target before storing.
			checkOneOf(w, id.First(), id.Second())
			// Store target in union store; fire OnRemove/OnAdd.
			unionStoreSet(w, relKey, e, id.First(), id.Second())
			// With co-adds: fire (With, Y) pairs on the relationship entity.
			applyWithCoAdds(w, e, id)
			return true
		}
	}
	// Usage-constraint enforcement: Relationship/Target/Trait checks fire for both
	// bare-tag and pair-form adds, before any migration. Mirrors C
	// component_index.c:384-481.
	checkUsageConstraints(w, id)
	// Acyclic cycle check: if adding (e, R, target) and R is acyclic, verify
	// that target cannot already reach e via R. Write-time rejection is a
	// deliberate divergence from C (which guards at lookup/traversal time).
	if id.IsPair() && w.acyclicPolicies[ID(id.First().Index())] {
		checkAcyclic(w, e, id.First(), id.Second())
	}
	// Final enforcement: adding (IsA, target) panics if target has the Final
	// trait. Mirrors C component_index.c:447-453. Fires for self-pairs too
	// (src == target), matching C's unconditional ecs_has_id check.
	if id.IsPair() && id.First().Index() == w.isAID.Index() {
		checkFinal(w, id.Second())
	}
	// OneOf constraint enforcement: adding (R, target) panics if R has a OneOf
	// constraint and target is not a direct child of the required parent. Mirrors
	// C component_index.c:418-441. Enforced before Exclusive migration so that
	// replacement targets are validated before the atomic swap occurs.
	if id.IsPair() {
		checkOneOf(w, id.First(), id.Second())
	}
	// Exclusive pair enforcement: if adding (R, B) and the entity already has
	// (R, A) where A != B, replace A with B in a single migration so that
	// OnRemove for A and OnAdd for B both fire via the standard emit path.
	// This mirrors C table_graph.c:1062-1073 flecs_table_traverse_add.
	if id.IsPair() && w.exclusivePolicies[id.First()] {
		rec = w.index.Get(e)
		if rec != nil && rec.Table != nil {
			relIdx := uint32(id.First())
			for _, sigID := range rec.Table.Type() {
				if sigID.IsPair() && uint32(sigID.First()) == relIdx && sigID.Second() != id.Second() {
					w.migrate(e, id, sigID, nil)
					// OrderedChildren re-parent hooks: run after migrate so table state is stable.
					// If (ChildOf, oldParent) → (ChildOf, newParent), update both ordered lists.
					// Mirrors upstream flecs_ordered_children_reparent/unparent called from
					// component_actions.c:126,141.
					if id.First().Index() == w.childOfID.Index() && w.orderedChildren != nil {
						oldParent := sigID.Second()
						if list, ok := w.orderedChildren[ID(oldParent.Index())]; ok {
							removeFromOrderedList(list, e)
						}
						newParent := id.Second()
						if list, ok := w.orderedChildren[ID(newParent.Index())]; ok {
							list.entries = append(list.entries, e)
						}
					}
					// Symmetric mirror: if (R, b) replaced (R, x) on a, add (R, a) to b.
					if w.symmetricPolicies[id.First()] {
						addIDImmediate(w, id.Second(), MakePair(id.First(), e))
					}
					// With co-add: fire (With, *) pairs registered on id's relationship.
					applyWithCoAdds(w, e, id)
					return true
				}
			}
		}
	}
	// Singleton enforcement: if componentID (or the First() of a pair) is marked
	// as a singleton, at most one entity may hold it. checkSingleton records or
	// validates the holder; it panics if a different entity already holds it.
	if !id.IsPair() && w.singletonPolicies[ID(id.Index())] {
		checkSingleton(w, id, e)
	} else if id.IsPair() && w.singletonPolicies[ID(id.First().Index())] {
		checkSingleton(w, id.First(), e)
	}
	w.migrate(e, id, 0, nil)
	// OrderedChildren child-add hook: if (ChildOf, parent) was added and parent is
	// ordered, append e to the parent's list. Runs after migrate so table state is
	// consistent. Mirrors upstream flecs_ordered_children_reparent in
	// src/storage/ordered_children.c:127-145 (called from component_actions.c:126).
	if id.IsPair() && id.First().Index() == w.childOfID.Index() && w.orderedChildren != nil {
		parent := id.Second()
		if list, ok := w.orderedChildren[ID(parent.Index())]; ok {
			list.entries = append(list.entries, e)
		}
	}
	// Symmetric mirror: if (R, b) was added to a, also add (R, a) to b.
	// The HasComponent early-return above provides the idempotence loop-guard:
	// the recursive call re-enters here, but b already has (R, a) and returns false.
	if id.IsPair() && w.symmetricPolicies[id.First()] {
		addIDImmediate(w, id.Second(), MakePair(id.First(), e))
	}
	// Override copy + subtree-copy hooks: after adding (IsA, prefab) to an entity,
	// eagerly copy Override-marked components and replicate the prefab's child
	// subtree onto the instance. Invalidate the propagation inheritor cache so that
	// the next propagateEvent call rebuilds the BFS-ordered inheritor list.
	if id.IsPair() && id.First().Index() == w.isAID.Index() {
		w.invalidateInheritorCache()
		prefab := id.Second()
		if w.index.IsAlive(prefab) {
			overrideCopyForInstance(w, e, prefab, nil)
			instantiateChildrenForInstance(w, e, prefab)
		}
	}
	// With co-add: after the originating add's table transition lands, fire any
	// (With, Y) pairs registered on the source entity. Each co-add is its own
	// independent addIDImmediate call (its own migration, its own OnAdd hook fire).
	// applyWithCoAdds manages withExpandStack for cycle detection.
	applyWithCoAdds(w, e, id)
	return true
}

// overrideCopyForInstance walks the IsA chain starting at prefab and copies
// Override-marked components into instance if instance does not already own them.
// seen is allocated lazily and prevents infinite cycles.
func overrideCopyForInstance(w *World, instance, prefab ID, seen map[ID]struct{}) {
	if !w.index.IsAlive(prefab) {
		return
	}
	if seen == nil {
		seen = map[ID]struct{}{instance: {}}
	}
	if _, ok := seen[prefab]; ok {
		return
	}
	seen[prefab] = struct{}{}

	prefabRec := w.index.Get(prefab)
	if prefabRec == nil || prefabRec.Table == nil {
		return
	}

	isAIdx := w.isAID.Index()
	for _, cid := range prefabRec.Table.Type() {
		if cid.IsPair() {
			if uint32(cid.First()) == isAIdx {
				overrideCopyForInstance(w, instance, cid.Second(), seen)
			}
			continue
		}
		if w.instantiatePolicies[cid]&policyOnInstantiateOverride == 0 {
			continue
		}
		instanceRec := w.index.Get(instance)
		if instanceRec == nil {
			return
		}
		if instanceRec.Table != nil && instanceRec.Table.HasComponent(cid) {
			continue // already owned locally (pre-set or copied from an earlier sub-prefab)
		}
		ptr := prefabRec.Table.Get(int(prefabRec.Row), cid)
		info, ok := w.registry.LookupByID(cid)
		if !ok {
			continue
		}
		setImmediateByPtr(w, instance, cid, ptr, info)
	}
}

func removeIDImmediate(w *World, e ID, id ID) bool {
	// Union pair: remove from union store; no archetype transition.
	if id.IsPair() {
		relKey := ID(id.First().Index())
		if w.unionPolicies[relKey] {
			store, ok := w.unionStore[relKey]
			if !ok {
				return false
			}
			entityKey := ID(e.Index())
			pos, has := store.index[entityKey]
			if !has {
				return false
			}
			currentTarget := store.dense[pos].target
			termTarget := id.Second()
			// Specific target: only clear if it matches the current target.
			// Wildcard target: clear regardless.
			if !isWildcardID(w, termTarget) && currentTarget.Index() != termTarget.Index() {
				return false // no-op: different specific target
			}
			// Fire OnRemove, then remove from store.
			oldPairID := MakePair(id.First(), currentTarget)
			w.registry.EnsureID(oldPairID)
			oldInfo, _ := w.registry.LookupByID(oldPairID)
			w.fireOnRemove(oldInfo, oldPairID, e, nil)
			unionStoreRemove(store, e)
			// Fire sparse monitors AFTER store is updated (pair no longer active).
			if len(w.monitors) > 0 {
				w.fireSparseMonitors(e, oldPairID, 0)
			}
			return true
		}
	}
	if !id.IsPair() {
		iIdx := ID(id.Index())
		// DontFragment (alone or with Sparse): remove from sparse-set; do NOT cause
		// an archetype transition (component was never in the archetype type).
		if w.dontFragmentPolicies[iIdx] {
			ptr := sparseSetGet(w, e, id)
			if ptr == nil {
				return false
			}
			info, _ := w.registry.LookupByID(id)
			w.fireOnRemove(info, id, e, ptr)
			sparseSetRemove(w, e, id)
			if len(w.monitors) > 0 {
				w.fireSparseMonitors(e, id, 0)
			}
			return true
		}
		// Sparse-only (no DontFragment): remove from sparse-set AND cause an archetype
		// transition (component IS in the archetype type).
		if w.sparsePolicies[iIdx] {
			ptr := sparseSetGet(w, e, id)
			if ptr == nil {
				return false
			}
			info, _ := w.registry.LookupByID(id)
			w.fireOnRemove(info, id, e, ptr)
			sparseSetRemove(w, e, id)
			rec := w.index.Get(e)
			if rec != nil && rec.Table != nil && rec.Table.HasComponent(id) {
				w.migrateArchetypeOnly(e, 0, id)
			}
			if len(w.monitors) > 0 {
				w.fireSparseMonitors(e, id, 0)
			}
			return true
		}
	}
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	if rec.Table == nil || !rec.Table.HasComponent(id) {
		return false
	}
	// Singleton slot release: when a singleton component is removed from its
	// holder, clear the instance record so a new entity may take the slot.
	if !id.IsPair() && w.singletonPolicies[ID(id.Index())] {
		if existing, ok := w.singletonInstances[ID(id.Index())]; ok && existing.Index() == e.Index() {
			delete(w.singletonInstances, ID(id.Index()))
		}
	} else if id.IsPair() && w.singletonPolicies[ID(id.First().Index())] {
		if existing, ok := w.singletonInstances[ID(id.First().Index())]; ok && existing.Index() == e.Index() {
			delete(w.singletonInstances, ID(id.First().Index()))
		}
	}
	// WriteOnce tracking clear: Remove resets the per-(entity, component) write
	// slot so a subsequent Add + Set cycle starts fresh.
	clearWriteOnceTracking(w, e, id)
	w.migrate(e, 0, id, nil)
	// OrderedChildren child-remove hook: if (ChildOf, parent) was removed and parent
	// is ordered, remove e from the parent's list. Runs after migrate so table state
	// is consistent. Mirrors upstream flecs_ordered_children_unparent in
	// src/storage/ordered_children.c:147-155 (called from component_actions.c:141).
	if id.IsPair() && id.First().Index() == w.childOfID.Index() && w.orderedChildren != nil {
		parent := id.Second()
		if list, ok := w.orderedChildren[ID(parent.Index())]; ok {
			removeFromOrderedList(list, e)
		}
	}
	// Propagation cache invalidation: removing (IsA, prefab) changes the inheritor
	// tree — evict the entire inheritor cache so the next propagateEvent call rebuilds.
	if id.IsPair() && id.First().Index() == w.isAID.Index() {
		w.invalidateInheritorCache()
	}
	// Symmetric mirror: if (R, b) was removed from a, also remove (R, a) from b.
	// The !HasComponent early-return above provides the idempotence loop-guard:
	// the recursive call re-enters here, but b no longer has (R, a) and returns false.
	if id.IsPair() && w.symmetricPolicies[id.First()] {
		removeIDImmediate(w, id.Second(), MakePair(id.First(), e))
	}
	return true
}

func setPairImmediate[T any](w *World, e ID, rel ID, tgt ID, v T) {
	checkPairIsTag(w, rel)
	checkUnionPair(w, rel)
	pairID := MakePair(rel, tgt)
	pairInfo := component.RegisterPairData[T](w.registry, pairID)
	rec := w.index.Get(e)
	if rec == nil {
		panic("flecs: SetPair called on dead entity")
	}
	t := rec.Table
	if t != nil && t.HasComponent(pairID) {
		newPtr := unsafe.Pointer(&v)
		oldPtr := t.Get(int(rec.Row), pairID) // capture before overwrite
		w.fireOnReplace(pairInfo, pairID, e, oldPtr, newPtr)
		t.Set(int(rec.Row), pairID, newPtr)
		w.fireOnSet(pairInfo, pairID, e, t.Get(int(rec.Row), pairID))
		t.BumpChange() // pair data column write
		return
	}
	w.migrate(e, pairID, 0, unsafe.Pointer(&v))
	// OnAdd fired inside migrate; fire OnSet now.
	rec = w.index.Get(e)
	w.fireOnSet(pairInfo, pairID, e, rec.Table.Get(int(rec.Row), pairID))
}

// getPairOnWorld returns the value of pair (rel, tgt) on entity e.
// Internal helper.
func getPairOnWorld[T any](w *World, e ID, rel ID, tgt ID) (T, bool) {
	var zero T
	pairID := MakePair(rel, tgt)
	info, ok := w.registry.LookupByID(pairID)
	if !ok {
		return zero, false
	}
	if info.Type != reflect.TypeFor[T]() {
		return zero, false
	}
	rec := w.index.Get(e)
	if rec == nil {
		return zero, false
	}
	t := rec.Table
	if t == nil || !t.HasComponent(pairID) {
		return zero, false
	}
	ptr := t.Get(int(rec.Row), pairID)
	if ptr == nil {
		// Zero-size pair data type (T = struct{}): entity has it but no data slot.
		return zero, true
	}
	return *(*T)(ptr), true
}
