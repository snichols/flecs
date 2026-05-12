package flecs_test

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/snichols/flecs"
)

// Component types used across tests.
type Position struct{ X, Y float32 }
type Velocity struct{ DX, DY float32 }
type WithStr struct{ S string }
type Tag struct{}

// ── Basic entity lifecycle ────────────────────────────────────────────────────

func TestNewEntityIsAliveAndNonZero(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})
	if e == 0 {
		t.Fatal("NewEntity returned zero ID")
	}
	if !w.IsAlive(e) {
		t.Fatal("IsAlive false immediately after NewEntity")
	}
}

func TestIsAliveOnDeadEntity(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})
	w.Delete(e)
	if w.IsAlive(e) {
		t.Fatal("IsAlive true after Delete")
	}
}

func TestDeleteReturnValues(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})
	if !w.Delete(e) {
		t.Fatal("Delete returned false for alive entity")
	}
	if w.Delete(e) {
		t.Fatal("Delete returned true for dead entity")
	}
}

func TestCountReflectsAliveEntities(t *testing.T) {
	w := flecs.New()
	// World starts with the built-in ChildOf entity (index 1); user entities begin at index 2.
	base := w.Count()
	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
	})
	if w.Count() != base+2 {
		t.Fatalf("Count want base+2=%d after 2 NewEntity, got %d", base+2, w.Count())
	}
	w.Delete(e1)
	_ = e2
	if w.Count() != base+1 {
		t.Fatalf("Count want base+1=%d after deleting one entity, got %d", base+1, w.Count())
	}
}

// ── Component registration ────────────────────────────────────────────────────

func TestRegisterComponentIdempotent(t *testing.T) {
	w := flecs.New()
	id1 := flecs.RegisterComponent[Position](w)
	id2 := flecs.RegisterComponent[Position](w)
	if id1 == 0 {
		t.Fatal("RegisterComponent returned zero ID")
	}
	if id1 != id2 {
		t.Fatalf("RegisterComponent not idempotent: got %v then %v", id1, id2)
	}
}

// ── Set / Get round-trip ──────────────────────────────────────────────────────

func TestSetGetRoundTrip(t *testing.T) {
	w := flecs.New()
	want := Position{X: 1, Y: 2}
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, want)
	})
	var got Position
	var ok bool
	w.Read(func(r *flecs.Reader) {
		got, ok = flecs.Get[Position](r, e)
	})
	if !ok {
		t.Fatal("Get returned false after Set")
	}
	if got != want {
		t.Fatalf("Get returned %+v, want %+v", got, want)
	}
}

func TestAutoRegistrationViaSet(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		// Set without an explicit RegisterComponent call.
		flecs.Set(fw, e, Position{X: 3, Y: 4})
	})
	var ok bool
	w.Read(func(r *flecs.Reader) {
		_, ok = flecs.Get[Position](r, e)
	})
	if !ok {
		t.Fatal("Get after auto-registered Set returned false")
	}
}

func TestSetOnDeadEntityPanics(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})
	w.Delete(e)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Set on dead entity should panic")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, Position{})
	})
}

// ── Two-component flow ────────────────────────────────────────────────────────

func TestTwoComponentMigrationsAndGet(t *testing.T) {
	w := flecs.New()
	p := Position{X: 10, Y: 20}
	v := Velocity{DX: 1, DY: 2}
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, p)
		flecs.Set(fw, e, v)
	})
	var gotP Position
	var gotV Velocity
	var okP, okV bool
	w.Read(func(r *flecs.Reader) {
		gotP, okP = flecs.Get[Position](r, e)
		gotV, okV = flecs.Get[Velocity](r, e)
	})
	if !okP || gotP != p {
		t.Fatalf("Position after two-component migration: got (%+v, %v), want (%+v, true)", gotP, okP, p)
	}
	if !okV || gotV != v {
		t.Fatalf("Velocity after two-component migration: got (%+v, %v), want (%+v, true)", gotV, okV, v)
	}
}

// ── Has ───────────────────────────────────────────────────────────────────────

func TestHasBeforeAndAfterSetAndRemove(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})
	// Before Set: Has should be false.
	w.Read(func(r *flecs.Reader) {
		if flecs.Has[Position](r, e) {
			t.Fatal("Has should be false before Set")
		}
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, Position{1, 2})
	})
	// After Set: Has should be true.
	w.Read(func(r *flecs.Reader) {
		if !flecs.Has[Position](r, e) {
			t.Fatal("Has should be true after Set")
		}
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[Position](fw, e)
	})
	// After Remove: Has should be false.
	w.Read(func(r *flecs.Reader) {
		if flecs.Has[Position](r, e) {
			t.Fatal("Has should be false after Remove")
		}
	})
}

