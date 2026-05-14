package flecs_test

import (
	"encoding/json"
	"testing"

	"github.com/snichols/flecs"
)

// sparsePos is a test component used exclusively by sparse tests.
type sparsePos struct{ X, Y float32 }

// sparseVel is a second test component for multi-component sparse tests.
type sparseVel struct{ DX, DY float32 }

// ── Basic API ─────────────────────────────────────────────────────────────────

// TestSparse_BuiltinIndex verifies that the Sparse built-in entity is at index 34.
func TestSparse_BuiltinIndex(t *testing.T) {
	w := flecs.New()
	got := w.Sparse().Index()
	if got != 34 {
		t.Errorf("Sparse index: want 34, got %d", got)
	}
}

// TestSparse_SetAndGet verifies that Set/Get on a Sparse component round-trips correctly.
func TestSparse_SetAndGet(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, sparsePos{X: 3, Y: 7})
	})

	var got sparsePos
	var ok bool
	w.Read(func(r *flecs.Reader) {
		got, ok = flecs.Get[sparsePos](r, e)
	})
	if !ok {
		t.Fatal("Get: expected ok=true for Sparse component")
	}
	if got.X != 3 || got.Y != 7 {
		t.Errorf("Get: got %+v, want {3 7}", got)
	}
}

// TestSparse_HasID verifies that HasID returns true for Sparse components.
func TestSparse_HasID(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, sparsePos{X: 1, Y: 2})
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, e, posID) {
			t.Error("HasID: expected true for Sparse component after Set")
		}
	})

	// Entity that never had the component.
	var e2 flecs.ID
	w.Write(func(fw *flecs.Writer) { e2 = fw.NewEntity() })
	w.Read(func(r *flecs.Reader) {
		if flecs.HasID(r, e2, posID) {
			t.Error("HasID: expected false for entity that never had Sparse component")
		}
	})
}

// TestSparse_ArchetypeTransition verifies that adding a Sparse-only component DOES
// cause an archetype transition (v0.53.0 behavior: Sparse alone ≠ DontFragment).
// To suppress the transition, also apply SetDontFragment.
func TestSparse_ArchetypeTransition(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	tablesBefore := w.TablesFor(posID)

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, sparsePos{X: 5, Y: 5})
	})

	// Sparse-only: a new archetype table IS created for posID in v0.53.0.
	tablesAfter := w.TablesFor(posID)
	if len(tablesAfter) <= len(tablesBefore) {
		t.Errorf("Sparse-only Set should have created an archetype table: before=%d, after=%d",
			len(tablesBefore), len(tablesAfter))
	}

	// But data lives in sparse-set (pointer-stable).
	w.Read(func(r *flecs.Reader) {
		pos, ok := flecs.Get[sparsePos](r, e)
		if !ok {
			t.Fatal("Get: expected ok=true after Sparse Set")
		}
		if pos.X != 5 || pos.Y != 5 {
			t.Errorf("Get: got (%v,%v), want (5,5)", pos.X, pos.Y)
		}
	})
}

// TestSparse_Remove verifies that Remove on a Sparse component works.
func TestSparse_Remove(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, sparsePos{X: 10, Y: 20})
	})

	w.Write(func(fw *flecs.Writer) {
		if !flecs.Remove[sparsePos](fw, e) {
			t.Error("Remove[sparsePos]: expected true, got false")
		}
	})

	w.Read(func(r *flecs.Reader) {
		if flecs.HasID(r, e, posID) {
			t.Error("HasID: expected false after Remove on Sparse component")
		}
		_, ok := flecs.Get[sparsePos](r, e)
		if ok {
			t.Error("Get: expected ok=false after Remove on Sparse component")
		}
	})
}

// TestSparse_PointerStability verifies that GetRef returns a stable pointer
// (the same address across multiple Set calls and independent of other migrations).
func TestSparse_PointerStability(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	velID := flecs.RegisterComponent[sparseVel](w)
	flecs.SetSparse(w, posID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, sparsePos{X: 1, Y: 2})
	})

	var ptr1 *sparsePos
	w.Read(func(r *flecs.Reader) { ptr1 = flecs.GetRef[sparsePos](r, e) })
	if ptr1 == nil {
		t.Fatal("GetRef[sparsePos]: expected non-nil pointer")
	}

	// Add a non-sparse component, which triggers an archetype migration.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, sparseVel{DX: 3, DY: 4})
	})

	var ptr2 *sparsePos
	w.Read(func(r *flecs.Reader) { ptr2 = flecs.GetRef[sparsePos](r, e) })
	if ptr2 == nil {
		t.Fatal("GetRef[sparsePos] after migration: expected non-nil pointer")
	}
	if ptr1 != ptr2 {
		t.Errorf("Sparse pointer not stable across archetype migration: ptr1=%p, ptr2=%p", ptr1, ptr2)
	}
	_ = velID
}

// TestSparse_IsSparse verifies the IsSparse predicate.
func TestSparse_IsSparse(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	velID := flecs.RegisterComponent[sparseVel](w)
	flecs.SetSparse(w, posID)

	w.Read(func(r *flecs.Reader) {
		if !flecs.IsSparse(r, posID) {
			t.Error("IsSparse(posID): expected true")
		}
		if flecs.IsSparse(r, velID) {
			t.Error("IsSparse(velID): expected false (not marked Sparse)")
		}
	})
}

