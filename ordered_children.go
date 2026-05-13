package flecs

import "github.com/snichols/flecs/internal/storage/table"

// orderedChildList holds the ordered list of child entity IDs for a parent
// that has been marked with the OrderedChildren trait. Entries are kept in
// insertion order; removal is O(n) with a slice-compact to preserve that
// order, matching the upstream C ecs_vec_remove_ordered_t approach in
// src/storage/ordered_children.c:99.
type orderedChildList struct {
	entries []ID
}

// OrderedChildren returns the ID of the built-in OrderedChildren trait entity (index 33).
//
// When added to a parent entity, OrderedChildren guarantees that [World.EachChild]
// iterates the parent's direct children in insertion order, independent of any
// archetype-table reshuffling caused by component composition changes on the
// children. Without the trait, EachChild uses archetype-derived iteration order.
//
// OrderedChildren is opt-in per parent:
//
//	flecs.SetOrderedChildren(w, parentID)
//	// or bare-tag form:
//	fw.AddID(parentID, w.OrderedChildren())
//
// Once set, EachChild snapshots the ordered list at iteration start, so
// mutations inside the callback (add/remove children) do not affect the
// in-progress iteration but are visible on the next call.
//
// If the parent already has children when the trait is applied, those children
// are snapshotted into the list in their current archetype order, matching the
// upstream flecs_ordered_children_populate behavior.
//
// ChildOf is bootstrapped Exclusive: each entity has at most one parent, so
// the ordered-children list tracks each child's slot unambiguously.
//
// Storage: plain []ID slice with O(n) ordered remove (append(s[:i], s[i+1:]...)).
// This matches the upstream C vec approach; swap-with-last would break the
// ordering guarantee that is the entire point of the trait.
func (w *World) OrderedChildren() ID { return w.orderedChildrenID }

// SetOrderedChildren marks parent as an ordered-children parent so that
// [World.EachChild] returns its direct children in insertion order.
//
// Equivalent to fw.AddID(parent, w.OrderedChildren()).
//
// If parent already has children at the time SetOrderedChildren is called,
// those children are snapshotted into the ordered list in their current
// archetype order. Subsequent child additions are appended in arrival order;
// removals compact the slice preserving relative order.
//
// Calling SetOrderedChildren twice on the same parent is a no-op.
func SetOrderedChildren(w *World, parent ID) {
	applyOrderedChildrenPolicy(w, parent)
}

// IsOrderedChildren reports whether parent has been marked with the
// OrderedChildren trait. Accepts scope so it can be called inside both
// Read and Write blocks (per Phase 15.8 convention).
func IsOrderedChildren(s scope, parent ID) bool {
	w := s.scopeWorld()
	if w.orderedChildren == nil {
		return false
	}
	_, ok := w.orderedChildren[ID(parent.Index())]
	return ok
}

// applyOrderedChildrenPolicy initializes the ordered-children list for parent
// and snapshots any existing (ChildOf, parent) children into it in their
// current archetype order. Called by SetOrderedChildren and by addIDImmediate
// when the bare OrderedChildren tag is added to an entity.
//
// Mirrors upstream flecs_ordered_children_populate in
// src/storage/ordered_children.c:25-42.
//
// Idempotent: a second call on an already-ordered parent is a no-op.
func applyOrderedChildrenPolicy(w *World, parent ID) {
	if w.orderedChildren == nil {
		w.orderedChildren = make(map[ID]*orderedChildList)
	}
	key := ID(parent.Index())
	if _, ok := w.orderedChildren[key]; ok {
		return
	}
	list := &orderedChildList{}
	pairID := MakePair(w.childOfID, parent)
	w.compIndex.Each(pairID, func(t *table.Table) bool {
		for _, child := range t.Entities() {
			list.entries = append(list.entries, child)
		}
		return true
	})
	w.orderedChildren[key] = list
}

// removeFromOrderedList removes e from list.entries using O(n) ordered removal.
// Only the first occurrence is removed (duplicates should never occur in practice).
func removeFromOrderedList(list *orderedChildList, e ID) {
	for i, child := range list.entries {
		if child.Index() == e.Index() {
			list.entries = append(list.entries[:i], list.entries[i+1:]...)
			return
		}
	}
}