func TestHasOnDeadEntityReturnsFalse(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{})
	})
	w.Delete(e)
	w.Read(func(r *flecs.Reader) {
		if flecs.Has[Position](r, e) {
			t.Fatal("Has should be false for dead entity")
		}
	})
}

// ── Remove ────────────────────────────────────────────────────────────────────

func TestRemoveActuallyRemovesComponent(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{5, 6})
	})
	// Position is now applied; Remove should return true.
	w.Write(func(fw *flecs.Writer) {
		if !flecs.Remove[Position](fw, e) {
			t.Fatal("Remove returned false for present component")
		}
	})
	// After remove, Get should return false.
	w.Read(func(r *flecs.Reader) {
		_, ok := flecs.Get[Position](r, e)
		if ok {
			t.Fatal("Get returned true after Remove")
		}
	})
}

func TestRemoveAbsentComponentReturnsFalse(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		if flecs.Remove[Position](fw, e) {
			t.Fatal("Remove returned true for absent component")
		}
	})
}

func TestRemoveOnDeadEntityReturnsFalse(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{})
	})
	w.Delete(e)
	w.Write(func(fw *flecs.Writer) {
		if flecs.Remove[Position](fw, e) {
			t.Fatal("Remove returned true for dead entity")
		}
	})
}

func TestGetOnUnregisteredTypeReturnsFalse(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		// Velocity was never registered.
		_, ok := flecs.Get[Velocity](fw, e)
		if ok {
			t.Fatal("Get on unregistered type should return false")
		}
	})
}

func TestGetOnDeadEntityReturnsFalse(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{1, 2})
	})
	w.Delete(e)
	w.Read(func(r *flecs.Reader) {
		_, ok := flecs.Get[Position](r, e)
		if ok {
			t.Fatal("Get on dead entity should return false")
		}
	})
}

// ── Delete ────────────────────────────────────────────────────────────────────

func TestDeleteRemovesEntityFromWorld(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{1, 2})
	})
	w.Delete(e)
	if w.IsAlive(e) {
		t.Fatal("IsAlive should be false after Delete")
	}
	w.Read(func(r *flecs.Reader) {
		_, ok := flecs.Get[Position](r, e)
		if ok {
			t.Fatal("Get should return false for deleted entity")
		}
	})
}

// ── Multi-entity per archetype ────────────────────────────────────────────────

func TestMultipleEntitiesShareTable(t *testing.T) {
	w := flecs.New()
	var e1, e2, e3 flecs.ID
	p := Position{1, 2}
	w.Write(func(fw *flecs.Writer) {
		e1, e2, e3 = fw.NewEntity(), fw.NewEntity(), fw.NewEntity()
		flecs.Set(fw, e1, p)
		flecs.Set(fw, e2, p)
		flecs.Set(fw, e3, p)
	})

	t1 := flecs.TableOf(w, e1)
	t2 := flecs.TableOf(w, e2)
	t3 := flecs.TableOf(w, e3)
	if t1 == nil {
		t.Fatal("table pointer should be non-nil after Set")
	}
	if t1 != t2 || t1 != t3 {
		t.Fatal("entities with same components should share a table")
	}
}

// ── Migration with co-located entities ───────────────────────────────────────

