package flecs_test

import (
	"encoding/json"
	"testing"

	"github.com/snichols/flecs"
)

// collectChildren returns all direct children of parent via EachChild.
func collectChildren(w *flecs.World, parent flecs.ID) []flecs.ID {
	var out []flecs.ID
	w.Read(func(r *flecs.Reader) {
		r.EachChild(parent, func(child flecs.ID) bool {
			out = append(out, child)
			return true
		})
	})
	return out
}

// TestOrderedChildren_BasicInsertion verifies that EachChild yields children
// in insertion order after SetOrderedChildren.
func TestOrderedChildren_BasicInsertion(t *testing.T) {
	w := flecs.New()
	var parent, c1, c2, c3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.SetOrderedChildren(w, parent)
		c1 = fw.NewEntity()
		flecs.AddID(fw, c1, flecs.MakePair(w.ChildOf(), parent))
		c2 = fw.NewEntity()
		flecs.AddID(fw, c2, flecs.MakePair(w.ChildOf(), parent))
		c3 = fw.NewEntity()
		flecs.AddID(fw, c3, flecs.MakePair(w.ChildOf(), parent))
	})

	got := collectChildren(w, parent)
	want := []flecs.ID{c1, c2, c3}
	if len(got) != len(want) {
		t.Fatalf("len: want %d, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d]: want %v, got %v", i, want[i], got[i])
		}
	}
}

// TestOrderedChildren_WithoutTrait verifies that existing archetype-derived
// iteration order is preserved when the trait is absent.
func TestOrderedChildren_WithoutTrait(t *testing.T) {
	w := flecs.New()
	var parent flecs.ID
	var children [3]flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		for i := range children {
			children[i] = fw.NewEntity()
			flecs.AddID(fw, children[i], flecs.MakePair(w.ChildOf(), parent))
		}
	})

	// Without the trait, EachChild uses the unordered archetype path.
	// Verify all three children are returned (order unspecified but count must match).
	got := collectChildren(w, parent)
	if len(got) != 3 {
		t.Fatalf("want 3 children, got %d", len(got))
	}
	// Also verify the trait is not set.
	w.Read(func(r *flecs.Reader) {
		if flecs.IsOrderedChildren(r, parent) {
			t.Error("IsOrderedChildren should be false for unset parent")
		}
	})
}

// TestOrderedChildren_RemoveMiddle verifies that removing the middle element
// preserves the relative order of the remaining children.
func TestOrderedChildren_RemoveMiddle(t *testing.T) {
	w := flecs.New()
	var parent flecs.ID
	var cs [5]flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.SetOrderedChildren(w, parent)
		for i := range cs {
			cs[i] = fw.NewEntity()
			flecs.AddID(fw, cs[i], flecs.MakePair(w.ChildOf(), parent))
		}
	})
	// Remove the middle (index 2).
	w.Write(func(fw *flecs.Writer) {
		flecs.RemoveID(fw, cs[2], flecs.MakePair(w.ChildOf(), parent))
	})

	got := collectChildren(w, parent)
	want := []flecs.ID{cs[0], cs[1], cs[3], cs[4]}
	if len(got) != len(want) {
		t.Fatalf("len: want %d, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d]: want %v, got %v", i, want[i], got[i])
		}
	}
}

// TestOrderedChildren_ReparentBothOrdered verifies that re-parenting a child
// from one ordered parent to another updates both lists.
func TestOrderedChildren_ReparentBothOrdered(t *testing.T) {
	w := flecs.New()
	var a, b, c flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a = fw.NewEntity()
		b = fw.NewEntity()
		flecs.SetOrderedChildren(w, a)
		flecs.SetOrderedChildren(w, b)
		c = fw.NewEntity()
		flecs.AddID(fw, c, flecs.MakePair(w.ChildOf(), a))
	})

	// Re-parent c from a to b.
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, c, flecs.MakePair(w.ChildOf(), b))
	})

	gotA := collectChildren(w, a)
	gotB := collectChildren(w, b)
	if len(gotA) != 0 {
		t.Errorf("A should have 0 children after re-parent, got %d", len(gotA))
	}
	if len(gotB) != 1 || gotB[0] != c {
		t.Errorf("B should have [c], got %v", gotB)
	}
}