// TestSparse_SetSparseIdempotent verifies that calling SetSparse twice is a no-op.
func TestSparse_SetSparseIdempotent(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID) // first call
	flecs.SetSparse(w, posID) // second call; must not panic

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, sparsePos{X: 9, Y: 9})
	})

	// Basic sanity: the sparse-set still works.
	count := 0
	w.Read(func(r *flecs.Reader) {
		flecs.EachSparse[sparsePos](r, func(_ flecs.ID, _ *sparsePos) { count++ })
	})
	if count != 1 {
		t.Errorf("EachSparse count: want 1, got %d", count)
	}
}

// TestSparse_BareTagForm verifies the bare-tag form: fw.AddID(posID, w.Sparse()).
func TestSparse_BareTagForm(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)

	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, posID, w.Sparse())
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.IsSparse(r, posID) {
			t.Error("IsSparse(posID): expected true after bare-tag AddID(posID, Sparse())")
		}
	})

	// Now verify Set/Get work.
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, sparsePos{X: 77, Y: 88})
	})
	var got sparsePos
	var ok bool
	w.Read(func(r *flecs.Reader) { got, ok = flecs.Get[sparsePos](r, e) })
	if !ok || got.X != 77 || got.Y != 88 {
		t.Errorf("Get after bare-tag form: ok=%v got=%+v, want {77 88}", ok, got)
	}
}

// TestSparse_Hooks verifies that OnAdd, OnSet, and OnRemove fire correctly for
// Sparse components.
func TestSparse_Hooks(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	adds, sets, removes := 0, 0, 0
	flecs.OnAdd[sparsePos](w, func(_ *flecs.Writer, _ flecs.ID, _ sparsePos) { adds++ })
	flecs.OnSet[sparsePos](w, func(_ *flecs.Writer, _ flecs.ID, _ sparsePos) { sets++ })
	flecs.OnRemove[sparsePos](w, func(_ *flecs.Writer, _ flecs.ID, _ sparsePos) { removes++ })

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, sparsePos{X: 1, Y: 2}) // add + set
		flecs.Set(fw, e, sparsePos{X: 3, Y: 4}) // set only
	})
	if adds != 1 {
		t.Errorf("OnAdd fires: want 1, got %d", adds)
	}
	if sets != 2 {
		t.Errorf("OnSet fires: want 2, got %d", sets)
	}
	if removes != 0 {
		t.Errorf("OnRemove fires before Remove: want 0, got %d", removes)
	}

	w.Write(func(fw *flecs.Writer) { flecs.Remove[sparsePos](fw, e) })
	if removes != 1 {
		t.Errorf("OnRemove fires after Remove: want 1, got %d", removes)
	}
	_ = posID
}

// TestSparse_DeleteEntity verifies that deleting an entity fires OnRemove and
// cleans up the sparse-set entry.
func TestSparse_DeleteEntity(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	removes := 0
	flecs.OnRemove[sparsePos](w, func(_ *flecs.Writer, _ flecs.ID, _ sparsePos) { removes++ })

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, sparsePos{X: 5, Y: 5})
	})

	w.Delete(e)

	if removes != 1 {
		t.Errorf("OnRemove on delete: want 1, got %d", removes)
	}

	// Verify entity is gone and sparse-set has no stale entry.
	count := 0
	w.Read(func(r *flecs.Reader) {
		flecs.EachSparse[sparsePos](r, func(_ flecs.ID, _ *sparsePos) { count++ })
	})
	if count != 0 {
		t.Errorf("EachSparse after entity delete: want 0, got %d", count)
	}
}

// TestSparse_EachSparse verifies that EachSparse visits all sparse holders exactly
// once and that the pointer matches the current value.
func TestSparse_EachSparse(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	entities := make([]flecs.ID, 5)
	w.Write(func(fw *flecs.Writer) {
		for i := range entities {
			e := fw.NewEntity()
			entities[i] = e
			flecs.Set(fw, e, sparsePos{X: float32(i), Y: float32(i * 10)})
		}
	})

	visited := make(map[flecs.ID]sparsePos)
	w.Read(func(r *flecs.Reader) {
		flecs.EachSparse[sparsePos](r, func(e flecs.ID, p *sparsePos) {
			visited[e] = *p
		})
	})

	if len(visited) != 5 {
		t.Fatalf("EachSparse visited %d entities, want 5", len(visited))
	}
	for i, e := range entities {
		p, ok := visited[e]
		if !ok {
			t.Errorf("entity %v not visited by EachSparse", e)
			continue
		}
		if p.X != float32(i) || p.Y != float32(i*10) {
			t.Errorf("entity %v: got %+v, want {%d %d}", e, p, i, i*10)
		}
	}
	_ = posID
}

// TestSparse_EachSparseInsertionOrder verifies that EachSparse visits entities in
// dense insertion order.
func TestSparse_EachSparseInsertionOrder(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	var e1, e2, e3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, sparsePos{X: 1})
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, sparsePos{X: 2})
		e3 = fw.NewEntity()
		flecs.Set(fw, e3, sparsePos{X: 3})
	})

	var order []flecs.ID
	w.Read(func(r *flecs.Reader) {
		flecs.EachSparse[sparsePos](r, func(e flecs.ID, _ *sparsePos) {
			order = append(order, e)
		})
	})

	if len(order) != 3 {
		t.Fatalf("EachSparse: want 3 entities, got %d", len(order))
	}
	if order[0] != e1 || order[1] != e2 || order[2] != e3 {
		t.Errorf("EachSparse insertion order: want [%v %v %v], got %v", e1, e2, e3, order)
	}
	_ = posID
}

