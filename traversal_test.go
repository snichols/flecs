package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// tPos is a position component used across traversal tests.
type tPos struct{ X, Y float32 }

// TestGetUp_SingleLevel verifies that GetUp returns the parent's component
// when the child has none but the parent does.
func TestGetUp_SingleLevel(t *testing.T) {
	w := flecs.New()
	parent := w.NewEntity()
	child := w.NewEntity()
	flecs.Set(w, parent, tPos{X: 1, Y: 2})
	flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), parent))

	pos, ok := flecs.GetUp[tPos](w, child, w.ChildOf())
	if !ok {
		t.Fatal("expected ok=true, got false")
	}
	if pos.X != 1 || pos.Y != 2 {
		t.Fatalf("got %+v, want {1 2}", pos)
	}
}

// TestGetUp_MultiLevel verifies that GetUp walks multiple hops to find a component.
func TestGetUp_MultiLevel(t *testing.T) {
	w := flecs.New()
	grandparent := w.NewEntity()
	parent := w.NewEntity()
	child := w.NewEntity()
	flecs.Set(w, grandparent, tPos{X: 10, Y: 20})
	flecs.AddID(w, parent, flecs.MakePair(w.ChildOf(), grandparent))
	flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), parent))

	pos, ok := flecs.GetUp[tPos](w, child, w.ChildOf())
	if !ok {
		t.Fatal("expected ok=true, got false")
	}
	if pos.X != 10 || pos.Y != 20 {
		t.Fatalf("got %+v, want {10 20}", pos)
	}
}

// TestGetUp_SelfFirst verifies that GetUp returns the entity's own component
// rather than a parent's when the entity locally owns the component.
func TestGetUp_SelfFirst(t *testing.T) {
	w := flecs.New()
	parent := w.NewEntity()
	child := w.NewEntity()
	flecs.Set(w, parent, tPos{X: 99, Y: 99})
	flecs.Set(w, child, tPos{X: 1, Y: 2})
	flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), parent))

	pos, ok := flecs.GetUp[tPos](w, child, w.ChildOf())
	if !ok {
		t.Fatal("expected ok=true, got false")
	}
	if pos.X != 1 || pos.Y != 2 {
		t.Fatalf("self-first failed: got %+v, want {1 2}", pos)
	}
}

// TestGetUp_NoRelationship verifies that GetUp returns false when the entity
// has no relationship pair.
func TestGetUp_NoRelationship(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.RegisterComponent[tPos](w)

	_, ok := flecs.GetUp[tPos](w, e, w.ChildOf())
	if ok {
		t.Fatal("expected ok=false for entity with no ChildOf pair")
	}
}

// TestGetUp_NoneInChain verifies that GetUp returns false when no entity in
// the chain owns the component.
func TestGetUp_NoneInChain(t *testing.T) {
	w := flecs.New()
	parent := w.NewEntity()
	child := w.NewEntity()
	flecs.RegisterComponent[tPos](w)
	flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), parent))

	_, ok := flecs.GetUp[tPos](w, child, w.ChildOf())
	if ok {
		t.Fatal("expected ok=false when no entity owns tPos")
	}
}

// TestGetUp_UnregisteredComponent verifies that GetUp returns false when the
// component type has never been registered.
func TestGetUp_UnregisteredComponent(t *testing.T) {
	type neverRegistered struct{ V int }
	w := flecs.New()
	e := w.NewEntity()

	_, ok := flecs.GetUp[neverRegistered](w, e, w.ChildOf())
	if ok {
		t.Fatal("expected ok=false for unregistered component type")
	}
}

// TestHasUp_Basic verifies HasUp returns true when a parent owns the component
// and false when there is no ChildOf relationship.
func TestHasUp_Basic(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[tPos](w)

	parent := w.NewEntity()
	child := w.NewEntity()
	flecs.Set(w, parent, tPos{X: 5})
	flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), parent))

	if !flecs.HasUp(w, child, posID, w.ChildOf()) {
		t.Fatal("expected HasUp=true, got false")
	}

	// Entity with no ChildOf and no component.
	lone := w.NewEntity()
	if flecs.HasUp(w, lone, posID, w.ChildOf()) {
		t.Fatal("expected HasUp=false for entity with no ChildOf pair")
	}
}

// TestHasUp_DeadEntity verifies HasUp returns false for a dead entity.
func TestHasUp_DeadEntity(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[tPos](w)

	e := w.NewEntity()
	w.Delete(e)

	if flecs.HasUp(w, e, posID, w.ChildOf()) {
		t.Fatal("expected HasUp=false for dead entity")
	}
}

