package docs_test

import (
	"testing"
	"unsafe"

	"github.com/snichols/flecs"
)

// TestObservers_HookOnAdd verifies that the OnAdd hook fires exactly once when
// a component is first added, and not on subsequent Set calls.
func TestObservers_HookOnAdd(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()

	var count int
	flecs.OnAdd[Position](w, func(fw *flecs.Writer, e flecs.ID, v Position) {
		count++
	})

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 10, Y: 20}) // OnAdd fires
		flecs.Set(fw, e, Position{X: 30, Y: 40}) // OnAdd does NOT fire again
	})

	if count != 1 {
		t.Errorf("OnAdd hook count = %d, want 1", count)
	}
}

// TestObservers_HookOnSet verifies that the OnSet hook receives the post-Set
// value and fires on every Set call.
func TestObservers_HookOnSet(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()

	var values []Position
	flecs.OnSet[Position](w, func(fw *flecs.Writer, e flecs.ID, v Position) {
		values = append(values, v)
	})

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 10, Y: 20})
		flecs.Set(fw, e, Position{X: 30, Y: 40})
	})

	if len(values) != 2 {
		t.Fatalf("OnSet count = %d, want 2", len(values))
	}
	if values[0] != (Position{X: 10, Y: 20}) {
		t.Errorf("OnSet[0] = %v, want {10, 20}", values[0])
	}
	if values[1] != (Position{X: 30, Y: 40}) {
		t.Errorf("OnSet[1] = %v, want {30, 40}", values[1])
	}
}

// TestObservers_HookOnRemove verifies that the OnRemove hook fires before
// the component is removed and receives the pre-remove value.
func TestObservers_HookOnRemove(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()

	var removed []Position
	flecs.OnRemove[Position](w, func(fw *flecs.Writer, e flecs.ID, v Position) {
		removed = append(removed, v)
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 10, Y: 20})
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[Position](fw, e) // OnRemove fires: {10, 20}
		flecs.Remove[Position](fw, e) // does NOT fire — already removed
	})

	if len(removed) != 1 {
		t.Fatalf("OnRemove count = %d, want 1", len(removed))
	}
	if removed[0] != (Position{X: 10, Y: 20}) {
		t.Errorf("OnRemove value = %v, want {10, 20}", removed[0])
	}
}

// TestObservers_HookOrdering verifies that the hook fires before the observer
// for OnAdd events.
func TestObservers_HookOrdering(t *testing.T) {
	type Tag struct{}

	w := flecs.New()
	var order []string

	flecs.OnAdd[Tag](w, func(fw *flecs.Writer, e flecs.ID, _ Tag) {
		order = append(order, "hook")
	})
	flecs.Observe[Tag](w, flecs.EventOnAdd, func(fw *flecs.Writer, e flecs.ID, _ Tag) {
		order = append(order, "observer")
	})

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		fw.AddID(e, flecs.RegisterComponent[Tag](w))
	})

	want := []string{"hook", "observer"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("order[%d] = %q, want %q", i, order[i], want[i])
		}
	}
}

// TestObservers_ObserveOnAdd verifies that Observe[T](EventOnAdd) fires once on
// first add and not on subsequent Set calls.
func TestObservers_ObserveOnAdd(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()

	var count int
	obs := flecs.Observe[Position](w, flecs.EventOnAdd, func(fw *flecs.Writer, e flecs.ID, v Position) {
		count++
	})
	_ = obs

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 10, Y: 20}) // OnAdd fires
		flecs.Set(fw, e, Position{X: 30, Y: 40}) // OnAdd does NOT fire
	})

	if count != 1 {
		t.Errorf("OnAdd observer count = %d, want 1", count)
	}
}

// TestObservers_ObserveOnSet verifies that Observe[T](EventOnSet) fires with
// the post-Set value on every Set call.
func TestObservers_ObserveOnSet(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()

	var values []Position
	obs := flecs.Observe[Position](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v Position) {
		values = append(values, v)
	})
	_ = obs

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 10, Y: 20})
		flecs.Set(fw, e, Position{X: 30, Y: 40})
	})

	if len(values) != 2 {
		t.Fatalf("OnSet observer count = %d, want 2", len(values))
	}
	if values[0] != (Position{X: 10, Y: 20}) {
		t.Errorf("OnSet observer[0] = %v, want {10, 20}", values[0])
	}
	if values[1] != (Position{X: 30, Y: 40}) {
		t.Errorf("OnSet observer[1] = %v, want {30, 40}", values[1])
	}
}