// TestOrderedChildren_ReparentSrcOrderedDestUnordered verifies re-parent from
// ordered A to unordered B: c is removed from A's list, B is unaffected.
func TestOrderedChildren_ReparentSrcOrderedDestUnordered(t *testing.T) {
	w := flecs.New()
	var a, b, c flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a = fw.NewEntity()
		b = fw.NewEntity()
		flecs.SetOrderedChildren(w, a)
		c = fw.NewEntity()
		flecs.AddID(fw, c, flecs.MakePair(w.ChildOf(), a))
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, c, flecs.MakePair(w.ChildOf(), b))
	})

	gotA := collectChildren(w, a)
	if len(gotA) != 0 {
		t.Errorf("A should have 0 children, got %d", len(gotA))
	}
	// B unordered: just verify c is there via count.
	gotB := collectChildren(w, b)
	if len(gotB) != 1 {
		t.Errorf("B should have 1 child, got %d", len(gotB))
	}
	// B has no ordered list.
	w.Read(func(r *flecs.Reader) {
		if flecs.IsOrderedChildren(r, b) {
			t.Error("B should not be ordered")
		}
	})
}

// TestOrderedChildren_ReparentSrcUnorderedDestOrdered verifies re-parent from
// unordered A to ordered B: c is appended to B's list, A is unaffected.
func TestOrderedChildren_ReparentSrcUnorderedDestOrdered(t *testing.T) {
	w := flecs.New()
	var a, b, c flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a = fw.NewEntity()
		b = fw.NewEntity()
		flecs.SetOrderedChildren(w, b)
		c = fw.NewEntity()
		flecs.AddID(fw, c, flecs.MakePair(w.ChildOf(), a))
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, c, flecs.MakePair(w.ChildOf(), b))
	})

	gotA := collectChildren(w, a)
	if len(gotA) != 0 {
		t.Errorf("A should have 0 children after re-parent, got %d", len(gotA))
	}
	gotB := collectChildren(w, b)
	if len(gotB) != 1 || gotB[0] != c {
		t.Errorf("B should have [c], got %v", gotB)
	}
}

// TestOrderedChildren_DeleteChild verifies that deleting a child removes it
// from the parent's ordered list.
func TestOrderedChildren_DeleteChild(t *testing.T) {
	w := flecs.New()
	var parent, c1, c2, c3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.SetOrderedChildren(w, parent)
		c1 = fw.NewEntity()
		flecs.AddID(fw, c1, flecs.MakePair(w.ChildOf(), parent))
		c2 = fw.NewEntity()
		flecs.AddID(fw, c2, flecs.MakePair(w.ChildOf(), parent))
		c3 = fw.NewEntity()
		flecs.AddID(fw, c3, flecs.MakePair(w.ChildOf(), parent))
	})

	w.Delete(c2)

	got := collectChildren(w, parent)
	want := []flecs.ID{c1, c3}
	if len(got) != len(want) {
		t.Fatalf("len: want %d, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d]: want %v, got %v", i, want[i], got[i])
		}
	}
}

// TestOrderedChildren_DeleteParent verifies that deleting an ordered parent
// drops its list, and cascade-deleted children are also cleaned up from any
// other ordered-parent lists they appear in (N/A here since ChildOf is exclusive).
func TestOrderedChildren_DeleteParent(t *testing.T) {
	w := flecs.New()
	var parent, c1, c2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.SetOrderedChildren(w, parent)
		c1 = fw.NewEntity()
		flecs.AddID(fw, c1, flecs.MakePair(w.ChildOf(), parent))
		c2 = fw.NewEntity()
		flecs.AddID(fw, c2, flecs.MakePair(w.ChildOf(), parent))
	})

	w.Delete(parent)

	if w.IsAlive(parent) {
		t.Error("parent should be dead")
	}
	if w.IsAlive(c1) || w.IsAlive(c2) {
		t.Error("cascade-deleted children should be dead")
	}
	// Verify parent no longer returns children (graceful no-op on dead entity).
	got := collectChildren(w, parent)
	if len(got) != 0 {
		t.Errorf("expected 0 children after parent delete, got %d", len(got))
	}
}

// TestOrderedChildren_DeferredAdd verifies that a deferred (ChildOf, parent) add
// inside a Write block appends the child to the ordered list at flush time.
func TestOrderedChildren_DeferredAdd(t *testing.T) {
	w := flecs.New()
	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
	})
	flecs.SetOrderedChildren(w, parent)

	w.Write(func(fw *flecs.Writer) {
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
	})

	got := collectChildren(w, parent)
	if len(got) != 1 || got[0] != child {
		t.Errorf("deferred add: want [child], got %v", got)
	}
}

