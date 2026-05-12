package docs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// TestQueries_ArchetypeTable verifies the archetype-table grouping snippet from Queries.md.
// A query for [Position, Velocity] must match only entities in the [Position, Velocity] table,
// not the entity in the [Position]-only table.
func TestQueries_ArchetypeTable(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Velocity struct{ DX, DY float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var e1, e2, e3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, Position{X: 1})

		e2 = fw.NewEntity()
		flecs.Set(fw, e2, Position{X: 2})
		flecs.Set(fw, e2, Velocity{DX: 1})

		e3 = fw.NewEntity()
		flecs.Set(fw, e3, Position{X: 3})
		flecs.Set(fw, e3, Velocity{DX: 2})
	})

	q := flecs.NewQuery(w, posID, velID)
	var matched []flecs.ID
	it := q.Iter()
	for it.Next() {
		for _, e := range it.Entities() {
			matched = append(matched, e)
		}
	}

	if len(matched) != 2 {
		t.Fatalf("want 2 matched entities (e2, e3), got %d", len(matched))
	}
	for _, e := range matched {
		if e == e1 {
			t.Error("e1 (Position-only) should not be matched by Position+Velocity query")
		}
	}
}

// TestQueries_NewQuery_Iter verifies the NewQuery + Iter + Field snippet from Queries.md.
func TestQueries_NewQuery_Iter(t *testing.T) {
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

	q  := flecs.NewQuery(w, posID, velID)
	it := q.Iter()
	for it.Next() {
		pos := flecs.Field[Position](it, posID)
		vel := flecs.Field[Velocity](it, velID)
		for i := range it.Entities() {
			pos[i].X += vel[i].DX
			pos[i].Y += vel[i].DY
		}
	}

	// Verify mutation was applied.
	w.Read(func(r *flecs.Reader) {
		flecs.Each1[Position](r, func(e flecs.ID, p *Position) {
			if p.X != 11 {
				t.Errorf("Position.X = %v, want 11", p.X)
			}
		})
	})
}

// TestQueries_NewCachedQuery_Iter verifies the NewCachedQuery + Iter snippet from Queries.md.
func TestQueries_NewCachedQuery_Iter(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Velocity struct{ DX, DY float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 5})
		flecs.Set(fw, e, Velocity{DX: 3})
	})

	cq := flecs.NewCachedQuery(w, posID, velID)
	it := cq.Iter()
	for it.Next() {
		pos := flecs.Field[Position](it, posID)
		vel := flecs.Field[Velocity](it, velID)
		for i := range it.Entities() {
			pos[i].X += vel[i].DX
		}
	}

	w.Read(func(r *flecs.Reader) {
		flecs.Each1[Position](r, func(_ flecs.ID, p *Position) {
			if p.X != 8 {
				t.Errorf("Position.X = %v, want 8", p.X)
			}
		})
	})
}

// TestQueries_NewQueryFromTerms verifies the NewQueryFromTerms structured-terms snippet.
func TestQueries_NewQueryFromTerms(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Velocity struct{ DX, DY float32 }
	type Mass     struct{ Value float32 }

	w := flecs.New()
	posID  := flecs.RegisterComponent[Position](w)
	velID  := flecs.RegisterComponent[Velocity](w)
	massID := flecs.RegisterComponent[Mass](w)

	var withMass, noMass flecs.ID
	w.Write(func(fw *flecs.Writer) {
		withMass = fw.NewEntity()
		flecs.Set(fw, withMass, Position{X: 1})
		flecs.Set(fw, withMass, Mass{Value: 10})

		noMass = fw.NewEntity()
		flecs.Set(fw, noMass, Position{X: 2})
		flecs.Set(fw, noMass, Velocity{DX: 1})
	})
	_ = withMass

	// Entities with Position, without Mass, optionally with Velocity.
	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Without(massID),
		flecs.Maybe(velID),
	)
	var matched []flecs.ID
	it := q.Iter()
	for it.Next() {
		for _, e := range it.Entities() {
			matched = append(matched, e)
		}
	}

	if len(matched) != 1 || matched[0] != noMass {
		t.Errorf("want [noMass], got %v", matched)
	}
}