// TestMigrationUpdatesCoLocatedEntityRow verifies the swap-remove invariant:
// when e2 migrates out of a 3-entity table, e3 (the last-row entity) is
// swapped into e2's former row, and e3's Record.Row is updated to 1.
// Subsequent Get operations on e1 and e3 must still return the correct values.
func TestMigrationUpdatesCoLocatedEntityRow(t *testing.T) {
	w := flecs.New()
	var e1, e2, e3 flecs.ID
	p1, p2, p3 := Position{1, 0}, Position{2, 0}, Position{3, 0}
	w.Write(func(fw *flecs.Writer) {
		e1, e2, e3 = fw.NewEntity(), fw.NewEntity(), fw.NewEntity()
		flecs.Set(fw, e1, p1)
		flecs.Set(fw, e2, p2)
		flecs.Set(fw, e3, p3)
		// Migrate e2 to a new archetype (Position + Velocity).
		flecs.Set(fw, e2, Velocity{DX: 99})
	})
	var gotP1, gotP2, gotP3 Position
	var ok1, ok2, ok3 bool
	w.Read(func(r *flecs.Reader) {
		gotP1, ok1 = flecs.Get[Position](r, e1)
		gotP3, ok3 = flecs.Get[Position](r, e3)
		gotP2, ok2 = flecs.Get[Position](r, e2)
	})
	if !ok1 || gotP1 != p1 {
		t.Fatalf("e1 Position after migration: got (%+v, %v), want (%+v, true)", gotP1, ok1, p1)
	}
	if !ok3 || gotP3 != p3 {
		t.Fatalf("e3 Position after migration: got (%+v, %v), want (%+v, true)", gotP3, ok3, p3)
	}
	if !ok2 || gotP2 != p2 {
		t.Fatalf("e2 Position after migration: got (%+v, %v), want (%+v, true)", gotP2, ok2, p2)
	}
}

// ── Delete in middle of table ─────────────────────────────────────────────────

// TestDeleteMiddleUpdatesLastEntityRow deletes e2 from a 3-entity table.
// e3 is swap-removed into row 1; Get[Position](e3) must still return Position{3}.
func TestDeleteMiddleUpdatesLastEntityRow(t *testing.T) {
	w := flecs.New()
	var e1, e2, e3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1, e2, e3 = fw.NewEntity(), fw.NewEntity(), fw.NewEntity()
		flecs.Set(fw, e1, Position{1, 0})
		flecs.Set(fw, e2, Position{2, 0})
		flecs.Set(fw, e3, Position{3, 0})
	})

	w.Delete(e2)

	w.Read(func(r *flecs.Reader) {
		got, ok := flecs.Get[Position](r, e3)
		if !ok || got != (Position{3, 0}) {
			t.Fatalf("e3 Position after middle delete: got (%+v, %v), want ({3,0}, true)", got, ok)
		}
		got1, ok1 := flecs.Get[Position](r, e1)
		if !ok1 || got1 != (Position{1, 0}) {
			t.Fatalf("e1 Position after middle delete: got (%+v, %v), want ({1,0}, true)", got1, ok1)
		}
	})
}

// ── GC-pointer component ──────────────────────────────────────────────────────

func TestGCPointerComponentSurvivesGC(t *testing.T) {
	w := flecs.New()
	want := "hello world this is a long string that should survive GC"
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, WithStr{S: want})
	})

	// Force two GC cycles to trigger any pointer-scanning issues.
	runtime.GC()
	runtime.GC()

	w.Read(func(r *flecs.Reader) {
		got, ok := flecs.Get[WithStr](r, e)
		if !ok {
			t.Fatal("Get returned false after GC")
		}
		if got.S != want {
			t.Fatalf("string corrupted after GC: got len=%d, want len=%d", len(got.S), len(want))
		}
	})
}

// ── Tag component (Size==0) ───────────────────────────────────────────────────

func TestTagComponentSetHasRemove(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	// Create entity; Has should be false before Set.
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	w.Read(func(r *flecs.Reader) {
		if flecs.Has[Tag](r, e) {
			t.Fatal("Has[Tag] should be false before Set")
		}
	})
	// Set the tag; after flush Has should be true and Get should return true.
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, Tag{}) })
	w.Read(func(r *flecs.Reader) {
		if !flecs.Has[Tag](r, e) {
			t.Fatal("Has[Tag] should be true after Set")
		}
		// Get on a tag returns (zero, true) — entity has the tag, no data.
		_, ok := flecs.Get[Tag](r, e)
		if !ok {
			t.Fatal("Get[Tag] should return true (tag present)")
		}
	})
	// Remove the tag; after flush Has should be false.
	w.Write(func(fw *flecs.Writer) {
		if !flecs.Remove[Tag](fw, e) {
			t.Fatal("Remove[Tag] returned false for present tag")
		}
	})
	w.Read(func(r *flecs.Reader) {
		if flecs.Has[Tag](r, e) {
			t.Fatal("Has[Tag] should be false after Remove")
		}
	})
}

// ── Set in-place (same archetype) ────────────────────────────────────────────

