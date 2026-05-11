package flecs_test

import (
	"testing"
	"unsafe"

	"github.com/snichols/flecs"
	"github.com/snichols/flecs/internal/component"
)

// ── Registration ──────────────────────────────────────────────────────────────

func TestOnSetRegistrationReplaces(t *testing.T) {
	w := flecs.New()
	calls := 0
	flecs.OnSet[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { calls++ })
	flecs.OnSet[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { calls += 10 })
	e := w.NewEntity()
	flecs.Set[Position](w.W(), e, Position{1, 2})
	if calls != 10 {
		t.Fatalf("want 10 (second hook only), got %d", calls)
	}
}

func TestOnSetClearWithNil(t *testing.T) {
	w := flecs.New()
	calls := 0
	flecs.OnSet[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { calls++ })
	flecs.OnSet[Position](w, nil)
	e := w.NewEntity()
	flecs.Set[Position](w.W(), e, Position{1, 2})
	if calls != 0 {
		t.Fatalf("hook should have been cleared, got %d calls", calls)
	}
}

func TestOnAddClearWithNil(t *testing.T) {
	w := flecs.New()
	calls := 0
	flecs.OnAdd[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { calls++ })
	flecs.OnAdd[Position](w, nil)
	e := w.NewEntity()
	flecs.Set[Position](w.W(), e, Position{1, 2})
	if calls != 0 {
		t.Fatalf("OnAdd hook should have been cleared, got %d calls", calls)
	}
}

func TestOnRemoveClearWithNil(t *testing.T) {
	w := flecs.New()
	calls := 0
	flecs.OnRemove[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { calls++ })
	flecs.OnRemove[Position](w, nil)
	e := w.NewEntity()
	flecs.Set[Position](w.W(), e, Position{1, 2})
	flecs.Remove[Position](w.W(), e)
	if calls != 0 {
		t.Fatalf("OnRemove hook should have been cleared, got %d calls", calls)
	}
}

// ── OnAdd / OnSet fire counts ────────────────────────────────────────────────

func TestOnAddFiresOnceOnInitialSet(t *testing.T) {
	w := flecs.New()
	addCount, setCount := 0, 0
	flecs.OnAdd[Position](w, func(_ *flecs.Writer, e flecs.ID, p Position) { addCount++ })
	flecs.OnSet[Position](w, func(_ *flecs.Writer, e flecs.ID, p Position) { setCount++ })
	e := w.NewEntity()
	flecs.Set[Position](w.W(), e, Position{1, 2})
	if addCount != 1 {
		t.Fatalf("OnAdd want 1, got %d", addCount)
	}
	if setCount != 1 {
		t.Fatalf("OnSet want 1, got %d", setCount)
	}
}

func TestOnSetFiresEverySet(t *testing.T) {
	w := flecs.New()
	addCount, setCount := 0, 0
	flecs.OnAdd[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { addCount++ })
	flecs.OnSet[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { setCount++ })
	e := w.NewEntity()
	flecs.Set[Position](w.W(), e, Position{1, 2})
	flecs.Set[Position](w.W(), e, Position{3, 4})
	if addCount != 1 {
		t.Fatalf("OnAdd want 1 (initial only), got %d", addCount)
	}
	if setCount != 2 {
		t.Fatalf("OnSet want 2, got %d", setCount)
	}
}

func TestOnSetReceivesCorrectValue(t *testing.T) {
	w := flecs.New()
	var got Position
	flecs.OnSet[Position](w, func(_ *flecs.Writer, _ flecs.ID, p Position) { got = p })
	e := w.NewEntity()
	flecs.Set[Position](w.W(), e, Position{7, 9})
	if got != (Position{7, 9}) {
		t.Fatalf("OnSet value want {7,9}, got %v", got)
	}
}

func TestOnSetReceivesEntityID(t *testing.T) {
	w := flecs.New()
	var gotID flecs.ID
	flecs.OnSet[Position](w, func(_ *flecs.Writer, e flecs.ID, _ Position) { gotID = e })
	e := w.NewEntity()
	flecs.Set[Position](w.W(), e, Position{1, 2})
	if gotID != e {
		t.Fatalf("OnSet entity want %v, got %v", e, gotID)
	}
}

// ── OnAdd fires on AddID ──────────────────────────────────────────────────────

func TestOnAddFiresOnAddID(t *testing.T) {
	w := flecs.New()
	addCount := 0
	flecs.OnAdd[Tag](w, func(_ *flecs.Writer, _ flecs.ID, _ Tag) { addCount++ })
	tagID := flecs.RegisterComponent[Tag](w)
	e := w.NewEntity()
	flecs.AddID(w.W(), e, tagID)
	if addCount != 1 {
		t.Fatalf("OnAdd want 1 after AddID, got %d", addCount)
	}
	// Second AddID on same entity is no-op: no hook.
	flecs.AddID(w.W(), e, tagID)
	if addCount != 1 {
		t.Fatalf("OnAdd want 1 (idempotent AddID), got %d", addCount)
	}
}