// TestTargetUp_Basic verifies TargetUp returns the first entity in the chain
// that locally owns the component.
func TestTargetUp_Basic(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[tPos](w)

	grandparent := w.NewEntity()
	parent := w.NewEntity()
	child := w.NewEntity()
	flecs.Set(w, grandparent, tPos{X: 7})
	flecs.AddID(w, parent, flecs.MakePair(w.ChildOf(), grandparent))
	flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), parent))

	owner, ok := flecs.TargetUp(w, child, posID, w.ChildOf())
	if !ok {
		t.Fatal("expected ok=true, got false")
	}
	if owner != grandparent {
		t.Fatalf("expected owner=%v, got %v", grandparent, owner)
	}
}

// TestTargetUp_Self verifies TargetUp returns the entity itself when it owns
// the component locally.
func TestTargetUp_Self(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[tPos](w)

	parent := w.NewEntity()
	child := w.NewEntity()
	flecs.Set(w, child, tPos{X: 3})
	flecs.Set(w, parent, tPos{X: 99})
	flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), parent))

	owner, ok := flecs.TargetUp(w, child, posID, w.ChildOf())
	if !ok {
		t.Fatal("expected ok=true, got false")
	}
	if owner != child {
		t.Fatalf("self-first failed: expected owner=%v, got %v", child, owner)
	}
}

// TestGetUp_CycleSelfLoop verifies that an entity pointing to itself via
// the relationship terminates cleanly.
func TestGetUp_CycleSelfLoop(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[tPos](w)

	e := w.NewEntity()
	// e → (ChildOf, e): self-loop; no Position.
	flecs.AddID(w, e, flecs.MakePair(w.ChildOf(), e))

	if flecs.HasUp(w, e, posID, w.ChildOf()) {
		t.Fatal("expected false for self-loop with no component")
	}

	// Now give e its own Position — self-first must return true before cycling.
	flecs.Set(w, e, tPos{X: 1})
	if !flecs.HasUp(w, e, posID, w.ChildOf()) {
		t.Fatal("expected true when self has the component (self-first)")
	}
}

// TestGetUp_CycleTwoEntities verifies that a two-entity cycle terminates cleanly.
func TestGetUp_CycleTwoEntities(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[tPos](w)

	// Use a custom relationship to avoid ChildOf cascade-delete semantics.
	relE := w.NewEntity()
	a := w.NewEntity()
	b := w.NewEntity()
	// A → (rel, B), B → (rel, A): cycle; neither has Position.
	flecs.AddID(w, a, flecs.MakePair(relE, b))
	flecs.AddID(w, b, flecs.MakePair(relE, a))

	if flecs.HasUp(w, a, posID, relE) {
		t.Fatal("expected false for two-entity cycle with no component")
	}
	if flecs.HasUp(w, b, posID, relE) {
		t.Fatal("expected false for two-entity cycle with no component")
	}
}

// TestGetUp_DeadParent verifies that GetUp returns false when the target
// entity in the chain is dead (dead-target guard).
func TestGetUp_DeadParent(t *testing.T) {
	w := flecs.New()

	// Use a custom relationship to avoid ChildOf cascade-delete semantics.
	// When we delete the parent, the child survives but its pair target is dead.
	relE := w.NewEntity()
	parent := w.NewEntity()
	child := w.NewEntity()
	flecs.Set(w, parent, tPos{X: 42})
	flecs.AddID(w, child, flecs.MakePair(relE, parent))

	// Verify GetUp works before deletion.
	posID := flecs.RegisterComponent[tPos](w)
	if !flecs.HasUp(w, child, posID, relE) {
		t.Fatal("precondition failed: expected HasUp=true before parent deleted")
	}

	// Delete only the parent (no cascade for custom relationship).
	w.Delete(parent)

	// Child is still alive; its pair-ID (rel, dead_parent) lingers.
	if !w.IsAlive(child) {
		t.Fatal("precondition: child should still be alive after deleting parent via custom rel")
	}

	_, ok := flecs.GetUp[tPos](w, child, relE)
	if ok {
		t.Fatal("expected GetUp=false when target is dead")
	}
	if flecs.HasUp(w, child, posID, relE) {
		t.Fatal("expected HasUp=false when target is dead")
	}
}

// TestGetUp_DepthLimit verifies that a chain deeper than maxTraversalDepth
// returns (zero, false) even when a deeper ancestor has the component.
func TestGetUp_DepthLimit(t *testing.T) {
	const chainLen = 100
	w := flecs.New()

	// Build chain: entities[0] → entities[1] → ... → entities[chainLen-1].
	entities := make([]flecs.ID, chainLen)
	for i := range chainLen {
		entities[i] = w.NewEntity()
	}
	// Connect each entity to the next via a custom relationship (no cascade).
	relE := w.NewEntity()
	for i := 0; i < chainLen-1; i++ {
		flecs.AddID(w, entities[i], flecs.MakePair(relE, entities[i+1]))
	}
	// Only the deepest ancestor has tPos.
	flecs.Set(w, entities[chainLen-1], tPos{X: 99})

	// The chain is 100 long but maxTraversalDepth=64, so we should not reach
	// entities[64] and beyond.
	_, ok := flecs.GetUp[tPos](w, entities[0], relE)
	if ok {
		t.Fatal("expected GetUp=false for chain deeper than maxTraversalDepth")
	}

	// Sanity check: placing component within depth range is found.
	flecs.Set(w, entities[63], tPos{X: 7})
	pos, ok := flecs.GetUp[tPos](w, entities[0], relE)
	if !ok {
		t.Fatal("expected GetUp=true when component is within depth limit")
	}
	if pos.X != 7 {
		t.Fatalf("got %v, want 7", pos.X)
	}
}

