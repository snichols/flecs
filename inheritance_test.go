package flecs_test

import (
	"testing"

	flecs "github.com/snichols/flecs"
)

// ── component types used across inheritance tests ──────────────────────────

type inhPos struct{ X, Y float32 }
type inhVel struct{ DX, DY float32 }

// TestInheritable_Each1MatchesInheritor verifies that Each1 yields an entity
// that owns no local Position but inherits it from a prefab via IsA, once
// Position is marked inheritable.
func TestInheritable_Each1MatchesInheritor(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[inhPos](w)
	flecs.SetInheritable[inhPos](w)

	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, inhPos{X: 10, Y: 20})
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})
	_ = posID

	var found []flecs.ID
	w.Read(func(r *flecs.Reader) {
		flecs.Each1[inhPos](r, func(e flecs.ID, p *inhPos) {
			found = append(found, e)
		})
	})

	if len(found) == 0 {
		t.Fatal("Each1[inhPos] yielded no entities; expected at least child")
	}
	sawChild := false
	for _, e := range found {
		if e == child {
			sawChild = true
		}
	}
	if !sawChild {
		t.Fatalf("Each1[inhPos] did not yield child (got %v)", found)
	}
}

// TestInheritable_Each1ValueFromPrefab verifies that the pointer passed to Each1
// for an up-matched entity points to the prefab's component value.
func TestInheritable_Each1ValueFromPrefab(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[inhPos](w)
	flecs.SetInheritable[inhPos](w)

	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, inhPos{X: 10, Y: 20})
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})

	var childPos inhPos
	var gotChild bool
	w.Read(func(r *flecs.Reader) {
		flecs.Each1[inhPos](r, func(e flecs.ID, p *inhPos) {
			if e == child {
				childPos = *p
				gotChild = true
			}
		})
	})

	if !gotChild {
		t.Fatal("child not visited")
	}
	if childPos.X != 10 || childPos.Y != 20 {
		t.Errorf("child got {%v %v}, want {10 20} (prefab value)", childPos.X, childPos.Y)
	}
}

// TestInheritable_LocalOverridesInherited verifies that when both the prefab and
// the child have Position, Each1 yields the child's local value (Self path wins).
func TestInheritable_LocalOverridesInherited(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[inhPos](w)
	flecs.SetInheritable[inhPos](w)

	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, inhPos{X: 10, Y: 20})
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
		flecs.Set(fw, child, inhPos{X: 99, Y: 88}) // local override
	})

	var childPos inhPos
	w.Read(func(r *flecs.Reader) {
		flecs.Each1[inhPos](r, func(e flecs.ID, p *inhPos) {
			if e == child {
				childPos = *p
			}
		})
	})
	if childPos.X != 99 || childPos.Y != 88 {
		t.Errorf("child got {%v %v}, want local {99 88}", childPos.X, childPos.Y)
	}
}

// TestInheritable_UnmarkedComponent_StaysExclusive verifies that Velocity, which
// is NOT marked inheritable, does not cause Each1 to match an inheritor.
func TestInheritable_UnmarkedComponent_StaysExclusive(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[inhVel](w) // intentionally NOT SetInheritable

	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, inhVel{DX: 1, DY: 2})
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})

	var found []flecs.ID
	w.Read(func(r *flecs.Reader) {
		flecs.Each1[inhVel](r, func(e flecs.ID, v *inhVel) {
			found = append(found, e)
		})
	})

	for _, e := range found {
		if e == child {
			t.Fatal("Each1[inhVel] yielded child even though inhVel is not inheritable")
		}
	}
}

// TestInheritable_ExplicitSelfOverridesAuto verifies that a term built with
// With(posID).Self() does NOT auto-promote, even when the component is inheritable.
func TestInheritable_ExplicitSelfOverridesAuto(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[inhPos](w)
	flecs.SetInheritable[inhPos](w)

	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, inhPos{X: 10, Y: 20})
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})

	// Explicit Self() suppresses inheritable auto-promotion.
	q := flecs.NewQueryFromTerms(w, flecs.With(posID).Self())
	var found []flecs.ID
	q.Each(func(it *flecs.QueryIter) {
		found = append(found, it.Entities()...)
	})

	for _, e := range found {
		if e == child {
			t.Fatal("explicit Self() term still matched child (should be local-only)")
		}
	}
	// prefab itself has a local Position, so it should be found.
	sawPrefab := false
	for _, e := range found {
		if e == prefab {
			sawPrefab = true
		}
	}
	if !sawPrefab {
		t.Fatal("prefab with local Position not found by explicit-Self query")
	}
}

