package docs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// TestComponentTraits_InheritableQueryMatchesPrefab verifies that marking a
// component as Inheritable causes Each1 to yield an entity that inherits the
// component from a prefab via IsA rather than owning it directly.
func TestComponentTraits_InheritableQueryMatchesPrefab(t *testing.T) {
	type Mass struct{ Value float32 }

	w := flecs.New()
	flecs.RegisterComponent[Mass](w)
	flecs.SetInheritable[Mass](w)

	var base, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		base = fw.NewEntity()
		flecs.Set(fw, base, Mass{Value: 100})

		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), base))
	})

	var found []flecs.ID
	w.Read(func(r *flecs.Reader) {
		flecs.Each1[Mass](r, func(e flecs.ID, _ *Mass) {
			found = append(found, e)
		})
	})

	sawInst := false
	for _, e := range found {
		if e == inst {
			sawInst = true
		}
	}
	if !sawInst {
		t.Fatalf("Each1[Mass] did not yield inheriting instance (got %v)", found)
	}
}

// TestComponentTraits_InheritableValueFromBase verifies that the component
// pointer passed to Each1 for an inheriting entity points to the base's value.
func TestComponentTraits_InheritableValueFromBase(t *testing.T) {
	type Mass struct{ Value float32 }

	w := flecs.New()
	flecs.RegisterComponent[Mass](w)
	flecs.SetInheritable[Mass](w)

	var base, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		base = fw.NewEntity()
		flecs.Set(fw, base, Mass{Value: 42})

		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), base))
	})

	var got Mass
	var found bool
	w.Read(func(r *flecs.Reader) {
		flecs.Each1[Mass](r, func(e flecs.ID, m *Mass) {
			if e == inst {
				got = *m
				found = true
			}
		})
	})

	if !found {
		t.Fatal("instance not visited by Each1")
	}
	if got.Value != 42 {
		t.Errorf("inherited Mass.Value = %v, want 42", got.Value)
	}
}

// TestComponentTraits_InheritableNotMatchedWithoutFlag verifies that without
// SetInheritable, Each1 does not yield the inheriting instance.
func TestComponentTraits_InheritableNotMatchedWithoutFlag(t *testing.T) {
	type Speed struct{ Value float32 }

	w := flecs.New()
	flecs.RegisterComponent[Speed](w)
	// NOTE: SetInheritable NOT called

	var base, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		base = fw.NewEntity()
		flecs.Set(fw, base, Speed{Value: 10})

		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), base))
	})

	var found []flecs.ID
	w.Read(func(r *flecs.Reader) {
		flecs.Each1[Speed](r, func(e flecs.ID, _ *Speed) {
			found = append(found, e)
		})
	})

	for _, e := range found {
		if e == inst {
			t.Errorf("instance unexpectedly matched by Each1 without SetInheritable")
		}
	}
}

// TestComponentTraits_InheritableByID verifies the w.SetInheritable(cid) form.
func TestComponentTraits_InheritableByID(t *testing.T) {
	type Armor struct{ Value int }

	w := flecs.New()
	cid := flecs.RegisterComponent[Armor](w)
	w.SetInheritable(cid) // register by entity ID rather than generic type

	var base, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		base = fw.NewEntity()
		flecs.Set(fw, base, Armor{Value: 50})

		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), base))
	})

	var sawInst bool
	w.Read(func(r *flecs.Reader) {
		flecs.Each1[Armor](r, func(e flecs.ID, _ *Armor) {
			if e == inst {
				sawInst = true
			}
		})
	})

	if !sawInst {
		t.Fatal("Each1[Armor] did not match inheriting instance after w.SetInheritable(cid)")
	}
}

// TestComponentTraits_OnInstantiateIDsNonZero verifies that the four built-in
// trait entity IDs are allocated and non-zero.
func TestComponentTraits_OnInstantiateIDsNonZero(t *testing.T) {
	w := flecs.New()
	if w.OnInstantiate() == 0 {
		t.Error("OnInstantiate ID should be non-zero")
	}
	if w.Inherit() == 0 {
		t.Error("Inherit ID should be non-zero")
	}
	if w.Override() == 0 {
		t.Error("Override ID should be non-zero")
	}
	if w.DontInherit() == 0 {
		t.Error("DontInherit ID should be non-zero")
	}
}

// TestComponentTraits_OnInstantiateIDsDistinct verifies that the four built-in
// trait entity IDs are all distinct from each other and from IsA/ChildOf.
func TestComponentTraits_OnInstantiateIDsDistinct(t *testing.T) {
	w := flecs.New()
	ids := map[flecs.ID]string{
		w.OnInstantiate(): "OnInstantiate",
		w.Inherit():       "Inherit",
		w.Override():      "Override",
		w.DontInherit():   "DontInherit",
		w.IsA():           "IsA",
		w.ChildOf():       "ChildOf",
	}
	if len(ids) != 6 {
		t.Errorf("expected 6 distinct built-in entity IDs, got %d (some share the same ID)", len(ids))
	}
}

// TestComponentTraits_InheritableGetFollowsIsA verifies that Get[T] follows the
// IsA chain for an inheritable component even without a query.
func TestComponentTraits_InheritableGetFollowsIsA(t *testing.T) {
	type Health struct{ Value int }

	w := flecs.New()
	flecs.RegisterComponent[Health](w)
	flecs.SetInheritable[Health](w)

	var base, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		base = fw.NewEntity()
		flecs.Set(fw, base, Health{Value: 200})

		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), base))
	})

	w.Read(func(r *flecs.Reader) {
		h, ok := flecs.Get[Health](r, inst)
		if !ok {
			t.Fatal("Get[Health] returned false for inheriting instance")
		}
		if h.Value != 200 {
			t.Errorf("inherited Health.Value = %d, want 200", h.Value)
		}
	})
}

// TestComponentTraits_InheritableCopyOnWriteOverride verifies that Set on an
// instance creates a local copy that shadows the base value.
func TestComponentTraits_InheritableCopyOnWriteOverride(t *testing.T) {
	type Shield struct{ Value int }

	w := flecs.New()
	flecs.RegisterComponent[Shield](w)
	flecs.SetInheritable[Shield](w)

	var base, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		base = fw.NewEntity()
		flecs.Set(fw, base, Shield{Value: 30})

		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), base))
	})

	// Override on the instance
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, inst, Shield{Value: 99})
	})

	w.Read(func(r *flecs.Reader) {
		instShield, ok := flecs.Get[Shield](r, inst)
		if !ok {
			t.Fatal("Get[Shield] returned false for instance after override")
		}
		if instShield.Value != 99 {
			t.Errorf("instance Shield after override = %d, want 99", instShield.Value)
		}

		baseShield, ok := flecs.Get[Shield](r, base)
		if !ok {
			t.Fatal("Get[Shield] returned false for base")
		}
		if baseShield.Value != 30 {
			t.Errorf("base Shield after instance override = %d, want 30 (should be unchanged)", baseShield.Value)
		}
	})
}
