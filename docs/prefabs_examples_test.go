package docs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// TestPrefabs_Instantiate verifies the introductory snippet from PrefabsManual.md:
// create a prefab, instantiate it twice, and confirm Get retrieves the inherited value.
func TestPrefabs_Instantiate(t *testing.T) {
	type Defense struct{ Value int }

	w := flecs.New()
	flecs.RegisterComponent[Defense](w)

	var spaceship, inst1, inst2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		spaceship = fw.NewEntity()
		flecs.Set(fw, spaceship, Defense{Value: 50})

		inst1 = fw.NewEntity()
		fw.AddID(inst1, flecs.MakePair(w.IsA(), spaceship))

		inst2 = fw.NewEntity()
		fw.AddID(inst2, flecs.MakePair(w.IsA(), spaceship))
	})

	w.Read(func(r *flecs.Reader) {
		d1, ok1 := flecs.Get[Defense](r, inst1)
		if !ok1 {
			t.Fatal("Get[Defense](inst1) should find inherited value")
		}
		if d1.Value != 50 {
			t.Errorf("inst1 Defense.Value = %d, want 50", d1.Value)
		}

		d2, ok2 := flecs.Get[Defense](r, inst2)
		if !ok2 {
			t.Fatal("Get[Defense](inst2) should find inherited value")
		}
		if d2.Value != 50 {
			t.Errorf("inst2 Defense.Value = %d, want 50", d2.Value)
		}
	})
}

// TestPrefabs_SetInheritableQuery verifies the SetInheritable snippet from PrefabsManual.md:
// after marking Defense as inheritable, Each1 matches both the prefab and its instance.
func TestPrefabs_SetInheritableQuery(t *testing.T) {
	type Defense struct{ Value int }

	w := flecs.New()
	flecs.RegisterComponent[Defense](w)
	flecs.SetInheritable[Defense](w)

	var spaceship, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		spaceship = fw.NewEntity()
		flecs.Set(fw, spaceship, Defense{Value: 50})

		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), spaceship))
	})

	var found []flecs.ID
	w.Read(func(r *flecs.Reader) {
		flecs.Each1[Defense](r, func(e flecs.ID, _ *Defense) {
			found = append(found, e)
		})
	})

	sawSpaceship, sawInst := false, false
	for _, e := range found {
		if e == spaceship {
			sawSpaceship = true
		}
		if e == inst {
			sawInst = true
		}
	}
	if !sawSpaceship {
		t.Error("Each1[Defense] should match spaceship (local owner)")
	}
	if !sawInst {
		t.Error("Each1[Defense] should match inst (inheritor via SetInheritable)")
	}
}

// TestPrefabs_OwnsCheck verifies the Owns snippet from PrefabsManual.md:
// an instance that inherits Defense does not locally own it.
func TestPrefabs_OwnsCheck(t *testing.T) {
	type Defense struct{ Value int }

	w := flecs.New()
	flecs.RegisterComponent[Defense](w)

	var spaceship, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		spaceship = fw.NewEntity()
		flecs.Set(fw, spaceship, Defense{Value: 50})
		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), spaceship))
	})

	w.Read(func(r *flecs.Reader) {
		ownsLocal := flecs.Owns[Defense](r, inst)
		if ownsLocal {
			t.Error("Owns[Defense](inst) should be false — component is inherited, not owned")
		}
		ownsOnPrefab := flecs.Owns[Defense](r, spaceship)
		if !ownsOnPrefab {
			t.Error("Owns[Defense](spaceship) should be true — prefab locally owns Defense")
		}
	})
}