// TestQueries_With_And verifies the With (And) operator snippet from Queries.md.
func TestQueries_With_And(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Velocity struct{ DX, DY float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var both flecs.ID
	w.Write(func(fw *flecs.Writer) {
		both = fw.NewEntity()
		flecs.Set(fw, both, Position{X: 1})
		flecs.Set(fw, both, Velocity{DX: 1})

		onlyPos := fw.NewEntity()
		flecs.Set(fw, onlyPos, Position{X: 2})
		_ = onlyPos
	})

	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.With(velID),
	)
	var matched []flecs.ID
	it := q.Iter()
	for it.Next() {
		for _, e := range it.Entities() {
			matched = append(matched, e)
		}
	}

	if len(matched) != 1 || matched[0] != both {
		t.Errorf("want [both], got %v", matched)
	}
}

// TestQueries_Without_Not verifies the Without (Not) operator snippet from Queries.md.
func TestQueries_Without_Not(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Velocity struct{ DX, DY float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var noVel, withVel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		noVel = fw.NewEntity()
		flecs.Set(fw, noVel, Position{X: 1})

		withVel = fw.NewEntity()
		flecs.Set(fw, withVel, Position{X: 2})
		flecs.Set(fw, withVel, Velocity{DX: 1})
	})

	// Match entities with Position that do NOT have Velocity.
	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Without(velID),
	)
	var matched []flecs.ID
	it := q.Iter()
	for it.Next() {
		for _, e := range it.Entities() {
			matched = append(matched, e)
		}
	}

	if len(matched) != 1 || matched[0] != noVel {
		t.Errorf("want [noVel], got %v", matched)
	}
	for _, e := range matched {
		if e == withVel {
			t.Error("withVel should be excluded by NOT Velocity")
		}
	}
}

// TestQueries_Maybe_Optional verifies the Maybe (Optional) operator and FieldMaybe snippet.
func TestQueries_Maybe_Optional(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Velocity struct{ DX, DY float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var withVel, noVel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		withVel = fw.NewEntity()
		flecs.Set(fw, withVel, Position{X: 1})
		flecs.Set(fw, withVel, Velocity{DX: 2})

		noVel = fw.NewEntity()
		flecs.Set(fw, noVel, Position{X: 3})
	})

	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Maybe(velID),
	)

	tablesWithVel, tablesWithout := 0, 0
	it := q.Iter()
	for it.Next() {
		pos        := flecs.Field[Position](it, posID)
		vel, hasVel := flecs.FieldMaybe[Velocity](it, velID)
		for i := range it.Entities() {
			if hasVel {
				pos[i].X += vel[i].DX
				tablesWithVel++
			} else {
				tablesWithout++
			}
		}
	}

	if tablesWithVel != 1 {
		t.Errorf("tablesWithVel = %d, want 1", tablesWithVel)
	}
	if tablesWithout != 1 {
		t.Errorf("tablesWithout = %d, want 1", tablesWithout)
	}
}

// TestQueries_Or_Terms verifies the Or operator and FieldMaybe-for-Or snippet from Queries.md.
func TestQueries_Or_Terms(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Speed    struct{ Value float32 }
	type Velocity struct{ DX, DY float32 }

	w := flecs.New()
	posID   := flecs.RegisterComponent[Position](w)
	speedID := flecs.RegisterComponent[Speed](w)
	velID   := flecs.RegisterComponent[Velocity](w)

	var hasSpeed, hasVel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		hasSpeed = fw.NewEntity()
		flecs.Set(fw, hasSpeed, Position{X: 1})
		flecs.Set(fw, hasSpeed, Speed{Value: 5})

		hasVel = fw.NewEntity()
		flecs.Set(fw, hasVel, Position{X: 2})
		flecs.Set(fw, hasVel, Velocity{DX: 3})
	})

	// Match entities with Position AND (Speed OR Velocity).
	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Or(speedID),
		flecs.Or(velID),
	)

	speedSeen, velSeen := 0, 0
	it := q.Iter()
	for it.Next() {
		speedCol, hasSpeedField := flecs.FieldMaybe[Speed](it, speedID)
		velCol,   hasVelField   := flecs.FieldMaybe[Velocity](it, velID)
		for i := range it.Entities() {
			if hasSpeedField {
				_ = speedCol[i].Value
				speedSeen++
			} else if hasVelField {
				_ = velCol[i].DX
				velSeen++
			}
		}
	}

	if speedSeen != 1 {
		t.Errorf("speedSeen = %d, want 1", speedSeen)
	}
	if velSeen != 1 {
		t.Errorf("velSeen = %d, want 1", velSeen)
	}
}

