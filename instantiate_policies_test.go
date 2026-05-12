package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// ── Test 1: Override copies prefab value to instance at IsA-add time ──────────
//
// A component with Override policy is automatically copied from the prefab into
// the instance when (IsA, prefab) is added. Mutating the instance copy does not
// affect the prefab or sibling instances.

func TestInstantiatePolicyOverrideCopiesAtIsAAdd(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	flecs.SetInstantiatePolicy(w, posID, w.Override())

	var prefab, inst1, inst2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 10, Y: 20})

		inst1 = fw.NewEntity()
		fw.AddID(inst1, flecs.MakePair(w.IsA(), prefab))

		inst2 = fw.NewEntity()
		fw.AddID(inst2, flecs.MakePair(w.IsA(), prefab))
	})

	w.Read(func(r *flecs.Reader) {
		p1, ok := flecs.Get[Position](r, inst1)
		if !ok {
			t.Fatal("inst1: expected Position after Override copy")
		}
		if p1.X != 10 || p1.Y != 20 {
			t.Fatalf("inst1: expected {10,20}, got {%v,%v}", p1.X, p1.Y)
		}
		// inst1 must own it locally (not via IsA chain)
		if !flecs.Owns[Position](r, inst1) {
			t.Fatal("inst1: expected local ownership of Override-copied Position")
		}
	})

	// Mutate inst1 — must not affect prefab or inst2.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, inst1, Position{X: 99, Y: 99})
	})

	w.Read(func(r *flecs.Reader) {
		pPrefab, _ := flecs.Get[Position](r, prefab)
		if pPrefab.X != 10 || pPrefab.Y != 20 {
			t.Fatalf("prefab position must be unaffected; got {%v,%v}", pPrefab.X, pPrefab.Y)
		}
		p2, _ := flecs.Get[Position](r, inst2)
		if p2.X != 10 || p2.Y != 20 {
			t.Fatalf("inst2 position must be unaffected; got {%v,%v}", p2.X, p2.Y)
		}
	})
}

// ── Test 2: DontInherit suppresses Get and Has via IsA ────────────────────────
//
// A component with DontInherit policy is invisible on instances via the IsA chain.
// Has and Get return false/zero even when the prefab owns the component.

func TestInstantiatePolicyDontInheritSuppressesGetHas(t *testing.T) {
	type Secret struct{ Code int }

	w := flecs.New()
	secretID := flecs.RegisterComponent[Secret](w)
	flecs.SetInstantiatePolicy(w, secretID, w.DontInherit())

	var prefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Secret{Code: 42})

		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), prefab))
	})

	w.Read(func(r *flecs.Reader) {
		if flecs.Has[Secret](r, inst) {
			t.Fatal("Has[Secret](inst) should return false for DontInherit component")
		}
		s, ok := flecs.Get[Secret](r, inst)
		if ok {
			t.Fatal("Get[Secret](inst) should return false for DontInherit component")
		}
		if s.Code != 0 {
			t.Fatalf("Get[Secret](inst) returned non-zero value %v for DontInherit component", s.Code)
		}
	})
}

// ── Test 3: DontInherit overrides Inheritable in query auto-promotion ─────────
//
// When both SetInheritable and SetInstantiatePolicy(DontInherit) are called on
// the same component, DontInherit wins: queries do NOT auto-promote to Up(IsA)
// and the instance does not appear in query results.

func TestInstantiatePolicyDontInheritOverridesInheritableQuery(t *testing.T) {
	type Tag struct{ V int }

	w := flecs.New()
	tagID := flecs.RegisterComponent[Tag](w)
	flecs.SetInheritable[Tag](w)
	flecs.SetInstantiatePolicy(w, tagID, w.DontInherit())

	var prefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Tag{V: 1})

		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), prefab))
	})

	// Query should only find entities that own Tag locally, not inst.
	var found []flecs.ID
	w.Read(func(r *flecs.Reader) {
		flecs.Each1[Tag](r, func(e flecs.ID, _ *Tag) {
			found = append(found, e)
		})
	})

	for _, e := range found {
		if e == inst {
			t.Fatal("DontInherit should suppress query auto-promotion; inst must not appear in query results")
		}
	}
	// prefab itself should still appear since it owns Tag locally.
	foundPrefab := false
	for _, e := range found {
		if e == prefab {
			foundPrefab = true
		}
	}
	if !foundPrefab {
		t.Fatal("prefab must appear in query results (it owns Tag locally)")
	}
}