// TestPrefabs_CopyOnWriteOverride verifies the copy-on-write snippet from PrefabsManual.md:
// Set on instA creates a local override; instB still sees the prefab's value.
func TestPrefabs_CopyOnWriteOverride(t *testing.T) {
	type Defense struct{ Value int }

	w := flecs.New()
	flecs.RegisterComponent[Defense](w)
	flecs.SetInheritable[Defense](w)

	var spaceship, instA, instB flecs.ID
	w.Write(func(fw *flecs.Writer) {
		spaceship = fw.NewEntity()
		flecs.Set(fw, spaceship, Defense{Value: 50})

		instA = fw.NewEntity()
		fw.AddID(instA, flecs.MakePair(w.IsA(), spaceship))

		instB = fw.NewEntity()
		fw.AddID(instB, flecs.MakePair(w.IsA(), spaceship))
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, instA, Defense{Value: 75})
	})

	w.Read(func(r *flecs.Reader) {
		dA, _ := flecs.Get[Defense](r, instA)
		dB, _ := flecs.Get[Defense](r, instB)

		if dA.Value != 75 {
			t.Errorf("instA Defense.Value = %d, want 75 (local override)", dA.Value)
		}
		if dB.Value != 50 {
			t.Errorf("instB Defense.Value = %d, want 50 (still inherited)", dB.Value)
		}
		if !flecs.Owns[Defense](r, instA) {
			t.Error("instA should own Defense after Set")
		}
		if flecs.Owns[Defense](r, instB) {
			t.Error("instB should not own Defense (still inherited)")
		}
	})
}

// TestPrefabs_RestoreInheritance verifies the Remove-to-restore snippet from PrefabsManual.md:
// removing the local override re-exposes the prefab's value via Get.
func TestPrefabs_RestoreInheritance(t *testing.T) {
	type Defense struct{ Value int }

	w := flecs.New()
	flecs.RegisterComponent[Defense](w)

	var spaceship, instA flecs.ID
	w.Write(func(fw *flecs.Writer) {
		spaceship = fw.NewEntity()
		flecs.Set(fw, spaceship, Defense{Value: 50})
		instA = fw.NewEntity()
		fw.AddID(instA, flecs.MakePair(w.IsA(), spaceship))
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, instA, Defense{Value: 75})
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[Defense](fw, instA)
	})

	w.Read(func(r *flecs.Reader) {
		d, ok := flecs.Get[Defense](r, instA)
		if !ok {
			t.Fatal("Get[Defense](instA) should still find value after Remove (inherits from prefab)")
		}
		if d.Value != 50 {
			t.Errorf("after Remove: instA Defense.Value = %d, want 50 (restored from prefab)", d.Value)
		}
		if flecs.Owns[Defense](r, instA) {
			t.Error("instA should not own Defense after Remove")
		}
	})
}

// TestPrefabs_Variant verifies the prefab-variant snippet from PrefabsManual.md:
// an instance of a variant prefab sees the variant's override and the base's other components.
func TestPrefabs_Variant(t *testing.T) {
	type Health struct{ HP int }
	type Defense struct{ Value int }

	w := flecs.New()
	flecs.RegisterComponent[Health](w)
	flecs.RegisterComponent[Defense](w)

	var spaceship, freighter, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		spaceship = fw.NewEntity()
		flecs.Set(fw, spaceship, Defense{Value: 50})
		flecs.Set(fw, spaceship, Health{HP: 100})

		freighter = fw.NewEntity()
		fw.AddID(freighter, flecs.MakePair(w.IsA(), spaceship))
		flecs.Set(fw, freighter, Health{HP: 150})

		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), freighter))
	})

	w.Read(func(r *flecs.Reader) {
		h, okH := flecs.Get[Health](r, inst)
		d, okD := flecs.Get[Defense](r, inst)

		if !okH {
			t.Fatal("Get[Health](inst) should succeed via variant chain")
		}
		if !okD {
			t.Fatal("Get[Defense](inst) should succeed via base prefab chain")
		}
		if h.HP != 150 {
			t.Errorf("inst Health.HP = %d, want 150 (from freighter override)", h.HP)
		}
		if d.Value != 50 {
			t.Errorf("inst Defense.Value = %d, want 50 (from spaceship via freighter)", d.Value)
		}
	})
}

// TestPrefabs_PrefabOf verifies the PrefabOf snippet from PrefabsManual.md:
// PrefabOf returns the first direct IsA target; returns (0, false) for entities with no IsA.
func TestPrefabs_PrefabOf(t *testing.T) {
	w := flecs.New()

	var spaceship, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		spaceship = fw.NewEntity()
		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), spaceship))
	})

	w.Read(func(r *flecs.Reader) {
		prefab, ok := flecs.PrefabOf(r, inst)
		if !ok {
			t.Fatal("PrefabOf(inst) should return ok=true")
		}
		if prefab != spaceship {
			t.Errorf("PrefabOf(inst) = %d, want spaceship (%d)", prefab, spaceship)
		}

		_, ok2 := flecs.PrefabOf(r, spaceship)
		if ok2 {
			t.Error("PrefabOf(spaceship) should return ok=false (no IsA)")
		}
	})
}