// ── OnAdd does NOT fire for carried-over components ───────────────────────────

func TestOnAddNotFiredForCarriedComponents(t *testing.T) {
	w := flecs.New()
	posAddCount, velAddCount := 0, 0
	flecs.OnAdd[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { posAddCount++ })
	flecs.OnAdd[Velocity](w, func(_ *flecs.Writer, _ flecs.ID, _ Velocity) { velAddCount++ })
	e := w.NewEntity()
	flecs.Set[Position](w.W(), e, Position{1, 2}) // migrates: adds Position → OnAdd[Pos] fires
	flecs.Set[Velocity](w.W(), e, Velocity{3, 4}) // migrates: adds Velocity → OnAdd[Vel] fires only
	if posAddCount != 1 {
		t.Fatalf("OnAdd[Position] want 1, got %d", posAddCount)
	}
	if velAddCount != 1 {
		t.Fatalf("OnAdd[Velocity] want 1, got %d", velAddCount)
	}
}

// ── OnRemove fires before removal ────────────────────────────────────────────

func TestOnRemoveFiresBeforeRemoval(t *testing.T) {
	w := flecs.New()
	var captured Position
	flecs.OnRemove[Position](w, func(_ *flecs.Writer, _ flecs.ID, p Position) { captured = p })
	e := w.NewEntity()
	flecs.Set[Position](w.W(), e, Position{1, 2})
	flecs.Remove[Position](w.W(), e)
	if captured != (Position{1, 2}) {
		t.Fatalf("OnRemove captured want {1,2}, got %v", captured)
	}
}

func TestOnRemoveEntityStillAliveInCallback(t *testing.T) {
	w := flecs.New()
	alive := false
	flecs.OnRemove[Position](w, func(_ *flecs.Writer, e flecs.ID, _ Position) {
		alive = w.IsAlive(e)
	})
	e := w.NewEntity()
	flecs.Set[Position](w.W(), e, Position{1, 2})
	flecs.Remove[Position](w.W(), e)
	if !alive {
		t.Fatal("entity should still be alive inside OnRemove callback")
	}
}

// ── OnRemove fires per-component on Delete ───────────────────────────────────

func TestOnRemoveFiresPerComponentOnDelete(t *testing.T) {
	w := flecs.New()
	posRemoved, velRemoved := false, false
	flecs.OnRemove[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { posRemoved = true })
	flecs.OnRemove[Velocity](w, func(_ *flecs.Writer, _ flecs.ID, _ Velocity) { velRemoved = true })
	e := w.NewEntity()
	flecs.Set[Position](w.W(), e, Position{1, 2})
	flecs.Set[Velocity](w.W(), e, Velocity{3, 4})
	w.Delete(e)
	if !posRemoved {
		t.Fatal("OnRemove[Position] did not fire on Delete")
	}
	if !velRemoved {
		t.Fatal("OnRemove[Velocity] did not fire on Delete")
	}
}

// ── OnRemove fires in cascade-delete (child-first) ───────────────────────────

func TestOnRemoveFiresCascadeChildFirst(t *testing.T) {
	w := flecs.New()
	var order []string
	flecs.OnRemove[Position](w, func(_ *flecs.Writer, e flecs.ID, _ Position) {
		order = append(order, "pos")
		_ = e
	})
	parent := w.NewEntity()
	child := w.NewEntity()
	flecs.Set[Position](w.W(), parent, Position{1, 2})
	flecs.Set[Position](w.W(), child, Position{3, 4})
	flecs.AddID(w.W(), child, flecs.MakePair(w.ChildOf(), parent))
	w.Delete(parent)
	// Both OnRemove[Position] calls must have fired.
	if len(order) != 2 {
		t.Fatalf("OnRemove want 2 calls (child+parent), got %d", len(order))
	}
}

// ── No hook fires for inherited components (IsA) ─────────────────────────────

func TestNoHookForInheritedComponents(t *testing.T) {
	w := flecs.New()
	addCount, setCount := 0, 0
	flecs.OnAdd[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { addCount++ })
	flecs.OnSet[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { setCount++ })
	prefab := w.NewEntity()
	flecs.Set[Position](w.W(), prefab, Position{1, 2})
	// Reset counts after setting prefab.
	addCount, setCount = 0, 0
	child := w.NewEntity()
	flecs.AddID(w.W(), child, flecs.MakePair(w.IsA(), prefab))
	// Get resolves via IsA chain — no hook should fire.
	v, ok := flecs.Get[Position](w.R(), child)
	if !ok || v != (Position{1, 2}) {
		t.Fatalf("Get via IsA: want {1,2} ok=true, got %v ok=%v", v, ok)
	}
	if addCount != 0 || setCount != 0 {
		t.Fatalf("inherited Get must not fire hooks: add=%d set=%d", addCount, setCount)
	}
}

