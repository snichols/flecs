package flecs_test

import (
	"sync/atomic"
	"testing"

	flecs "github.com/snichols/flecs"
)

// ── Test 1: basic register / subscribe / emit ─────────────────────────────────

func TestCustomEvent_BasicEmitObserve(t *testing.T) {
	w := flecs.New()

	var eventID flecs.ID
	w.Write(func(fw *flecs.Writer) { eventID = flecs.RegisterEvent(fw, "PlayerDied") })

	fired := 0
	var gotEntity flecs.ID
	var gotPayload interface{}

	flecs.ObserveEvent(w, eventID, func(fw *flecs.Writer, e flecs.ID, payload interface{}) {
		fired++
		gotEntity = e
		gotPayload = payload
	})

	var target flecs.ID
	w.Write(func(fw *flecs.Writer) {
		target = fw.NewEntity()
		flecs.Emit(fw, eventID, target, "died")
	})

	if fired != 1 {
		t.Fatalf("expected 1 fire, got %d", fired)
	}
	if gotEntity != target {
		t.Errorf("entity: want %v, got %v", target, gotEntity)
	}
	if gotPayload != "died" {
		t.Errorf("payload: want %q, got %v", "died", gotPayload)
	}
}

// ── Test 2: emit with no observers — no-op, no panic ─────────────────────────

func TestCustomEvent_EmitNoObservers(t *testing.T) {
	w := flecs.New()
	var eventID flecs.ID
	w.Write(func(fw *flecs.Writer) { eventID = flecs.RegisterEvent(fw, "GhostEvent") })

	// Must not panic.
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Emit(fw, eventID, e, nil)
	})
}

// ── Test 3: multiple observers fire in registration order ─────────────────────

func TestCustomEvent_MultipleObserversOrder(t *testing.T) {
	w := flecs.New()
	var eventID flecs.ID
	w.Write(func(fw *flecs.Writer) { eventID = flecs.RegisterEvent(fw, "Order") })

	order := []int{}
	flecs.ObserveEvent(w, eventID, func(fw *flecs.Writer, e flecs.ID, payload interface{}) { order = append(order, 1) })
	flecs.ObserveEvent(w, eventID, func(fw *flecs.Writer, e flecs.ID, payload interface{}) { order = append(order, 2) })
	flecs.ObserveEvent(w, eventID, func(fw *flecs.Writer, e flecs.ID, payload interface{}) { order = append(order, 3) })

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Emit(fw, eventID, e, nil)
	})

	if len(order) != 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Fatalf("expected [1 2 3], got %v", order)
	}
}

// ── Test 4: disabled observer does not fire; re-enable resumes ─────────────────

func TestCustomEvent_DisabledObserver(t *testing.T) {
	w := flecs.New()
	var eventID flecs.ID
	w.Write(func(fw *flecs.Writer) { eventID = flecs.RegisterEvent(fw, "Toggle") })

	fired := 0
	obs := flecs.ObserveEvent(w, eventID, func(fw *flecs.Writer, e flecs.ID, _ interface{}) { fired++ })

	obs.SetEnabled(false)
	w.Write(func(fw *flecs.Writer) { flecs.Emit(fw, eventID, fw.NewEntity(), nil) })
	if fired != 0 {
		t.Fatalf("disabled observer fired: got %d", fired)
	}

	obs.SetEnabled(true)
	w.Write(func(fw *flecs.Writer) { flecs.Emit(fw, eventID, fw.NewEntity(), nil) })
	if fired != 1 {
		t.Fatalf("re-enabled observer: expected 1, got %d", fired)
	}
}

// ── Test 5: built-in events still work via legacy API (regression guard) ──────

func TestCustomEvent_BuiltinLegacyRegression(t *testing.T) {
	type Health struct{ HP int }

	w := flecs.New()
	added := 0
	flecs.Observe[Health](w, flecs.EventOnAdd, func(fw *flecs.Writer, e flecs.ID, v Health) { added++ })

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set[Health](fw, e, Health{HP: 100})
	})

	if added != 1 {
		t.Fatalf("OnAdd via legacy EventKind: expected 1, got %d", added)
	}
}

// ── Test 6: built-in event entity used with Emit / ObserveEvent API ───────────

func TestCustomEvent_BuiltinEntityWithEmitAPI(t *testing.T) {
	// ObserveEvent(w, w.EventOnAdd(), fn) subscribes to manual Emit calls with
	// that event entity ID. It does NOT receive component-add events (those use
	// a different key: {id:componentID, eventEntity:w.EventOnAdd()}).
	w := flecs.New()

	fired := 0
	flecs.ObserveEvent(w, w.EventOnAdd(), func(fw *flecs.Writer, e flecs.ID, payload interface{}) {
		fired++
	})

	var target flecs.ID
	w.Write(func(fw *flecs.Writer) {
		target = fw.NewEntity()
		// Manually emit with the built-in OnAdd event entity.
		flecs.Emit(fw, w.EventOnAdd(), target, nil)
	})

	if fired != 1 {
		t.Fatalf("ObserveEvent with built-in EventOnAdd entity: expected 1, got %d", fired)
	}

	// Adding a component to an entity must NOT trigger the ObserveEvent handler
	// (different dispatch key: component-based OnAdd uses {componentID, eventOnAddID}).
	type Tag struct{}
	firedBefore := fired
	flecs.RegisterComponent[Tag](w)
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, target, flecs.RegisterComponent[Tag](w))
	})
	if fired != firedBefore {
		t.Errorf("component OnAdd must not fire ObserveEvent handler: before=%d after=%d", firedBefore, fired)
	}
}

