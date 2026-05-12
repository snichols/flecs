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
// Behavior is undefined if fn calls Delete, AddID, RemoveID, or Set during
// iteration; any of those operations may mutate the table being iterated.
func (w *World) EachChild(parent ID, fn func(child ID) bool) {
	w.checkExclusiveAccessRead()
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
	return firstPairTarget(rec.Table.Type(), w.childOfID.Index())
}