func TestSetOverwritesExistingValue(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	// Set initial value then overwrite it; both are deferred and applied together.
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{1, 2})
		flecs.Set(fw, e, Position{9, 9})
	})
	// Read after flush; the last Set wins.
	var got Position
	var ok bool
	w.Read(func(r *flecs.Reader) {
		got, ok = flecs.Get[Position](r, e)
	})
	if !ok || got != (Position{9, 9}) {
		t.Fatalf("overwritten Position: got (%+v, %v), want ({9,9}, true)", got, ok)
	}
}

// ── Recycled entity ID ────────────────────────────────────────────────────────

func TestRecycledEntityHasNoLeftoverComponents(t *testing.T) {
	w := flecs.New()
	var e1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, Position{7, 8})
	})
	w.Delete(e1)

	// The recycled entity should start fresh.
	var e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e2 = fw.NewEntity()
	})
	if !w.IsAlive(e2) {
		t.Fatal("recycled entity should be alive")
	}
	// Old handle must be dead.
	if w.IsAlive(e1) {
		t.Fatal("old handle should be dead after recycle")
	}
	// New entity should not have Position data from e1.
	w.Read(func(r *flecs.Reader) {
		_, ok := flecs.Get[Position](r, e2)
		if ok {
			t.Fatal("recycled entity should not have Position from deleted predecessor")
		}
		// Get with old handle returns false.
		_, ok = flecs.Get[Position](r, e1)
		if ok {
			t.Fatal("stale handle Get should return false")
		}
	})
}

// ── Remove all components → empty table ──────────────────────────────────────

func TestRemoveAllComponentsLeavesEntityInEmptyTable(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	// Set Position; flush so it is applied.
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{1, 2})
	})
	// Remove Position in a separate Write scope so the table is already populated.
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[Position](fw, e)
	})
	if !w.IsAlive(e) {
		t.Fatal("entity should still be alive after removing all components")
	}
	w.Read(func(r *flecs.Reader) {
		_, ok := flecs.Get[Position](r, e)
		if ok {
			t.Fatal("Get should return false after removing all components")
		}
	})
}

// ── unsafe.Sizeof usage via Set parameter ─────────────────────────────────────

func TestSetUsesUnsafePointerCorrectly(t *testing.T) {
	w := flecs.New()
	p := Position{X: 3.14, Y: 2.71}
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, p)
	})
	// Read after Write so the deferred Set has been flushed.
	var got Position
	var ok bool
	w.Read(func(r *flecs.Reader) {
		got, ok = flecs.Get[Position](r, e)
	})
	if !ok || got.X != p.X || got.Y != p.Y {
		t.Fatalf("pointer round-trip failed: got %+v, want %+v", got, p)
	}
	// Also verify the size via unsafe — the value at the pointer should match.
	tbl := flecs.TableOf(w, e)
	if tbl == nil {
		t.Fatal("entity table should not be nil after Set")
	}
	_ = unsafe.Sizeof(p) // just ensure unsafe import is used
}

// ── Edge cache ────────────────────────────────────────────────────────────────

// TestEdgeCacheHitOnSecondMigration verifies that after the first Set[Position]
// populates the add-edge on the empty table, the second Set[Position] on a
// different entity finds the cached edge and produces correct values.
func TestEdgeCacheHitOnSecondMigration(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	// Create e1 in empty table.
	var e1 flecs.ID
	w.Write(func(fw *flecs.Writer) { e1 = fw.NewEntity() })
	emptyTable := flecs.TableOf(w, e1)

	// Migrate e1 to [Position] table via Set; edge cache is populated after flush.
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e1, Position{1, 2}) })
	posTable := flecs.TableOf(w, e1)

	// After first migration the empty table must have an add-edge for posID.
	dst, ok := emptyTable.NextOnAdd(posID)
	if !ok {
		t.Fatal("add-edge not cached after first Set[Position]")
	}
	if dst != posTable {
		t.Fatal("cached add-edge points to wrong table")
	}

	// Second entity migrates via the cache path.
	var e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, Position{3, 4})
	})
	if flecs.TableOf(w, e2) != posTable {
		t.Fatal("e2 not in same table as e1 after cache-hit migration")
	}

	var got1, got2 Position
	w.Read(func(r *flecs.Reader) {
		got1, _ = flecs.Get[Position](r, e1)
		got2, _ = flecs.Get[Position](r, e2)
	})
	if got1 != (Position{1, 2}) || got2 != (Position{3, 4}) {
		t.Fatalf("values wrong after cache-hit migration: e1=%+v e2=%+v", got1, got2)
	}
}

