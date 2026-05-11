package flecs_test

import (
	"reflect"
	"testing"

	"github.com/snichols/flecs"
)

// ── GetByID basics ────────────────────────────────────────────────────────────

func TestGetByIDBasic(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 3, Y: 7})
	})

	v, ok := w.GetByID(e, posID)
	if !ok {
		t.Fatal("GetByID returned false for registered component on live entity")
	}
	pos, ok2 := v.(Position)
	if !ok2 {
		t.Fatalf("GetByID returned %T, want Position", v)
	}
	if pos != (Position{X: 3, Y: 7}) {
		t.Errorf("GetByID returned %v, want {3 7}", pos)
	}
}

// TestGetByIDTagComponent verifies that GetByID on a zero-size (tag) component
// returns the zero value of its registered type, ok=true.
func TestGetByIDTagComponent(t *testing.T) {
	w := flecs.New()
	// Use AddID with a raw entity ID so EnsureID creates a struct{}{}-typed tag.
	var tagEnt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tagEnt = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, tagEnt)
	})

	v, ok := w.GetByID(e, tagEnt)
	if !ok {
		t.Fatal("GetByID returned false for tag component")
	}
	if v != (struct{}{}) {
		t.Errorf("GetByID tag: got %v (%T), want struct{}{}", v, v)
	}
}

// TestGetByIDDeadEntity verifies that GetByID returns nil, false for a dead entity.
func TestGetByIDDeadEntity(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1, Y: 2})
	})
	w.Delete(e)

	v, ok := w.GetByID(e, posID)
	if ok || v != nil {
		t.Errorf("GetByID on dead entity: got (%v, %v), want (nil, false)", v, ok)
	}
}

// TestGetByIDUnregisteredID verifies that GetByID returns nil, false for an ID
// that has no TypeInfo (never registered as a component).
func TestGetByIDUnregisteredID(t *testing.T) {
	w := flecs.New()
	var e, unregistered flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		unregistered = fw.NewEntity() // just an entity, no TypeInfo associated
	})

	v, ok := w.GetByID(e, unregistered)
	if ok || v != nil {
		t.Errorf("GetByID on unregistered ID: got (%v, %v), want (nil, false)", v, ok)
	}
}

// TestGetByIDMissingComponent verifies that GetByID returns nil, false when the
// entity is alive but does not have the component.
func TestGetByIDMissingComponent(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity() // alive, but no Position
	})

	v, ok := w.GetByID(e, posID)
	if ok || v != nil {
		t.Errorf("GetByID on entity without component: got (%v, %v), want (nil, false)", v, ok)
	}
}

// TestGetByIDIsAInheritance verifies that GetByID walks the IsA chain when the
// component is not locally owned.
func TestGetByIDIsAInheritance(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 10, Y: 20})

		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})

	v, ok := w.GetByID(child, posID)
	if !ok {
		t.Fatal("GetByID via IsA: returned false")
	}
	pos, ok2 := v.(Position)
	if !ok2 {
		t.Fatalf("GetByID via IsA: returned %T, want Position", v)
	}
	if pos != (Position{X: 10, Y: 20}) {
		t.Errorf("GetByID via IsA: got %v, want {10 20}", pos)
	}
}

// ── SetByID basics ────────────────────────────────────────────────────────────

func TestSetByIDBasic(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})

	w.SetByID(e, posID, Position{X: 1, Y: 2})

	v, ok := w.GetByID(e, posID)
	if !ok {
		t.Fatal("GetByID after SetByID returned false")
	}
	pos, _ := v.(Position)
	if pos != (Position{X: 1, Y: 2}) {
		t.Errorf("SetByID then GetByID: got %v, want {1 2}", pos)
	}
}

// TestSetByIDTypeMismatchPanics verifies that SetByID panics when v's type does
// not match the component's registered type.
func TestSetByIDTypeMismatchPanics(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})

	defer func() {
		if r := recover(); r == nil {
			t.Error("SetByID with wrong type: expected panic, got none")
		}
	}()
	w.SetByID(e, posID, "not a Position")
}

// TestSetByIDDeadEntityPanics verifies that SetByID panics when e is not alive.
func TestSetByIDDeadEntityPanics(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})
	w.Delete(e)

	defer func() {
		if r := recover(); r == nil {
			t.Error("SetByID on dead entity: expected panic, got none")
		}
	}()
	w.SetByID(e, posID, Position{X: 1, Y: 2})
}

