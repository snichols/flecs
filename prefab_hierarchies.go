package flecs

import (
	"unsafe"

	"github.com/snichols/flecs/internal/component"
)

// instantiateChildrenForInstance replicates the prefab's child subtree onto
// instance. Called from addIDImmediate immediately after overrideCopyForInstance
// when (IsA, prefab) is added to instance.
//
// Deferred-path note: this is always called in the immediate execution context
// (from addIDImmediate or from dispatch). Each individual AddID/Set on the newly
// created instance children also runs immediate, coalescing through the standard
// cmd_queue path only when the originating Write scope re-enters. No new
// coalescer logic is needed.
func instantiateChildrenForInstance(w *World, instance, prefab ID) {
	// Collect the direct children of the prefab.
	var children []ID
	w.EachChild(prefab, func(c ID) bool {
		children = append(children, c)
		return true
	})
	if len(children) == 0 {
		return
	}

	// If the prefab is ordered, mark the instance as ordered BEFORE adding any
	// instance children so the ordered list starts empty and gets populated in
	// insertion order via the addIDImmediate ChildOf hook.
	if w.orderedChildren != nil {
		if _, ok := w.orderedChildren[ID(prefab.Index())]; ok {
			if _, alreadyOrdered := w.orderedChildren[ID(instance.Index())]; !alreadyOrdered {
				applyOrderedChildrenPolicy(w, instance)
			}
		}
	}

	// Pass 1: Allocate all entities in the subtree and build the prefab→instance
	// map. This two-pass approach handles sibling forward references: a pair
	// (R, siblingB) on siblingA can be rewritten to (R, instanceB) even when
	// siblingA is processed before siblingB.
	subtreeMap := make(map[uint32]ID, len(children)*2)
	allocSubtreeEntities(w, children, subtreeMap)

	// Pass 2: Copy components, add ChildOf, and handle slot resolution.
	for _, c := range children {
		ic := subtreeMap[c.Index()]
		copyInstanceChild(w, instance, prefab, c, ic, subtreeMap)
	}
}

// allocSubtreeEntities recursively pre-allocates instance entity IDs for all
// descendants of prefabChildren, recording prefabEntity.Index() → instanceEntity
// in subtreeMap before any component copying begins.
func allocSubtreeEntities(w *World, prefabChildren []ID, subtreeMap map[uint32]ID) {
	for _, c := range prefabChildren {
		ic := w.newEntityInternal()
		subtreeMap[c.Index()] = ic
		var grandChildren []ID
		w.EachChild(c, func(gc ID) bool {
			grandChildren = append(grandChildren, gc)
			return true
		})
		if len(grandChildren) > 0 {
			allocSubtreeEntities(w, grandChildren, subtreeMap)
		}
	}
}

// copyInstanceChild copies prefab child c to instance child ic:
//   - Applies OrderedChildren to ic (if c is ordered) before adding ic's children
//   - Adds (ChildOf, instanceParent) to ic
//   - Resolves (SlotOf, prefabParent) → adds (c, ic) to instanceParent
//   - Copies components, rewriting same-subtree cross-references
//   - Recurses on grandchildren
//
// prefabParent is the prefab-side parent of c; instanceParent is the
// corresponding instance-side entity.
func copyInstanceChild(w *World, instanceParent, prefabParent, c, ic ID, subtreeMap map[uint32]ID) {
	// Apply OrderedChildren to ic BEFORE adding ic's own children, so the
	// ordered list starts empty and gets populated in insertion order.
	if w.orderedChildren != nil {
		if _, ok := w.orderedChildren[ID(c.Index())]; ok {
			applyOrderedChildrenPolicy(w, ic)
		}
	}

	// Add (ChildOf, instanceParent) to ic. If instanceParent is ordered, this
	// appends ic to instanceParent's ordered list via the addIDImmediate hook.
	addIDImmediate(w, ic, MakePair(w.childOfID, instanceParent))

	// Copy archetype components from c to ic.
	copyPrefabChildComponents(w, instanceParent, prefabParent, c, ic, subtreeMap)

	// Recurse on grandchildren.
	var grandChildren []ID
	w.EachChild(c, func(gc ID) bool {
		grandChildren = append(grandChildren, gc)
		return true
	})
	for _, gc := range grandChildren {
		igc := subtreeMap[gc.Index()]
		copyInstanceChild(w, ic, c, gc, igc, subtreeMap)
	}
}