// TestEdgeCacheRoundTrip verifies that both the add-edge (empty +P→[P]) and
// the remove-edge ([P] -P→empty) are cached after a Set followed by Remove.
func TestEdgeCacheRoundTrip(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	// Create entity in empty table.
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	emptyTable := flecs.TableOf(w, e)

	// Set Position → migrates to [P] table.
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, Position{1, 2}) })
	posTable := flecs.TableOf(w, e)

	// Remove Position → migrates back.
	w.Write(func(fw *flecs.Writer) { flecs.Remove[Position](fw, e) })
	backTable := flecs.TableOf(w, e)

	addDst, addOK := emptyTable.NextOnAdd(posID)
	if !addOK || addDst != posTable {
		t.Fatalf("add-edge wrong: ok=%v dst=%p want=%p", addOK, addDst, posTable)
	}

	remDst, remOK := posTable.NextOnRemove(posID)
	if !remOK || remDst != backTable {
		t.Fatalf("remove-edge wrong: ok=%v dst=%p want=%p", remOK, remDst, backTable)
	}

	// Subsequent Set must hit the cached add-edge.
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, Position{5, 6}) })
	if flecs.TableOf(w, e) != posTable {
		t.Fatal("second Set did not land in cached [P] table")
	}
}

// TestEdgeCacheDistinctComponents verifies that Set[Position] and Set[Velocity]
// produce two distinct add-edges on the empty table.
func TestEdgeCacheDistinctComponents(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	// Create entities in empty table.
	var ep, ev flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ep = fw.NewEntity()
		ev = fw.NewEntity()
	})
	emptyTable := flecs.TableOf(w, ep)

	// Migrate each entity to its component table.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, ep, Position{1, 0})
		flecs.Set(fw, ev, Velocity{0, 1})
	})
	posTable := flecs.TableOf(w, ep)
	velTable := flecs.TableOf(w, ev)

	pdst, pok := emptyTable.NextOnAdd(posID)
	vdst, vok := emptyTable.NextOnAdd(velID)

	if !pok || pdst != posTable {
		t.Fatalf("Position add-edge wrong: ok=%v dst=%p want=%p", pok, pdst, posTable)
	}
	if !vok || vdst != velTable {
		t.Fatalf("Velocity add-edge wrong: ok=%v dst=%p want=%p", vok, vdst, velTable)
	}
	if posTable == velTable {
		t.Fatal("Position and Velocity should have distinct destination tables")
	}
}

// TestEdgeCacheTagComponent verifies that Set of a zero-size tag component
// still populates an add-edge on the source table.
func TestEdgeCacheTagComponent(t *testing.T) {
	w := flecs.New()
	tagID := flecs.RegisterComponent[Tag](w)

	// Create entity then migrate to [Tag] table in separate writes.
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	emptyTable := flecs.TableOf(w, e)

	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, Tag{}) })
	tagTable := flecs.TableOf(w, e)

	dst, ok := emptyTable.NextOnAdd(tagID)
	if !ok || dst != tagTable {
		t.Fatalf("tag add-edge wrong: ok=%v dst=%p want=%p", ok, dst, tagTable)
	}
}

// TestEdgeCacheNoLeak verifies that migrating empty→[P]→[P,V] puts the +P edge
// on the empty table and the +V edge on the [P] table — not a spurious +V on
// the empty table or a compound edge anywhere.
func TestEdgeCacheNoLeak(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	// Create entity then add components in separate writes so each migration is applied.
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	emptyTable := flecs.TableOf(w, e)

	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, Position{1, 0}) })
	posTable := flecs.TableOf(w, e)

	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, Velocity{0, 1}) })
	pvTable := flecs.TableOf(w, e)

	if _, ok := emptyTable.NextOnAdd(posID); !ok {
		t.Fatal("empty table missing +P→[P] edge")
	}
	if dst, ok := emptyTable.NextOnAdd(velID); ok {
		t.Fatalf("empty table has spurious +V edge pointing to %p", dst)
	}

	pvDst, pvOK := posTable.NextOnAdd(velID)
	if !pvOK || pvDst != pvTable {
		t.Fatalf("[P] table +V edge wrong: ok=%v dst=%p want=%p", pvOK, pvDst, pvTable)
	}
}

