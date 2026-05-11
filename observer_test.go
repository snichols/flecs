package flecs_test

import (
	"testing"
	"unsafe"

	"github.com/snichols/flecs"
)

// ── Single observer fires ────────────────────────────────────────────────────

func TestObserveSingleFires(t *testing.T) {
	w := flecs.New()
	var gotID flecs.ID
	var gotVal Position

	_ = flecs.Observe[Position](w, flecs.EventOnSet, func(e flecs.ID, p *Position) {
		gotID = e
		gotVal = *p
	})

	e := w.NewEntity()
	flecs.Set[Position](w, e, Position{3, 7})

	if gotID != e {
		t.Fatalf("want entity %v, got %v", e, gotID)
	}
	if gotVal != (Position{3, 7}) {
		t.Fatalf("want {3,7}, got %v", gotVal)
	}
}

// ── Multiple observers fire in registration order ────────────────────────────

func TestObserveMultipleFiringOrder(t *testing.T) {
	w := flecs.New()
	var order []string

	_ = flecs.Observe[Position](w, flecs.EventOnSet, func(_ flecs.ID, _ *Position) {
		order = append(order, "A")
	})
	_ = flecs.Observe[Position](w, flecs.EventOnSet, func(_ flecs.ID, _ *Position) {
		order = append(order, "B")
	})

	e := w.NewEntity()
	flecs.Set[Position](w, e, Position{1, 2})

	if len(order) != 2 || order[0] != "A" || order[1] != "B" {
		t.Fatalf("want [A B], got %v", order)
	}
}

// ── Observer on different event does not fire ─────────────────────────────────

func TestObserveDifferentEventNoFire(t *testing.T) {
	w := flecs.New()
	addCount, setCount := 0, 0

	_ = flecs.Observe[Position](w, flecs.EventOnAdd, func(_ flecs.ID, _ *Position) { addCount++ })
	_ = flecs.Observe[Position](w, flecs.EventOnSet, func(_ flecs.ID, _ *Position) { setCount++ })

	e := w.NewEntity()
	flecs.Set[Position](w, e, Position{1, 2}) // fires OnAdd + OnSet
	flecs.Set[Position](w, e, Position{3, 4}) // fires OnSet only

	if addCount != 1 {
		t.Fatalf("OnAdd observer want 1, got %d", addCount)
	}
	if setCount != 2 {
		t.Fatalf("OnSet observer want 2, got %d", setCount)
	}
}

// ── Unsubscribe stops firing ──────────────────────────────────────────────────

func TestObserveUnsubscribeStopsFiring(t *testing.T) {
	w := flecs.New()
	calls := 0

	obs := flecs.Observe[Position](w, flecs.EventOnSet, func(_ flecs.ID, _ *Position) { calls++ })
	obs.Unsubscribe()

	e := w.NewEntity()
	flecs.Set[Position](w, e, Position{1, 2})

	if calls != 0 {
		t.Fatalf("want 0 calls after Unsubscribe, got %d", calls)
	}
}

// ── Unsubscribe is idempotent ─────────────────────────────────────────────────

func TestObserveUnsubscribeIdempotent(t *testing.T) {
	w := flecs.New()
	calls := 0

	obs := flecs.Observe[Position](w, flecs.EventOnSet, func(_ flecs.ID, _ *Position) { calls++ })
	obs.Unsubscribe()
	obs.Unsubscribe() // must not panic

	e := w.NewEntity()
	flecs.Set[Position](w, e, Position{1, 2})

	if calls != 0 {
		t.Fatalf("want 0 after double Unsubscribe, got %d", calls)
	}
}

// ── Unsubscribe during dispatch: deferred removal ────────────────────────────
//
// A fires, A's callback calls obs2.Unsubscribe(). Deferred-removal contract:
// A fires; B (obs2) still fires for this event; B does NOT fire on the next Set.

func TestObserveUnsubscribeDuringDispatch(t *testing.T) {
	w := flecs.New()
	var order []string
	var obs2 *flecs.Observer

	_ = flecs.Observe[Position](w, flecs.EventOnSet, func(_ flecs.ID, _ *Position) {
		order = append(order, "A")
		obs2.Unsubscribe()
	})
	obs2 = flecs.Observe[Position](w, flecs.EventOnSet, func(_ flecs.ID, _ *Position) {
		order = append(order, "B")
	})

	e := w.NewEntity()
	flecs.Set[Position](w, e, Position{1, 2}) // A fires, calls obs2.Unsubscribe(); B still fires

	if len(order) != 2 || order[0] != "A" || order[1] != "B" {
		t.Fatalf("first Set: want [A B], got %v", order)
	}

	order = order[:0]
	flecs.Set[Position](w, e, Position{3, 4}) // B must NOT fire (removed)

	if len(order) != 1 || order[0] != "A" {
		t.Fatalf("second Set: want [A], got %v", order)
	}
}

// ── Observer and hook coexist: hook fires first ───────────────────────────────