// TestOrderedChildren_IdempotenceAndRoundTrip verifies that SetOrderedChildren
// called twice is a no-op, and IsOrderedChildren round-trips correctly.
func TestOrderedChildren_IdempotenceAndRoundTrip(t *testing.T) {
	w := flecs.New()
	var parent flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
	})

	flecs.SetOrderedChildren(w, parent)
	flecs.SetOrderedChildren(w, parent) // second call is a no-op

	w.Read(func(r *flecs.Reader) {
		if !flecs.IsOrderedChildren(r, parent) {
			t.Error("IsOrderedChildren should be true")
		}
	})
	w.Write(func(fw *flecs.Writer) {
		if !flecs.IsOrderedChildren(fw, parent) {
			t.Error("IsOrderedChildren should be true inside Write block")
		}
	})
}

// TestOrderedChildren_EachChildFromReadBlock verifies that EachChild honors
// ordering when called via a Reader scope.
func TestOrderedChildren_EachChildFromReadBlock(t *testing.T) {
	w := flecs.New()
	var parent, c1, c2, c3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.SetOrderedChildren(w, parent)
		c1 = fw.NewEntity()
		flecs.AddID(fw, c1, flecs.MakePair(w.ChildOf(), parent))
		c2 = fw.NewEntity()
		flecs.AddID(fw, c2, flecs.MakePair(w.ChildOf(), parent))
		c3 = fw.NewEntity()
		flecs.AddID(fw, c3, flecs.MakePair(w.ChildOf(), parent))
	})

	var got []flecs.ID
	w.Read(func(r *flecs.Reader) {
		r.EachChild(parent, func(child flecs.ID) bool {
			got = append(got, child)
			return true
		})
	})

	want := []flecs.ID{c1, c2, c3}
	if len(got) != len(want) {
		t.Fatalf("len: want %d, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d]: want %v, got %v", i, want[i], got[i])
		}
	}
}

// TestOrderedChildren_Stress adds 1000 children, verifies iteration order,
// removes every other child, verifies surviving 500 are in original order.
func TestOrderedChildren_Stress(t *testing.T) {
	const n = 1000
	w := flecs.New()
	var parent flecs.ID
	children := make([]flecs.ID, n)

	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.SetOrderedChildren(w, parent)
		for i := range children {
			children[i] = fw.NewEntity()
			flecs.AddID(fw, children[i], flecs.MakePair(w.ChildOf(), parent))
		}
	})

	// Verify full order.
	got := collectChildren(w, parent)
	if len(got) != n {
		t.Fatalf("want %d children, got %d", n, len(got))
	}
	for i, child := range children {
		if got[i] != child {
			t.Fatalf("[%d]: want %v, got %v", i, child, got[i])
		}
	}

	// Remove every other child (even indices).
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < n; i += 2 {
			flecs.RemoveID(fw, children[i], flecs.MakePair(w.ChildOf(), parent))
		}
	})

	got2 := collectChildren(w, parent)
	if len(got2) != n/2 {
		t.Fatalf("want %d surviving children, got %d", n/2, len(got2))
	}
	// Survivors are odd-indexed children in original order.
	for j, child := range got2 {
		expected := children[j*2+1]
		if child != expected {
			t.Errorf("[%d]: want %v, got %v", j, expected, child)
		}
	}
}

// TestOrderedChildren_SetAfterChildrenExist verifies snapshot semantics:
// create parent + 3 children first, then apply SetOrderedChildren.
// EachChild should return the 3 existing children in their archetype order.
func TestOrderedChildren_SetAfterChildrenExist(t *testing.T) {
	w := flecs.New()
	var parent, c1, c2, c3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		c1 = fw.NewEntity()
		flecs.AddID(fw, c1, flecs.MakePair(w.ChildOf(), parent))
		c2 = fw.NewEntity()
		flecs.AddID(fw, c2, flecs.MakePair(w.ChildOf(), parent))
		c3 = fw.NewEntity()
		flecs.AddID(fw, c3, flecs.MakePair(w.ChildOf(), parent))
	})

	// Children exist before trait is set.
	flecs.SetOrderedChildren(w, parent)

	got := collectChildren(w, parent)
	// Snapshot captures whatever archetype order they have; must include all three.
	if len(got) != 3 {
		t.Fatalf("want 3 children after snapshot, got %d", len(got))
	}
	// Verify all three are present (order is archetype-derived, not asserted here).
	seen := map[flecs.ID]bool{c1: false, c2: false, c3: false}
	for _, child := range got {
		if _, ok := seen[child]; !ok {
			t.Errorf("unexpected child %v in snapshot", child)
		}
		seen[child] = true
	}
	for child, found := range seen {
		if !found {
			t.Errorf("child %v missing from snapshot", child)
		}
	}
}