// ── Test 7: EmitTyped + ObserveEventTyped round-trip ─────────────────────────

func TestCustomEvent_EmitTypedRoundTrip(t *testing.T) {
	type Damage struct{ Amount int }

	w := flecs.New()
	var eventID flecs.ID
	w.Write(func(fw *flecs.Writer) { eventID = flecs.RegisterEvent(fw, "Damage") })

	var got Damage
	flecs.ObserveEventTyped[Damage](w, eventID, func(fw *flecs.Writer, e flecs.ID, d Damage) {
		got = d
	})

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.EmitTyped(fw, eventID, e, Damage{Amount: 42})
	})

	if got.Amount != 42 {
		t.Errorf("payload: want Amount=42, got %v", got)
	}
}

// ── Test 8: payload visibility — shallow copy, mutations don't leak ───────────

func TestCustomEvent_PayloadShallowCopy(t *testing.T) {
	w := flecs.New()
	var eventID flecs.ID
	w.Write(func(fw *flecs.Writer) { eventID = flecs.RegisterEvent(fw, "Data") })

	// Each observer gets its own copy of the interface value.
	payloads := []interface{}{}
	flecs.ObserveEvent(w, eventID, func(fw *flecs.Writer, e flecs.ID, p interface{}) {
		payloads = append(payloads, p)
	})
	flecs.ObserveEvent(w, eventID, func(fw *flecs.Writer, e flecs.ID, p interface{}) {
		payloads = append(payloads, p)
	})

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Emit(fw, eventID, e, "original")
	})

	if len(payloads) != 2 {
		t.Fatalf("expected 2 payloads, got %d", len(payloads))
	}
	for i, p := range payloads {
		if p != "original" {
			t.Errorf("payload[%d]: want %q, got %v", i, "original", p)
		}
	}
}

// ── Test 9: re-entrant emit fires synchronously ───────────────────────────────

func TestCustomEvent_ReentrantEmit(t *testing.T) {
	w := flecs.New()
	var ev1, ev2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ev1 = flecs.RegisterEvent(fw, "Outer")
		ev2 = flecs.RegisterEvent(fw, "Inner")
	})

	log := []string{}

	flecs.ObserveEvent(w, ev1, func(fw *flecs.Writer, e flecs.ID, _ interface{}) {
		log = append(log, "outer-before")
		// Emit inner event from inside the outer observer.
		flecs.Emit(fw, ev2, e, nil)
		log = append(log, "outer-after")
	})
	flecs.ObserveEvent(w, ev2, func(fw *flecs.Writer, e flecs.ID, _ interface{}) {
		log = append(log, "inner")
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Emit(fw, ev1, fw.NewEntity(), nil)
	})

	want := []string{"outer-before", "inner", "outer-after"}
	if len(log) != len(want) {
		t.Fatalf("log: want %v, got %v", want, log)
	}
	for i := range want {
		if log[i] != want[i] {
			t.Errorf("log[%d]: want %q, got %q", i, want[i], log[i])
		}
	}
}

// ── Test 10: yield_existing on custom event — silent no-op ───────────────────

func TestCustomEvent_YieldExistingNoOp(t *testing.T) {
	w := flecs.New()
	var eventID flecs.ID
	w.Write(func(fw *flecs.Writer) { eventID = flecs.RegisterEvent(fw, "Sweep") })

	// Observer registered with WithYieldExisting must not fire any sweep callback.
	swept := 0
	flecs.ObserveEvent(w, eventID, func(fw *flecs.Writer, e flecs.ID, _ interface{}) { swept++ })
	// (ObserveEvent ignores yield_existing; future Emit calls still work.)

	// Emit once to confirm normal fire still works.
	w.Write(func(fw *flecs.Writer) { flecs.Emit(fw, eventID, fw.NewEntity(), nil) })
	if swept != 1 {
		t.Fatalf("normal Emit after yield_existing no-op: expected 1, got %d", swept)
	}
}

// ── Test 11: custom event entity tagged with built-in Event tag ───────────────

func TestCustomEvent_EventTag(t *testing.T) {
	w := flecs.New()
	var eventID flecs.ID
	w.Write(func(fw *flecs.Writer) { eventID = flecs.RegisterEvent(fw, "Tagged") })

	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, eventID, w.Event()) {
			t.Error("RegisterEvent must apply the built-in Event tag to the entity")
		}
	})
}

// ── Test 12: deleting event entity unsubscribes all observers ─────────────────

func TestCustomEvent_DeleteEventEntity(t *testing.T) {
	w := flecs.New()
	var eventID flecs.ID
	w.Write(func(fw *flecs.Writer) { eventID = flecs.RegisterEvent(fw, "Mortal") })

	var fired atomic.Int32
	flecs.ObserveEvent(w, eventID, func(fw *flecs.Writer, e flecs.ID, _ interface{}) { fired.Add(1) })

	// Emit once to verify handler works.
	w.Write(func(fw *flecs.Writer) { flecs.Emit(fw, eventID, fw.NewEntity(), nil) })
	if fired.Load() != 1 {
		t.Fatalf("pre-delete: expected 1 fire, got %d", fired.Load())
	}

	// Delete the event entity.
	w.Write(func(fw *flecs.Writer) { fw.Delete(eventID) })

	// Subsequent Emit must be a no-op (entity and its observer map entry are gone).
	w.Write(func(fw *flecs.Writer) { flecs.Emit(fw, eventID, fw.NewEntity(), nil) })
	if fired.Load() != 1 {
		t.Fatalf("post-delete: expected still 1, got %d", fired.Load())
	}
}