// ── Test 4: Multi-component prefab with mixed policies ────────────────────────
//
// A prefab with three components, each with a different policy:
//   A → Inherit (default chain walk),
//   B → Override (eager copy at IsA-add),
//   C → DontInherit (invisible on instance).

func TestInstantiatePolicyMixedPolicies(t *testing.T) {
	type CompA struct{ V int }
	type CompB struct{ V int }
	type CompC struct{ V int }

	w := flecs.New()
	flecs.RegisterComponent[CompA](w)
	bID := flecs.RegisterComponent[CompB](w)
	cID := flecs.RegisterComponent[CompC](w)

	flecs.SetInheritable[CompA](w) // A is inheritable (chain walk)
	flecs.SetInstantiatePolicy(w, bID, w.Override())
	flecs.SetInstantiatePolicy(w, cID, w.DontInherit())

	var prefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, CompA{V: 1})
		flecs.Set(fw, prefab, CompB{V: 2})
		flecs.Set(fw, prefab, CompC{V: 3})

		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), prefab))
	})

	w.Read(func(r *flecs.Reader) {
		// A: instance sees A via IsA chain, does NOT own locally.
		a, ok := flecs.Get[CompA](r, inst)
		if !ok || a.V != 1 {
			t.Fatalf("A: expected V=1 via IsA chain, got ok=%v v=%v", ok, a.V)
		}
		if flecs.Owns[CompA](r, inst) {
			t.Fatal("A: instance should NOT own A locally (Inherit policy)")
		}

		// B: instance has a local copy (Override).
		b, ok := flecs.Get[CompB](r, inst)
		if !ok || b.V != 2 {
			t.Fatalf("B: expected V=2 via Override copy, got ok=%v v=%v", ok, b.V)
		}
		if !flecs.Owns[CompB](r, inst) {
			t.Fatal("B: instance should own B locally (Override policy)")
		}

		// C: invisible on instance.
		if flecs.Has[CompC](r, inst) {
			t.Fatal("C: Has should return false (DontInherit policy)")
		}
		if flecs.HasID(r, inst, cID) {
			t.Fatal("C: HasID should return false (DontInherit policy)")
		}
	})
}

// ── Test 5: Override removal restores IsA path ────────────────────────────────
//
// After Override copies a component to the instance, removing it from the
// instance restores the IsA chain walk (the prefab's value becomes visible again
// through inheritance, assuming SetInheritable was also called).

func TestInstantiatePolicyOverrideRemovalRestoresIsA(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	flecs.SetInheritable[Position](w)
	flecs.SetInstantiatePolicy(w, posID, w.Override())

	var prefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 5, Y: 7})

		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), prefab))
	})

	// Sanity: inst has a local copy.
	w.Read(func(r *flecs.Reader) {
		if !flecs.Owns[Position](r, inst) {
			t.Fatal("inst should own Position locally after Override copy")
		}
	})

	// Remove the local copy.
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[Position](fw, inst)
	})

	// Get must now walk the IsA chain and return the prefab's value.
	w.Read(func(r *flecs.Reader) {
		if flecs.Owns[Position](r, inst) {
			t.Fatal("inst should NOT own Position after Remove")
		}
		p, ok := flecs.Get[Position](r, inst)
		if !ok {
			t.Fatal("Get[Position](inst) should walk IsA chain after Remove")
		}
		if p.X != 5 || p.Y != 7 {
			t.Fatalf("expected prefab position {5,7} via IsA; got {%v,%v}", p.X, p.Y)
		}
	})
}