// TestInheritable_Each2_OneInheritedOneLocal verifies that Each2 handles one
// inheritable and one non-inheritable component correctly. The prefab has Position
// (inheritable) but not Velocity; the child has IsA(prefab) and local Velocity.
// Each2 should yield the child with Position from the prefab and Velocity from self.
func TestInheritable_Each2_OneInheritedOneLocal(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[inhPos](w)
	flecs.SetInheritable[inhPos](w)
	flecs.RegisterComponent[inhVel](w)

	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, inhPos{X: 10, Y: 20})

		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
		flecs.Set(fw, child, inhVel{DX: 3, DY: 4})
	})

	var gotChild bool
	var childPos inhPos
	var childVel inhVel
	w.Read(func(r *flecs.Reader) {
		flecs.Each2[inhPos, inhVel](r, func(e flecs.ID, p *inhPos, v *inhVel) {
			if e == child {
				gotChild = true
				childPos = *p
				childVel = *v
			}
		})
	})

	if !gotChild {
		t.Fatal("Each2 did not yield child")
	}
	if childPos.X != 10 || childPos.Y != 20 {
		t.Errorf("Position: got {%v %v}, want {10 20} (from prefab)", childPos.X, childPos.Y)
	}
	if childVel.DX != 3 || childVel.DY != 4 {
		t.Errorf("Velocity: got {%v %v}, want {3 4} (from self)", childVel.DX, childVel.DY)
	}
}

// TestInheritable_NewQueryFromTerms_FieldShared verifies that for explicit
// NewQueryFromTerms callers, IsFieldSelf correctly distinguishes local vs.
// inherited components and FieldShared returns the prefab value.
func TestInheritable_NewQueryFromTerms_FieldShared(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[inhPos](w)
	flecs.SetInheritable[inhPos](w)

	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, inhPos{X: 10, Y: 20})
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})

	// NewQueryFromTerms with an auto-promoted term (no explicit traversal set).
	q := flecs.NewQueryFromTerms(w, flecs.With(posID))
	var childSelf bool
	var prefabShared bool
	q.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			isSelf := flecs.IsFieldSelf(it, posID)
			if e == child {
				childSelf = isSelf
				if !isSelf {
					val, ok := flecs.FieldShared[inhPos](it, posID)
					if !ok {
						t.Errorf("FieldShared returned false for up-matched child")
					}
					if val.X != 10 || val.Y != 20 {
						t.Errorf("FieldShared: got {%v %v}, want {10 20}", val.X, val.Y)
					}
					prefabShared = true
				}
			}
		}
	})

	if childSelf {
		t.Error("IsFieldSelf(child) should be false (component inherited, not owned locally)")
	}
	if !prefabShared {
		t.Error("FieldShared was never called; child may not have been matched")
	}
}

// TestInheritable_CachedQueryRematch verifies that a cached query picks up a new
// inheritor table created AFTER the query was built.
func TestInheritable_CachedQueryRematch(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[inhPos](w)
	flecs.SetInheritable[inhPos](w)
	velID := flecs.RegisterComponent[inhVel](w) // used to create a distinct table

	var prefab flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, inhPos{X: 1, Y: 2})
	})

	// Build cached query BEFORE creating the inheritor.
	cq := flecs.NewCachedQuery(w, posID)

	// Now create a child that inherits Position via IsA + has Velocity (distinct table).
	var child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
		flecs.Set(fw, child, inhVel{DX: 5, DY: 6})
	})
	_ = velID

	// The cached query must pick up the new inheritor table.
	var found []flecs.ID
	it := cq.Iter()
	for it.Next() {
		found = append(found, it.Entities()...)
	}

	sawChild := false
	for _, e := range found {
		if e == child {
			sawChild = true
		}
	}
	if !sawChild {
		t.Fatalf("cached query did not match child created after query build (found=%v)", found)
	}
}

