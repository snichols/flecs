package docs_test

import (
	"fmt"
	"testing"

	"github.com/snichols/flecs"
)

// TestQS_World verifies the World creation snippet from Quickstart.md.
func TestQS_World(t *testing.T) {
	w := flecs.New()
	_ = w
}

// TestQS_Entities verifies the Entities snippet from Quickstart.md.
func TestQS_Entities(t *testing.T) {
	w := flecs.New()

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})

	if !w.IsAlive(e) {
		t.Error("entity should be alive after creation")
	}

	w.Delete(e)
	if w.IsAlive(e) {
		t.Error("entity should be dead after deletion")
	}
}

// TestQS_Components verifies the Components snippet from Quickstart.md.
func TestQS_Components(t *testing.T) {
	type Health struct{ HP int }

	w := flecs.New()
	healthID := flecs.RegisterComponent[Health](w)
	_ = healthID

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Health{HP: 100})
	})

	w.Read(func(r *flecs.Reader) {
		h, ok := flecs.Get[Health](r, e)
		if !ok {
			t.Error("expected Health to be present")
			return
		}
		if h.HP != 100 {
			t.Errorf("expected HP=100, got %d", h.HP)
		}
		if !flecs.Has[Health](r, e) {
			t.Error("Has[Health] should be true")
		}
		if !flecs.Owns[Health](r, e) {
			t.Error("Owns[Health] should be true")
		}
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[Health](fw, e)
	})

	w.Read(func(r *flecs.Reader) {
		if flecs.Has[Health](r, e) {
			t.Error("Has[Health] should be false after Remove")
		}
	})
}

// TestQS_NamedEntities verifies the Named Entities snippet from Quickstart.md.
func TestQS_NamedEntities(t *testing.T) {
	w := flecs.New()

	var scene, player flecs.ID
	w.Write(func(fw *flecs.Writer) {
		scene = fw.NewEntity()
		player = fw.NewEntity()
		w.SetName(scene, "scene")
		w.SetName(player, "player")
		flecs.AddID(fw, player, flecs.MakePair(w.ChildOf(), scene))
	})

	if path := w.PathOf(player); path != "scene.player" {
		t.Errorf("expected path 'scene.player', got %q", path)
	}

	found, ok := w.Lookup("scene.player")
	if !ok || found != player {
		t.Error("Lookup(scene.player) should return player entity")
	}

	name, ok := w.GetName(player)
	if !ok || name != "player" {
		t.Errorf("GetName should return 'player', got %q", name)
	}
}

// TestQS_Tags verifies the Tags snippets from Quickstart.md.
func TestQS_Tags(t *testing.T) {
	type Enemy struct{}

	w := flecs.New()

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Enemy{})
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.Has[Enemy](r, e) {
			t.Error("Has[Enemy] should be true")
		}
	})

	// Dynamic runtime tag.
	var tagEnemy, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tagEnemy = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.AddID(fw, e2, tagEnemy)
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, e2, tagEnemy) {
			t.Error("HasID should be true for dynamic tag")
		}
	})
}

// TestQS_ErgoIteration verifies the Ergonomic Iteration snippet from Quickstart.md.
func TestQS_ErgoIteration(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Velocity struct{ DX, DY float32 }

	w := flecs.New()

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1, Y: 2})
		flecs.Set(fw, e, Velocity{DX: 0.5, DY: 1})
	})

	w.Read(func(r *flecs.Reader) {
		flecs.Each2[Position, Velocity](r, func(_ flecs.ID, p *Position, v *Velocity) {
			p.X += v.DX
			p.Y += v.DY
		})
	})

	w.Read(func(r *flecs.Reader) {
		if pos, ok := flecs.Get[Position](r, e); !ok || pos.X != 1.5 {
			t.Errorf("expected X=1.5 after Each2 update, got %v (ok=%v)", pos, ok)
		}
	})

	w.Read(func(r *flecs.Reader) {
		flecs.Each1[Position](r, func(eid flecs.ID, p *Position) {
			_ = fmt.Sprintf("entity %d pos: %.1f %.1f", eid, p.X, p.Y)
		})
	})
}

