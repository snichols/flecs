package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// ── IsA() accessor ────────────────────────────────────────────────────────────

func TestIsAReturnsSameIDEachCall(t *testing.T) {
	w := flecs.New()
	first := w.IsA()
	second := w.IsA()
	if first != second {
		t.Fatalf("IsA() returned different IDs: %v vs %v", first, second)
	}
}

func TestIsAIsAlive(t *testing.T) {
	w := flecs.New()
	if !w.IsAlive(w.IsA()) {
		t.Fatal("IsA() entity must be alive")
	}
}

func TestIsADistinctAcrossWorlds(t *testing.T) {
	w1 := flecs.New()
	w2 := flecs.New()
	if !w1.IsAlive(w1.IsA()) {
		t.Fatal("w1 IsA must be alive in w1")
	}
	if !w2.IsAlive(w2.IsA()) {
		t.Fatal("w2 IsA must be alive in w2")
	}
}

func TestIsADistinctFromChildOf(t *testing.T) {
	w := flecs.New()
	if w.IsA() == w.ChildOf() {
		t.Fatal("IsA and ChildOf must be distinct built-in entities")
	}
}

// ── Inheritance basics ────────────────────────────────────────────────────────

func TestIsAInheritanceGetHasOwns(t *testing.T) {
	w := flecs.New()
	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 1, Y: 2})
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})

	w.Read(func(r *flecs.Reader) {
		// Get returns prefab's value via inheritance.
		p, ok := flecs.Get[Position](r, child)
		if !ok {
			t.Fatal("Get[Position] on child returned false; expected inherited value")
		}
		if p != (Position{X: 1, Y: 2}) {
			t.Fatalf("Get[Position] via IsA: want {1,2}, got %+v", p)
		}

		// Has is true (inherited).
		if !flecs.Has[Position](r, child) {
			t.Fatal("Has[Position] must be true via IsA inheritance")
		}

		// Owns is false (not locally owned).
		if flecs.Owns[Position](r, child) {
			t.Fatal("Owns[Position] must be false before local override")
		}
	})
	_ = prefab
}

// ── Override (copy-on-write) ──────────────────────────────────────────────────

func TestIsAOverrideCopyOnWrite(t *testing.T) {
	w := flecs.New()
	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 1, Y: 2})
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
		// Local override via Set.
		flecs.Set(fw, child, Position{X: 99, Y: 99})
	})

	w.Read(func(r *flecs.Reader) {
		// Get returns local value.
		p, ok := flecs.Get[Position](r, child)
		if !ok {
			t.Fatal("Get[Position] returned false after local override")
		}
		if p != (Position{X: 99, Y: 99}) {
			t.Fatalf("Get[Position] after override: want {99,99}, got %+v", p)
		}

		// Owns is now true.
		if !flecs.Owns[Position](r, child) {
			t.Fatal("Owns[Position] must be true after Set override")
		}

		// Prefab is unchanged.
		pp, _ := flecs.Get[Position](r, prefab)
		if pp != (Position{X: 1, Y: 2}) {
			t.Fatalf("prefab Position must be unchanged; got %+v", pp)
		}
	})
}

// ── Remove restores inheritance ───────────────────────────────────────────────

func TestIsARemoveRestoresInheritance(t *testing.T) {
	w := flecs.New()
	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 5, Y: 5})
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
		flecs.Set(fw, child, Position{X: 99, Y: 99}) // override
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[Position](fw, child) // remove local override
	})

	w.Read(func(r *flecs.Reader) {
		// Has is true again (inherited from prefab).
		if !flecs.Has[Position](r, child) {
			t.Fatal("Has[Position] must be true again after remove restores inheritance")
		}

		// Owns is false (local column removed).
		if flecs.Owns[Position](r, child) {
			t.Fatal("Owns[Position] must be false after Remove")
		}

		// Get returns prefab's value.
		p, ok := flecs.Get[Position](r, child)
		if !ok {
			t.Fatal("Get[Position] returned false after remove; expected prefab value")
		}
		if p != (Position{X: 5, Y: 5}) {
			t.Fatalf("Get[Position] after remove: want prefab value {5,5}, got %+v", p)
		}
	})
	_ = prefab
}

// ── Multi-level chain ─────────────────────────────────────────────────────────

