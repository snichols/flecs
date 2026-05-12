package flecs

import (
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
					// Symmetric mirror: if (R, b) replaced (R, x) on a, add (R, a) to b.
					if w.symmetricPolicies[id.First()] {
						addIDImmediate(w, id.Second(), MakePair(id.First(), e))
					}
					return true
				}
			}
		}
	}
	w.migrate(e, id, 0, nil)
	// Symmetric mirror: if (R, b) was added to a, also add (R, a) to b.
	// The HasComponent early-return above provides the idempotence loop-guard:
	// the recursive call re-enters here, but b already has (R, a) and returns false.
	if id.IsPair() && w.symmetricPolicies[id.First()] {
		addIDImmediate(w, id.Second(), MakePair(id.First(), e))
	}
	// Override copy hook: after adding (IsA, prefab) to an entity, walk the prefab's
	// component chain and eagerly copy any Override-marked components into the instance.
	if id.IsPair() && id.First().Index() == w.isAID.Index() {
		prefab := id.Second()
		if w.index.IsAlive(prefab) {
			overrideCopyForInstance(w, e, prefab, nil)
		}
	}
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
	rec := w.index.Get(e)
	if rec == nil {
		return false
	}
	if rec.Table == nil || !rec.Table.HasComponent(id) {
		return false
	}
	w.migrate(e, 0, id, nil)
	// Symmetric mirror: if (R, b) was removed from a, also remove (R, a) from b.
	// The !HasComponent early-return above provides the idempotence loop-guard:
	// the recursive call re-enters here, but b no longer has (R, a) and returns false.
	if id.IsPair() && w.symmetricPolicies[id.First()] {
		removeIDImmediate(w, id.Second(), MakePair(id.First(), e))
	}
	return true
}

func setPairImmediate[T any](w *World, e ID, rel ID, tgt ID, v T) {
	pairID := MakePair(rel, tgt)
	pairInfo := component.RegisterPairData[T](w.registry, pairID)
	rec := w.index.Get(e)
	if rec == nil {
		panic("flecs: SetPair called on dead entity")
	}
	t := rec.Table
	if t != nil && t.HasComponent(pairID) {
		t.Set(int(rec.Row), pairID, unsafe.Pointer(&v))
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