// TestQS_Queries verifies the Queries snippet from Quickstart.md.
func TestQS_Queries(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Velocity struct{ DX, DY float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 10, Y: 20})
		flecs.Set(fw, e, Velocity{DX: 1})
	})

	// Simple AND query.
	q := flecs.NewQuery(w, posID)
	it := q.Iter()
	count := 0
	for it.Next() {
		positions := flecs.Field[Position](it, posID)
		for _, p := range positions {
			_ = fmt.Sprintf("%.1f %.1f", p.X, p.Y)
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 entity from simple query, got %d", count)
	}

	// NOT and Optional terms.
	var deadID flecs.ID
	w.Write(func(fw *flecs.Writer) { deadID = fw.NewEntity() })

	qAlive := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Without(deadID),
	)

	qMaybe := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Maybe(velID),
	)
	qMaybe.Each(func(it *flecs.QueryIter) {
		for it.Next() {
			positions := flecs.Field[Position](it, posID)
			velocities, hasVel := flecs.FieldMaybe[Velocity](it, velID)
			for i := range positions {
				if hasVel {
					positions[i].X += velocities[i].DX
				}
			}
		}
	})
	_ = qAlive

	// Cached query.
	cq := flecs.NewCachedQuery(w, posID)
	if cq.EntityCount() < 1 {
		t.Error("cached query should find at least one entity")
	}
	defer cq.Close()
}

// TestQS_Relationships verifies the Relationships snippet from Quickstart.md.
func TestQS_Relationships(t *testing.T) {
	type Distance struct{ Meters float32 }

	w := flecs.New()

	// Tag pair.
	var likes, alice, pizza flecs.ID
	w.Write(func(fw *flecs.Writer) {
		likes = fw.NewEntity()
		alice = fw.NewEntity()
		pizza = fw.NewEntity()
		flecs.AddID(fw, alice, flecs.MakePair(likes, pizza))
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, alice, flecs.MakePair(likes, pizza)) {
			t.Error("alice should like pizza (tag pair)")
		}
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.RemoveID(fw, alice, flecs.MakePair(likes, pizza))
	})

	w.Read(func(r *flecs.Reader) {
		if flecs.HasID(r, alice, flecs.MakePair(likes, pizza)) {
			t.Error("pair should be removed")
		}
	})

	// Data pair.
	var near, bob, office flecs.ID
	w.Write(func(fw *flecs.Writer) {
		near = fw.NewEntity()
		bob = fw.NewEntity()
		office = fw.NewEntity()
		flecs.SetPair(fw, bob, near, office, Distance{Meters: 500})
	})

	w.Read(func(r *flecs.Reader) {
		d, ok := flecs.GetPair[Distance](r, bob, near, office)
		if !ok || d.Meters != 500 {
			t.Errorf("expected Distance{500}, got %v (ok=%v)", d, ok)
		}
	})
}

// TestQS_Hierarchies verifies the Hierarchies snippet from Quickstart.md.
func TestQS_Hierarchies(t *testing.T) {
	w := flecs.New()

	var scene, car, wheel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		scene = fw.NewEntity()
		car = fw.NewEntity()
		wheel = fw.NewEntity()
		w.SetName(scene, "scene")
		w.SetName(car, "car")
		w.SetName(wheel, "wheel")
		flecs.AddID(fw, car, flecs.MakePair(w.ChildOf(), scene))
		flecs.AddID(fw, wheel, flecs.MakePair(w.ChildOf(), car))
	})

	parent, ok := w.ParentOf(wheel)
	if !ok || parent != car {
		t.Error("wheel's parent should be car")
	}

	childCount := 0
	w.EachChild(scene, func(child flecs.ID) bool {
		childCount++
		_ = child
		return true
	})
	if childCount != 1 {
		t.Errorf("scene should have 1 direct child, got %d", childCount)
	}

	w.Delete(scene)
	if w.IsAlive(wheel) {
		t.Error("wheel should be dead after cascade delete of scene")
	}
}