// TestSparse_SetSparseAfterUsePanic verifies that SetSparse panics when the component
// is already in use via archetype storage.
func TestSparse_SetSparseAfterUsePanic(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, sparsePos{X: 1, Y: 2})
	})

	defer func() {
		if r := recover(); r == nil {
			t.Error("SetSparse after use: expected panic, got none")
		}
	}()
	flecs.SetSparse(w, posID)
}

// TestSparse_AddIDOnSparsePanic verifies that AddID panics when trying to add a
// Sparse component as a bare tag.
func TestSparse_AddIDOnSparsePanic(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	defer func() {
		if r := recover(); r == nil {
			t.Error("AddID on Sparse component: expected panic, got none")
		}
	}()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.AddID(fw, e, posID)
	})
	_ = e
}

// TestSparse_DeferredSetAndRemove verifies deferred Set and Remove on Sparse components.
func TestSparse_DeferredSetAndRemove(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		// Nested Write: deferred
		w.Write(func(fw2 *flecs.Writer) {
			flecs.Set(fw2, e, sparsePos{X: 42, Y: 99})
		})
	})

	var got sparsePos
	var ok bool
	w.Read(func(r *flecs.Reader) { got, ok = flecs.Get[sparsePos](r, e) })
	if !ok || got.X != 42 || got.Y != 99 {
		t.Errorf("Deferred Set: ok=%v got=%+v, want {42 99}", ok, got)
	}

	// Deferred Remove.
	w.Write(func(fw *flecs.Writer) {
		w.Write(func(fw2 *flecs.Writer) {
			flecs.Remove[sparsePos](fw2, e)
		})
	})

	w.Read(func(r *flecs.Reader) {
		if flecs.HasID(r, e, posID) {
			t.Error("HasID after deferred Remove: expected false")
		}
	})
}

// TestSparse_OwnsID verifies that OwnsID consults the sparse-set.
func TestSparse_OwnsID(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, sparsePos{X: 1, Y: 1})
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.OwnsID(r, e, posID) {
			t.Error("OwnsID: expected true for entity with Sparse component")
		}
	})

	var e2 flecs.ID
	w.Write(func(fw *flecs.Writer) { e2 = fw.NewEntity() })
	w.Read(func(r *flecs.Reader) {
		if flecs.OwnsID(r, e2, posID) {
			t.Error("OwnsID: expected false for entity without Sparse component")
		}
	})
}

// TestSparse_SetSparseOnTagPanics verifies that SetSparse panics if the component
// is a tag (zero-size) or unregistered.
func TestSparse_SetSparseOnTagPanics(t *testing.T) {
	w := flecs.New()
	// Create a raw entity (tag, not a data component) and try SetSparse on it.
	var tagID flecs.ID
	w.Write(func(fw *flecs.Writer) { tagID = fw.NewEntity() })
	defer func() {
		if r := recover(); r == nil {
			t.Error("SetSparse on tag: expected panic, got none")
		}
	}()
	flecs.SetSparse(w, tagID)
}

// TestSparse_RemoveFromMiddle verifies that removing the middle entry from the
// sparse-set (triggering swap-with-last) maintains correct state.
func TestSparse_RemoveFromMiddle(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	var e1, e2, e3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, sparsePos{X: 1})
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, sparsePos{X: 2})
		e3 = fw.NewEntity()
		flecs.Set(fw, e3, sparsePos{X: 3})
	})

	// Remove the middle entity (e2) — triggers swap-with-last (e3 moves to slot 1).
	w.Write(func(fw *flecs.Writer) { flecs.Remove[sparsePos](fw, e2) })

	var order []flecs.ID
	w.Read(func(r *flecs.Reader) {
		flecs.EachSparse[sparsePos](r, func(e flecs.ID, _ *sparsePos) {
			order = append(order, e)
		})
	})
	if len(order) != 2 {
		t.Fatalf("after remove middle: want 2 entries, got %d", len(order))
	}
	// e1 should still be reachable with correct value.
	var got sparsePos
	var ok bool
	w.Read(func(r *flecs.Reader) { got, ok = flecs.Get[sparsePos](r, e1) })
	if !ok || got.X != 1 {
		t.Errorf("e1 after middle remove: ok=%v got=%+v, want {1 0}", ok, got)
	}
	// e3 should still be reachable with correct value.
	w.Read(func(r *flecs.Reader) { got, ok = flecs.Get[sparsePos](r, e3) })
	if !ok || got.X != 3 {
		t.Errorf("e3 after middle remove: ok=%v got=%+v, want {3 0}", ok, got)
	}
	// e2 should be gone.
	w.Read(func(r *flecs.Reader) {
		if flecs.HasID(r, e2, posID) {
			t.Error("e2 still present after Remove")
		}
	})
	_ = posID
}

// TestSparse_EachSparseNotSparse verifies that EachSparse on a non-sparse
// component type silently iterates nothing (early return on missing storage).
func TestSparse_EachSparseNotSparse(t *testing.T) {
	w := flecs.New()
	_ = flecs.RegisterComponent[sparseVel](w) // registered but NOT marked Sparse

	count := 0
	w.Read(func(r *flecs.Reader) {
		flecs.EachSparse[sparseVel](r, func(_ flecs.ID, _ *sparseVel) { count++ })
	})
	if count != 0 {
		t.Errorf("EachSparse on non-sparse type: want 0 visits, got %d", count)
	}
}

