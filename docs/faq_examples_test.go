package docs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// TestFAQ_QueryBuildOnce verifies the "build queries once" pattern from FAQ.md.
func TestFAQ_QueryBuildOnce(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Velocity struct{ DX, DY float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 0})
		flecs.Set(fw, e, Velocity{DX: 1})
	})

	// GOOD: build the cached query once, reuse it every frame.
	q := flecs.NewCachedQuery(w, posID, velID)
	moved := 0
	flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			positions := flecs.Field[Position](it, posID)
			velocities := flecs.Field[Velocity](it, velID)
			for i := range positions {
				positions[i].X += velocities[i].DX * dt
				moved++
			}
		}
	})

	w.Progress(1.0)
	if moved == 0 {
		t.Error("system should have iterated at least one entity")
	}

	w.Read(func(r *flecs.Reader) {
		p, ok := flecs.Get[Position](r, e)
		if !ok {
			t.Fatal("Position should still be present")
		}
		if p.X == 0 {
			t.Error("Position.X should have changed after progress")
		}
	})
}

// TestFAQ_EntityIDRecycling verifies the entity ID recycling behaviour from FAQ.md.
func TestFAQ_EntityIDRecycling(t *testing.T) {
	w := flecs.New()

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity() // small id
	})

	if !w.IsAlive(e1) {
		t.Fatal("e1 should be alive")
	}

	w.Delete(e1) // slot available for recycling

	if w.IsAlive(e1) {
		t.Error("e1 should be dead after deletion")
	}

	w.Write(func(fw *flecs.Writer) {
		e2 = fw.NewEntity() // reuses e1's slot; generation counter incremented
	})

	// e1 and e2 share the same lower-32-bit slot but differ in generation.
	if e1 == e2 {
		t.Error("e1 and e2 should differ in generation bits after recycling")
	}

	w.Read(func(r *flecs.Reader) {
		if r.IsAlive(e1) {
			t.Error("stale e1 should be dead")
		}
		if !r.IsAlive(e2) {
			t.Error("fresh e2 should be alive")
		}
	})
}

// TestFAQ_AddIDVsSet verifies AddID (tag) vs Set (valued component) semantics from FAQ.md.
func TestFAQ_AddIDVsSet(t *testing.T) {
	type Poisoned struct{} // tag — no data
	type Position struct{ X, Y float32 }

	w := flecs.New()
	poisonedID := flecs.RegisterComponent[Poisoned](w)
	flecs.RegisterComponent[Position](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		fw.AddID(e, poisonedID)          // tag: AddID is correct
		flecs.Set(fw, e, Position{1, 2}) // data: Set writes the value
	})

	w.Read(func(r *flecs.Reader) {
		if !r.HasID(e, poisonedID) {
			t.Error("entity should have Poisoned tag")
		}
		p, ok := flecs.Get[Position](r, e)
		if !ok {
			t.Fatal("entity should have Position component")
		}
		if p.X != 1 || p.Y != 2 {
			t.Errorf("Position = {%v, %v}, want {1, 2}", p.X, p.Y)
		}
	})
}

// TestFAQ_LookupFullPath verifies that Lookup requires the full dot-separated path from FAQ.md.
func TestFAQ_LookupFullPath(t *testing.T) {
	w := flecs.New()

	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		fw.SetName(parent, "Galaxy")
		child = fw.NewEntity()
		fw.SetName(child, "Sol")
		fw.AddID(child, flecs.MakePair(w.ChildOf(), parent))
	})

	w.Read(func(r *flecs.Reader) {
		// bare name lookup should not find non-root child
		_, rootOK := r.Lookup("Sol")
		if rootOK {
			t.Error("bare 'Sol' lookup should fail for non-root entity")
		}

		// full path lookup should succeed
		id, pathOK := r.Lookup("Galaxy.Sol")
		if !pathOK {
			t.Error("'Galaxy.Sol' path lookup should succeed")
		}
		if id != child {
			t.Errorf("lookup returned %v, want %v", id, child)
		}

		// LookupChild without full path also works
		id2, childOK := r.LookupChild(parent, "Sol")
		if !childOK {
			t.Error("LookupChild(parent, 'Sol') should succeed")
		}
		if id2 != child {
			t.Errorf("LookupChild returned %v, want %v", id2, child)
		}
	})
}

// TestFAQ_DeferredMutationsInSystem verifies that component mutations inside a
// system are deferred and applied after the system body, as described in FAQ.md.
func TestFAQ_DeferredMutationsInSystem(t *testing.T) {
	type Position struct{ X float32 }
	type Velocity struct{ DX float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 0})
		flecs.Set(fw, e, Velocity{DX: 5})
	})

	q := flecs.NewCachedQuery(w, posID, velID)
	flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		fw := it.Writer()
		for it.Next() {
			velocities := flecs.Field[Velocity](it, velID)
			for i, ent := range it.Entities() {
				// Deferred: sets Position.X to Velocity.DX * dt
				flecs.Set(fw, ent, Position{X: velocities[i].DX * dt})
			}
		}
	})

	w.Progress(2.0) // dt = 2.0 → Position.X = 5 * 2 = 10

	w.Read(func(r *flecs.Reader) {
		p, ok := flecs.Get[Position](r, e)
		if !ok {
			t.Fatal("Position should be present after progress")
		}
		const want = float32(10)
		if p.X != want {
			t.Errorf("Position.X = %v, want %v", p.X, want)
		}
	})
}

// TestFAQ_ChangeDetectionObserver verifies the OnSet observer pattern from FAQ.md.
func TestFAQ_ChangeDetectionObserver(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()
	flecs.RegisterComponent[Position](w)

	changes := 0
	flecs.Observe[Position](w, flecs.EventOnSet, func(_ *flecs.Writer, _ flecs.ID, _ Position) {
		changes++
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1, Y: 2}) // fires OnSet → changes = 1
	})

	if changes != 1 {
		t.Errorf("changes after first Set = %d, want 1", changes)
	}

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, Position{X: 3, Y: 4}) // fires OnSet again → changes = 2
	})

	if changes != 2 {
		t.Errorf("changes after second Set = %d, want 2", changes)
	}
}