// ── Override fires OnAdd ──────────────────────────────────────────────────────

func TestOverrideFiresOnAdd(t *testing.T) {
	w := flecs.New()
	addCount := 0
	flecs.OnAdd[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { addCount++ })
	prefab := w.NewEntity()
	flecs.Set[Position](w.W(), prefab, Position{1, 2})
	// Reset after setting prefab.
	addCount = 0
	child := w.NewEntity()
	flecs.AddID(w.W(), child, flecs.MakePair(w.IsA(), prefab))
	// Copy-on-write override: Position is not locally owned, so migrate fires OnAdd.
	flecs.Set[Position](w.W(), child, Position{3, 4})
	if addCount != 1 {
		t.Fatalf("override OnAdd want 1, got %d", addCount)
	}
}

// ── No hook fires when no hook is registered ─────────────────────────────────

func TestNoHookNoPanic(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set[Position](w.W(), e, Position{1, 2})
	flecs.Set[Position](w.W(), e, Position{3, 4})
	flecs.Remove[Position](w.W(), e)
	w.Delete(e)
	// Must not panic.
}

// ── Hook panic propagates ─────────────────────────────────────────────────────

func TestHookPanicPropagates(t *testing.T) {
	w := flecs.New()
	flecs.OnSet[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) {
		panic("hook panic")
	})
	e := w.NewEntity()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic to propagate through Set")
		}
	}()
	flecs.Set[Position](w.W(), e, Position{1, 2})
}

// ── Re-entrancy: read-only is safe ───────────────────────────────────────────

func TestOnSetReadOnlyReentrancy(t *testing.T) {
	w := flecs.New()
	var gotVel Velocity
	e := w.NewEntity()
	flecs.Set[Velocity](w.W(), e, Velocity{5, 6})
	flecs.OnSet[Position](w, func(_ *flecs.Writer, eid flecs.ID, _ Position) {
		gotVel, _ = flecs.Get[Velocity](w.R(), eid)
	})
	flecs.Set[Position](w.W(), e, Position{1, 2})
	if gotVel != (Velocity{5, 6}) {
		t.Fatalf("re-entrant Get[Velocity] want {5,6}, got %v", gotVel)
	}
}

// ── Tag (Size==0) — OnAdd fires ───────────────────────────────────────────────

func TestOnAddTagFires(t *testing.T) {
	w := flecs.New()
	addCount := 0
	flecs.OnAdd[Tag](w, func(_ *flecs.Writer, _ flecs.ID, _ Tag) { addCount++ })
	tagID := flecs.RegisterComponent[Tag](w)
	e := w.NewEntity()
	flecs.AddID(w.W(), e, tagID)
	if addCount != 1 {
		t.Fatalf("OnAdd[Tag] want 1, got %d", addCount)
	}
}

// ── SetPair fires OnAdd and OnSet on pair's TypeInfo ─────────────────────────

func TestSetPairFiresPairTypeInfoHooks(t *testing.T) {
	w := flecs.New()
	rel := w.NewEntity()
	tgt := w.NewEntity()
	pairID := flecs.MakePair(rel, tgt)
	pairInfo := component.RegisterPairData[Position](w.Registry(), pairID)

	addCount, setCount := 0, 0
	pairInfo.Hooks.OnAdd = func(_ any, _ flecs.ID, ptr unsafe.Pointer) { addCount++ }
	pairInfo.Hooks.OnSet = func(_ any, _ flecs.ID, ptr unsafe.Pointer) { setCount++ }

	e := w.NewEntity()
	flecs.SetPair[Position](w.W(), e, rel, tgt, Position{1, 2})
	if addCount != 1 {
		t.Fatalf("pair OnAdd want 1, got %d", addCount)
	}
	if setCount != 1 {
		t.Fatalf("pair OnSet want 1, got %d", setCount)
	}

	// Second call: OnAdd must NOT fire again, OnSet must fire.
	flecs.SetPair[Position](w.W(), e, rel, tgt, Position{3, 4})
	if addCount != 1 {
		t.Fatalf("pair OnAdd want 1 (no re-add), got %d", addCount)
	}
	if setCount != 2 {
		t.Fatalf("pair OnSet want 2, got %d", setCount)
	}
}

// ── TestHookReceivesWriter verifies that the *Writer passed to hooks is non-nil ──

func TestHookReceivesWriter(t *testing.T) {
	w := flecs.New()
	var gotWriter *flecs.Writer
	flecs.OnSet[Position](w, func(fw *flecs.Writer, _ flecs.ID, _ Position) {
		gotWriter = fw
	})
	e := w.NewEntity()
	flecs.Set[Position](w.W(), e, Position{1, 2})
	if gotWriter == nil {
		t.Fatal("OnSet hook received nil *Writer")
	}
}