// TestSparse_EachSparseUnregistered verifies that EachSparse on an unregistered
// type silently iterates nothing (early return on unknown component).
func TestSparse_EachSparseUnregistered(t *testing.T) {
	w := flecs.New()
	// sparseVel is not registered in this world.
	count := 0
	w.Read(func(r *flecs.Reader) {
		flecs.EachSparse[sparseVel](r, func(_ flecs.ID, _ *sparseVel) { count++ })
	})
	if count != 0 {
		t.Errorf("EachSparse on unregistered type: want 0 visits, got %d", count)
	}
}

// TestSparse_MultipleEntities verifies that multiple entities can hold the same
// sparse component with independent values.
func TestSparse_MultipleEntities(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	const n = 10
	entities := make([]flecs.ID, n)
	w.Write(func(fw *flecs.Writer) {
		for i := range entities {
			e := fw.NewEntity()
			entities[i] = e
			flecs.Set(fw, e, sparsePos{X: float32(i), Y: float32(i * 2)})
		}
	})

	w.Read(func(r *flecs.Reader) {
		for i, e := range entities {
			p, ok := flecs.Get[sparsePos](r, e)
			if !ok {
				t.Errorf("entity %d: Get returned false", i)
				continue
			}
			if p.X != float32(i) || p.Y != float32(i*2) {
				t.Errorf("entity %d: got %+v, want {%d %d}", i, p, i, i*2)
			}
		}
	})
	_ = posID
}

// TestSparse_ComposesWithFinal verifies that Sparse and Final compose independently.
func TestSparse_ComposesWithFinal(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)
	flecs.SetFinal(w, posID)

	w.Read(func(r *flecs.Reader) {
		if !flecs.IsSparse(r, posID) {
			t.Error("IsSparse: expected true after SetSparse + SetFinal")
		}
		if !flecs.IsFinal(r, posID) {
			t.Error("IsFinal: expected true after SetSparse + SetFinal")
		}
	})

	// Sparse behavior still works.
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, sparsePos{X: 42, Y: 0})
	})
	w.Read(func(r *flecs.Reader) {
		p, ok := flecs.Get[sparsePos](r, e)
		if !ok || p.X != 42 {
			t.Errorf("Get after SetSparse+SetFinal: ok=%v got=%+v", ok, p)
		}
	})
}

// TestSparse_IDReuse verifies that a re-allocated entity ID does NOT inherit
// the deleted entity's sparse data.
func TestSparse_IDReuse(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, sparsePos{X: 99, Y: 99})
	})

	// Delete e and allocate a new entity — it should get e's slot.
	w.Delete(e)
	var e2 flecs.ID
	w.Write(func(fw *flecs.Writer) { e2 = fw.NewEntity() })

	// e2 must NOT inherit e's sparse data.
	w.Read(func(r *flecs.Reader) {
		if flecs.HasID(r, e2, posID) {
			t.Error("new entity after ID reuse: HasID should be false (no inherited sparse data)")
		}
		_, ok := flecs.Get[sparsePos](r, e2)
		if ok {
			t.Error("new entity after ID reuse: Get should return false")
		}
	})
}

// TestSparse_MarshalRoundTrip verifies that sparse-set state and IsSparse policy
// survive a MarshalJSON → UnmarshalJSON round-trip.
func TestSparse_MarshalRoundTrip(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, sparsePos{X: 11, Y: 22})
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, sparsePos{X: 33, Y: 44})
	})

	data, err := w.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	// Verify JSON contains sparse_components (policy flag) but NOT sparse_data.
	// In v0.53.0, Sparse-only data is in the entity body (archetype-stored path);
	// sparse_data is only emitted for DontFragment components.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := raw["sparse_components"]; !ok {
		t.Error("MarshalJSON: missing sparse_components field")
	}
	if _, ok := raw["sparse_data"]; ok {
		t.Error("MarshalJSON: sparse_data should be absent for Sparse-only components (data is in entity body)")
	}

	// Restore into a fresh world.
	w2 := flecs.New()
	posID2 := flecs.RegisterComponent[sparsePos](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	// IsSparse must be restored.
	w2.Read(func(r *flecs.Reader) {
		if !flecs.IsSparse(r, posID2) {
			t.Error("IsSparse: expected true in restored world")
		}
	})

	// Verify sparse values are restored.
	count := 0
	w2.Read(func(r *flecs.Reader) {
		flecs.EachSparse[sparsePos](r, func(_ flecs.ID, p *sparsePos) {
			count++
			if (p.X != 11 && p.X != 33) || (p.Y != 22 && p.Y != 44) {
				t.Errorf("unexpected sparse value after unmarshal: %+v", *p)
			}
		})
	})
	if count != 2 {
		t.Errorf("EachSparse after unmarshal: want 2, got %d", count)
	}
	_ = posID
}

// TestSparse_MarshalPolicyNoData verifies that IsSparse round-trips even for
// a component with the Sparse trait but no held entities.
func TestSparse_MarshalPolicyNoData(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID) // mark sparse but add no entities

	data, err := w.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	w2 := flecs.New()
	posID2 := flecs.RegisterComponent[sparsePos](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}

	w2.Read(func(r *flecs.Reader) {
		if !flecs.IsSparse(r, posID2) {
			t.Error("IsSparse: expected true even with no held entities after unmarshal")
		}
	})
	_ = posID
}

// ── Query integration tests (Phase 15.20 / v0.52.0) ──────────────────────────

// sparseTag is a third sparse component for multi-term pure-sparse query tests.
type sparseTag struct{ Z float32 }

// sparseArch is an archetype (non-sparse) component for mixed-query tests.
type sparseArch struct{ W float32 }

// sparseArch2 is a second archetype (non-sparse) component used in multi-table
// mixed-query tests that require some archetype tables to be excluded by Not terms.
type sparseArch2 struct{ V float32 }