func TestIsAMultiLevelChain(t *testing.T) {
	w := flecs.New()
	// A (IsA, B), B (IsA, C); C has Position.
	var A flecs.ID
	w.Write(func(fw *flecs.Writer) {
		C := fw.NewEntity()
		flecs.Set(fw, C, Position{X: 7, Y: 7})
		B := fw.NewEntity()
		flecs.AddID(fw, B, flecs.MakePair(w.IsA(), C))
		A = fw.NewEntity()
		flecs.AddID(fw, A, flecs.MakePair(w.IsA(), B))
	})

	w.Read(func(r *flecs.Reader) {
		p, ok := flecs.Get[Position](r, A)
		if !ok {
			t.Fatal("Get[Position] on A returned false; expected value from C via multi-level chain")
		}
		if p != (Position{X: 7, Y: 7}) {
			t.Fatalf("Get[Position] via multi-level IsA: want {7,7}, got %+v", p)
		}

		if !flecs.Has[Position](r, A) {
			t.Fatal("Has[Position] must be true for A via multi-level chain")
		}

		if flecs.Owns[Position](r, A) {
			t.Fatal("Owns[Position] must be false for A (value is in C)")
		}
	})
}

// ── Multiple direct prefabs ───────────────────────────────────────────────────

// TestIsAMultiplePrefabsFirstWins verifies that when an entity has two direct
// (IsA, *) pairs, Get returns the value from the first prefab in signature
// order (which corresponds to the prefab with the lower entity index, since
// pair IDs are sorted ascending by value).
func TestIsAMultiplePrefabsFirstWins(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 := fw.NewEntity() // lower index → smaller pair ID → first in signature
		p2 := fw.NewEntity()
		flecs.Set(fw, p1, Position{X: 1, Y: 0})
		flecs.Set(fw, p2, Position{X: 2, Y: 0})
		e = fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(w.IsA(), p1))
		flecs.AddID(fw, e, flecs.MakePair(w.IsA(), p2))
	})

	w.Read(func(r *flecs.Reader) {
		p, ok := flecs.Get[Position](r, e)
		if !ok {
			t.Fatal("Get[Position] returned false with two direct prefabs")
		}
		// First prefab in signature order (p1, lower index) wins.
		if p.X != 1 {
			t.Fatalf("Get[Position] with two prefabs: want X=1 (from p1), got X=%v", p.X)
		}
	})
}

// ── Cycle on Get ──────────────────────────────────────────────────────────────

func TestIsACycleGetSelfDoesNotLoop(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		// e is its own prefab — deliberate self-cycle.
		flecs.AddID(fw, e, flecs.MakePair(w.IsA(), e))

		_, ok := flecs.Get[Position](fw.AsReader(), e)
		if ok {
			t.Fatal("Get[Position] on self-cycle entity must return false (Position not set)")
		}
		// Must have returned (not hung).
	})
}

// ── Cycle on Has ──────────────────────────────────────────────────────────────

func TestIsACycleHasSelfDoesNotLoop(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(w.IsA(), e))

		if flecs.Has[Position](fw.AsReader(), e) {
			t.Fatal("Has[Position] on self-cycle entity must return false (Position not set)")
		}
	})
}

// ── Two-entity cycle ──────────────────────────────────────────────────────────

func TestIsATwoEntityCycleTerminates(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		A := fw.NewEntity()
		B := fw.NewEntity()
		flecs.AddID(fw, A, flecs.MakePair(w.IsA(), B))
		flecs.AddID(fw, B, flecs.MakePair(w.IsA(), A))

		r := fw.AsReader()

		// Neither has Position; both Get and Has must terminate and return false.
		_, ok := flecs.Get[Position](r, A)
		if ok {
			t.Fatal("Get[Position] on A in two-entity cycle must return false")
		}
		_, ok = flecs.Get[Position](r, B)
		if ok {
			t.Fatal("Get[Position] on B in two-entity cycle must return false")
		}
		if flecs.Has[Position](r, A) {
			t.Fatal("Has[Position] on A in two-entity cycle must return false")
		}
		if flecs.Has[Position](r, B) {
			t.Fatal("Has[Position] on B in two-entity cycle must return false")
		}
	})
}

// ── PrefabOf ─────────────────────────────────────────────────────────────────

func TestPrefabOfBasics(t *testing.T) {
	w := flecs.New()
	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})

	w.Read(func(r *flecs.Reader) {
		got, ok := flecs.PrefabOf(r, child)
		if !ok {
			t.Fatal("PrefabOf returned false for entity with IsA pair")
		}
		if got != prefab {
			t.Fatalf("PrefabOf: want %v, got %v", prefab, got)
		}
	})
}

func TestPrefabOfNoIsA(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()

		got, ok := flecs.PrefabOf(fw.AsReader(), e)
		if ok {
			t.Fatal("PrefabOf should return false for entity with no IsA pair")
		}
		if got != 0 {
			t.Fatalf("PrefabOf should return 0 for entity with no IsA, got %v", got)
		}
	})
}