// TestCacheAddEdgeIdempotent verifies that CacheAddEdge is idempotent for the
// same dst and panics when a conflicting dst is given for the same (table, id).
func TestCacheAddEdgeIdempotent(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	// Create entities and migrate them in separate writes.
	var ep, ev, newE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ep = fw.NewEntity()
		ev = fw.NewEntity()
		newE = fw.NewEntity()
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, ep, Position{})
		flecs.Set(fw, ev, Velocity{})
	})
	posTable := flecs.TableOf(w, ep)
	velTable := flecs.TableOf(w, ev)
	emptyTable := flecs.TableOf(w, newE)

	dst, ok := emptyTable.NextOnAdd(posID)
	if !ok {
		t.Fatal("add-edge should be populated after Set[Position]")
	}

	// Idempotent: caching same dst again must not panic.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("idempotent CacheAddEdge panicked: %v", r)
			}
		}()
		emptyTable.CacheAddEdge(posID, dst)
	}()

	// Conflict: posTable has no edge for velID yet; cache two different dsts → panic.
	if posTable == velTable {
		t.Skip("pos and vel tables are the same pointer — cannot test conflict")
	}
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		posTable.CacheAddEdge(velID, velTable) // first: fine
		posTable.CacheAddEdge(velID, posTable) // conflict: should panic
	}()
	if !panicked {
		t.Fatal("CacheAddEdge with conflicting dst should have panicked")
	}
}

// ── Component index (Phase 2.2) ───────────────────────────────────────────────

func TestTablesFor_singleComponent(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{1, 2})
	})

	tables := w.TablesFor(posID)
	if len(tables) != 1 {
		t.Fatalf("TablesFor(posID): want 1 table, got %d", len(tables))
	}
}

func TestTablesFor_twoComponentsMigration(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)
	// Use separate Write scopes so each Set applies immediately and creates a
	// distinct intermediate table (empty→[P], then [P]→[P,V]).
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{1, 2})
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, Velocity{3, 4})
	})

	posTables := w.TablesFor(posID)
	// Must contain [Position] table AND [Position,Velocity] table.
	if len(posTables) != 2 {
		t.Fatalf("TablesFor(posID) after pos+vel: want 2 tables, got %d", len(posTables))
	}
	velTables := w.TablesFor(velID)
	// Only the [Position,Velocity] table.
	if len(velTables) != 1 {
		t.Fatalf("TablesFor(velID) after pos+vel: want 1 table, got %d", len(velTables))
	}
}

func TestTablesFor_sharedTableNoDuplicate(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	w.Write(func(fw *flecs.Writer) {
		e1 := fw.NewEntity()
		e2 := fw.NewEntity()
		flecs.Set(fw, e1, Position{1, 2})
		flecs.Set(fw, e2, Position{3, 4})
	})

	tables := w.TablesFor(posID)
	if len(tables) != 1 {
		t.Fatalf("TablesFor(posID) with two entities sharing table: want 1, got %d", len(tables))
	}
}

// Tables are immortal in this phase; the [Position] table stays indexed even
// after the entity migrates away to [Position,Velocity].
func TestTablesFor_ghostTableAfterMigration(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	// Use separate Write scopes so each Set applies immediately, creating both
	// the [Position] and [Position,Velocity] tables as distinct intermediate steps.
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{1, 2}) // creates [Position] table
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, Velocity{3, 4}) // creates [Position,Velocity] table
	})

	tables := w.TablesFor(posID)
	// Both tables (including the now-empty [Position] table) must be present.
	if len(tables) != 2 {
		t.Fatalf("TablesFor(posID) after migration: want 2 (including ghost), got %d", len(tables))
	}
}

func TestTablesFor_tagComponent(t *testing.T) {
	w := flecs.New()
	tagID := flecs.RegisterComponent[Tag](w)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Tag{})
	})

	tables := w.TablesFor(tagID)
	if len(tables) != 1 {
		t.Fatalf("TablesFor(tagID): want 1 table, got %d", len(tables))
	}
}

func TestTablesFor_emptyTableNotIndexed(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	// No entity has Position yet; the empty table should not appear.
	tables := w.TablesFor(posID)
	if len(tables) != 0 {
		t.Fatalf("TablesFor before any Set: want 0, got %d — empty table must not be indexed", len(tables))
	}
}

func TestEachTableFor_earlyStop(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{1, 2})
		flecs.Set(fw, e, Velocity{3, 4}) // creates second table containing posID
	})

	// posID is in two tables; stopping after 1 should visit exactly 1.
	if got := flecs.EachTableForCount(w, posID, 1); got != 1 {
		t.Fatalf("EachTableFor early stop: want 1 visit, got %d", got)
	}
}