// TestQueries_Iter_Each verifies the Iter / Each pull-style snippet from Queries.md.
func TestQueries_Iter_Each(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var entities [3]flecs.ID
	w.Write(func(fw *flecs.Writer) {
		for i := range entities {
			entities[i] = fw.NewEntity()
			flecs.Set(fw, entities[i], Position{X: float32(i)})
		}
	})

	q := flecs.NewQuery(w, posID)

	// Pull-style Iter.
	visitedIter := 0
	it := q.Iter()
	for it.Next() {
		for _, e := range it.Entities() {
			_ = e
			visitedIter++
		}
	}

	// Each convenience wrapper.
	visitedEach := 0
	q.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			_ = e
			visitedEach++
		}
	})

	if visitedIter != 3 {
		t.Errorf("Iter visited %d entities, want 3", visitedIter)
	}
	if visitedEach != 3 {
		t.Errorf("Each visited %d entities, want 3", visitedEach)
	}
}

// TestQueries_Field_Access verifies the Field[T] outer/inner loop snippet from Queries.md.
func TestQueries_Field_Access(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Velocity struct{ DX, DY float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 0, Y: 0})
		flecs.Set(fw, e, Velocity{DX: 3, DY: 4})
	})

	q  := flecs.NewQuery(w, posID, velID)
	it := q.Iter()
	for it.Next() {
		pos := flecs.Field[Position](it, posID)
		vel := flecs.Field[Velocity](it, velID)
		for i := range it.Entities() {
			pos[i].X += vel[i].DX
			pos[i].Y += vel[i].DY
		}
	}

	w.Read(func(r *flecs.Reader) {
		flecs.Each1[Position](r, func(_ flecs.ID, p *Position) {
			if p.X != 3 || p.Y != 4 {
				t.Errorf("Position = (%v, %v), want (3, 4)", p.X, p.Y)
			}
		})
	})
}

// TestQueries_FieldShared_IsFieldSelf verifies IsFieldSelf + FieldShared for SelfUp traversal.
func TestQueries_FieldShared_IsFieldSelf(t *testing.T) {
	type Mass struct{ Value float32 }

	w := flecs.New()
	massID := flecs.RegisterComponent[Mass](w)

	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.Set(fw, parent, Mass{Value: 100})

		child = fw.NewEntity()
		fw.AddID(child, flecs.MakePair(w.ChildOf(), parent))
	})

	q := flecs.NewQueryFromTerms(w,
		flecs.With(massID).SelfUp(w.ChildOf()),
	)

	selfCount, inheritedCount := 0, 0
	it := q.Iter()
	for it.Next() {
		if flecs.IsFieldSelf(it, massID) {
			col := flecs.Field[Mass](it, massID)
			for i := range it.Entities() {
				_ = col[i].Value
				selfCount++
			}
		} else {
			inherited, ok := flecs.FieldShared[Mass](it, massID)
			if ok {
				if inherited.Value != 100 {
					t.Errorf("inherited Mass = %v, want 100", inherited.Value)
				}
				for range it.Entities() {
					inheritedCount++
				}
			}
		}
	}

	if selfCount != 1 {
		t.Errorf("selfCount = %d, want 1 (parent)", selfCount)
	}
	if inheritedCount != 1 {
		t.Errorf("inheritedCount = %d, want 1 (child)", inheritedCount)
	}
}

// TestQueries_Each2_Typed verifies the Each2 typed-iteration snippet from Queries.md.
func TestQueries_Each2_Typed(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Velocity struct{ DX, DY float32 }

	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1, Y: 2})
		flecs.Set(fw, e, Velocity{DX: 0.5, DY: 0.5})
	})

	w.Read(func(fr *flecs.Reader) {
		flecs.Each2(fr, func(e flecs.ID, p *Position, v *Velocity) {
			p.X += v.DX
			p.Y += v.DY
			_ = e
		})
	})

	w.Read(func(r *flecs.Reader) {
		flecs.Each1[Position](r, func(_ flecs.ID, p *Position) {
			if p.X != 1.5 || p.Y != 2.5 {
				t.Errorf("Position = (%v, %v), want (1.5, 2.5)", p.X, p.Y)
			}
		})
	})
}

// TestQueries_Pairs_In_Query verifies the With(MakePair(rel, tgt)) snippet from Queries.md.
func TestQueries_Pairs_In_Query(t *testing.T) {
	w := flecs.New()

	var rel, alice, bob, carol flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel   = fw.NewEntity()
		alice = fw.NewEntity()
		bob   = fw.NewEntity()
		carol = fw.NewEntity()
		fw.AddID(alice, flecs.MakePair(rel, bob))   // alice Likes bob
		fw.AddID(carol, flecs.MakePair(rel, alice)) // carol Likes alice
	})

	pairID := flecs.MakePair(rel, bob)
	q  := flecs.NewQueryFromTerms(w, flecs.With(pairID))
	var matched []flecs.ID
	it := q.Iter()
	for it.Next() {
		for _, e := range it.Entities() {
			matched = append(matched, e)
		}
	}

	if len(matched) != 1 || matched[0] != alice {
		t.Errorf("want [alice], got %v", matched)
	}
}

