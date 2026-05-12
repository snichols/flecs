package docs_test

import (
	"testing"
	"unsafe"

	"github.com/snichols/flecs"
)

// TestEC_NewEntity verifies the entity-creation snippet from EntitiesComponents.md.
func TestEC_NewEntity(t *testing.T) {
	w := flecs.New()

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})

	if !w.IsAlive(e) {
		t.Error("newly created entity should be alive")
	}
}

// TestEC_DeleteEntity verifies the deletion and generation-counter snippets from EntitiesComponents.md.
func TestEC_DeleteEntity(t *testing.T) {
	w := flecs.New()

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) { e1 = fw.NewEntity() })
	w.Delete(e1)
	w.Write(func(fw *flecs.Writer) { e2 = fw.NewEntity() })

	if w.IsAlive(e1) {
		t.Error("e1 should not be alive after deletion")
	}
	if !w.IsAlive(e2) {
		t.Error("e2 (recycled slot, new generation) should be alive")
	}

	// Deleting an already-dead entity is a no-op (post condition satisfied).
	w.Delete(e1) // OK
	w.Delete(e1) // OK again
}

// TestEC_Liveness verifies the liveliness-check snippet from EntitiesComponents.md.
func TestEC_Liveness(t *testing.T) {
	w := flecs.New()

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
	})
	w.Delete(e1)

	if w.IsAlive(e1) {
		t.Error("e1 should be dead after Delete")
	}
	if !w.IsAlive(e2) {
		t.Error("e2 should still be alive")
	}
}

// TestEC_Names verifies the basic name set/get/lookup/rename snippets from EntitiesComponents.md.
func TestEC_Names(t *testing.T) {
	w := flecs.New()

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		w.SetName(e, "MyEntity")
	})

	found, ok := w.Lookup("MyEntity")
	if !ok || found != e {
		t.Errorf("Lookup(%q) = (%d, %v), want (%d, true)", "MyEntity", found, ok, e)
	}

	name, ok := w.GetName(e)
	if !ok || name != "MyEntity" {
		t.Errorf("GetName = (%q, %v), want (\"MyEntity\", true)", name, ok)
	}

	// Rename.
	w.SetName(e, "NewName")
	name2, _ := w.GetName(e)
	if name2 != "NewName" {
		t.Errorf("GetName after rename = %q, want \"NewName\"", name2)
	}
}

// TestEC_HierarchicalNames verifies hierarchical name lookup from EntitiesComponents.md.
func TestEC_HierarchicalNames(t *testing.T) {
	w := flecs.New()

	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		child = fw.NewEntity()
		w.SetName(parent, "Parent")
		w.SetName(child, "Child")
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
	})

	found, ok := w.Lookup("Parent.Child")
	if !ok || found != child {
		t.Errorf("Lookup(\"Parent.Child\") = (%d, %v), want (%d, true)", found, ok, child)
	}

	// Relative lookup.
	rel, ok := w.LookupChild(parent, "Child")
	if !ok || rel != child {
		t.Errorf("LookupChild(parent, \"Child\") = (%d, %v), want (%d, true)", rel, ok, child)
	}
}

// TestEC_NamePath verifies GetName vs PathOf from EntitiesComponents.md.
func TestEC_NamePath(t *testing.T) {
	w := flecs.New()

	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		child = fw.NewEntity()
		w.SetName(parent, "Parent")
		w.SetName(child, "Child")
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
	})

	name, _ := w.GetName(child)
	if name != "Child" {
		t.Errorf("GetName = %q, want \"Child\"", name)
	}

	path := w.PathOf(child)
	if path != "Parent.Child" {
		t.Errorf("PathOf = %q, want \"Parent.Child\"", path)
	}
}

// TestEC_SetGetRemove verifies the component Set/Get/Has/Owns/Remove snippets from EntitiesComponents.md.
func TestEC_SetGetRemove(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()
	flecs.RegisterComponent[Position](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 10, Y: 20})
	})

	w.Read(func(r *flecs.Reader) {
		p, ok := flecs.Get[Position](r, e)
		if !ok {
			t.Error("Get[Position] should succeed")
			return
		}
		if p.X != 10 || p.Y != 20 {
			t.Errorf("Get[Position] = %v, want {10 20}", p)
		}
		if !flecs.Has[Position](r, e) {
			t.Error("Has[Position] should be true")
		}
		if !flecs.Owns[Position](r, e) {
			t.Error("Owns[Position] should be true")
		}
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[Position](fw, e)
	})

	w.Read(func(r *flecs.Reader) {
		if flecs.Has[Position](r, e) {
			t.Error("Has[Position] should be false after Remove")
		}
	})
}

// TestEC_Tags verifies the static-tag snippet from EntitiesComponents.md.
func TestEC_Tags(t *testing.T) {
	type Enemy struct{} // zero-size struct — a tag

	w := flecs.New()

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Enemy{})
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.Has[Enemy](r, e) {
			t.Error("Has[Enemy] should be true after Set")
		}
	})
}

// TestEC_DynamicTags verifies the dynamic-tag (runtime entity ID) snippet from EntitiesComponents.md.
func TestEC_DynamicTags(t *testing.T) {
	w := flecs.New()

	var tagEnemy, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tagEnemy = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, tagEnemy)
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, e, tagEnemy) {
			t.Error("HasID should be true for dynamic tag")
		}
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.RemoveID(fw, e, tagEnemy)
	})

	w.Read(func(r *flecs.Reader) {
		if flecs.HasID(r, e, tagEnemy) {
			t.Error("HasID should be false after RemoveID")
		}
	})
}