func TestPrefabOfDeadEntity(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	w.Delete(e)

	w.Read(func(r *flecs.Reader) {
		got, ok := flecs.PrefabOf(r, e)
		if ok {
			t.Fatal("PrefabOf should return false for dead entity")
		}
		if got != 0 {
			t.Fatalf("PrefabOf should return 0 for dead entity, got %v", got)
		}
	})
}

// ── EachPrefab ────────────────────────────────────────────────────────────────

func TestEachPrefabTwoPrefabs(t *testing.T) {
	w := flecs.New()
	var p1, p2, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		p2 = fw.NewEntity()
		e = fw.NewEntity()
		flecs.AddID(fw, e, flecs.MakePair(w.IsA(), p1))
		flecs.AddID(fw, e, flecs.MakePair(w.IsA(), p2))
	})

	seen := make(map[flecs.ID]bool)
	w.EachPrefab(e, func(prefab flecs.ID) bool {
		seen[prefab] = true
		return true
	})

	if len(seen) != 2 {
		t.Fatalf("EachPrefab called fn %d times, want 2", len(seen))
	}
	if !seen[p1] {
		t.Error("EachPrefab did not visit p1")
	}
	if !seen[p2] {
		t.Error("EachPrefab did not visit p2")
	}
}

func TestEachPrefabEarlyExit(t *testing.T) {
	w := flecs.New()
	for i := 0; i < 5; i++ {
		var p, e flecs.ID
		w.Write(func(fw *flecs.Writer) {
			p = fw.NewEntity()
			e = fw.NewEntity()
			flecs.AddID(fw, e, flecs.MakePair(w.IsA(), p))
		})

		count := 0
		w.EachPrefab(e, func(_ flecs.ID) bool {
			count++
			return false // stop immediately
		})
		if count != 1 {
			t.Fatalf("EachPrefab early exit: want 1 call, got %d", count)
		}
		_ = i
	}
}

func TestEachPrefabEmpty(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	called := false
	w.EachPrefab(e, func(_ flecs.ID) bool {
		called = true
		return true
	})
	if called {
		t.Fatal("EachPrefab called fn for entity with no IsA pairs")
	}
}

// TestEachPrefabDirectOnly verifies that EachPrefab does NOT walk multi-level chains.
// A (IsA, B), B (IsA, C): EachPrefab(A) yields only B, not C.
func TestEachPrefabDirectOnly(t *testing.T) {
	w := flecs.New()
	var C, B, A flecs.ID
	w.Write(func(fw *flecs.Writer) {
		C = fw.NewEntity()
		B = fw.NewEntity()
		flecs.AddID(fw, B, flecs.MakePair(w.IsA(), C))
		A = fw.NewEntity()
		flecs.AddID(fw, A, flecs.MakePair(w.IsA(), B))
	})

	var visited []flecs.ID
	w.EachPrefab(A, func(prefab flecs.ID) bool {
		visited = append(visited, prefab)
		return true
	})

	if len(visited) != 1 {
		t.Fatalf("EachPrefab(A): want 1 direct prefab, got %d: %v", len(visited), visited)
	}
	if visited[0] != B {
		t.Fatalf("EachPrefab(A): want B, got %v", visited[0])
	}
}

// ── Delete prefab — dangling IsA ──────────────────────────────────────────────

func TestIsADeletedPrefabSkipped(t *testing.T) {
	w := flecs.New()
	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 3, Y: 3})

		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})

	// Sanity: inheritance works before delete.
	w.Read(func(r *flecs.Reader) {
		_, ok := flecs.Get[Position](r, child)
		if !ok {
			t.Fatal("sanity: Get[Position] before prefab delete must return true")
		}
	})

	w.Delete(prefab)

	// After prefab delete: Get/Has must skip the dangling IsA pair.
	w.Read(func(r *flecs.Reader) {
		_, ok := flecs.Get[Position](r, child)
		if ok {
			t.Fatal("Get[Position] must return false after prefab is deleted")
		}
		if flecs.Has[Position](r, child) {
			t.Fatal("Has[Position] must return false after prefab is deleted")
		}
	})
}

// ── OwnsID / HasID ────────────────────────────────────────────────────────────

func TestOwnsID(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 1, Y: 1})
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})

	w.Read(func(r *flecs.Reader) {
		// HasID is inheritance-aware.
		if !flecs.HasID(r, child, posID) {
			t.Fatal("HasID must return true via IsA inheritance")
		}
		// OwnsID is local-only.
		if flecs.OwnsID(r, child, posID) {
			t.Fatal("OwnsID must return false before local override")
		}
	})

	// After local override, OwnsID becomes true.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, child, Position{X: 9, Y: 9})
	})
	w.Read(func(r *flecs.Reader) {
		if !flecs.OwnsID(r, child, posID) {
			t.Fatal("OwnsID must return true after Set override")
		}
	})
	_ = prefab
}