// TestInheritable_TermNot_NotPromoted verifies that a TermNot term for an
// inheritable component is NOT auto-promoted to SelfUp. Not semantics require
// the component to be absent from the table's own signature; Up traversal
// doesn't change archetype exclusion.
func TestInheritable_TermNot_NotPromoted(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[inhPos](w)
	velID := flecs.RegisterComponent[inhVel](w)
	flecs.SetInheritable[inhVel](w) // Velocity is inheritable

	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, inhVel{DX: 1, DY: 2}) // prefab has inheritable Vel

		child = fw.NewEntity()
		flecs.Set(fw, child, inhPos{X: 3, Y: 4})
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab)) // inherits Vel
	})

	// child inherits Vel via IsA. A NOT Velocity term should still match child
	// because the table doesn't have Velocity locally (Not checks own archetype).
	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Without(velID),
	)
	var found []flecs.ID
	q.Each(func(it *flecs.QueryIter) {
		found = append(found, it.Entities()...)
	})

	sawChild := false
	for _, e := range found {
		if e == child {
			sawChild = true
		}
	}
	if !sawChild {
		t.Fatal("NOT Velocity query should still match child (Velocity not in local archetype); child not found")
	}
}

// TestInheritable_SetInheritableNotRegisteredPanics verifies that calling
// SetInheritable with an ID that is not a registered component panics.
func TestInheritable_SetInheritableNotRegisteredPanics(t *testing.T) {
	w := flecs.New()
	var rawID flecs.ID
	w.Write(func(fw *flecs.Writer) { rawID = fw.NewEntity() })

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for unregistered ID, got none")
		}
	}()
	w.SetInheritable(rawID)
}

// TestInheritable_GenericSetInheritableNotRegisteredPanics verifies that
// SetInheritable[T] panics when T has not been registered first.
func TestInheritable_GenericSetInheritableNotRegisteredPanics(t *testing.T) {
	type unregistered struct{ V int }
	w := flecs.New()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for unregistered type, got none")
		}
	}()
	flecs.SetInheritable[unregistered](w)
}

// TestInheritable_TraversalAttribute_UpExplicit verifies that an explicit
// With(posID).Up(w.IsA()) works on an inheritable component just as it did
// before: the Go port is permissive and accepts explicit Up on any component.
func TestInheritable_TraversalAttribute_UpExplicit(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[inhPos](w)
	flecs.SetInheritable[inhPos](w)

	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, inhPos{X: 7, Y: 8})
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})

	// Explicit Up(IsA): only matches entities WITHOUT local Position but WITH ancestor.
	q := flecs.NewQueryFromTerms(w, flecs.With(posID).Up(w.IsA()))
	var found []flecs.ID
	q.Each(func(it *flecs.QueryIter) {
		found = append(found, it.Entities()...)
	})

	sawChild, sawPrefab := false, false
	for _, e := range found {
		if e == child {
			sawChild = true
		}
		if e == prefab {
			sawPrefab = true
		}
	}
	if !sawChild {
		t.Error("Up(IsA) query should match child (has no local Position, inherits from prefab)")
	}
	if sawPrefab {
		t.Error("Up(IsA) query should NOT match prefab (prefab owns Position locally; Up skips self)")
	}
}

// TestInheritable_BuiltinTraitEntities verifies that the new built-in trait
// entities are allocated and distinct.
func TestInheritable_BuiltinTraitEntities(t *testing.T) {
	w := flecs.New()
	ids := []flecs.ID{
		w.OnInstantiate(), w.Inherit(), w.Override(), w.DontInherit(),
	}
	seen := make(map[flecs.ID]bool)
	for _, id := range ids {
		if id == 0 {
			t.Errorf("expected non-zero built-in trait ID; got 0 for one of %v", ids)
		}
		if seen[id] {
			t.Errorf("duplicate built-in trait ID %v", id)
		}
		seen[id] = true
	}
	// All must be distinct from other well-known built-ins.
	others := []flecs.ID{w.ChildOf(), w.IsA()}
	for _, o := range others {
		for _, id := range ids {
			if id == o {
				t.Errorf("trait entity %v collides with ChildOf/IsA", id)
			}
		}
	}
}

// inhMass is a third component type for Each3 tests.
type inhMass struct{ M float32 }

// inhScale is a fourth component type for Each4 tests.
type inhScale struct{ S float32 }