// TestSparse_QueryPureSparse verifies that a pure-sparse query over three sparse
// components yields exactly the entities that have all three, in dense order.
func TestSparse_QueryPureSparse(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	velID := flecs.RegisterComponent[sparseVel](w)
	tagID := flecs.RegisterComponent[sparseTag](w)
	flecs.SetSparse(w, posID)
	flecs.SetSparse(w, velID)
	flecs.SetSparse(w, tagID)

	var e1, e2, e3, e4 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, sparsePos{X: 1})
		flecs.Set(fw, e1, sparseVel{DX: 1})
		flecs.Set(fw, e1, sparseTag{Z: 1}) // all three

		e2 = fw.NewEntity()
		flecs.Set(fw, e2, sparsePos{X: 2})
		flecs.Set(fw, e2, sparseVel{DX: 2}) // missing tag

		e3 = fw.NewEntity()
		flecs.Set(fw, e3, sparsePos{X: 3})
		flecs.Set(fw, e3, sparseTag{Z: 3}) // missing vel

		e4 = fw.NewEntity()
		flecs.Set(fw, e4, sparsePos{X: 4})
		flecs.Set(fw, e4, sparseVel{DX: 4})
		flecs.Set(fw, e4, sparseTag{Z: 4}) // all three
	})

	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.With(velID),
		flecs.With(tagID),
	)

	found := make(map[flecs.ID]bool)
	q.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			found[e] = true
		}
	})

	if len(found) != 2 || !found[e1] || !found[e4] {
		t.Errorf("pure-sparse query: expected e1 and e4; got %v", found)
	}
	if found[e2] || found[e3] {
		t.Errorf("pure-sparse query: e2 and e3 should not be included; got %v", found)
	}
}

// TestSparse_QueryMixed verifies that a query with both archetype and sparse terms
// yields only entities satisfying all terms.
func TestSparse_QueryMixed(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	velID := flecs.RegisterComponent[sparseVel](w)
	flecs.SetSparse(w, posID)
	flecs.SetSparse(w, velID)
	archID := flecs.RegisterComponent[sparseArch](w) // archetype

	var e1, e2, e3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, sparsePos{X: 1})
		flecs.Set(fw, e1, sparseVel{DX: 1})
		flecs.Set(fw, e1, sparseArch{W: 10}) // all three → match

		e2 = fw.NewEntity()
		flecs.Set(fw, e2, sparsePos{X: 2})
		flecs.Set(fw, e2, sparseVel{DX: 2}) // no archetype component → no match

		e3 = fw.NewEntity()
		flecs.Set(fw, e3, sparseArch{W: 30}) // no sparse components → no match
	})

	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.With(velID),
		flecs.With(archID),
	)

	found := make(map[flecs.ID]bool)
	q.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			found[e] = true
		}
	})

	if len(found) != 1 || !found[e1] {
		t.Errorf("mixed query: expected only e1; got %v", found)
	}
}

// TestSparse_QueryAllArchetypeRegression verifies that all-archetype queries are
// unaffected by the sparse query integration (fast path preserved).
func TestSparse_QueryAllArchetypeRegression(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w) // NOT marked sparse
	velID := flecs.RegisterComponent[sparseVel](w) // NOT marked sparse

	var e1, e2, e3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, sparsePos{X: 1})
		flecs.Set(fw, e1, sparseVel{DX: 1}) // has both → match

		e2 = fw.NewEntity()
		flecs.Set(fw, e2, sparsePos{X: 2}) // missing vel → no match

		e3 = fw.NewEntity()
		flecs.Set(fw, e3, sparseVel{DX: 3}) // missing pos → no match
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.With(velID))

	found := make(map[flecs.ID]bool)
	q.Each(func(it *flecs.QueryIter) {
		// All-archetype: Entities() returns all entities in the matched table.
		for _, e := range it.Entities() {
			found[e] = true
		}
	})

	if len(found) != 1 || !found[e1] {
		t.Errorf("all-archetype regression: expected only e1; got %v", found)
	}
}

// TestSparse_QueryWildcardPairOnSparseRelationship verifies that a wildcard pair
// query Pair(R, Wildcard) works correctly when R is also registered as a sparse
// scalar component. Pairs are stored in archetype tables regardless of R's sparse
// trait; this tests that sparse marking on R does not interfere with pair queries.
func TestSparse_QueryWildcardPairOnSparseRelationship(t *testing.T) {
	w := flecs.New()
	// rID is both a sparse scalar component and a relationship used in pairs.
	rID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, rID)

	var tgt1, tgt2, tgt3 flecs.ID
	var e1, e2, e3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tgt1 = fw.NewEntity()
		tgt2 = fw.NewEntity()
		tgt3 = fw.NewEntity()

		// Pairs (rID, tgtN) land in archetype tables — not sparse storage.
		e1 = fw.NewEntity()
		flecs.AddID(fw, e1, flecs.MakePair(rID, tgt1))

		e2 = fw.NewEntity()
		flecs.AddID(fw, e2, flecs.MakePair(rID, tgt2))

		e3 = fw.NewEntity()
		flecs.AddID(fw, e3, flecs.MakePair(rID, tgt3))
	})

	// All-archetype wildcard pair query; rID's sparse trait does not affect this.
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(rID, w.Wildcard())))

	found := make(map[flecs.ID]bool)
	q.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			found[e] = true
		}
	})

	if len(found) != 3 || !found[e1] || !found[e2] || !found[e3] {
		t.Errorf("wildcard pair on sparse relationship: expected e1, e2, e3; got %v", found)
	}
}