// TestQueries_Traversal_Up verifies the Up traversal snippet from Queries.md.
func TestQueries_Traversal_Up(t *testing.T) {
	type Mass struct{ Value float32 }

	w := flecs.New()
	massID := flecs.RegisterComponent[Mass](w)

	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.Set(fw, parent, Mass{Value: 100})

		child = fw.NewEntity()
		fw.AddID(child, flecs.MakePair(w.ChildOf(), parent))
	})

	// Match entities whose parent (via ChildOf) owns Mass.
	q  := flecs.NewQueryFromTerms(w, flecs.With(massID).Up(w.ChildOf()))
	var matched []flecs.ID
	it := q.Iter()
	for it.Next() {
		inherited, ok := flecs.FieldShared[Mass](it, massID)
		if !ok {
			t.Error("FieldShared should return ok=true for Up traversal")
		}
		if inherited.Value != 100 {
			t.Errorf("inherited Mass.Value = %v, want 100", inherited.Value)
		}
		for _, e := range it.Entities() {
			matched = append(matched, e)
		}
	}

	if len(matched) != 1 || matched[0] != child {
		t.Errorf("want [child], got %v", matched)
	}
}

// TestQueries_Traversal_SelfUp verifies the SelfUp traversal snippet from Queries.md.
func TestQueries_Traversal_SelfUp(t *testing.T) {
	type Mass struct{ Value float32 }

	w := flecs.New()
	massID := flecs.RegisterComponent[Mass](w)

	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.Set(fw, parent, Mass{Value: 50})

		child = fw.NewEntity()
		fw.AddID(child, flecs.MakePair(w.ChildOf(), parent))
		flecs.Set(fw, child, Mass{Value: 25}) // child owns its own Mass
	})

	// SelfUp: match both parent (self) and child (self) — both own Mass locally.
	q := flecs.NewQueryFromTerms(w,
		flecs.With(massID).SelfUp(w.ChildOf()),
	)
	selfCount, inheritedCount := 0, 0
	it := q.Iter()
	for it.Next() {
		if flecs.IsFieldSelf(it, massID) {
			selfCount += it.Count()
		} else {
			inheritedCount += it.Count()
		}
	}

	// Both parent and child own Mass locally → both are self-matched.
	if selfCount != 2 {
		t.Errorf("selfCount = %d, want 2", selfCount)
	}
	if inheritedCount != 0 {
		t.Errorf("inheritedCount = %d, want 0", inheritedCount)
	}
}

// TestQueries_Traversal_Cascade verifies the Cascade traversal snippet from Queries.md.
func TestQueries_Traversal_Cascade(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var root, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		root = fw.NewEntity()
		flecs.Set(fw, root, Position{X: 0, Y: 0})

		child = fw.NewEntity()
		fw.AddID(child, flecs.MakePair(w.ChildOf(), root))
		flecs.Set(fw, child, Position{X: 1, Y: 1})
	})

	// Cascade: iterate root-first, then children.
	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(posID).Cascade(w.ChildOf()),
	)
	var order []flecs.ID
	cq.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			order = append(order, e)
		}
	})

	if len(order) != 2 {
		t.Fatalf("want 2 entities in cascade order, got %d", len(order))
	}
	if order[0] != root {
		t.Errorf("order[0] = %d, want root (%d) — root must be visited first", order[0], root)
	}
	if order[1] != child {
		t.Errorf("order[1] = %d, want child (%d)", order[1], child)
	}
}

// TestQueries_CustomTraversalRelationship verifies the custom traversal relationship snippet.
// In Go flecs any entity can be used as a traversal relationship — no registration needed.
func TestQueries_CustomTraversalRelationship(t *testing.T) {
	type Mass struct{ Value float32 }

	w := flecs.New()
	massID := flecs.RegisterComponent[Mass](w)

	// Any entity works as a custom traversal relationship in Go flecs.
	var containedBy flecs.ID
	w.Write(func(fw *flecs.Writer) {
		containedBy = fw.NewEntity()
	})

	var container, item flecs.ID
	w.Write(func(fw *flecs.Writer) {
		container = fw.NewEntity()
		flecs.Set(fw, container, Mass{Value: 99})

		item = fw.NewEntity()
		fw.AddID(item, flecs.MakePair(containedBy, container))
	})

	q  := flecs.NewQueryFromTerms(w, flecs.With(massID).Up(containedBy))
	var matched []flecs.ID
	it := q.Iter()
	for it.Next() {
		for _, e := range it.Entities() {
			matched = append(matched, e)
		}
	}

	if len(matched) != 1 || matched[0] != item {
		t.Errorf("want [item], got %v", matched)
	}
}