// TestInheritable_Each3_AllInherited verifies that Each3 yields an entity
// when all three components are inherited from a prefab.
func TestInheritable_Each3_AllInherited(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[inhPos](w)
	flecs.SetInheritable[inhPos](w)
	flecs.RegisterComponent[inhVel](w)
	flecs.SetInheritable[inhVel](w)
	flecs.RegisterComponent[inhMass](w)
	flecs.SetInheritable[inhMass](w)

	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, inhPos{X: 1, Y: 2})
		flecs.Set(fw, prefab, inhVel{DX: 3, DY: 4})
		flecs.Set(fw, prefab, inhMass{M: 5})
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})

	var gotChild bool
	w.Read(func(r *flecs.Reader) {
		flecs.Each3[inhPos, inhVel, inhMass](r, func(e flecs.ID, p *inhPos, v *inhVel, m *inhMass) {
			if e == child {
				gotChild = true
				if p.X != 1 || p.Y != 2 {
					t.Errorf("pos: got {%v %v}, want {1 2}", p.X, p.Y)
				}
				if v.DX != 3 || v.DY != 4 {
					t.Errorf("vel: got {%v %v}, want {3 4}", v.DX, v.DY)
				}
				if m.M != 5 {
					t.Errorf("mass: got %v, want 5", m.M)
				}
			}
		})
	})
	if !gotChild {
		t.Fatal("Each3 did not yield child with all-inherited components")
	}
}

// TestInheritable_Each3_MixedInheritedLocal verifies Each3 when some components
// are inherited and some are local.
func TestInheritable_Each3_MixedInheritedLocal(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[inhPos](w)
	flecs.SetInheritable[inhPos](w)
	flecs.RegisterComponent[inhVel](w) // NOT inheritable
	flecs.RegisterComponent[inhMass](w)
	flecs.SetInheritable[inhMass](w)

	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, inhPos{X: 10, Y: 20})
		flecs.Set(fw, prefab, inhMass{M: 99})
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
		flecs.Set(fw, child, inhVel{DX: 7, DY: 8})
	})

	var gotChild bool
	w.Read(func(r *flecs.Reader) {
		flecs.Each3[inhPos, inhVel, inhMass](r, func(e flecs.ID, p *inhPos, v *inhVel, m *inhMass) {
			if e == child {
				gotChild = true
				if p.X != 10 {
					t.Errorf("pos.X: got %v, want 10 (from prefab)", p.X)
				}
				if v.DX != 7 {
					t.Errorf("vel.DX: got %v, want 7 (local)", v.DX)
				}
				if m.M != 99 {
					t.Errorf("mass: got %v, want 99 (from prefab)", m.M)
				}
			}
		})
	})
	if !gotChild {
		t.Fatal("Each3 mixed did not yield child")
	}
}

// TestInheritable_Each4_AllInherited verifies that Each4 yields an entity
// when all four components are inherited from a prefab.
func TestInheritable_Each4_AllInherited(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[inhPos](w)
	flecs.SetInheritable[inhPos](w)
	flecs.RegisterComponent[inhVel](w)
	flecs.SetInheritable[inhVel](w)
	flecs.RegisterComponent[inhMass](w)
	flecs.SetInheritable[inhMass](w)
	flecs.RegisterComponent[inhScale](w)
	flecs.SetInheritable[inhScale](w)

	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, inhPos{X: 1, Y: 2})
		flecs.Set(fw, prefab, inhVel{DX: 3, DY: 4})
		flecs.Set(fw, prefab, inhMass{M: 5})
		flecs.Set(fw, prefab, inhScale{S: 6})
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})

	var gotChild bool
	w.Read(func(r *flecs.Reader) {
		flecs.Each4[inhPos, inhVel, inhMass, inhScale](r, func(e flecs.ID, p *inhPos, v *inhVel, m *inhMass, s *inhScale) {
			if e == child {
				gotChild = true
				if p.X != 1 {
					t.Errorf("pos.X: got %v, want 1", p.X)
				}
				if s.S != 6 {
					t.Errorf("scale.S: got %v, want 6", s.S)
				}
			}
		})
	})
	if !gotChild {
		t.Fatal("Each4 did not yield child with all-inherited components")
	}
}

// TestInheritable_Each4_MixedInheritedLocal verifies Each4 with a mix of
// inherited and locally-owned components.
func TestInheritable_Each4_MixedInheritedLocal(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[inhPos](w)
	flecs.SetInheritable[inhPos](w)
	flecs.RegisterComponent[inhVel](w) // NOT inheritable
	flecs.RegisterComponent[inhMass](w)
	flecs.SetInheritable[inhMass](w)
	flecs.RegisterComponent[inhScale](w) // NOT inheritable

	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, inhPos{X: 10, Y: 20})
		flecs.Set(fw, prefab, inhMass{M: 99})
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
		flecs.Set(fw, child, inhVel{DX: 3, DY: 4})
		flecs.Set(fw, child, inhScale{S: 7})
	})

	var gotChild bool
	w.Read(func(r *flecs.Reader) {
		flecs.Each4[inhPos, inhVel, inhMass, inhScale](r, func(e flecs.ID, p *inhPos, v *inhVel, m *inhMass, s *inhScale) {
			if e == child {
				gotChild = true
				if p.X != 10 {
					t.Errorf("pos.X: got %v, want 10 (from prefab)", p.X)
				}
				if v.DX != 3 {
					t.Errorf("vel.DX: got %v, want 3 (local)", v.DX)
				}
				if m.M != 99 {
					t.Errorf("mass: got %v, want 99 (from prefab)", m.M)
				}
				if s.S != 7 {
					t.Errorf("scale: got %v, want 7 (local)", s.S)
				}
			}
		})
	})
	if !gotChild {
		t.Fatal("Each4 mixed did not yield child")
	}
}