// TestOrderedChildren_IterationDuringMutation verifies snapshot-at-start
// semantics: adds or removes inside fn do not affect the current iteration but
// are visible on the next call.
func TestOrderedChildren_IterationDuringMutation(t *testing.T) {
	w := flecs.New()
	var parent, c1, c2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.SetOrderedChildren(w, parent)
		c1 = fw.NewEntity()
		flecs.AddID(fw, c1, flecs.MakePair(w.ChildOf(), parent))
		c2 = fw.NewEntity()
		flecs.AddID(fw, c2, flecs.MakePair(w.ChildOf(), parent))
	})

	// Add a new child inside the callback; it must NOT be seen in this iteration.
	var newChild flecs.ID
	var seenInFirstIter []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		fw.EachChild(parent, func(child flecs.ID) bool {
			seenInFirstIter = append(seenInFirstIter, child)
			if newChild == 0 {
				// Add a new child during iteration.
				newChild = fw.NewEntity()
				// Deferred — will be flushed after Write block ends.
				flecs.AddID(fw, newChild, flecs.MakePair(w.ChildOf(), parent))
			}
			return true
		})
	})
	if len(seenInFirstIter) != 2 {
		t.Errorf("first iter: want 2 seen, got %d", len(seenInFirstIter))
	}

	// After flush, newChild must appear in the next EachChild.
	got := collectChildren(w, parent)
	if len(got) != 3 {
		t.Errorf("next iter: want 3 children (including new), got %d", len(got))
	}
	found := false
	for _, ch := range got {
		if ch == newChild {
			found = true
		}
	}
	if !found {
		t.Error("newChild not visible after flush")
	}

	// Remove a child during iteration; it must still be visited in current pass.
	var seenInRemoveIter []flecs.ID
	var removedDuring flecs.ID
	w.Write(func(fw *flecs.Writer) {
		fw.EachChild(parent, func(child flecs.ID) bool {
			seenInRemoveIter = append(seenInRemoveIter, child)
			if removedDuring == 0 {
				removedDuring = child
				flecs.RemoveID(fw, child, flecs.MakePair(w.ChildOf(), parent))
			}
			return true
		})
	})
	// All 3 should have been visited (snapshot taken at entry).
	if len(seenInRemoveIter) != 3 {
		t.Errorf("remove-during iter: want 3 seen, got %d", len(seenInRemoveIter))
	}
	// After flush, the removed child is gone.
	got2 := collectChildren(w, parent)
	if len(got2) != 2 {
		t.Errorf("after remove-during: want 2 children, got %d", len(got2))
	}
	for _, ch := range got2 {
		if ch == removedDuring {
			t.Error("removed child still in list after flush")
		}
	}
}

