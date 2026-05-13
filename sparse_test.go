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

// TestSparse_NoArchetypeTransition verifies that adding a Sparse component does NOT
// change the entity's archetype. The entity stays in the same table.
func TestSparse_NoArchetypeTransition(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[sparsePos](w)
	flecs.SetSparse(w, posID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// Capture the table before Set.
	tablesBefore := w.TablesFor(posID)

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, sparsePos{X: 5, Y: 5})
	})

	// No new archetype table should be created for posID.
	tablesAfter := w.TablesFor(posID)
	if len(tablesAfter) != len(tablesBefore) {
		t.Errorf("Sparse Set created an archetype table: before=%d tables, after=%d tables",
			len(tablesBefore), len(tablesAfter))
	}
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

	// Verify JSON contains sparse metadata.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := raw["sparse_components"]; !ok {
		t.Error("MarshalJSON: missing sparse_components field")
	}
	if _, ok := raw["sparse_data"]; !ok {
		t.Error("MarshalJSON: missing sparse_data field")
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