// TestEC_HookOnAdd verifies the OnAdd hook snippet from EntitiesComponents.md.
func TestEC_HookOnAdd(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()

	addCount := 0
	flecs.OnAdd[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) {
		addCount++
	})

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1, Y: 2}) // OnAdd fires
		flecs.Set(fw, e, Position{X: 3, Y: 4}) // OnAdd does NOT fire again
	})

	if addCount != 1 {
		t.Errorf("OnAdd fired %d times, want 1", addCount)
	}
}

// TestEC_HookOnSet verifies the OnSet hook snippet from EntitiesComponents.md.
func TestEC_HookOnSet(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()

	setCount := 0
	flecs.OnSet[Position](w, func(_ *flecs.Writer, _ flecs.ID, v Position) {
		setCount++
		_ = v.X
	})

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1, Y: 2}) // OnSet fires
		flecs.Set(fw, e, Position{X: 3, Y: 4}) // OnSet fires again
	})

	if setCount != 2 {
		t.Errorf("OnSet fired %d times, want 2", setCount)
	}
}

// TestEC_HookOnRemove verifies the OnRemove hook snippet from EntitiesComponents.md.
func TestEC_HookOnRemove(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()

	removed := false
	flecs.OnRemove[Position](w, func(_ *flecs.Writer, _ flecs.ID, v Position) {
		removed = true
		_ = v
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1, Y: 2})
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[Position](fw, e) // OnRemove fires
	})

	if !removed {
		t.Error("OnRemove hook should have fired")
	}
}

// TestEC_HookOrdering verifies that OnAdd fires before OnSet, and OnRemove fires
// before the entity loses the component — illustrating the hook-ordering prose
// in EntitiesComponents.md. Operations run in separate Write scopes so that
// deferred-command coalescing does not elide the remove.
func TestEC_HookOrdering(t *testing.T) {
	type Score struct{ Points int }

	w := flecs.New()

	var order []string
	flecs.OnAdd[Score](w, func(_ *flecs.Writer, _ flecs.ID, _ Score) {
		order = append(order, "add")
	})
	flecs.OnSet[Score](w, func(_ *flecs.Writer, _ flecs.ID, _ Score) {
		order = append(order, "set")
	})
	flecs.OnRemove[Score](w, func(_ *flecs.Writer, _ flecs.ID, _ Score) {
		order = append(order, "remove")
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Score{Points: 10}) // OnAdd fires, then OnSet fires
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[Score](fw, e) // OnRemove fires
	})

	want := []string{"add", "set", "remove"}
	if len(order) != len(want) {
		t.Fatalf("hook order = %v, want %v", order, want)
	}
	for i, v := range want {
		if order[i] != v {
			t.Errorf("order[%d] = %q, want %q", i, order[i], v)
		}
	}
}

// TestEC_ComponentEntityHandle verifies the RegisterComponent + ComponentInfo snippet
// from EntitiesComponents.md.
func TestEC_ComponentEntityHandle(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	if !w.IsAlive(posID) {
		t.Error("component entity should be alive")
	}

	w.Read(func(r *flecs.Reader) {
		info, ok := r.ComponentInfo(posID)
		if !ok {
			t.Error("ComponentInfo should return true for a registered component")
			return
		}
		if info.Size != unsafe.Sizeof(Position{}) {
			t.Errorf("info.Size = %d, want %d", info.Size, unsafe.Sizeof(Position{}))
		}
		if info.Align != unsafe.Alignof(Position{}) {
			t.Errorf("info.Align = %d, want %d", info.Align, unsafe.Alignof(Position{}))
		}
		if info.ID != posID {
			t.Errorf("info.ID = %d, want %d", info.ID, posID)
		}
	})
}

// TestEC_Registration verifies explicit component registration from EntitiesComponents.md.
func TestEC_Registration(t *testing.T) {
	type Velocity struct{ DX, DY float32 }

	w := flecs.New()
	velID := flecs.RegisterComponent[Velocity](w)

	if !w.IsAlive(velID) {
		t.Error("Velocity component entity should be alive after registration")
	}

	// Idempotent: a second registration returns the same ID.
	velID2 := flecs.RegisterComponent[Velocity](w)
	if velID2 != velID {
		t.Errorf("second RegisterComponent returned %d, want %d", velID2, velID)
	}
}

// TestEC_Singleton verifies the singleton workaround pattern from EntitiesComponents.md.
func TestEC_Singleton(t *testing.T) {
	type TimeOfDay struct{ Value float32 }

	w := flecs.New()
	todID := flecs.RegisterComponent[TimeOfDay](w)

	// Set singleton on the component entity itself.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, todID, TimeOfDay{Value: 0.5})
	})

	// Get singleton.
	w.Read(func(r *flecs.Reader) {
		tod, ok := flecs.Get[TimeOfDay](r, todID)
		if !ok {
			t.Error("singleton Get should succeed")
			return
		}
		if tod.Value != 0.5 {
			t.Errorf("singleton value = %v, want 0.5", tod.Value)
		}
	})
}