// TestSetByIDAutoMigratesArchetype verifies that SetByID adds the component to
// an entity that doesn't have it, triggering archetype migration.
func TestSetByIDAutoMigratesArchetype(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})

	w.Read(func(r *flecs.Reader) {
		if flecs.HasID(r, e, posID) {
			t.Fatal("entity should not have Position before SetByID")
		}
	})
	w.SetByID(e, posID, Position{X: 5, Y: 6})
	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, e, posID) {
			t.Fatal("entity should have Position after SetByID migration")
		}
	})
	v, _ := w.GetByID(e, posID)
	pos, _ := v.(Position)
	if pos != (Position{X: 5, Y: 6}) {
		t.Errorf("after migration, got %v, want {5 6}", pos)
	}
}

// TestSetByIDFiresOnAddAndOnSet verifies that SetByID fires both OnAdd (first
// time) and OnSet (every call) hooks.
func TestSetByIDFiresOnAddAndOnSet(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var addCount, setCount int
	flecs.OnAdd[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { addCount++ })
	flecs.OnSet[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { setCount++ })

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})
	w.SetByID(e, posID, Position{X: 1, Y: 1})
	if addCount != 1 {
		t.Errorf("OnAdd fired %d times after first SetByID, want 1", addCount)
	}
	if setCount != 1 {
		t.Errorf("OnSet fired %d times after first SetByID, want 1", setCount)
	}

	// Second SetByID on same component: OnAdd must NOT fire again, OnSet must fire.
	w.SetByID(e, posID, Position{X: 2, Y: 2})
	if addCount != 1 {
		t.Errorf("OnAdd fired %d times after second SetByID, want 1 (no re-add)", addCount)
	}
	if setCount != 2 {
		t.Errorf("OnSet fired %d times after second SetByID, want 2", setCount)
	}
}

// TestSetByIDFiresObserver verifies that SetByID fires registered observers.
func TestSetByIDFiresObserver(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var addFired, setFired bool
	flecs.Observe[Position](w, flecs.EventOnAdd, func(_ *flecs.Writer, _ flecs.ID, _ Position) { addFired = true })
	flecs.Observe[Position](w, flecs.EventOnSet, func(_ *flecs.Writer, _ flecs.ID, _ Position) { setFired = true })

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})
	w.SetByID(e, posID, Position{X: 3, Y: 3})

	if !addFired {
		t.Error("EventOnAdd observer not fired by SetByID")
	}
	if !setFired {
		t.Error("EventOnSet observer not fired by SetByID")
	}
}

// ── Pair support ──────────────────────────────────────────────────────────────

// TestGetByIDViaPairData verifies that GetByID returns the value of a pair set
// via SetPair[T].
func TestGetByIDViaPairData(t *testing.T) {
	w := flecs.New()
	var e, rel, tgt flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		rel = fw.NewEntity()
		tgt = fw.NewEntity()

		flecs.SetPair[Edge](fw, e, rel, tgt, Edge{Weight: 3.14})
	})
	pairID := flecs.MakePair(rel, tgt)

	v, ok := w.GetByID(e, pairID)
	if !ok {
		t.Fatal("GetByID on pair returned false")
	}
	edge, ok2 := v.(Edge)
	if !ok2 {
		t.Fatalf("GetByID pair: returned %T, want Edge", v)
	}
	if edge.Weight != 3.14 {
		t.Errorf("GetByID pair: Weight = %v, want 3.14", edge.Weight)
	}
}

// TestSetByIDOnPairID verifies that SetByID can write data to a pair that was
// previously registered via SetPair[T].
func TestSetByIDOnPairID(t *testing.T) {
	w := flecs.New()
	var e, rel, tgt flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		rel = fw.NewEntity()
		tgt = fw.NewEntity()

		// First call registers the pair TypeInfo.
		flecs.SetPair[Edge](fw, e, rel, tgt, Edge{Weight: 1.0})
	})
	pairID := flecs.MakePair(rel, tgt)

	// Overwrite via SetByID.
	w.SetByID(e, pairID, Edge{Weight: 9.9})

	v, ok := w.GetByID(e, pairID)
	if !ok {
		t.Fatal("GetByID after SetByID on pair returned false")
	}
	edge, _ := v.(Edge)
	if edge.Weight != 9.9 {
		t.Errorf("SetByID pair: Weight = %v, want 9.9", edge.Weight)
	}
}