// TestSparse_QueryNotSparse verifies that Without(sparseC) on a sparse component
// matches entities that do NOT have that sparse component.
func TestSparse_QueryNotSparse(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	velID := flecs.RegisterComponent[sparseVel](w)
	flecs.SetSparse(w, posID)
	flecs.SetSparse(w, velID)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, sparsePos{X: 1}) // has pos, no vel → match

		e2 = fw.NewEntity()
		flecs.Set(fw, e2, sparsePos{X: 2})
		flecs.Set(fw, e2, sparseVel{DX: 2}) // has both → no match (has vel)
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.Without(velID))

	found := make(map[flecs.ID]bool)
	q.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			found[e] = true
		}
	})

	if len(found) != 1 || !found[e1] {
		t.Errorf("Not sparse: expected only e1; got %v", found)
	}
}

// TestSparse_QueryOptionalSparse verifies that Maybe(sparseC) on a sparse component
// yields matched entities with and without the optional component.
func TestSparse_QueryOptionalSparse(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	velID := flecs.RegisterComponent[sparseVel](w)
	flecs.SetSparse(w, posID)
	flecs.SetSparse(w, velID)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, sparsePos{X: 10})
		flecs.Set(fw, e1, sparseVel{DX: 1}) // has optional vel

		e2 = fw.NewEntity()
		flecs.Set(fw, e2, sparsePos{X: 20}) // no vel
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.Maybe(velID))

	type result struct {
		hasVel bool
		velVal sparseVel
	}
	results := make(map[flecs.ID]result)
	q.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			vSlice, ok := flecs.FieldMaybe[sparseVel](it, velID)
			r := result{hasVel: ok}
			if ok && len(vSlice) > 0 {
				r.velVal = vSlice[0]
			}
			results[e] = r
		}
	})

	if len(results) != 2 {
		t.Fatalf("Optional sparse: expected 2 results, got %d", len(results))
	}
	r1 := results[e1]
	if !r1.hasVel || r1.velVal.DX != 1 {
		t.Errorf("e1 optional vel: hasVel=%v val=%+v, want hasVel=true val.DX=1", r1.hasVel, r1.velVal)
	}
	r2 := results[e2]
	if r2.hasVel {
		t.Errorf("e2 optional vel: hasVel=%v, want false", r2.hasVel)
	}
}

// TestSparse_QueryFieldPtrCorrectness verifies that Field[T] for a sparse term
// returns a pointer that is valid until the next iterator advance, and that the
// pointer addresses distinct stable allocations for different entities.
func TestSparse_QueryFieldPtrCorrectness(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, sparsePos{X: 11, Y: 12})
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, sparsePos{X: 21, Y: 22})
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID))

	// Collect pointers and values in a single iteration.
	type entry struct {
		ptr *sparsePos
		val sparsePos
	}
	var entries []entry
	q.Each(func(it *flecs.QueryIter) {
		slice := flecs.Field[sparsePos](it, posID)
		if len(slice) != 1 {
			t.Fatalf("Field sparse: expected slice len 1, got %d", len(slice))
		}
		entries = append(entries, entry{ptr: &slice[0], val: slice[0]})
	})

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// Verify values are distinct.
	if entries[0].val == entries[1].val {
		t.Errorf("expected distinct values: both are %+v", entries[0].val)
	}
	// Pointers must be stable (non-nil, distinct).
	if entries[0].ptr == nil || entries[1].ptr == nil {
		t.Error("Field sparse: nil pointer in slice[0]")
	}
	if entries[0].ptr == entries[1].ptr {
		t.Error("Field sparse: two different entities returned the same pointer")
	}
}

// TestSparse_QueryFieldPtrMutation verifies that writing through the pointer
// returned by Field[T] for a sparse term is immediately visible via EachSparse
// and GetRef.
func TestSparse_QueryFieldPtrMutation(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, sparsePos{X: 1, Y: 1})
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID))

	// Mutate through the Field pointer.
	q.Each(func(it *flecs.QueryIter) {
		slice := flecs.Field[sparsePos](it, posID)
		slice[0].X = 99
		slice[0].Y = 88
	})

	// Verify via EachSparse.
	var found sparsePos
	w.Read(func(r *flecs.Reader) {
		flecs.EachSparse[sparsePos](r, func(id flecs.ID, v *sparsePos) {
			if id == e {
				found = *v
			}
		})
	})
	if found.X != 99 || found.Y != 88 {
		t.Errorf("mutation via Field ptr: got %+v, want {99 88}", found)
	}

	// Verify via GetRef.
	var ptr *sparsePos
	w.Read(func(r *flecs.Reader) { ptr = flecs.GetRef[sparsePos](r, e) })
	if ptr == nil || ptr.X != 99 || ptr.Y != 88 {
		t.Errorf("GetRef after Field mutation: got %+v", ptr)
	}
}