// TestPrefabs_EachPrefab verifies the EachPrefab snippet from PrefabsManual.md:
// EachPrefab iterates direct IsA targets only; early exit works.
func TestPrefabs_EachPrefab(t *testing.T) {
	w := flecs.New()

	var base, variant, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		base = fw.NewEntity()
		variant = fw.NewEntity()
		fw.AddID(variant, flecs.MakePair(w.IsA(), base))
		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), variant))
	})

	w.Read(func(r *flecs.Reader) {
		// inst has one direct prefab: variant
		var direct []flecs.ID
		r.EachPrefab(inst, func(p flecs.ID) bool {
			direct = append(direct, p)
			return true
		})
		if len(direct) != 1 {
			t.Fatalf("EachPrefab(inst): want 1 direct prefab, got %d", len(direct))
		}
		if direct[0] != variant {
			t.Errorf("EachPrefab(inst): got %d, want variant (%d)", direct[0], variant)
		}

		// base should not appear — EachPrefab is direct only
		for _, p := range direct {
			if p == base {
				t.Error("EachPrefab should not yield transitive ancestor base")
			}
		}

		// early exit: stop after first
		count := 0
		r.EachPrefab(inst, func(p flecs.ID) bool {
			count++
			return false
		})
		if count != 1 {
			t.Errorf("early exit: want 1 iteration, got %d", count)
		}
	})
}

// TestPrefabs_GetUp verifies the GetUp snippet from PrefabsManual.md:
// GetUp[T] with w.IsA() traverses the IsA chain to find the component owner.
func TestPrefabs_GetUp(t *testing.T) {
	type Defense struct{ Value int }

	w := flecs.New()
	flecs.RegisterComponent[Defense](w)

	var spaceship, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		spaceship = fw.NewEntity()
		flecs.Set(fw, spaceship, Defense{Value: 50})
		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), spaceship))
	})

	// Entity with no IsA and no local Defense.
	var lone flecs.ID
	w.Write(func(fw *flecs.Writer) {
		lone = fw.NewEntity()
	})

	w.Read(func(r *flecs.Reader) {
		d, ok := flecs.GetUp[Defense](r, inst, w.IsA())
		if !ok {
			t.Fatal("GetUp[Defense](inst, IsA) should find value via IsA chain")
		}
		if d.Value != 50 {
			t.Errorf("GetUp Defense.Value = %d, want 50", d.Value)
		}

		_, okLone := flecs.GetUp[Defense](r, lone, w.IsA())
		if okLone {
			t.Error("GetUp on entity with no IsA and no Defense should return ok=false")
		}
	})
}

// TestPrefabs_OverridePolicy verifies the Override code block from PrefabsManual.md §Override.
func TestPrefabs_OverridePolicy(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	flecs.SetInstantiatePolicy(w, posID, w.Override())

	var prefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 10, Y: 20})

		inst = fw.NewEntity()
		fw.AddID(inst, flecs.MakePair(w.IsA(), prefab))
	})

	w.Read(func(r *flecs.Reader) {
		p, ok := flecs.Get[Position](r, inst)
		if !ok {
			t.Fatal("inst: expected Position after Override copy")
		}
		if p.X != 10 || p.Y != 20 {
			t.Fatalf("inst: expected {10,20}, got {%v,%v}", p.X, p.Y)
		}
		if !flecs.Owns[Position](r, inst) {
			t.Fatal("inst: should own Position locally (Override policy)")
		}
	})
}

// TestPrefabs_DontInheritPolicy verifies the DontInherit code block from PrefabsManual.md §DontInherit.
func TestPrefabs_DontInheritPolicy(t *testing.T) {
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
		has := flecs.Has[Secret](r, inst)
		if has {
			t.Fatal("inst: Has[Secret] should return false (DontInherit policy)")
		}
	})
}
