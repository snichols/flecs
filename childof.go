package flecs

import "github.com/snichols/flecs/internal/storage/table"

// ChildOf returns the built-in ChildOf relationship entity.
//
// Use MakePair(w.ChildOf(), parent) as the pair ID when expressing a
// parent-child link. Deleting a parent cascades to all (ChildOf, parent)
// children recursively.
func (w *World) ChildOf() ID { return w.childOfID }

// EachChild calls fn for every direct child of parent — every entity e whose
// archetype signature contains (ChildOf, parent).
//
// fn returns false to stop iteration early.
//
// If parent has the [OrderedChildren] trait, children are visited in insertion
// order. The list is snapshotted at iteration start, so adds or removes
// performed inside fn do not affect the current iteration but are visible on
// the next EachChild call. This matches the safe-iteration pattern used
// elsewhere in flecs (mirrors upstream ecs_children in src/each.c:110-141).
//
// Without OrderedChildren, the existing archetype-derived iteration order is
// preserved unchanged.
func (w *World) EachChild(parent ID, fn func(child ID) bool) {
	w.checkExclusiveAccessRead()
	if w.orderedChildren != nil {
		if list, ok := w.orderedChildren[ID(parent.Index())]; ok {
			snapshot := append([]ID(nil), list.entries...)
			for _, child := range snapshot {
				if !fn(child) {
					return
				}
			}
			return
		}
	}
	// Parent-storage mode: scan the parent column of tables with the marker.
	childOfKey := ID(w.childOfID.Index())
	if w.parentStoragePolicies[childOfKey] {
		parentIdx := parent.Index()
		marker := w.parentStorageMarker(w.childOfID)
		w.compIndex.Each(marker, func(t *table.Table) bool {
			col := t.GetParentCol(childOfKey)
			ents := t.Entities()
			for i, child := range ents {
				if col != nil && i < len(col) && col[i].Index() == parentIdx {
					if !fn(child) {
						return false
					}
				}
			}
			return true
		})
		return
	}
	pairID := MakePair(w.childOfID, parent)
	w.compIndex.Each(pairID, func(t *table.Table) bool {
		for _, child := range t.Entities() {
			if !fn(child) {
				return false
			}
		}
		return true
	})
}

// ParentOf returns the parent of entity e: the target of the first
// (ChildOf, *) pair found in e's archetype signature.
//
// ChildOf is an exclusive relationship: at most one parent is allowed. Adding a
// second (ChildOf, parent) pair replaces the first. The returned value is always
// the entity's sole parent.
//
// Returns (0, false) if e is not alive or has no ChildOf relationship.
func (w *World) ParentOf(e ID) (ID, bool) {
	w.checkExclusiveAccessRead()
	rec := w.index.Get(e)
	if rec == nil || rec.Table == nil {
		return 0, false
	}
	// Parent-storage mode: read from the parent column.
	childOfKey := ID(w.childOfID.Index())
	if w.parentStoragePolicies[childOfKey] {
		marker := w.parentStorageMarker(w.childOfID)
		if rec.Table.HasComponent(marker) {
			if parent, ok := rec.Table.GetParentEntry(int(rec.Row), childOfKey); ok {
				return parent, true
			}
		}
		return 0, false
	}
	return firstPairTarget(rec.Table.Type(), w.childOfID.Index())
}
