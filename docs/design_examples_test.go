package docs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// TestDesign_EntityCreation verifies the entity-creation snippet from DesignWithFlecs.md.
func TestDesign_EntityCreation(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Health struct{ Current, Max int }

	w := flecs.New()
	flecs.RegisterComponent[Position](w)
	flecs.RegisterComponent[Health](w)

	var heroTemplate, hero flecs.ID
	w.Write(func(fw *flecs.Writer) {
		heroTemplate = fw.NewEntity()
		flecs.Set(fw, heroTemplate, Position{X: 0, Y: 0})
		flecs.Set(fw, heroTemplate, Health{Max: 100, Current: 100})

		hero = fw.NewEntity()
		fw.AddID(hero, flecs.MakePair(w.IsA(), heroTemplate))
	})

	w.Read(func(r *flecs.Reader) {
		h, ok := flecs.Get[Health](r, hero)
		if !ok {
			t.Fatal("hero should inherit Health from heroTemplate")
		}
		if h.Max != 100 {
			t.Errorf("Health.Max = %d, want 100", h.Max)
		}
	})
}

// TestDesign_EntityLifecycle verifies the IsAlive guard pattern from DesignWithFlecs.md.
func TestDesign_EntityLifecycle(t *testing.T) {
	w := flecs.New()

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	if !w.IsAlive(e) {
		t.Fatal("entity should be alive after creation")
	}

	w.Delete(e)
	if w.IsAlive(e) {
		t.Error("entity should not be alive after deletion")
	}

	// Guard pattern from the doc.
	called := false
	if w.IsAlive(e) {
		called = true
	}
	if called {
		t.Error("IsAlive guard should have prevented the branch")
	}
}

// TestDesign_EntityNames verifies the SetName + Lookup snippet from DesignWithFlecs.md.
func TestDesign_EntityNames(t *testing.T) {
	w := flecs.New()

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		w.SetName(e, "player")
	})

	found, ok := w.Lookup("player")
	if !ok {
		t.Fatal("Lookup(\"player\") should return the entity")
	}
	if found != e {
		t.Errorf("Lookup returned %d, want %d", found, e)
	}

	name, ok := w.GetName(e)
	if !ok || name != "player" {
		t.Errorf("GetName = (%q, %v), want (\"player\", true)", name, ok)
	}
}

// TestDesign_ComponentAtomic verifies that small atomic components compile and
// can be queried independently, as shown in DesignWithFlecs.md.
func TestDesign_ComponentAtomic(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Velocity struct{ DX, DY float32 }
	type Health struct{ Current, Max int }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)
	flecs.RegisterComponent[Health](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1, Y: 2})
		flecs.Set(fw, e, Velocity{DX: 3, DY: 4})
		flecs.Set(fw, e, Health{Current: 80, Max: 100})
	})

	// Query for Position+Velocity only; Health is not loaded.
	q := flecs.NewQuery(w, posID, velID)
	count := 0
	it := q.Iter()
	for it.Next() {
		count += len(it.Entities())
	}
	if count != 1 {
		t.Errorf("pos+vel query matched %d entities, want 1", count)
	}

	w.Read(func(r *flecs.Reader) {
		h, ok := flecs.Get[Health](r, e)
		if !ok {
			t.Fatal("Health should still be present")
		}
		if h.Current != 80 {
			t.Errorf("Health.Current = %d, want 80", h.Current)
		}
	})
}

// TestDesign_UncachedQuery verifies the NewQuery (uncached) snippet from DesignWithFlecs.md.
func TestDesign_UncachedQuery(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Velocity struct{ DX, DY float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 10})
		flecs.Set(fw, e, Velocity{DX: 1})
	})

	q := flecs.NewQuery(w, posID, velID)
	it := q.Iter()
	for it.Next() {
		pos := flecs.Field[Position](it, posID)
		vel := flecs.Field[Velocity](it, velID)
		for i := range it.Entities() {
			pos[i].X += vel[i].DX
			pos[i].Y += vel[i].DY
		}
	}
}

