package flecs_test

import (
	"testing"

	flecs "github.com/snichols/flecs"
)

// shared position type for DontFragment tests
type dfPos struct{ X, Y int }

// TestDontFragment_BuiltinIndex verifies that DontFragment is at index 35.
func TestDontFragment_BuiltinIndex(t *testing.T) {
	w := flecs.New()
	got := w.DontFragment().Index()
	if got != 35 {
		t.Errorf("DontFragment index: want 35, got %d", got)
	}
}

// TestDontFragment_NoArchetypeTransition verifies that SetDontFragment alone
// suppresses archetype transitions: the entity stays in its current table.
func TestDontFragment_NoArchetypeTransition(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[dfPos](w)
	flecs.SetDontFragment(w, posID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	tablesBefore := w.TablesFor(posID)

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, dfPos{X: 1, Y: 2})
	})

	// DontFragment: no new archetype table is created.
	tablesAfter := w.TablesFor(posID)
	if len(tablesAfter) != len(tablesBefore) {
		t.Errorf("DontFragment Set created an archetype table: before=%d, after=%d",
			len(tablesBefore), len(tablesAfter))
	}

	// But the value is accessible.
	w.Read(func(r *flecs.Reader) {
		pos, ok := flecs.Get[dfPos](r, e)
		if !ok {
			t.Fatal("Get: expected ok=true for DontFragment component")
		}
		if pos.X != 1 || pos.Y != 2 {
			t.Errorf("Get: got (%d,%d), want (1,2)", pos.X, pos.Y)
		}
	})
}

// TestDontFragment_SparseOnly_DoesArchetypeTransition verifies that Sparse alone
// (without DontFragment) DOES cause an archetype transition in v0.53.0.
func TestDontFragment_SparseOnly_DoesArchetypeTransition(t *testing.T) {
	w := flecs.New()
	type sparseOnly struct{ V int }
	posID := flecs.RegisterComponent[sparseOnly](w)
	flecs.SetSparse(w, posID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	tablesBefore := w.TablesFor(posID)

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, sparseOnly{V: 42})
	})

	// Sparse-only in v0.53.0: a new archetype table IS created.
	tablesAfter := w.TablesFor(posID)
	if len(tablesAfter) <= len(tablesBefore) {
		t.Errorf("Sparse-only Set should create an archetype table: before=%d, after=%d",
			len(tablesBefore), len(tablesAfter))
	}
}

// TestDontFragment_SparsePlusDontFragment_OldBehavior verifies that Sparse+DontFragment
// together matches v0.51.0–v0.52.0 behavior: data in sparse-set, no archetype transition.
func TestDontFragment_SparsePlusDontFragment_OldBehavior(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[dfPos](w)
	flecs.SetSparse(w, posID)
	flecs.SetDontFragment(w, posID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	tablesBefore := w.TablesFor(posID)

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, dfPos{X: 10, Y: 20})
	})

	// Sparse+DontFragment: no archetype transition (matches old Sparse behavior).
	tablesAfter := w.TablesFor(posID)
	if len(tablesAfter) != len(tablesBefore) {
		t.Errorf("Sparse+DontFragment should not create archetype table: before=%d, after=%d",
			len(tablesBefore), len(tablesAfter))
	}

	// Value accessible via Get.
	w.Read(func(r *flecs.Reader) {
		pos, ok := flecs.Get[dfPos](r, e)
		if !ok {
			t.Fatal("Get: expected ok=true for Sparse+DontFragment component")
		}
		if pos.X != 10 || pos.Y != 20 {
			t.Errorf("Get: got (%d,%d), want (10,20)", pos.X, pos.Y)
		}
	})
}

// TestDontFragment_IsDontFragment verifies the IsDontFragment predicate.
func TestDontFragment_IsDontFragment(t *testing.T) {
	w := flecs.New()
	type cA struct{ V int }
	type cB struct{ V int }
	aID := flecs.RegisterComponent[cA](w)
	bID := flecs.RegisterComponent[cB](w)
	flecs.SetDontFragment(w, aID)

	w.Read(func(r *flecs.Reader) {
		if !flecs.IsDontFragment(r, aID) {
			t.Error("IsDontFragment(aID): expected true")
		}
		if flecs.IsDontFragment(r, bID) {
			t.Error("IsDontFragment(bID): expected false")
		}
	})
}

// TestDontFragment_HasIDAndOwnsID verifies HasID and OwnsID consult the sparse-set.
func TestDontFragment_HasIDAndOwnsID(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[dfPos](w)
	flecs.SetDontFragment(w, posID)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.Set(fw, e1, dfPos{X: 3, Y: 4})
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, e1, posID) {
			t.Error("HasID(e1, posID): expected true")
		}
		if flecs.HasID(r, e2, posID) {
			t.Error("HasID(e2, posID): expected false")
		}
		if !flecs.OwnsID(r, e1, posID) {
			t.Error("OwnsID(e1, posID): expected true")
		}
		if flecs.OwnsID(r, e2, posID) {
			t.Error("OwnsID(e2, posID): expected false")
		}
	})
}