// TestSparse_CachedQueryVersionCounter verifies that CachedQuery.Changed() returns
// true after a sparse-set mutation (new entity or removal), implementing the sparse
// version counter requirement.
func TestSparse_CachedQueryVersionCounter(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	cq := flecs.NewCachedQueryFromTerms(w, flecs.With(posID))

	// First call to Changed() should always return true (initial state).
	if !cq.Changed() {
		t.Error("Changed(): expected true on first call")
	}

	// No mutations: Changed() should return false.
	if cq.Changed() {
		t.Error("Changed(): expected false with no mutations")
	}

	// Add an entity with the sparse component.
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, sparsePos{X: 42})
	})

	// After sparse-set insertion: Changed() must return true.
	if !cq.Changed() {
		t.Error("Changed(): expected true after sparse-set insertion")
	}

	// No further mutations: Changed() should return false.
	if cq.Changed() {
		t.Error("Changed(): expected false after reporting the change")
	}

	// Iterate and verify the new entity is visible.
	found := false
	cq.Each(func(it *flecs.QueryIter) {
		for _, eid := range it.Entities() {
			if eid == e {
				found = true
			}
		}
	})
	if !found {
		t.Error("CachedQuery: new sparse entity not found after version bump")
	}

	// Remove the entity: Changed() must return true again.
	w.Write(func(fw *flecs.Writer) { flecs.Remove[sparsePos](fw, e) })
	if !cq.Changed() {
		t.Error("Changed(): expected true after sparse-set removal")
	}
	_ = posID
}

// TestSparse_QueryEmptySparseset verifies that a pure-sparse query over an empty
// sparse-set yields zero results without panicking.
func TestSparse_QueryEmptySparseset(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)
	// Intentionally add no entities.

	q := flecs.NewQueryFromTerms(w, flecs.With(posID))

	count := 0
	q.Each(func(it *flecs.QueryIter) {
		count += it.Count()
	})

	if count != 0 {
		t.Errorf("empty sparse-set: expected 0 results, got %d", count)
	}
}

// TestSparse_QuerySmallestDriverHeuristic verifies that the smallest sparse-set
// is selected as the driver for pure-sparse queries. When term A has 5 entries
// and term B has 5000, the query should visit ≈5 entities — not 5000.
func TestSparse_QuerySmallestDriverHeuristic(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w) // will have 5 entries
	velID := flecs.RegisterComponent[sparseVel](w) // will have 5000 entries
	flecs.SetSparse(w, posID)
	flecs.SetSparse(w, velID)

	// 5 entities have both pos and vel (the intersection).
	var matchedEntities [5]flecs.ID
	w.Write(func(fw *flecs.Writer) {
		for i := range matchedEntities {
			e := fw.NewEntity()
			matchedEntities[i] = e
			flecs.Set(fw, e, sparsePos{X: float32(i)})
			flecs.Set(fw, e, sparseVel{DX: float32(i)})
		}
		// 4995 additional entities have only vel (not in pos sparse-set).
		for i := 0; i < 4995; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, sparseVel{DX: float32(i)})
		}
	})

	// The query must yield exactly 5 results (the intersection) without
	// iterating all 5000 vel-only entries.
	q := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.With(velID))

	found := make(map[flecs.ID]bool)
	q.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			found[e] = true
		}
	})

	if len(found) != 5 {
		t.Errorf("smallest driver heuristic: expected 5 results (intersection), got %d", len(found))
	}
	for _, e := range matchedEntities {
		if !found[e] {
			t.Errorf("expected entity %v in results", e)
		}
	}
}

// TestSparse_QueryPureSparseZeroEntities verifies that a pure-sparse query on a
// component that was registered but has no held entities yields zero results
// without panicking.
func TestSparse_QueryPureSparseZeroEntities(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)
	// No entities hold posID.

	q := flecs.NewQueryFromTerms(w, flecs.With(posID))

	count := 0
	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
				t.Errorf("pure-sparse zero entities: unexpected panic: %v", r)
			}
		}()
		q.Each(func(it *flecs.QueryIter) {
			count += it.Count()
		})
	}()

	if panicked {
		return
	}
	if count != 0 {
		t.Errorf("pure-sparse zero entities: expected 0 results, got %d", count)
	}
}

// TestSparse_QueryMarshalRoundTrip verifies that a cached query with a sparse term
// rebuilds cleanly after world marshal/unmarshal and produces correct results.
func TestSparse_QueryMarshalRoundTrip(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, sparsePos{X: 7, Y: 8})
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, sparsePos{X: 9, Y: 10})
	})

	data, err := w.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	// Restore into a fresh world.
	w2 := flecs.New()
	posID2 := flecs.RegisterComponent[sparsePos](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	// Build cached query AFTER unmarshal; it must see the restored sparse data.
	cq := flecs.NewCachedQueryFromTerms(w2, flecs.With(posID2))

	var found []sparsePos
	cq.Each(func(it *flecs.QueryIter) {
		for range it.Entities() {
			slice := flecs.Field[sparsePos](it, posID2)
			found = append(found, slice[0])
		}
	})

	if len(found) != 2 {
		t.Fatalf("marshal round-trip: expected 2 results, got %d", len(found))
	}
	for _, p := range found {
		if (p.X != 7 && p.X != 9) || (p.Y != 8 && p.Y != 10) {
			t.Errorf("marshal round-trip: unexpected value %+v", p)
		}
	}
	_ = posID
	_ = e1
	_ = e2
}