// TestDesign_CachedQuery verifies the NewCachedQuery snippet from DesignWithFlecs.md.
func TestDesign_CachedQuery(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Velocity struct{ DX, DY float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	w.Write(func(fw *flecs.Writer) {
		for range 5 {
			e := fw.NewEntity()
			flecs.Set(fw, e, Position{X: 1})
			flecs.Set(fw, e, Velocity{DX: 1})
		}
	})

	cq := flecs.NewCachedQuery(w, posID, velID)
	defer cq.Close()

	total := 0
	it := cq.Iter()
	for it.Next() {
		total += len(it.Entities())
	}
	if total != 5 {
		t.Errorf("cached query matched %d entities, want 5", total)
	}
}

// TestDesign_SystemScope verifies the single-responsibility system snippet from DesignWithFlecs.md.
func TestDesign_SystemScope(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Velocity struct{ DX, DY float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 0, Y: 0})
		flecs.Set(fw, e, Velocity{DX: 5, DY: 3})
	})

	moveQ := flecs.NewCachedQuery(w, posID, velID)
	flecs.NewSystem(w, moveQ, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			pos := flecs.Field[Position](it, posID)
			vel := flecs.Field[Velocity](it, velID)
			for i := range it.Entities() {
				pos[i].X += vel[i].DX * dt
				pos[i].Y += vel[i].DY * dt
			}
		}
	})

	w.Progress(1.0)

	w.Read(func(r *flecs.Reader) {
		p, ok := flecs.Get[Position](r, e)
		if !ok {
			t.Fatal("Position not found after Progress")
		}
		if p.X != 5 || p.Y != 3 {
			t.Errorf("Position = {%.1f, %.1f}, want {5.0, 3.0}", p.X, p.Y)
		}
	})
}

// TestDesign_PhaseOrder verifies that PreUpdate → OnUpdate → PostUpdate runs in
// canonical order, as shown in DesignWithFlecs.md.
func TestDesign_PhaseOrder(t *testing.T) {
	type Tag struct{}

	w := flecs.New()
	tagID := flecs.RegisterComponent[Tag](w)

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Tag{})
	})

	var order []string
	q := flecs.NewCachedQuery(w, tagID)

	flecs.NewSystemInPhase(w, w.PreUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "PreUpdate")
		}
	})
	flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "OnUpdate")
		}
	})
	flecs.NewSystemInPhase(w, w.PostUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "PostUpdate")
		}
	})

	w.Progress(0.016)

	want := []string{"PreUpdate", "OnUpdate", "PostUpdate"}
	if len(order) != len(want) {
		t.Fatalf("phase order: got %v, want %v", order, want)
	}
	for i, got := range order {
		if got != want[i] {
			t.Errorf("order[%d] = %q, want %q", i, got, want[i])
		}
	}
}

// TestDesign_Relationships verifies the tag-relationship snippet from DesignWithFlecs.md.
func TestDesign_Relationships(t *testing.T) {
	w := flecs.New()

	var likes, alice, bob, carol flecs.ID
	w.Write(func(fw *flecs.Writer) {
		likes = fw.NewEntity()
		alice = fw.NewEntity()
		bob = fw.NewEntity()
		carol = fw.NewEntity()

		// bob likes both alice and carol.
		fw.AddID(bob, flecs.MakePair(likes, alice))
		fw.AddID(bob, flecs.MakePair(likes, carol))
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, bob, flecs.MakePair(likes, alice)) {
			t.Error("bob should have (likes, alice)")
		}
		if !flecs.HasID(r, bob, flecs.MakePair(likes, carol)) {
			t.Error("bob should have (likes, carol)")
		}
		if flecs.HasID(r, alice, flecs.MakePair(likes, bob)) {
			t.Error("alice should not have (likes, bob) — not symmetric")
		}
	})
}

// TestDesign_PrefabVariant verifies that a prefab variant inherits from its base
// and that instances of the variant get the overridden values.
func TestDesign_PrefabVariant(t *testing.T) {
	type Speed struct{ Value float32 }

	w := flecs.New()
	flecs.RegisterComponent[Speed](w)

	var base, fast, instance flecs.ID
	w.Write(func(fw *flecs.Writer) {
		base = fw.NewEntity()
		flecs.Set(fw, base, Speed{Value: 10})

		fast = fw.NewEntity()
		fw.AddID(fast, flecs.MakePair(w.IsA(), base))
		flecs.Set(fw, fast, Speed{Value: 25}) // override

		instance = fw.NewEntity()
		fw.AddID(instance, flecs.MakePair(w.IsA(), fast))
	})

	w.Read(func(r *flecs.Reader) {
		s, ok := flecs.Get[Speed](r, instance)
		if !ok {
			t.Fatal("instance should inherit Speed from fast prefab")
		}
		if s.Value != 25 {
			t.Errorf("Speed.Value = %.0f, want 25 (from fast variant)", s.Value)
		}
	})
}