// TestOrderedChildren_MarshalRoundTrip verifies that marshal/unmarshal preserves
// ordered-children state via policy-replay semantics.
func TestOrderedChildren_MarshalRoundTrip(t *testing.T) {
	w := flecs.New()
	var p, c1, c2, c3, g1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		flecs.SetOrderedChildren(w, p)
		c1 = fw.NewEntity()
		flecs.AddID(fw, c1, flecs.MakePair(w.ChildOf(), p))
		c2 = fw.NewEntity()
		flecs.AddID(fw, c2, flecs.MakePair(w.ChildOf(), p))
		c3 = fw.NewEntity()
		flecs.AddID(fw, c3, flecs.MakePair(w.ChildOf(), p))
		// Grandchild under c2, itself ordered.
		g1 = fw.NewEntity()
		flecs.SetOrderedChildren(w, c2)
		flecs.AddID(fw, g1, flecs.MakePair(w.ChildOf(), c2))
	})

	data, err := w.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if !json.Valid(data) {
		t.Fatal("invalid JSON")
	}

	w2 := flecs.New()
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	// Find the deserialized entities by their indices.
	pIdx := p.Index()
	c1Idx, c2Idx, c3Idx, g1Idx := c1.Index(), c2.Index(), c3.Index(), g1.Index()
	var p2, c12, c22, c32, g12 flecs.ID
	w2.EachEntity(func(e flecs.ID) bool {
		switch e.Index() {
		case pIdx:
			p2 = e
		case c1Idx:
			c12 = e
		case c2Idx:
			c22 = e
		case c3Idx:
			c32 = e
		case g1Idx:
			g12 = e
		}
		return true
	})
	if p2 == 0 || c12 == 0 || c22 == 0 || c32 == 0 || g12 == 0 {
		t.Fatal("could not find all deserialized entities")
	}

	// p2 should be ordered and return c12, c22, c32 in order.
	w2.Read(func(r *flecs.Reader) {
		if !flecs.IsOrderedChildren(r, p2) {
			t.Error("p2 should be ordered after unmarshal")
		}
		if !flecs.IsOrderedChildren(r, c22) {
			t.Error("c22 should be ordered after unmarshal")
		}
	})

	gotP := collectChildren(w2, p2)
	if len(gotP) != 3 {
		t.Fatalf("p2 children: want 3, got %d", len(gotP))
	}
	wantP := []flecs.ID{c12, c22, c32}
	for i := range wantP {
		if gotP[i] != wantP[i] {
			t.Errorf("p2 child[%d]: want %v, got %v", i, wantP[i], gotP[i])
		}
	}

	gotC2 := collectChildren(w2, c22)
	if len(gotC2) != 1 || gotC2[0] != g12 {
		t.Errorf("c22 children: want [g12], got %v", gotC2)
	}
}

// TestOrderedChildren_BareTagForm verifies the fw.AddID(parent, w.OrderedChildren())
// bare-tag form is equivalent to SetOrderedChildren.
func TestOrderedChildren_BareTagForm(t *testing.T) {
	w := flecs.New()
	var parent, c1, c2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.AddID(fw, parent, w.OrderedChildren())
		c1 = fw.NewEntity()
		flecs.AddID(fw, c1, flecs.MakePair(w.ChildOf(), parent))
		c2 = fw.NewEntity()
		flecs.AddID(fw, c2, flecs.MakePair(w.ChildOf(), parent))
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.IsOrderedChildren(r, parent) {
			t.Error("bare-tag form should mark parent as ordered")
		}
	})

	got := collectChildren(w, parent)
	want := []flecs.ID{c1, c2}
	if len(got) != len(want) {
		t.Fatalf("len: want %d, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d]: want %v, got %v", i, want[i], got[i])
		}
	}
}

// TestOrderedChildren_BuiltinIndex verifies OrderedChildren is at index 33
// and that the reindex of Wildcard/Any is correct.
func TestOrderedChildren_BuiltinIndex(t *testing.T) {
	w := flecs.New()
	if got := w.OrderedChildren().Index(); got != 33 {
		t.Errorf("OrderedChildren index: want 33, got %d", got)
	}
	if got := w.Wildcard().Index(); got != 34 {
		t.Errorf("Wildcard index: want 34, got %d", got)
	}
	if got := w.Any().Index(); got != 35 {
		t.Errorf("Any index: want 35, got %d", got)
	}
}

// TestOrderedChildren_DeferredBareTag verifies that a deferred bare-tag add
// (fw.AddID inside a Write block) applies the ordered-children policy at flush.
func TestOrderedChildren_DeferredBareTag(t *testing.T) {
	w := flecs.New()
	var parent, c1, c2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		c1 = fw.NewEntity()
		flecs.AddID(fw, c1, flecs.MakePair(w.ChildOf(), parent))
		c2 = fw.NewEntity()
		flecs.AddID(fw, c2, flecs.MakePair(w.ChildOf(), parent))
		// Deferred bare-tag OrderedChildren add.
		flecs.AddID(fw, parent, w.OrderedChildren())
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.IsOrderedChildren(r, parent) {
			t.Error("deferred bare-tag: IsOrderedChildren should be true after flush")
		}
	})
	// Children should be iterable (order captured at flush of parent's batch).
	got := collectChildren(w, parent)
	if len(got) != 2 {
		t.Errorf("deferred bare-tag: want 2 children, got %d", len(got))
	}
}