// copyPrefabChildComponents walks the archetype of prefab child c and copies
// each component to instance child ic, applying cross-reference rewriting and
// slot resolution.
func copyPrefabChildComponents(w *World, instanceParent, prefabParent, c, ic ID, subtreeMap map[uint32]ID) {
	prefabRec := w.index.Get(c)
	if prefabRec == nil || prefabRec.Table == nil {
		return
	}

	childOfIdx := w.childOfID.Index()
	isAIdx := w.isAID.Index()
	slotOfIdx := uint32(0)
	if w.slotOfID != 0 {
		slotOfIdx = w.slotOfID.Index()
	}
	srcTable := prefabRec.Table
	srcRow := int(prefabRec.Row)

	for _, cid := range srcTable.Type() {
		if cid.IsPair() {
			firstIdx := cid.First().Index()

			// (ChildOf, *): handled explicitly in copyInstanceChild; skip.
			if firstIdx == childOfIdx {
				continue
			}
			// (IsA, *): prefab-of-prefab is deferred to a future phase; skip.
			if firstIdx == isAIdx {
				continue
			}
			// (SlotOf, prefabParent): resolve to (c, ic) on instanceParent.
			// Only the direct case is handled here; the nested-slot branch
			// (C instantiate.c:28-87, SlotOf targeting a grandparent prefab)
			// is deferred as a future enhancement.
			if slotOfIdx != 0 && firstIdx == uint32(slotOfIdx) {
				if cid.Second().Index() == prefabParent.Index() {
					addIDImmediate(w, instanceParent, MakePair(c, ic))
				}
				continue
			}

			// Rewrite same-subtree cross-references; leave external refs unchanged.
			tgt := ID(cid.Second())
			if mappedTgt, ok := subtreeMap[tgt.Index()]; ok {
				rewrittenID := MakePair(cid.First(), mappedTgt)
				copyPairToIC(w, srcTable, srcRow, ic, cid, rewrittenID)
			} else {
				copyPairToIC(w, srcTable, srcRow, ic, cid, cid)
			}
		} else {
			// Non-pair: skip DontInherit-policy components (includes the Prefab tag).
			if w.instantiatePolicies[cid]&policyOnInstantiateDontInherit != 0 {
				continue
			}
			ptr := srcTable.Get(srcRow, cid)
			info, _ := w.registry.LookupByID(cid)
			setImmediateByPtr(w, ic, cid, ptr, info)
		}
	}
}

// tableGetter is the subset of *table.Table used by copyPairToIC.
type tableGetter interface {
	Get(row int, id ID) unsafe.Pointer
}

// copyPairToIC copies originalID from srcTable[srcRow] to instance child ic
// using rewrittenID as the actual pair ID on ic (which may differ from
// originalID when same-subtree cross-reference rewriting is applied).
func copyPairToIC(w *World, srcTable tableGetter, srcRow int, ic, originalID, rewrittenID ID) {
	info, ok := w.registry.LookupByID(originalID)
	if !ok || info.Size == 0 {
		addIDImmediate(w, ic, rewrittenID)
		return
	}
	// Data-bearing pair: ensure the rewritten pair ID has a TypeInfo registered
	// before migrate is called to create its table column.
	if originalID != rewrittenID {
		ensureRewrittenPairRegistered(w, rewrittenID, info)
	}
	rewrittenInfo, _ := w.registry.LookupByID(rewrittenID)
	ptr := srcTable.Get(srcRow, originalID)
	setImmediateByPtr(w, ic, rewrittenID, ptr, rewrittenInfo)
}

// ensureRewrittenPairRegistered associates rewrittenID with a TypeInfo that
// matches the layout of origInfo. Used when cross-reference rewriting changes the
// target of a data-bearing pair, requiring the new pair ID to be registry-visible
// before addIDImmediate / migrate can create a table column for it.
func ensureRewrittenPairRegistered(w *World, rewrittenID ID, origInfo *component.TypeInfo) {
	if _, already := w.registry.LookupByID(rewrittenID); already {
		return
	}
	if origInfo.Type != nil {
		component.RegisterPairDataByType(w.registry, rewrittenID, origInfo.Type)
		return
	}
	// Dynamic component: create a fresh TypeInfo with matching layout.
	newInfo := &component.TypeInfo{
		Size:  origInfo.Size,
		Align: origInfo.Align,
		Name:  origInfo.Name,
	}
	w.registry.AssociateID(newInfo, rewrittenID)
}