// ── Test 6: SetInstantiatePolicy / GetInstantiatePolicy round-trip ────────────
//
// For each of the three actions, Set then Get returns the same action ID.

func TestInstantiatePolicyRoundTrip(t *testing.T) {
	type Comp struct{}

	w := flecs.New()
	cID := flecs.RegisterComponent[Comp](w)

	cases := []struct {
		name   string
		action flecs.ID
	}{
		{"Override", w.Override()},
		{"Inherit", w.Inherit()},
		{"DontInherit", w.DontInherit()},
	}
	for _, tc := range cases {
		flecs.SetInstantiatePolicy(w, cID, tc.action)
		got, ok := flecs.GetInstantiatePolicy(w, cID)
		if !ok {
			t.Errorf("%s: GetInstantiatePolicy returned false after Set", tc.name)
		}
		if got != tc.action {
			t.Errorf("%s: expected action %v, got %v", tc.name, tc.action, got)
		}
	}
}

// ── Test 7: Pair-add form equivalent to SetInstantiatePolicy ─────────────────
//
// fw.AddID(cid, MakePair(w.OnInstantiate(), w.Override())) must produce the same
// GetInstantiatePolicy result as SetInstantiatePolicy(w, cid, w.Override()).

func TestInstantiatePolicyPairAddEquivalent(t *testing.T) {
	type Comp struct{}

	w := flecs.New()
	cID := flecs.RegisterComponent[Comp](w)

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(cID, flecs.MakePair(w.OnInstantiate(), w.Override()))
	})

	action, ok := flecs.GetInstantiatePolicy(w, cID)
	if !ok {
		t.Fatal("GetInstantiatePolicy returned false after pair-add")
	}
	if action != w.Override() {
		t.Fatalf("expected Override(), got %v", action)
	}

	// Also verify DontInherit pair-add.
	type Comp2 struct{}
	c2ID := flecs.RegisterComponent[Comp2](w)
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(c2ID, flecs.MakePair(w.OnInstantiate(), w.DontInherit()))
	})
	action2, ok2 := flecs.GetInstantiatePolicy(w, c2ID)
	if !ok2 {
		t.Fatal("GetInstantiatePolicy returned false after DontInherit pair-add")
	}
	if action2 != w.DontInherit() {
		t.Fatalf("expected DontInherit(), got %v", action2)
	}
}

// ── Test 8: Multi-level IsA chain — Override propagates through chain ─────────
//
// instance IsA prefab1 IsA prefab2; prefab2 owns Position with Override.
// After (IsA, prefab1) add, instance gets a local copy of Position.

func TestInstantiatePolicyMultiLevelChain(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	flecs.SetInstantiatePolicy(w, posID, w.Override())

	var prefab1, prefab2, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab2 = fw.NewEntity()
		flecs.Set(fw, prefab2, Position{X: 3, Y: 4})

		prefab1 = fw.NewEntity()
		fw.AddID(prefab1, flecs.MakePair(w.IsA(), prefab2))

		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), prefab1))
	})

	w.Read(func(r *flecs.Reader) {
		p, ok := flecs.Get[Position](r, inst)
		if !ok {
			t.Fatal("inst: expected Position from multi-level Override chain")
		}
		if p.X != 3 || p.Y != 4 {
			t.Fatalf("expected {3,4} from prefab2, got {%v,%v}", p.X, p.Y)
		}
		if !flecs.Owns[Position](r, inst) {
			t.Fatal("inst should own Position locally after Override copy through chain")
		}
	})
}

// ── Test: GetInstantiatePolicy returns (0, false) for unregistered component ──