func TestObserveHookAndObserverOrder(t *testing.T) {
	w := flecs.New()
	var order []string

	flecs.OnSet[Position](w, func(_ flecs.ID, _ *Position) { order = append(order, "hook") })
	_ = flecs.Observe[Position](w, flecs.EventOnSet, func(_ flecs.ID, _ *Position) { order = append(order, "obs") })

	e := w.NewEntity()
	flecs.Set[Position](w, e, Position{1, 2})

	if len(order) != 2 || order[0] != "hook" || order[1] != "obs" {
		t.Fatalf("want [hook obs], got %v", order)
	}
}

// ── Observer on raw entity (tag) ──────────────────────────────────────────────

func TestObserveIDTag(t *testing.T) {
	w := flecs.New()
	fired := false

	tagID := w.NewEntity()
	_ = flecs.ObserveID(w, tagID, flecs.EventOnAdd, func(e flecs.ID, _ unsafe.Pointer) {
		fired = true
	})

	e := w.NewEntity()
	flecs.AddID(w, e, tagID)

	if !fired {
		t.Fatal("ObserveID observer did not fire for AddID")
	}
}

// ── Observer on pair-id ───────────────────────────────────────────────────────

func TestObserveIDPair(t *testing.T) {
	w := flecs.New()

	rel := w.NewEntity()
	tgt := w.NewEntity()
	pairID := flecs.MakePair(rel, tgt)

	var gotPtr unsafe.Pointer
	_ = flecs.ObserveID(w, pairID, flecs.EventOnSet, func(_ flecs.ID, ptr unsafe.Pointer) {
		gotPtr = ptr
	})

	e := w.NewEntity()
	flecs.SetPair[Position](w, e, rel, tgt, Position{5, 6})

	if gotPtr == nil {
		t.Fatal("ObserveID pair observer did not fire")
	}
	got := *(*Position)(gotPtr)
	if got != (Position{5, 6}) {
		t.Fatalf("pair observer value: want {5,6}, got %v", got)
	}
}

// ── No firing for inherited components ───────────────────────────────────────

func TestObserveNoFireForInherited(t *testing.T) {
	w := flecs.New()
	addCount, setCount := 0, 0

	_ = flecs.Observe[Position](w, flecs.EventOnAdd, func(_ flecs.ID, _ *Position) { addCount++ })
	_ = flecs.Observe[Position](w, flecs.EventOnSet, func(_ flecs.ID, _ *Position) { setCount++ })

	prefab := w.NewEntity()
	flecs.Set[Position](w, prefab, Position{1, 2})
	addCount, setCount = 0, 0 // reset after prefab setup

	child := w.NewEntity()
	flecs.AddID(w, child, flecs.MakePair(w.IsA(), prefab))
	v, ok := flecs.Get[Position](w, child)
	if !ok || v != (Position{1, 2}) {
		t.Fatalf("Get via IsA: want {1,2}, got %v ok=%v", v, ok)
	}

	if addCount != 0 || setCount != 0 {
		t.Fatalf("inherited Get must not fire observers: add=%d set=%d", addCount, setCount)
	}
}

// ── Cascade delete fires observers in post-order (child first) ───────────────

func TestObserveCascadeDeletePostOrder(t *testing.T) {
	w := flecs.New()
	var order []flecs.ID

	_ = flecs.Observe[Position](w, flecs.EventOnRemove, func(e flecs.ID, _ *Position) {
		order = append(order, e)
	})

	parent := w.NewEntity()
	child := w.NewEntity()
	flecs.Set[Position](w, parent, Position{1, 2})
	flecs.Set[Position](w, child, Position{3, 4})
	flecs.AddID(w, child, flecs.MakePair(w.ChildOf(), parent))

	w.Delete(parent)

	if len(order) != 2 {
		t.Fatalf("want 2 observer fires, got %d", len(order))
	}
	// post-order: child deleted before parent
	if order[0] != child {
		t.Fatalf("want child first in cascade, got %v", order)
	}
	if order[1] != parent {
		t.Fatalf("want parent second in cascade, got %v", order)
	}
}

// ── Multiple event kinds, correct subset fires ────────────────────────────────

func TestObserveMultipleEventKinds(t *testing.T) {
	w := flecs.New()
	addCount, setCount, remCount := 0, 0, 0

	_ = flecs.Observe[Position](w, flecs.EventOnAdd, func(_ flecs.ID, _ *Position) { addCount++ })
	_ = flecs.Observe[Position](w, flecs.EventOnSet, func(_ flecs.ID, _ *Position) { setCount++ })
	_ = flecs.Observe[Position](w, flecs.EventOnRemove, func(_ flecs.ID, _ *Position) { remCount++ })

	e := w.NewEntity()
	flecs.Set[Position](w, e, Position{1, 2}) // OnAdd + OnSet
	flecs.Set[Position](w, e, Position{3, 4}) // OnSet only
	flecs.Remove[Position](w, e)              // OnRemove only

	if addCount != 1 {
		t.Fatalf("OnAdd want 1, got %d", addCount)
	}
	if setCount != 2 {
		t.Fatalf("OnSet want 2, got %d", setCount)
	}
	if remCount != 1 {
		t.Fatalf("OnRemove want 1, got %d", remCount)
	}
}