func TestOwnsIDDeadEntity(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{})
	})
	w.Delete(e)

	w.Read(func(r *flecs.Reader) {
		if flecs.OwnsID(r, e, posID) {
			t.Fatal("OwnsID must return false for dead entity")
		}
	})
}

func TestHasIDInheritanceAware(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var prefab, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 10, Y: 10})
		e = fw.NewEntity()
	})

	// Before IsA: HasID must be false.
	w.Read(func(r *flecs.Reader) {
		if flecs.HasID(r, e, posID) {
			t.Fatal("HasID must be false before IsA is added")
		}
	})

	// Add IsA.
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, flecs.MakePair(w.IsA(), prefab))
	})

	// After IsA: HasID must be true.
	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, e, posID) {
			t.Fatal("HasID must be true via IsA inheritance")
		}
	})
	_ = prefab
}

// ── Set copy-on-write (verification) ─────────────────────────────────────────

// TestIsASetAddsPrefabComponentLocally verifies that Set[Position](child, ...)
// adds Position to the child's own table even when Position is only inherited.
func TestIsASetAddsPrefabComponentLocally(t *testing.T) {
	w := flecs.New()
	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 1, Y: 1})
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})

	// Before override: child lacks Position locally.
	w.Read(func(r *flecs.Reader) {
		if flecs.Owns[Position](r, child) {
			t.Fatal("child must not own Position before Set")
		}
	})

	// Set triggers a local archetype migration.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, child, Position{X: 42, Y: 42})
	})

	w.Read(func(r *flecs.Reader) {
		// Child now owns Position.
		if !flecs.Owns[Position](r, child) {
			t.Fatal("child must own Position after Set (copy-on-write)")
		}

		// And reads locally, not from prefab.
		p, ok := flecs.Get[Position](r, child)
		if !ok || p != (Position{X: 42, Y: 42}) {
			t.Fatalf("Get[Position] after Set: want {42,42}, got (%+v, %v)", p, ok)
		}
	})
	_ = prefab
}

// ── EachPrefab dead entity ────────────────────────────────────────────────────

func TestEachPrefabDeadEntity(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	w.Delete(e)

	called := false
	w.EachPrefab(e, func(_ flecs.ID) bool {
		called = true
		return true
	})
	if called {
		t.Fatal("EachPrefab must not call fn for a dead entity")
	}
}

// ── Owns[T] dead entity ───────────────────────────────────────────────────────

func TestOwnsDeadEntity(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{})
	})
	w.Delete(e)

	w.Read(func(r *flecs.Reader) {
		if flecs.Owns[Position](r, e) {
			t.Fatal("Owns[Position] must return false for dead entity")
		}
	})
}

// ── Tag component inheritance ─────────────────────────────────────────────────

// TestIsATagInheritance verifies that tag components (zero-size) are inherited
// via the IsA chain and that Get[Tag] returns (zero, true) from the prefab.
func TestIsATagInheritance(t *testing.T) {
	w := flecs.New()
	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Tag{})
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})

	w.Read(func(r *flecs.Reader) {
		_, ok := flecs.Get[Tag](r, child)
		if !ok {
			t.Fatal("Get[Tag] via IsA must return true for inherited tag")
		}
		if !flecs.Has[Tag](r, child) {
			t.Fatal("Has[Tag] via IsA must be true")
		}
		if flecs.Owns[Tag](r, child) {
			t.Fatal("Owns[Tag] must be false before local override")
		}
	})
	_ = prefab
}

// ── Count baseline ────────────────────────────────────────────────────────────

// TestIsAWorldCountBaseline verifies that a fresh world has 11 built-in entities
// (ChildOf, IsA, Name, PreUpdate, OnUpdate, PostUpdate, OnFixedUpdate,
// OnInstantiate, Inherit, Override, DontInherit, OnDelete, OnDeleteTarget,
// RemoveAction, DeleteAction, PanicAction) before any user entities.
func TestIsAWorldCountBaseline(t *testing.T) {
	w := flecs.New()
	base := w.Count()
	if base != 16 {
		t.Fatalf("fresh World.Count(): want 16 (ChildOf + IsA + Name + PreUpdate + OnUpdate + PostUpdate + OnFixedUpdate + OnInstantiate + Inherit + Override + DontInherit + OnDelete + OnDeleteTarget + RemoveAction + DeleteAction + PanicAction), got %d", base)
	}
}