func TestGetInstantiatePolicyNoPolicy(t *testing.T) {
	type Comp struct{}
	w := flecs.New()
	cID := flecs.RegisterComponent[Comp](w)

	action, ok := flecs.GetInstantiatePolicy(w, cID)
	if ok {
		t.Fatal("GetInstantiatePolicy should return false for component with no policy")
	}
	if action != 0 {
		t.Fatalf("GetInstantiatePolicy should return zero action for no policy, got %v", action)
	}
}

// ── Test: SetInstantiatePolicy with unknown action panics ─────────────────────

func TestSetInstantiatePolicyUnknownActionPanics(t *testing.T) {
	type Comp struct{}
	w := flecs.New()
	cID := flecs.RegisterComponent[Comp](w)

	var badAction flecs.ID
	w.Write(func(fw *flecs.Writer) { badAction = fw.NewEntity() })

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for unknown action, got none")
		}
	}()
	flecs.SetInstantiatePolicy(w, cID, badAction)
}

// ── Test: Override does not overwrite a pre-set value ────────────────────────
//
// If instance already owns a component locally before (IsA, prefab) is added,
// the Override copy must be skipped (user value wins).

func TestInstantiatePolicyOverrideSkipsPreSetValue(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	flecs.SetInstantiatePolicy(w, posID, w.Override())

	var prefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 10, Y: 20})

		inst = fw.NewEntity()
		// Pre-set BEFORE adding IsA.
		flecs.Set(fw, inst, Position{X: 99, Y: 88})
		fw.AddID(inst, flecs.MakePair(w.IsA(), prefab))
	})

	w.Read(func(r *flecs.Reader) {
		p, ok := flecs.Get[Position](r, inst)
		if !ok {
			t.Fatal("expected Position on inst")
		}
		if p.X != 99 || p.Y != 88 {
			t.Fatalf("pre-set value should win over Override copy; got {%v,%v}", p.X, p.Y)
		}
	})
}

// ── Test: Pair-add form for Inherit action ────────────────────────────────────

func TestInstantiatePolicyPairAddInheritEquivalent(t *testing.T) {
	type Comp struct{}
	w := flecs.New()
	cID := flecs.RegisterComponent[Comp](w)

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(cID, flecs.MakePair(w.OnInstantiate(), w.Inherit()))
	})

	action, ok := flecs.GetInstantiatePolicy(w, cID)
	if !ok {
		t.Fatal("GetInstantiatePolicy should return true after Inherit pair-add")
	}
	if action != w.Inherit() {
		t.Fatalf("expected Inherit(), got %v", action)
	}
}

// ── Test 9: Default behavior unchanged ────────────────────────────────────────
//
// A component with no policy and no SetInheritable behaves identically to the
// existing tests in isa_test.go / inheritance_test.go:
//   - Get/Has walk the IsA chain on a local miss (value visible via inheritance).
//   - No eager copy: instance does NOT own the component locally.
//   - No DontInherit suppression: the chain walk proceeds normally.

func TestInstantiatePolicyDefaultBehaviorUnchanged(t *testing.T) {
	type Velocity struct{ DX, DY float32 }

	w := flecs.New()
	// Intentionally: no SetInstantiatePolicy, no SetInheritable.
	_ = flecs.RegisterComponent[Velocity](w)

	var prefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Velocity{DX: 1, DY: 2})

		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), prefab))
	})

	w.Read(func(r *flecs.Reader) {
		// Get and Has still walk the IsA chain (default behavior in Go flecs).
		if !flecs.Has[Velocity](r, inst) {
			t.Fatal("default: Has[Velocity](inst) should return true via IsA chain walk")
		}
		v, ok := flecs.Get[Velocity](r, inst)
		if !ok || v.DX != 1 || v.DY != 2 {
			t.Fatalf("default: Get[Velocity](inst) via IsA chain: ok=%v got {%v,%v}", ok, v.DX, v.DY)
		}
		// No eager copy: instance does NOT own Velocity locally.
		if flecs.Owns[Velocity](r, inst) {
			t.Fatal("default: inst should not own Velocity locally (no Override policy set)")
		}
	})
}
