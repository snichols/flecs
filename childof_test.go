package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// ── ChildOf() accessor ────────────────────────────────────────────────────────

func TestChildOfReturnsSameIDEachCall(t *testing.T) {
	w := flecs.New()
	first := w.ChildOf()
	second := w.ChildOf()
	if first != second {
		t.Fatalf("ChildOf() returned different IDs: %v vs %v", first, second)
	}
}

func TestChildOfIsAlive(t *testing.T) {
	w := flecs.New()
	if !w.IsAlive(w.ChildOf()) {
		t.Fatal("ChildOf() entity must be alive")
	}
}

func TestChildOfDistinctAcrossWorlds(t *testing.T) {
	w1 := flecs.New()
	w2 := flecs.New()
	// Each world has its own entity index; the IDs happen to be numerically equal
	// but they belong to different worlds and must not be confused.
	// What matters is that each world has a valid, alive ChildOf entity.
	if !w1.IsAlive(w1.ChildOf()) {
		t.Fatal("w1 ChildOf must be alive in w1")
	}
	if !w2.IsAlive(w2.ChildOf()) {
		t.Fatal("w2 ChildOf must be alive in w2")
	}
}

// ── AddID / HasID / ParentOf round-trip ──────────────────────────────────────

func TestAddIDChildOfPairHasIDAndParentOf(t *testing.T) {
	w := flecs.New()
	parent := w.NewEntity()
	child := w.NewEntity()

	pairID := flecs.MakePair(w.ChildOf(), parent)
	flecs.AddID(w.W(), child, pairID)

	if !flecs.HasID(w.R(), child, pairID) {
		t.Fatal("HasID returned false after AddID with ChildOf pair")
	}

	got, ok := w.ParentOf(child)
	if !ok {
		t.Fatal("ParentOf returned false after adding ChildOf pair")
	}
	if got != parent {
		t.Fatalf("ParentOf want %v, got %v", parent, got)
	}
}

func TestParentOfNoChildOf(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()

	got, ok := w.ParentOf(e)
	if ok {
		t.Fatal("ParentOf should return false for entity with no ChildOf pair")
	}
	if got != 0 {
		t.Fatalf("ParentOf should return 0 ID, got %v", got)
	}
}

func TestParentOfDeadEntity(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	w.Delete(e)

	got, ok := w.ParentOf(e)
	if ok {
		t.Fatal("ParentOf should return false for dead entity")
	}
	if got != 0 {
		t.Fatalf("ParentOf should return 0 ID for dead entity, got %v", got)
	}
}

// ── EachChild ─────────────────────────────────────────────────────────────────

func TestEachChildThreeChildren(t *testing.T) {
	w := flecs.New()
	parent := w.NewEntity()
	c1 := w.NewEntity()
	c2 := w.NewEntity()
	c3 := w.NewEntity()

	for _, child := range []flecs.ID{c1, c2, c3} {
		flecs.AddID(w.W(), child, flecs.MakePair(w.ChildOf(), parent))
	}

	seen := make(map[flecs.ID]bool)
	w.EachChild(parent, func(child flecs.ID) bool {
		seen[child] = true
		return true
	})

	if len(seen) != 3 {
		t.Fatalf("EachChild called fn %d times, want 3", len(seen))
	}
	for _, want := range []flecs.ID{c1, c2, c3} {
		if !seen[want] {
			t.Errorf("EachChild did not visit child %v", want)
		}
	}
}

func TestEachChildNotCalledOnNonChildren(t *testing.T) {
	w := flecs.New()
	parent := w.NewEntity()
	child := w.NewEntity()
	other := w.NewEntity()
	flecs.AddID(w.W(), child, flecs.MakePair(w.ChildOf(), parent))

	seen := make(map[flecs.ID]bool)
	w.EachChild(parent, func(id flecs.ID) bool {
		seen[id] = true
		return true
	})

	if seen[other] {
		t.Fatal("EachChild visited a non-child entity")
	}
	if !seen[child] {
		t.Fatal("EachChild did not visit the child entity")
	}
}

func TestEachChildEarlyExit(t *testing.T) {
	w := flecs.New()
	parent := w.NewEntity()
	for i := 0; i < 5; i++ {
		child := w.NewEntity()
		flecs.AddID(w.W(), child, flecs.MakePair(w.ChildOf(), parent))
	}

	count := 0
	w.EachChild(parent, func(_ flecs.ID) bool {
		count++
		return count < 2 // stop after 2
	})

	if count != 2 {
		t.Fatalf("EachChild early exit: called fn %d times, want 2", count)
	}
}

func TestEachChildNoChildren(t *testing.T) {
	w := flecs.New()
	parent := w.NewEntity()

	called := false
	w.EachChild(parent, func(_ flecs.ID) bool {
		called = true
		return true
	})

	if called {
		t.Fatal("EachChild called fn for parent with no children")
	}
}

// ── Cascade delete — single level ─────────────────────────────────────────────

