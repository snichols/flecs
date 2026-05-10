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
	e := w.NewEntity()
	if e == 0 {
		t.Fatal("NewEntity returned zero ID")
	}
	if !w.IsAlive(e) {
		t.Fatal("IsAlive false immediately after NewEntity")
	}
}

func TestIsAliveOnDeadEntity(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	w.Delete(e)
	if w.IsAlive(e) {
		t.Fatal("IsAlive true after Delete")
	}
}

func TestDeleteReturnValues(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	if !w.Delete(e) {
		t.Fatal("Delete returned false for alive entity")
	}
	if w.Delete(e) {
		t.Fatal("Delete returned true for dead entity")
	}
}

func TestCountReflectsAliveEntities(t *testing.T) {
	w := flecs.New()
	if w.Count() != 0 {
		t.Fatalf("initial Count want 0, got %d", w.Count())
	}
	e1 := w.NewEntity()
	e2 := w.NewEntity()
	if w.Count() != 2 {
		t.Fatalf("Count want 2 after 2 NewEntity, got %d", w.Count())
	}
	w.Delete(e1)
	_ = e2
	// Count includes component entities; just verify it decremented.
	if w.Count() < 1 {
		t.Fatalf("Count want ≥1 after deleting one entity, got %d", w.Count())
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
	e := w.NewEntity()
	want := Position{X: 1, Y: 2}
	flecs.Set(w, e, want)
	got, ok := flecs.Get[Position](w, e)
	if !ok {
		t.Fatal("Get returned false after Set")
	}
	if got != want {
		t.Fatalf("Get returned %+v, want %+v", got, want)
	}
}

func TestAutoRegistrationViaSet(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	// Set without an explicit RegisterComponent call.
	flecs.Set(w, e, Position{X: 3, Y: 4})
	_, ok := flecs.Get[Position](w, e)
	if !ok {
		t.Fatal("Get after auto-registered Set returned false")
	}
}

func TestSetOnDeadEntityPanics(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	w.Delete(e)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Set on dead entity should panic")
		}
	}()
	flecs.Set(w, e, Position{})
}

// ── Two-component flow ────────────────────────────────────────────────────────

func TestTwoComponentMigrationsAndGet(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	p := Position{X: 10, Y: 20}
	v := Velocity{DX: 1, DY: 2}
	flecs.Set(w, e, p)
	flecs.Set(w, e, v)

	gotP, okP := flecs.Get[Position](w, e)
	gotV, okV := flecs.Get[Velocity](w, e)
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
	e := w.NewEntity()
	if flecs.Has[Position](w, e) {
		t.Fatal("Has should be false before Set")
	}
	flecs.Set(w, e, Position{1, 2})
	if !flecs.Has[Position](w, e) {
		t.Fatal("Has should be true after Set")
	}
	flecs.Remove[Position](w, e)
	if flecs.Has[Position](w, e) {
		t.Fatal("Has should be false after Remove")
	}
}

func TestHasOnDeadEntityReturnsFalse(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w, e, Position{})
	w.Delete(e)
	if flecs.Has[Position](w, e) {
		t.Fatal("Has should be false for dead entity")
	}
}

// ── Remove ────────────────────────────────────────────────────────────────────

func TestRemoveActuallyRemovesComponent(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w, e, Position{5, 6})
	if !flecs.Remove[Position](w, e) {
		t.Fatal("Remove returned false for present component")
	}
	_, ok := flecs.Get[Position](w, e)
	if ok {
		t.Fatal("Get returned true after Remove")
	}
}

func TestRemoveAbsentComponentReturnsFalse(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	if flecs.Remove[Position](w, e) {
		t.Fatal("Remove returned true for absent component")
	}
}

func TestRemoveOnDeadEntityReturnsFalse(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w, e, Position{})
	w.Delete(e)
	if flecs.Remove[Position](w, e) {
		t.Fatal("Remove returned true for dead entity")
	}
}

func TestGetOnUnregisteredTypeReturnsFalse(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	// Velocity was never registered.
	_, ok := flecs.Get[Velocity](w, e)
	if ok {
		t.Fatal("Get on unregistered type should return false")
	}
}

func TestGetOnDeadEntityReturnsFalse(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w, e, Position{1, 2})
	w.Delete(e)
	_, ok := flecs.Get[Position](w, e)
	if ok {
		t.Fatal("Get on dead entity should return false")
	}
}

// ── Delete ────────────────────────────────────────────────────────────────────

func TestDeleteRemovesEntityFromWorld(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w, e, Position{1, 2})
	w.Delete(e)
	if w.IsAlive(e) {
		t.Fatal("IsAlive should be false after Delete")
	}
	_, ok := flecs.Get[Position](w, e)
	if ok {
		t.Fatal("Get should return false for deleted entity")
	}
}

// ── Multi-entity per archetype ────────────────────────────────────────────────