// TestQS_Prefabs verifies the Prefabs (IsA) snippet from Quickstart.md.
func TestQS_Prefabs(t *testing.T) {
	type Health struct{ HP int }

	w := flecs.New()

	var dragon, redDragon flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dragon = fw.NewEntity()
		flecs.Set(fw, dragon, Health{HP: 100})

		redDragon = fw.NewEntity()
		flecs.AddID(fw, redDragon, flecs.MakePair(w.IsA(), dragon))
	})

	w.Read(func(r *flecs.Reader) {
		h, ok := flecs.Get[Health](r, redDragon)
		if !ok || h.HP != 100 {
			t.Errorf("expected inherited HP=100, got %v (ok=%v)", h, ok)
		}
		if flecs.Owns[Health](r, redDragon) {
			t.Error("redDragon should not own Health locally (inherited from dragon)")
		}
	})

	// Local override.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, redDragon, Health{HP: 150})
	})

	w.Read(func(r *flecs.Reader) {
		h, _ := flecs.Get[Health](r, redDragon)
		if h.HP != 150 {
			t.Errorf("expected overridden HP=150, got %d", h.HP)
		}
		if !flecs.Owns[Health](r, redDragon) {
			t.Error("redDragon should own Health locally after Set")
		}
	})

	// Restore inheritance.
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[Health](fw, redDragon)
	})

	w.Read(func(r *flecs.Reader) {
		h, ok := flecs.Get[Health](r, redDragon)
		if !ok || h.HP != 100 {
			t.Errorf("expected restored HP=100, got %v (ok=%v)", h, ok)
		}
	})
}

// TestQS_Systems verifies the Systems snippet from Quickstart.md.
func TestQS_Systems(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Velocity struct{ DX, DY float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 0, Y: 0})
		flecs.Set(fw, e, Velocity{DX: 1, DY: 0})
	})

	moveQ := flecs.NewCachedQuery(w, posID, velID)
	flecs.NewSystem(w, moveQ, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			positions := flecs.Field[Position](it, posID)
			velocities := flecs.Field[Velocity](it, velID)
			for i := range positions {
				positions[i].X += velocities[i].DX * dt
				positions[i].Y += velocities[i].DY * dt
			}
		}
	})

	const dt = 1.0 / 60.0
	w.Progress(dt)

	if w.FrameCount() != 1 {
		t.Errorf("expected FrameCount=1, got %d", w.FrameCount())
	}

	w.Read(func(r *flecs.Reader) {
		pos, ok := flecs.Get[Position](r, e)
		if !ok {
			t.Fatal("Position not found after Progress")
		}
		want := float32(1.0) * dt
		if pos.X != want {
			t.Errorf("expected X=%.6f after one frame, got %.6f", want, pos.X)
		}
	})

	// NewSystemInPhase demo (compile check only).
	inputQ := flecs.NewCachedQuery(w, posID)
	flecs.NewSystemInPhase(w, w.PreUpdate(), inputQ, func(_ float32, _ *flecs.QueryIter) {})
}

// TestQS_Observers verifies the Observers snippet from Quickstart.md.
func TestQS_Observers(t *testing.T) {
	type Score struct{ Points int }

	w := flecs.New()

	fired := 0
	obs := flecs.Observe[Score](w, flecs.EventOnSet, func(_ *flecs.Writer, _ flecs.ID, s Score) {
		fired++
		_ = s.Points
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Score{Points: 42})
	})

	if fired != 1 {
		t.Errorf("expected observer to fire once, fired %d times", fired)
	}

	obs.Unsubscribe()

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, Score{Points: 99})
	})

	if fired != 1 {
		t.Errorf("observer fired after Unsubscribe: fired=%d", fired)
	}
}