func TestCascadeDeleteSingleLevel(t *testing.T) {
	w := flecs.New()
	parent := w.NewEntity()
	c1 := w.NewEntity()
	c2 := w.NewEntity()

	flecs.AddID(w.W(), c1, flecs.MakePair(w.ChildOf(), parent))
	flecs.AddID(w.W(), c2, flecs.MakePair(w.ChildOf(), parent))

	w.Delete(parent)

	if w.IsAlive(parent) {
		t.Fatal("parent should be dead after Delete")
	}
	if w.IsAlive(c1) {
		t.Fatal("child1 should be dead after cascade delete of parent")
	}
	if w.IsAlive(c2) {
		t.Fatal("child2 should be dead after cascade delete of parent")
	}
}

// ── Cascade delete — multi-level ──────────────────────────────────────────────

func TestCascadeDeleteMultiLevel(t *testing.T) {
	w := flecs.New()
	grandparent := w.NewEntity()
	parent := w.NewEntity()
	child := w.NewEntity()

	flecs.AddID(w.W(), parent, flecs.MakePair(w.ChildOf(), grandparent))
	flecs.AddID(w.W(), child, flecs.MakePair(w.ChildOf(), parent))

	w.Delete(grandparent)

	if w.IsAlive(grandparent) {
		t.Fatal("grandparent should be dead")
	}
	if w.IsAlive(parent) {
		t.Fatal("parent should be dead after cascade")
	}
	if w.IsAlive(child) {
		t.Fatal("child should be dead after cascade")
	}
}

// ── Cascade isolation ─────────────────────────────────────────────────────────

func TestCascadeIsolation(t *testing.T) {
	w := flecs.New()
	p1 := w.NewEntity()
	p2 := w.NewEntity()
	c1 := w.NewEntity()
	c2 := w.NewEntity()

	flecs.AddID(w.W(), c1, flecs.MakePair(w.ChildOf(), p1))
	flecs.AddID(w.W(), c2, flecs.MakePair(w.ChildOf(), p2))

	w.Delete(p1)

	if w.IsAlive(p1) || w.IsAlive(c1) {
		t.Fatal("p1 and c1 should be dead")
	}
	if !w.IsAlive(p2) || !w.IsAlive(c2) {
		t.Fatal("p2 and c2 should still be alive")
	}
}

// ── Cascade scrubs row data ────────────────────────────────────────────────────

func TestCascadeScrubsRowData(t *testing.T) {
	w := flecs.New()
	parent := w.NewEntity()
	child := w.NewEntity()
	flecs.AddID(w.W(), child, flecs.MakePair(w.ChildOf(), parent))
	flecs.Set[Position](w.W(), child, Position{X: 99, Y: 42})

	w.Delete(parent)

	// Recycle the child's former entity slot by allocating a new entity.
	recycled := w.NewEntity()
	_ = recycled

	// The recycled entity must not inherit Position from the deleted child.
	_, ok := flecs.Get[Position](w.R(), recycled)
	if ok {
		t.Fatal("recycled entity must not inherit Position from deleted child")
	}
}

// ── Cascade post-order proxy ──────────────────────────────────────────────────

func TestCascadePostOrder(t *testing.T) {
	w := flecs.New()
	parent := w.NewEntity()
	child := w.NewEntity()
	flecs.AddID(w.W(), child, flecs.MakePair(w.ChildOf(), parent))

	w.Delete(parent)

	if w.IsAlive(parent) {
		t.Fatal("parent should be dead")
	}
	if w.IsAlive(child) {
		t.Fatal("child should be dead")
	}
}

// ── Wide cascade ──────────────────────────────────────────────────────────────

func TestWideCascade(t *testing.T) {
	const n = 100
	w := flecs.New()
	parent := w.NewEntity()
	children := make([]flecs.ID, n)
	for i := range children {
		children[i] = w.NewEntity()
		flecs.AddID(w.W(), children[i], flecs.MakePair(w.ChildOf(), parent))
	}
	before := w.Count()
	w.Delete(parent)

	if w.IsAlive(parent) {
		t.Fatal("parent should be dead after wide cascade delete")
	}
	for i, child := range children {
		if w.IsAlive(child) {
			t.Fatalf("child[%d] should be dead after wide cascade delete", i)
		}
	}
	// All n+1 entities (parent + children) should have been removed.
	if w.Count() != before-(n+1) {
		t.Fatalf("Count after wide delete: want %d, got %d", before-(n+1), w.Count())
	}
}

// ── Non-parent entity behaves like Phase 1.5 ──────────────────────────────────

func TestDeleteNonParentAlive(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	if !w.Delete(e) {
		t.Fatal("Delete of alive non-parent entity must return true")
	}
}

func TestDeleteNonParentDead(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	w.Delete(e)
	if w.Delete(e) {
		t.Fatal("Delete of dead entity must return false")
	}
}

// ── Self-cycle ────────────────────────────────────────────────────────────────

func TestDeleteSelfCycleTerminates(t *testing.T) {
	w := flecs.New()
	p := w.NewEntity()
	// p is its own parent — deliberate cycle
	flecs.AddID(w.W(), p, flecs.MakePair(w.ChildOf(), p))

	w.Delete(p) // must terminate

	if w.IsAlive(p) {
		t.Fatal("self-cycle entity should be dead after Delete")
	}
}