// ── Observe[T] auto-registers component ──────────────────────────────────────

type NewObsType struct{ V int }

func TestObserveAutoRegisters(t *testing.T) {
	w := flecs.New()
	before := w.Count()

	fired := false
	_ = flecs.Observe[NewObsType](w, flecs.EventOnSet, func(_ flecs.ID, _ *NewObsType) {
		fired = true
	})

	if w.Count() <= before {
		t.Fatal("Observe[T] should increment world component count via auto-registration")
	}

	e := w.NewEntity()
	flecs.Set[NewObsType](w, e, NewObsType{42})
	if !fired {
		t.Fatal("observer for auto-registered type did not fire")
	}
}

// ── ObserveID does NOT auto-register ─────────────────────────────────────────

func TestObserveIDNoAutoRegister(t *testing.T) {
	w := flecs.New()
	rawID := w.NewEntity() // raw entity, not a registered component
	before := w.Count()

	fired := false
	_ = flecs.ObserveID(w, rawID, flecs.EventOnAdd, func(_ flecs.ID, _ unsafe.Pointer) {
		fired = true
	})

	if w.Count() != before {
		t.Fatal("ObserveID must not auto-register or change Count")
	}

	// Observer is registered but fires only when AddID uses that exact id.
	e := w.NewEntity()
	flecs.AddID(w, e, rawID)
	if !fired {
		t.Fatal("ObserveID observer did not fire after AddID with the raw id")
	}
}

// ── Existing Phase 5.1 hook tests still pass (smoke check) ───────────────────

func TestObserveHooksStillGreen(t *testing.T) {
	w := flecs.New()
	addCount, setCount, remCount := 0, 0, 0

	flecs.OnAdd[Position](w, func(_ flecs.ID, _ *Position) { addCount++ })
	flecs.OnSet[Position](w, func(_ flecs.ID, _ *Position) { setCount++ })
	flecs.OnRemove[Position](w, func(_ flecs.ID, _ *Position) { remCount++ })

	e := w.NewEntity()
	flecs.Set[Position](w, e, Position{1, 2})
	flecs.Set[Position](w, e, Position{3, 4})
	flecs.Remove[Position](w, e)

	if addCount != 1 || setCount != 2 || remCount != 1 {
		t.Fatalf("hook counts: add=%d set=%d rem=%d (want 1/2/1)", addCount, setCount, remCount)
	}
}

// ── Observe2[T] subscribes to multiple events with one call ──────────────────

func TestObserve2MultipleEvents(t *testing.T) {
	w := flecs.New()
	var fired []flecs.EventKind

	_ = flecs.Observe2[Position](w, []flecs.EventKind{flecs.EventOnAdd, flecs.EventOnSet, flecs.EventOnRemove},
		func(ev flecs.EventKind, _ flecs.ID, _ *Position) {
			fired = append(fired, ev)
		})

	e := w.NewEntity()
	flecs.Set[Position](w, e, Position{1, 2}) // OnAdd + OnSet
	flecs.Remove[Position](w, e)              // OnRemove

	if len(fired) != 3 {
		t.Fatalf("want 3 events, got %d: %v", len(fired), fired)
	}
	if fired[0] != flecs.EventOnAdd || fired[1] != flecs.EventOnSet || fired[2] != flecs.EventOnRemove {
		t.Fatalf("unexpected event order: %v", fired)
	}
}

// ── Observe2[T] Unsubscribe stops all events ──────────────────────────────────

func TestObserve2UnsubscribeAll(t *testing.T) {
	w := flecs.New()
	calls := 0

	obs := flecs.Observe2[Position](w, []flecs.EventKind{flecs.EventOnAdd, flecs.EventOnSet},
		func(_ flecs.EventKind, _ flecs.ID, _ *Position) { calls++ })
	obs.Unsubscribe()

	e := w.NewEntity()
	flecs.Set[Position](w, e, Position{1, 2})

	if calls != 0 {
		t.Fatalf("want 0 after Unsubscribe, got %d", calls)
	}
}

// ── EventKind.String() ────────────────────────────────────────────────────────

func TestEventKindString(t *testing.T) {
	if s := flecs.EventOnAdd.String(); s != "OnAdd" {
		t.Fatalf("EventOnAdd.String() want OnAdd, got %q", s)
	}
	if s := flecs.EventOnSet.String(); s != "OnSet" {
		t.Fatalf("EventOnSet.String() want OnSet, got %q", s)
	}
	if s := flecs.EventOnRemove.String(); s != "OnRemove" {
		t.Fatalf("EventOnRemove.String() want OnRemove, got %q", s)
	}
	if s := flecs.EventKind(99).String(); s != "Unknown" {
		t.Fatalf("unknown EventKind.String() want Unknown, got %q", s)
	}
}