// TestQueries_Inheritable verifies the SetInheritable + auto-promotion snippet from Queries.md.
func TestQueries_Inheritable(t *testing.T) {
	type Mass struct{ Value float32 }

	w := flecs.New()
	flecs.RegisterComponent[Mass](w)
	flecs.SetInheritable[Mass](w)
	massID := flecs.RegisterComponent[Mass](w)

	var prefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Mass{Value: 50})

		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), prefab))
	})

	q := flecs.NewQuery(w, massID)
	var matched []flecs.ID
	it := q.Iter()
	for it.Next() {
		for _, e := range it.Entities() {
			matched = append(matched, e)
		}
	}

	foundPrefab, foundInst := false, false
	for _, e := range matched {
		if e == prefab {
			foundPrefab = true
		}
		if e == inst {
			foundInst = true
		}
	}
	if !foundPrefab {
		t.Error("prefab should be matched (owns Mass locally)")
	}
	if !foundInst {
		t.Error("inst should be matched (inherits Mass via IsA)")
	}
}

// TestQueries_InheritableSelfSuppress verifies that With(id).Self() suppresses inheritable auto-promotion.
func TestQueries_InheritableSelfSuppress(t *testing.T) {
	type Mass struct{ Value float32 }

	w := flecs.New()
	flecs.RegisterComponent[Mass](w)
	flecs.SetInheritable[Mass](w)
	massID := flecs.RegisterComponent[Mass](w)

	var prefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Mass{Value: 50})

		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), prefab))
	})

	// .Self() suppresses IsA traversal — only locally-owned Mass is matched.
	q := flecs.NewQueryFromTerms(w,
		flecs.With(massID).Self(),
	)
	var matched []flecs.ID
	it := q.Iter()
	for it.Next() {
		for _, e := range it.Entities() {
			matched = append(matched, e)
		}
	}

	for _, e := range matched {
		if e == inst {
			t.Error("inst should NOT be matched when Self() suppresses IsA traversal")
		}
	}
	if len(matched) != 1 || matched[0] != prefab {
		t.Errorf("want [prefab] only, got %v", matched)
	}
}

// TestQueries_ChangeDetection verifies the CachedQuery.Changed() snippet from Queries.md.
func TestQueries_ChangeDetection(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1})
	})

	cq := flecs.NewCachedQuery(w, posID)

	// First call always returns true.
	if !cq.Changed() {
		t.Error("first Changed() call should return true")
	}

	// No mutations — should return false.
	if cq.Changed() {
		t.Error("second Changed() call with no mutations should return false")
	}

	// Write a new value.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, Position{X: 2})
	})

	if !cq.Changed() {
		t.Error("Changed() after a write should return true")
	}
}

// TestQueries_CachedQuery_Close verifies the Close / IsClosed snippet from Queries.md.
func TestQueries_CachedQuery_Close(t *testing.T) {
	type Position struct{ X, Y float32 }

	w  := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	cq := flecs.NewCachedQuery(w, posID)

	if cq.IsClosed() {
		t.Error("IsClosed should be false before Close")
	}

	cq.Close()

	if !cq.IsClosed() {
		t.Error("IsClosed should be true after Close")
	}

	// Iter after close returns no results.
	it := cq.Iter()
	if it.Next() {
		t.Error("Iter().Next() should return false after Close")
	}

	// Changed after close returns false.
	if cq.Changed() {
		t.Error("Changed() should return false after Close")
	}
}

// TestQueries_CachedQuery_EntityCount verifies Count and EntityCount on a cached query.
func TestQueries_CachedQuery_EntityCount(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < 5; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, Position{X: float32(i)})
		}
	})

	cq := flecs.NewCachedQuery(w, posID)

	if cq.EntityCount() != 5 {
		t.Errorf("EntityCount = %d, want 5", cq.EntityCount())
	}
	if cq.Count() < 1 {
		t.Errorf("Count (tables) = %d, want >= 1", cq.Count())
	}
}