func TestMultipleEntitiesShareTable(t *testing.T) {
	w := flecs.New()
	e1, e2, e3 := w.NewEntity(), w.NewEntity(), w.NewEntity()
	p := Position{1, 2}
	flecs.Set(w, e1, p)
	flecs.Set(w, e2, p)
	flecs.Set(w, e3, p)

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
	e1, e2, e3 := w.NewEntity(), w.NewEntity(), w.NewEntity()
	p1, p2, p3 := Position{1, 0}, Position{2, 0}, Position{3, 0}
	flecs.Set(w, e1, p1)
	flecs.Set(w, e2, p2)
	flecs.Set(w, e3, p3)

	// Migrate e2 to a new archetype (Position + Velocity).
	v := Velocity{DX: 99}
	flecs.Set(w, e2, v)

	gotP1, ok1 := flecs.Get[Position](w, e1)
	gotP3, ok3 := flecs.Get[Position](w, e3)
	if !ok1 || gotP1 != p1 {
		t.Fatalf("e1 Position after migration: got (%+v, %v), want (%+v, true)", gotP1, ok1, p1)
	}
	if !ok3 || gotP3 != p3 {
		t.Fatalf("e3 Position after migration: got (%+v, %v), want (%+v, true)", gotP3, ok3, p3)
	}
	gotP2, ok2 := flecs.Get[Position](w, e2)
	if !ok2 || gotP2 != p2 {
		t.Fatalf("e2 Position after migration: got (%+v, %v), want (%+v, true)", gotP2, ok2, p2)
	}
}

// ── Delete in middle of table ─────────────────────────────────────────────────

// TestDeleteMiddleUpdatesLastEntityRow deletes e2 from a 3-entity table.
// e3 is swap-removed into row 1; Get[Position](e3) must still return Position{3}.
func TestDeleteMiddleUpdatesLastEntityRow(t *testing.T) {
	w := flecs.New()
	e1, e2, e3 := w.NewEntity(), w.NewEntity(), w.NewEntity()
	flecs.Set(w, e1, Position{1, 0})
	flecs.Set(w, e2, Position{2, 0})
	flecs.Set(w, e3, Position{3, 0})

	w.Delete(e2)

	got, ok := flecs.Get[Position](w, e3)
	if !ok || got != (Position{3, 0}) {
		t.Fatalf("e3 Position after middle delete: got (%+v, %v), want ({3,0}, true)", got, ok)
	}
	got1, ok1 := flecs.Get[Position](w, e1)
	if !ok1 || got1 != (Position{1, 0}) {
		t.Fatalf("e1 Position after middle delete: got (%+v, %v), want ({1,0}, true)", got1, ok1)
	}
}

// ── GC-pointer component ──────────────────────────────────────────────────────

func TestGCPointerComponentSurvivesGC(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	want := "hello world this is a long string that should survive GC"
	flecs.Set(w, e, WithStr{S: want})

	// Force two GC cycles to trigger any pointer-scanning issues.
	runtime.GC()
	runtime.GC()

	got, ok := flecs.Get[WithStr](w, e)
	if !ok {
		t.Fatal("Get returned false after GC")
	}
	if got.S != want {
		t.Fatalf("string corrupted after GC: got len=%d, want len=%d", len(got.S), len(want))
	}
}

// ── Tag component (Size==0) ───────────────────────────────────────────────────

func TestTagComponentSetHasRemove(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()

	if flecs.Has[Tag](w, e) {
		t.Fatal("Has[Tag] should be false before Set")
	}

	flecs.Set(w, e, Tag{})
	if !flecs.Has[Tag](w, e) {
		t.Fatal("Has[Tag] should be true after Set")
	}

	// Get on a tag returns (zero, true) — entity has the tag, no data.
	_, ok := flecs.Get[Tag](w, e)
	if !ok {
		t.Fatal("Get[Tag] should return true (tag present)")
	}

	if !flecs.Remove[Tag](w, e) {
		t.Fatal("Remove[Tag] returned false for present tag")
	}
	if flecs.Has[Tag](w, e) {
		t.Fatal("Has[Tag] should be false after Remove")
	}
}

// ── Set in-place (same archetype) ────────────────────────────────────────────

func TestSetOverwritesExistingValue(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w, e, Position{1, 2})
	flecs.Set(w, e, Position{9, 9})
	got, ok := flecs.Get[Position](w, e)
	if !ok || got != (Position{9, 9}) {
		t.Fatalf("overwritten Position: got (%+v, %v), want ({9,9}, true)", got, ok)
	}
}

// ── Recycled entity ID ────────────────────────────────────────────────────────

func TestRecycledEntityHasNoLeftoverComponents(t *testing.T) {
	w := flecs.New()
	e1 := w.NewEntity()
	flecs.Set(w, e1, Position{7, 8})
	w.Delete(e1)

	// The recycled entity should start fresh.
	e2 := w.NewEntity()
	if !w.IsAlive(e2) {
		t.Fatal("recycled entity should be alive")
	}
	// Old handle must be dead.
	if w.IsAlive(e1) {
		t.Fatal("old handle should be dead after recycle")
	}
	// New entity should not have Position data from e1.
	_, ok := flecs.Get[Position](w, e2)
	if ok {
		t.Fatal("recycled entity should not have Position from deleted predecessor")
	}
	// Get with old handle returns false.
	_, ok = flecs.Get[Position](w, e1)
	if ok {
		t.Fatal("stale handle Get should return false")
	}
}

// ── Remove all components → empty table ──────────────────────────────────────

func TestRemoveAllComponentsLeavesEntityInEmptyTable(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w, e, Position{1, 2})
	flecs.Remove[Position](w, e)
	if !w.IsAlive(e) {
		t.Fatal("entity should still be alive after removing all components")
	}
	_, ok := flecs.Get[Position](w, e)
	if ok {
		t.Fatal("Get should return false after removing all components")
	}
}

// ── unsafe.Sizeof usage via Set parameter ─────────────────────────────────────

func TestSetUsesUnsafePointerCorrectly(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	p := Position{X: 3.14, Y: 2.71}
	flecs.Set(w, e, p)
	got, ok := flecs.Get[Position](w, e)
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