// TestGetByIDTagPair verifies that GetByID on a tag pair (no data, added via
// AddID) returns struct{}{}, true.
func TestGetByIDTagPair(t *testing.T) {
	w := flecs.New()
	var e, rel, tgt flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
	})
	pairID := flecs.MakePair(rel, tgt)

	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, pairID)
	})

	v, ok := w.GetByID(e, pairID)
	if !ok {
		t.Fatal("GetByID on tag pair returned false")
	}
	if v != (struct{}{}) {
		t.Errorf("GetByID tag pair: got %v (%T), want struct{}{}", v, v)
	}
}

// ── Defer support ─────────────────────────────────────────────────────────────

// TestSetByIDRespectsDefer verifies that SetByID queues its operation when the
// world is deferred, and the value is only visible after the defer block closes.
func TestSetByIDRespectsDefer(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1, Y: 1})
	})

	flecs.DeferBeginForTest(w)
	w.SetByID(e, posID, Position{X: 99, Y: 99})
	// Inside defer: should still see the old value.
	{
		v, _ := w.GetByID(e, posID)
		pos, _ := v.(Position)
		if pos != (Position{X: 1, Y: 1}) {
			t.Errorf("inside defer: got %v, want {1 1} (deferred write not yet applied)", pos)
		}
	}
	flecs.DeferEndForTest(w)

	// After DeferEnd: new value should be visible.
	v, ok := w.GetByID(e, posID)
	if !ok {
		t.Fatal("GetByID after Defer returned false")
	}
	pos, _ := v.(Position)
	if pos != (Position{X: 99, Y: 99}) {
		t.Errorf("after Defer: got %v, want {99 99}", pos)
	}
}

// ── Round-trip: GetByID / SetByID reflect.Type consistency ───────────────────

// TestGetByIDReturnsCorrectReflectType verifies that the dynamic type returned
// by GetByID matches the component's registered reflect.Type.
func TestGetByIDReturnsCorrectReflectType(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 7, Y: 8})
	})

	v, _ := w.GetByID(e, posID)
	got := reflect.TypeOf(v)
	want := reflect.TypeFor[Position]()
	if got != want {
		t.Errorf("reflect.TypeOf(GetByID) = %v, want %v", got, want)
	}
}

// TestSetByIDThenGetTyped verifies that a value written via SetByID is
// readable via the typed Get[T] path (round-trip).
func TestSetByIDThenGetTyped(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})

	w.SetByID(e, posID, Position{X: 42, Y: 43})

	w.Read(func(r *flecs.Reader) {
		pos, ok := flecs.Get[Position](r, e)
		if !ok {
			t.Fatal("Get[Position] after SetByID returned false")
		}
		if pos != (Position{X: 42, Y: 43}) {
			t.Errorf("Get[Position] after SetByID: got %v, want {42 43}", pos)
		}
	})
}

// TestSetTypedThenGetByID verifies that a value written via Set[T] is
// readable via the dynamic GetByID path (round-trip).
func TestSetTypedThenGetByID(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 11, Y: 22})
	})

	v, ok := w.GetByID(e, posID)
	if !ok {
		t.Fatal("GetByID after Set[T] returned false")
	}
	pos, _ := v.(Position)
	if pos != (Position{X: 11, Y: 22}) {
		t.Errorf("GetByID after Set[T]: got %v, want {11 22}", pos)
	}
}

// TestSetByIDTagComponent verifies that SetByID on a zero-size (tag) component
// does not panic and fires hooks correctly.
func TestSetByIDTagComponent(t *testing.T) {
	w := flecs.New()
	// Tag registered via RegisterComponent[Tag]; size == 0.
	tagID := flecs.RegisterComponent[Tag](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})

	var addFired, setFired bool
	flecs.OnAdd[Tag](w, func(_ *flecs.Writer, _ flecs.ID, _ Tag) { addFired = true })
	flecs.OnSet[Tag](w, func(_ *flecs.Writer, _ flecs.ID, _ Tag) { setFired = true })

	w.SetByID(e, tagID, Tag{})
	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, e, tagID) {
			t.Fatal("entity should have tag after SetByID")
		}
	})
	if !addFired {
		t.Error("OnAdd not fired for tag component SetByID")
	}
	if !setFired {
		t.Error("OnSet not fired for tag component SetByID")
	}

	// Second call on same entity: OnAdd must NOT fire again.
	addFired = false
	w.SetByID(e, tagID, Tag{})
	if addFired {
		t.Error("OnAdd fired again on second SetByID for already-present tag")
	}
}