// TestObservers_ObserveOnRemove verifies that Observe[T](EventOnRemove) fires
// with the pre-remove value and does not fire on a redundant remove.
func TestObservers_ObserveOnRemove(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()

	var removed []Position
	obs := flecs.Observe[Position](w, flecs.EventOnRemove, func(fw *flecs.Writer, e flecs.ID, v Position) {
		removed = append(removed, v)
	})
	_ = obs

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 10, Y: 20})
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[Position](fw, e)
		flecs.Remove[Position](fw, e) // redundant; no event
	})

	if len(removed) != 1 {
		t.Fatalf("OnRemove observer count = %d, want 1", len(removed))
	}
	if removed[0] != (Position{X: 10, Y: 20}) {
		t.Errorf("OnRemove observer value = %v, want {10, 20}", removed[0])
	}
}

// TestObservers_MultipleSubscribers verifies that two observers on the same
// event both fire in registration order.
func TestObservers_MultipleSubscribers(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()
	var order []string

	obs1 := flecs.Observe[Position](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v Position) {
		order = append(order, "obs1")
	})
	obs2 := flecs.Observe[Position](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v Position) {
		order = append(order, "obs2")
	})
	_, _ = obs1, obs2

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1, Y: 2})
	})

	want := []string{"obs1", "obs2"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("order[%d] = %q, want %q", i, order[i], want[i])
		}
	}
}

// TestObservers_Unsubscribe verifies that after Unsubscribe the observer no
// longer fires, and that the call is idempotent.
func TestObservers_Unsubscribe(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()

	var count int
	obs := flecs.Observe[Position](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v Position) {
		count++
	})

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 10, Y: 20}) // fires
	})

	obs.Unsubscribe()
	obs.Unsubscribe() // idempotent

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 30, Y: 40}) // does NOT fire
	})

	if count != 1 {
		t.Errorf("observer count = %d, want 1", count)
	}
}

// TestObservers_UnsubscribeFromCallback verifies that unsubscribing from within
// a callback causes the observer to fire only once.
func TestObservers_UnsubscribeFromCallback(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()

	var count int
	var obs *flecs.Observer
	obs = flecs.Observe[Position](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v Position) {
		count++
		obs.Unsubscribe()
	})

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1, Y: 2}) // fires, then unsubscribes
		flecs.Set(fw, e, Position{X: 3, Y: 4}) // does NOT fire
	})

	if count != 1 {
		t.Errorf("observer count = %d, want 1 (should fire once then unsubscribe)", count)
	}
}

// TestObservers_ObserveID verifies that ObserveID subscribes to a raw tag ID
// and fires on AddID.
func TestObservers_ObserveID(t *testing.T) {
	type MyTag struct{}

	w := flecs.New()
	tagID := flecs.RegisterComponent[MyTag](w)

	var count int
	obs := flecs.ObserveID(w, tagID, flecs.EventOnAdd, func(fw *flecs.Writer, e flecs.ID, ptr unsafe.Pointer) {
		count++
	})
	_ = obs

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		fw.AddID(e, tagID)
	})

	if count != 1 {
		t.Errorf("ObserveID count = %d, want 1", count)
	}
}

// TestObservers_Observe2MultiEvent verifies that Observe2 fires for multiple
// events and delivers the correct EventKind to the callback.
func TestObservers_Observe2MultiEvent(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()

	type fired struct {
		event flecs.EventKind
		val   Position
	}
	var events []fired

	obs := flecs.Observe2[Position](w,
		[]flecs.EventKind{flecs.EventOnAdd, flecs.EventOnRemove},
		func(fw *flecs.Writer, event flecs.EventKind, e flecs.ID, v Position) {
			events = append(events, fired{event, v})
		},
	)
	_ = obs

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 5, Y: 10}) // OnAdd fires
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[Position](fw, e) // OnRemove fires
	})

	if len(events) != 2 {
		t.Fatalf("Observe2 event count = %d, want 2", len(events))
	}
	if events[0].event != flecs.EventOnAdd {
		t.Errorf("events[0].event = %v, want EventOnAdd", events[0].event)
	}
	if events[1].event != flecs.EventOnRemove {
		t.Errorf("events[1].event = %v, want EventOnRemove", events[1].event)
	}
	if events[1].val != (Position{X: 5, Y: 10}) {
		t.Errorf("events[1].val = %v, want {5, 10}", events[1].val)
	}
}

// TestObservers_WriterIsAlive verifies that the *Writer passed to an observer
// callback is non-nil and that read operations (IsAlive, Get) are safe to call
// from within the callback.
func TestObservers_WriterIsAlive(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()

	var gotAlive bool
	var gotValue Position

	flecs.Observe[Position](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v Position) {
		// Read operations are explicitly safe from within a callback.
		gotAlive = fw.IsAlive(e)
		if p, ok := flecs.Get[Position](fw, e); ok {
			gotValue = p
		}
	})

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 7, Y: 9})
	})

	if !gotAlive {
		t.Error("entity should be alive inside observer callback")
	}
	if gotValue != (Position{X: 7, Y: 9}) {
		t.Errorf("Get inside callback = %v, want {7, 9}", gotValue)
	}
}