// TestDontFragment_Remove verifies Remove on a DontFragment component cleans up the sparse-set.
func TestDontFragment_Remove(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[dfPos](w)
	flecs.SetDontFragment(w, posID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, dfPos{X: 5, Y: 6})
	})

	tablesBefore := w.TablesFor(posID)

	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[dfPos](fw, e)
	})

	// No archetype change (component was never in archetype).
	tablesAfter := w.TablesFor(posID)
	if len(tablesAfter) != len(tablesBefore) {
		t.Errorf("DontFragment Remove changed table count: before=%d, after=%d",
			len(tablesBefore), len(tablesAfter))
	}

	w.Read(func(r *flecs.Reader) {
		if flecs.HasID(r, e, posID) {
			t.Error("HasID: expected false after Remove on DontFragment component")
		}
		_, ok := flecs.Get[dfPos](r, e)
		if ok {
			t.Error("Get: expected ok=false after Remove on DontFragment component")
		}
	})
}

// TestDontFragment_AfterUsePanic verifies that SetDontFragment panics if the
// component already has entities.
func TestDontFragment_AfterUsePanic(t *testing.T) {
	w := flecs.New()
	type used struct{ V int }
	posID := flecs.RegisterComponent[used](w)

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, used{V: 1})
	})

	defer func() {
		if r := recover(); r == nil {
			t.Error("SetDontFragment on in-use component: expected panic, got none")
		}
	}()
	flecs.SetDontFragment(w, posID)
}

// TestDontFragment_QueryIntegration verifies that a DontFragment component is
// found by a query even though it never appears in an archetype table.
func TestDontFragment_QueryIntegration(t *testing.T) {
	w := flecs.New()
	type dfComp struct{ Score int }
	compID := flecs.RegisterComponent[dfComp](w)
	flecs.SetDontFragment(w, compID)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.Set(fw, e1, dfComp{Score: 10})
		flecs.Set(fw, e2, dfComp{Score: 20})
	})

	q := flecs.NewQuery(w, compID)
	var visited []flecs.ID
	q.Each(func(it *flecs.QueryIter) {
		visited = append(visited, it.Entities()...)
	})

	if len(visited) != 2 {
		t.Fatalf("Query visited %d entities, want 2; e1=%v e2=%v", len(visited), e1, e2)
	}
}

// TestDontFragment_MarshalRoundtrip verifies marshal/unmarshal roundtrip for
// a DontFragment-only component (data not in entity archetype body).
func TestDontFragment_MarshalRoundtrip(t *testing.T) {
	type dfData struct{ Value int }

	w1 := flecs.New()
	dfID := flecs.RegisterComponent[dfData](w1)
	flecs.SetDontFragment(w1, dfID)

	w1.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, dfData{Value: 99})
	})

	data, err := w1.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	w2 := flecs.New()
	flecs.RegisterComponent[dfData](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	// EachSparse works on the DontFragment sparse-set (same backing storage).
	found := false
	w2.Read(func(r *flecs.Reader) {
		flecs.EachSparse[dfData](r, func(_ flecs.ID, d *dfData) {
			if d.Value == 99 {
				found = true
			}
		})
	})
	if !found {
		t.Error("MarshalJSON roundtrip: DontFragment value 99 not found after unmarshal")
	}
}

// TestDontFragment_BareTagAddID verifies that fw.AddID(posID, w.DontFragment())
// applies the DontFragment policy (mirror of C ecs_add_id(world, ecs_id(T), EcsDontFragment)).
func TestDontFragment_BareTagAddID(t *testing.T) {
	w := flecs.New()
	type dfTag struct{ N int }
	posID := flecs.RegisterComponent[dfTag](w)

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(posID, w.DontFragment())
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.IsDontFragment(r, posID) {
			t.Error("IsDontFragment: expected true after bare AddID(posID, DontFragment())")
		}
	})

	// Now set a value — must go to sparse-set, no archetype transition.
	tablesBefore := w.TablesFor(posID)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, dfTag{N: 7})
	})
	tablesAfter := w.TablesFor(posID)
	if len(tablesAfter) != len(tablesBefore) {
		t.Errorf("bare-tag DontFragment: Set created archetype table: before=%d, after=%d",
			len(tablesBefore), len(tablesAfter))
	}

	w.Read(func(r *flecs.Reader) {
		v, ok := flecs.Get[dfTag](r, e)
		if !ok {
			t.Fatal("Get: expected ok=true after bare-tag DontFragment Set")
		}
		if v.N != 7 {
			t.Errorf("Get: want N=7, got N=%d", v.N)
		}
	})
}