// inhMarker is a zero-size tag component for testing inherited tags.
type inhMarker struct{}

// TestInheritable_Each1_InheritedTagComponent verifies that Each1 works when the
// inherited component is a zero-size tag (no data backing store).
func TestInheritable_Each1_InheritedTagComponent(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[inhMarker](w)
	flecs.SetInheritable[inhMarker](w)

	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.AddID(fw, prefab, flecs.RegisterComponent[inhMarker](w))
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})

	var count int
	w.Read(func(r *flecs.Reader) {
		flecs.Each1[inhMarker](r, func(e flecs.ID, m *inhMarker) {
			count++
		})
	})
	if count == 0 {
		t.Fatal("Each1 with inherited tag yielded no entities")
	}
}

// TestInheritable_Each3_FirstLocalRestInherited covers the Each3 path where the
// first component is local and the latter two are inherited.
func TestInheritable_Each3_FirstLocalRestInherited(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[inhPos](w) // NOT inheritable
	flecs.RegisterComponent[inhVel](w)
	flecs.SetInheritable[inhVel](w)
	flecs.RegisterComponent[inhMass](w)
	flecs.SetInheritable[inhMass](w)

	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, inhVel{DX: 3, DY: 4})
		flecs.Set(fw, prefab, inhMass{M: 5})
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
		flecs.Set(fw, child, inhPos{X: 10, Y: 20}) // local
	})

	var gotChild bool
	w.Read(func(r *flecs.Reader) {
		flecs.Each3[inhPos, inhVel, inhMass](r, func(e flecs.ID, p *inhPos, v *inhVel, m *inhMass) {
			if e == child {
				gotChild = true
				if p.X != 10 {
					t.Errorf("pos.X: got %v, want 10 (local)", p.X)
				}
				if v.DX != 3 {
					t.Errorf("vel.DX: got %v, want 3 (from prefab)", v.DX)
				}
				if m.M != 5 {
					t.Errorf("mass: got %v, want 5 (from prefab)", m.M)
				}
			}
		})
	})
	if !gotChild {
		t.Fatal("Each3 first-local/rest-inherited did not yield child")
	}
}

// TestInheritable_Each4_FirstLocalRestInherited covers the Each4 path where the
// first component is local and the latter three are inherited.
func TestInheritable_Each4_FirstLocalRestInherited(t *testing.T) {
	w := flecs.New()
	flecs.RegisterComponent[inhPos](w) // NOT inheritable
	flecs.RegisterComponent[inhVel](w)
	flecs.SetInheritable[inhVel](w)
	flecs.RegisterComponent[inhMass](w)
	flecs.SetInheritable[inhMass](w)
	flecs.RegisterComponent[inhScale](w)
	flecs.SetInheritable[inhScale](w)

	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, inhVel{DX: 3, DY: 4})
		flecs.Set(fw, prefab, inhMass{M: 5})
		flecs.Set(fw, prefab, inhScale{S: 6})
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
		flecs.Set(fw, child, inhPos{X: 10, Y: 20}) // local
	})

	var gotChild bool
	w.Read(func(r *flecs.Reader) {
		flecs.Each4[inhPos, inhVel, inhMass, inhScale](r, func(e flecs.ID, p *inhPos, v *inhVel, m *inhMass, s *inhScale) {
			if e == child {
				gotChild = true
				if p.X != 10 {
					t.Errorf("pos.X: got %v, want 10 (local)", p.X)
				}
				if v.DX != 3 {
					t.Errorf("vel: got %v, want 3 (from prefab)", v.DX)
				}
				if s.S != 6 {
					t.Errorf("scale: got %v, want 6 (from prefab)", s.S)
				}
			}
		})
	})
	if !gotChild {
		t.Fatal("Each4 first-local/rest-inherited did not yield child")
	}
}