// TestSetByIDUnregisteredIDPanics verifies that SetByID panics when the
// component ID has no TypeInfo registered.
func TestSetByIDUnregisteredIDPanics(t *testing.T) {
	w := flecs.New()
	var unregistered, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		unregistered = fw.NewEntity()
		e = fw.NewEntity()
	})

	defer func() {
		if r := recover(); r == nil {
			t.Error("SetByID with unregistered ID: expected panic, got none")
		}
	}()
	w.SetByID(e, unregistered, struct{}{})
}

// TestGetByIDMultiLevelIsA verifies that GetByID traverses a multi-level IsA
// chain (A isA B isA C, C has Position) correctly.
func TestGetByIDMultiLevelIsA(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var a, b, c flecs.ID
	w.Write(func(fw *flecs.Writer) {
		// C is the root prefab; it owns Position.
		c = fw.NewEntity()
		flecs.Set(fw, c, Position{X: 50, Y: 60})

		// B inherits from C.
		b = fw.NewEntity()
		flecs.AddID(fw, b, flecs.MakePair(w.IsA(), c))

		// A inherits from B.
		a = fw.NewEntity()
		flecs.AddID(fw, a, flecs.MakePair(w.IsA(), b))
	})

	v, ok := w.GetByID(a, posID)
	if !ok {
		t.Fatal("GetByID multi-level IsA: returned false")
	}
	pos, _ := v.(Position)
	if pos != (Position{X: 50, Y: 60}) {
		t.Errorf("GetByID multi-level IsA: got %v, want {50 60}", pos)
	}
}

// TestGetByIDIsAWithMixedComponents verifies that GetByID correctly skips
// non-IsA components during the IsA chain walk. The entity has a data component
// AND an IsA relationship; the target component is only on the prefab.
func TestGetByIDIsAWithMixedComponents(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	flecs.RegisterComponent[Velocity](w)

	var prefab, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 33, Y: 44})

		// e has Velocity (locally) and IsA(prefab). It does NOT have Position locally.
		e = fw.NewEntity()
		flecs.Set(fw, e, Velocity{DX: 1, DY: 2})
		flecs.AddID(fw, e, flecs.MakePair(w.IsA(), prefab))
	})

	// GetByID should skip the Velocity component and find Position via IsA.
	v, ok := w.GetByID(e, posID)
	if !ok {
		t.Fatal("GetByID mixed components + IsA: returned false")
	}
	pos, _ := v.(Position)
	if pos != (Position{X: 33, Y: 44}) {
		t.Errorf("GetByID mixed components + IsA: got %v, want {33 44}", pos)
	}
}

// TestGetByIDDeadPrefabSkipped verifies that GetByID skips dead prefabs in the
// IsA chain without returning incorrect results.
func TestGetByIDDeadPrefabSkipped(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var deadPrefab, livePrefab, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		// deadPrefab had Position but will be deleted.
		deadPrefab = fw.NewEntity()
		flecs.Set(fw, deadPrefab, Position{X: 99, Y: 99})

		// livePrefab also has Position.
		livePrefab = fw.NewEntity()
		flecs.Set(fw, livePrefab, Position{X: 7, Y: 8})

		// e has IsA(deadPrefab) and IsA(livePrefab).
		e = fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(w.IsA(), deadPrefab))
		flecs.AddID(fw, e, flecs.MakePair(w.IsA(), livePrefab))
	})

	// Now delete deadPrefab.
	w.Delete(deadPrefab)

	// GetByID should skip the dead prefab and find Position via livePrefab.
	v, ok := w.GetByID(e, posID)
	if !ok {
		t.Fatal("GetByID with dead prefab: returned false, expected to find via live prefab")
	}
	pos, _ := v.(Position)
	if pos != (Position{X: 7, Y: 8}) {
		t.Errorf("GetByID with dead prefab: got %v, want {7 8}", pos)
	}
}