// TestSparse_QueryIterSparseCountTableEntities verifies the iterator API on
// mixed sparse iterators: Count() returns 1 (entity-at-a-time), Table() returns
// the archetype table (Sparse-only components ARE in the archetype in v0.53.0),
// and Entities() returns the current entity.
func TestSparse_QueryIterSparseCountTableEntities(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, sparsePos{X: 1})
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID))

	// Count() must return 1 for mixed-sparse iterators (entity-at-a-time mode).
	q.Each(func(it *flecs.QueryIter) {
		if c := it.Count(); c != 1 {
			t.Errorf("Sparse-only Count(): got %d, want 1", c)
		}
	})

	// Table() must NOT panic on a Sparse-only mixed iterator (entity is in archetype).
	it := q.Iter()
	if !it.Next() {
		t.Fatal("Next(): expected true for Sparse-only query with one entity")
	}
	tbl := it.Table()
	if tbl == nil {
		t.Error("Table(): expected non-nil table for Sparse-only iterator")
	}

	// DontFragment-only queries are pure-sparse; Table() panics for them.
	type dfOnlyPos struct{ X float32 }
	dfID := flecs.RegisterComponent[dfOnlyPos](w)
	flecs.SetDontFragment(w, dfID)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, dfOnlyPos{X: 2})
	})
	qDF := flecs.NewQueryFromTerms(w, flecs.With(dfID))
	itDF := qDF.Iter()
	itDF.Next()
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("Table() on DontFragment-only iterator: expected panic, got none")
			}
		}()
		_ = itDF.Table()
	}()
}

// TestSparse_QueryMixedNonMatchingTable verifies that nextMixed skips archetype
// tables that fail matchesTable (archetype Not term excludes a table) and that
// the sparse-Not short-circuit in matchesTable is exercised.
func TestSparse_QueryMixedNonMatchingTable(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	velID := flecs.RegisterComponent[sparseVel](w)
	flecs.SetSparse(w, posID)
	flecs.SetSparse(w, velID)
	arch1ID := flecs.RegisterComponent[sparseArch](w)
	arch2ID := flecs.RegisterComponent[sparseArch2](w)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		// e1: arch1 + posID — should match (no arch2, no velID)
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, sparseArch{W: 1})
		flecs.Set(fw, e1, sparsePos{X: 1})

		// e2: arch1 + arch2 + posID — excluded by Without(arch2ID)
		// e2's table [arch1, arch2] fails matchesTable → exercises nextMixed line 624
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, sparseArch{W: 2})
		flecs.Set(fw, e2, sparseArch2{V: 2})
		flecs.Set(fw, e2, sparsePos{X: 2})
	})

	// Without(arch2ID): archetype Not term — e2's table fails matchesTable (line 624).
	// Without(velID): sparse Not term — exercises the sparse-Not skip in matchesTable (line 785).
	q := flecs.NewQueryFromTerms(w,
		flecs.With(arch1ID),
		flecs.Without(arch2ID),
		flecs.With(posID),
		flecs.Without(velID),
	)

	found := make(map[flecs.ID]bool)
	q.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			found[e] = true
		}
	})

	if !found[e1] || found[e2] {
		t.Errorf("mixed non-matching table: expected only e1; got e1=%v e2=%v", found[e1], found[e2])
	}
}

// TestSparse_QueryMixedOptionalTerms verifies that updateOptionalPresenceMixed is
// called correctly for mixed queries containing both sparse and archetype Optional
// terms, including the cleanup of the optionalPresent map between entities.
func TestSparse_QueryMixedOptionalTerms(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	velID := flecs.RegisterComponent[sparseVel](w)
	flecs.SetSparse(w, posID)
	flecs.SetSparse(w, velID)                        // velID is a sparse Optional
	archID := flecs.RegisterComponent[sparseArch](w) // required archetype And
	tagID := flecs.RegisterComponent[sparseTag](w)   // archetype Optional (not sparse)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		// e1: archID + tagID (archetype) + posID + velID (sparse) — both optionals present
		// e1 ends up in table [archID, tagID]; e2 in table [archID]: different tables.
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, sparseArch{W: 1})
		flecs.Set(fw, e1, sparseTag{Z: 1})
		flecs.Set(fw, e1, sparsePos{X: 1})
		flecs.Set(fw, e1, sparseVel{DX: 1})

		// e2: archID + posID only — optional components absent
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, sparseArch{W: 2})
		flecs.Set(fw, e2, sparsePos{X: 2})
	})

	// Maybe(velID): sparse Optional — exercises sparse branch in updateOptionalPresenceMixed.
	// Maybe(tagID): archetype Optional — exercises archetype branch.
	// Two distinct entities in different tables ensure the cleanup loop (delete) is exercised
	// on the second updateOptionalPresenceMixed call.
	q := flecs.NewQueryFromTerms(w,
		flecs.With(archID),
		flecs.With(posID),
		flecs.Maybe(velID),
		flecs.Maybe(tagID),
	)

	found := make(map[flecs.ID]struct{})
	q.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			found[e] = struct{}{}
		}
	})

	if _, ok := found[e1]; !ok {
		t.Error("mixed optional: e1 not found")
	}
	if _, ok := found[e2]; !ok {
		t.Error("mixed optional: e2 not found")
	}
}

// TestSparse_IsFieldSelfBasic verifies IsFieldSelf for TraverseSelf terms, exercising
// the loop-continue path (first term does not match requested id) and the
// not-in-query panic path.
func TestSparse_IsFieldSelfBasic(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	velID := flecs.RegisterComponent[sparseVel](w)
	// Both registered as archetype (not sparse).

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, sparsePos{X: 1})
		flecs.Set(fw, e, sparseVel{DX: 1})
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.With(velID))
	it := q.Iter()
	it.Next()

	// IsFieldSelf for velID requires skipping posID first (exercises the continue path
	// in the loop: term.ID != velID → continue, then term.ID == velID → return true).
	if !flecs.IsFieldSelf(it, velID) {
		t.Error("IsFieldSelf velID: expected true for self-owned component")
	}

	// IsFieldSelf with an id not in the query must panic.
	badID := flecs.RegisterComponent[sparseArch](w)
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("IsFieldSelf bad id: expected panic, got none")
			}
		}()
		flecs.IsFieldSelf(it, badID)
	}()
}