// TestGetUp_ViaIsA verifies that GetUp works with w.IsA() as the relationship,
// walking prefab chains.
func TestGetUp_ViaIsA(t *testing.T) {
	w := flecs.New()
	prefab := w.NewEntity()
	child := w.NewEntity()
	flecs.Set(w, prefab, tPos{X: 5, Y: 6})
	flecs.AddID(w, child, flecs.MakePair(w.IsA(), prefab))

	// GetUp with IsA finds the prefab's local tPos (which prefab owns directly).
	pos, ok := flecs.GetUp[tPos](w, child, w.IsA())
	if !ok {
		t.Fatal("expected ok=true walking via IsA")
	}
	if pos.X != 5 || pos.Y != 6 {
		t.Fatalf("got %+v, want {5 6}", pos)
	}
}

// TestGetUp_CustomRelationship verifies that GetUp works with an arbitrary
// user-defined relationship entity.
func TestGetUp_CustomRelationship(t *testing.T) {
	w := flecs.New()
	myRel := w.NewEntity() // user-defined relationship
	source := w.NewEntity()
	dest := w.NewEntity()
	flecs.Set(w, dest, tPos{X: 3, Y: 4})
	flecs.AddID(w, source, flecs.MakePair(myRel, dest))

	pos, ok := flecs.GetUp[tPos](w, source, myRel)
	if !ok {
		t.Fatal("expected ok=true for custom relationship")
	}
	if pos.X != 3 || pos.Y != 4 {
		t.Fatalf("got %+v, want {3 4}", pos)
	}
}

// TestGetUp_ZeroAllocSelfHit verifies that GetUp allocates nothing when the
// component is found on the entity itself (depth-0 fast path).
func TestGetUp_ZeroAllocSelfHit(t *testing.T) {
	w := flecs.New()
	parent := w.NewEntity()
	child := w.NewEntity()
	flecs.Set(w, child, tPos{X: 1, Y: 2})
	flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), parent))

	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, _ = flecs.GetUp[tPos](w, child, w.ChildOf())
		}
	})
	if result.AllocsPerOp() != 0 {
		t.Fatalf("expected 0 allocs/op for self-hit, got %d", result.AllocsPerOp())
	}
}

// TestHasUp_SelfOwns verifies HasUp returns true when the entity itself owns
// the component.
func TestHasUp_SelfOwns(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[tPos](w)

	e := w.NewEntity()
	flecs.Set(w, e, tPos{X: 1})

	if !flecs.HasUp(w, e, posID, w.ChildOf()) {
		t.Fatal("expected HasUp=true when entity itself owns the component")
	}
}

// TestTargetUp_NotFound verifies TargetUp returns (0, false) when no entity
// in the chain owns the component.
func TestTargetUp_NotFound(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[tPos](w)

	parent := w.NewEntity()
	child := w.NewEntity()
	flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), parent))

	owner, ok := flecs.TargetUp(w, child, posID, w.ChildOf())
	if ok {
		t.Fatal("expected ok=false, got true")
	}
	if owner != 0 {
		t.Fatalf("expected owner=0, got %v", owner)
	}
}

// TestGetUp_ZeroSizeComponent verifies GetUp handles zero-size component types
// (tags used as components) by returning (zero, true) when the entity owns one.
func TestGetUp_ZeroSizeComponent(t *testing.T) {
	type tag struct{}
	w := flecs.New()
	parent := w.NewEntity()
	child := w.NewEntity()
	flecs.Set(w, parent, tag{})
	flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), parent))

	_, ok := flecs.GetUp[tag](w, child, w.ChildOf())
	if !ok {
		t.Fatal("expected ok=true for zero-size component on parent")
	}
}

// TestGetUp_ExistingTestsStillPass is a regression guard that runs a basic
// world operation to confirm traversal.go does not break existing behavior.
func TestGetUp_ExistingTestsStillPass(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w, e, tPos{X: 9, Y: 8})
	pos, ok := flecs.Get[tPos](w, e)
	if !ok || pos.X != 9 {
		t.Fatalf("basic Get[T] broken: got %+v ok=%v", pos, ok)
	}
}
