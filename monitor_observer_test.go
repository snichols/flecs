package flecs_test

import (
	"log/slog"
	"sort"
	"testing"

	flecs "github.com/snichols/flecs"
)

// ---- component types used by monitor tests ----

type monHealth struct{ HP int }
type monFrozen struct{}
type monSpeed struct{ V float32 }
type monDontFragHP struct{ HP int }

// ---- helpers ----

type monEvent struct {
	e       flecs.ID
	entered bool
}

func newMonWorld() *flecs.World { return flecs.New() }

// ---- Test 1: single-term enter/exit/re-enter ----

func TestMonitorSingleTermEnterExitReenter(t *testing.T) {
	w := newMonWorld()
	healthID := flecs.RegisterComponent[monHealth](w)

	var events []monEvent
	flecs.Monitor(w, []flecs.Term{flecs.With(healthID)}, func(fw *flecs.Writer, e flecs.ID, entered bool) {
		events = append(events, monEvent{e: e, entered: entered})
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// Add Health → entered=true
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monHealth{HP: 10}) })
	if len(events) != 1 || !events[0].entered || events[0].e != e {
		t.Fatalf("after Add: want [entered=true], got %+v", events)
	}

	// Remove Health → entered=false
	w.Write(func(fw *flecs.Writer) { flecs.Remove[monHealth](fw, e) })
	if len(events) != 2 || events[1].entered || events[1].e != e {
		t.Fatalf("after Remove: want [entered=false], got %+v", events[1:])
	}

	// Add again → entered=true
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monHealth{HP: 5}) })
	if len(events) != 3 || !events[2].entered || events[2].e != e {
		t.Fatalf("after re-Add: want [entered=true], got %+v", events[2:])
	}
}

// ---- Test 2: multi-term With+Without ----

func TestMonitorMultiTermWithWithout(t *testing.T) {
	w := newMonWorld()
	healthID := flecs.RegisterComponent[monHealth](w)
	frozenID := flecs.RegisterComponent[monFrozen](w)

	var events []monEvent
	flecs.Monitor(w,
		[]flecs.Term{flecs.With(healthID), flecs.Without(frozenID)},
		func(fw *flecs.Writer, e flecs.ID, entered bool) {
			events = append(events, monEvent{e: e, entered: entered})
		})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// Add Health → entered=true (has Health, no Frozen)
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monHealth{HP: 10}) })
	if len(events) != 1 || !events[0].entered {
		t.Fatalf("after Add Health: want [entered=true], got %+v", events)
	}

	// Add Frozen → entered=false (has Health, has Frozen → doesn't match Without)
	w.Write(func(fw *flecs.Writer) { flecs.AddID(fw, e, frozenID) })
	if len(events) != 2 || events[1].entered {
		t.Fatalf("after Add Frozen: want [entered=false], got %+v", events[1:])
	}

	// Remove Frozen → entered=true again
	w.Write(func(fw *flecs.Writer) { flecs.RemoveID(fw, e, frozenID) })
	if len(events) != 3 || !events[2].entered {
		t.Fatalf("after Remove Frozen: want [entered=true], got %+v", events[2:])
	}
}

// ---- Test 3: 100 entities, only matching subset fires ----

func TestMonitorFiringSubset(t *testing.T) {
	w := newMonWorld()
	healthID := flecs.RegisterComponent[monHealth](w)
	frozenID := flecs.RegisterComponent[monFrozen](w)

	var fired []flecs.ID
	flecs.Monitor(w,
		[]flecs.Term{flecs.With(healthID), flecs.Without(frozenID)},
		func(fw *flecs.Writer, e flecs.ID, entered bool) {
			if entered {
				fired = append(fired, e)
			}
		})

	const N = 100
	entities := make([]flecs.ID, N)
	w.Write(func(fw *flecs.Writer) {
		for i := range N {
			e := fw.NewEntity()
			entities[i] = e
			flecs.Set(fw, e, monHealth{HP: i})
			if i%2 == 0 {
				flecs.AddID(fw, e, frozenID) // even entities are frozen → don't match
			}
		}
	})

	// Only odd-indexed entities should have entered.
	if len(fired) != N/2 {
		t.Fatalf("want %d entered events, got %d", N/2, len(fired))
	}
	firedSet := make(map[flecs.ID]struct{}, len(fired))
	for _, id := range fired {
		firedSet[id] = struct{}{}
	}
	for i, e := range entities {
		if i%2 == 0 {
			if _, present := firedSet[e]; present {
				t.Errorf("entity %v (index %d, frozen) should not have entered", e, i)
			}
		} else {
			if _, present := firedSet[e]; !present {
				t.Errorf("entity %v (index %d, not frozen) should have entered", e, i)
			}
		}
	}
}

// ---- Test 4: yield_existing ----

func TestMonitorYieldExisting(t *testing.T) {
	w := newMonWorld()
	healthID := flecs.RegisterComponent[monHealth](w)

	const N = 5
	entities := make([]flecs.ID, N)
	w.Write(func(fw *flecs.Writer) {
		for i := range N {
			e := fw.NewEntity()
			entities[i] = e
			flecs.Set(fw, e, monHealth{HP: i + 1})
		}
	})

	// Register monitor with yield_existing AFTER entities are created.
	var fired []flecs.ID
	flecs.MonitorWithOptions(w,
		[]flecs.Term{flecs.With(healthID)},
		flecs.WithYieldExisting(),
		func(fw *flecs.Writer, e flecs.ID, entered bool) {
			if entered {
				fired = append(fired, e)
			}
		})

	if len(fired) != N {
		t.Fatalf("yield_existing: want %d entered, got %d", N, len(fired))
	}
	sort.Slice(fired, func(i, j int) bool { return fired[i] < fired[j] })
	expected := append([]flecs.ID(nil), entities...)
	sort.Slice(expected, func(i, j int) bool { return expected[i] < expected[j] })
	for i := range N {
		if fired[i] != expected[i] {
			t.Errorf("yield_existing[%d]: got %v, want %v", i, fired[i], expected[i])
		}
	}
}

// ---- Test 5: disabled monitor ----

func TestMonitorDisabled(t *testing.T) {
	w := newMonWorld()
	healthID := flecs.RegisterComponent[monHealth](w)

	var events []monEvent
	obs := flecs.Monitor(w, []flecs.Term{flecs.With(healthID)}, func(fw *flecs.Writer, e flecs.ID, entered bool) {
		events = append(events, monEvent{e: e, entered: entered})
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// Disable the monitor.
	obs.SetEnabled(false)

	// Changes while disabled should NOT fire.
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monHealth{HP: 10}) })
	if len(events) != 0 {
		t.Fatalf("while disabled: expected 0 events, got %+v", events)
	}

	w.Write(func(fw *flecs.Writer) { flecs.Remove[monHealth](fw, e) })
	if len(events) != 0 {
		t.Fatalf("while disabled (remove): expected 0 events, got %+v", events)
	}

	// Re-enable. Next state change fires from the current (non-matching) state.
	// No catch-up for changes that happened while disabled.
	obs.SetEnabled(true)
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monHealth{HP: 5}) })
	if len(events) != 1 || !events[0].entered {
		t.Fatalf("after re-enable + add: want [entered=true], got %+v", events)
	}
}

// ---- Test 6: overlapping monitors ----

func TestMonitorOverlapping(t *testing.T) {
	w := newMonWorld()
	healthID := flecs.RegisterComponent[monHealth](w)
	speedID := flecs.RegisterComponent[monSpeed](w)

	var eventsA, eventsB []monEvent

	// Monitor A: requires Health
	flecs.Monitor(w, []flecs.Term{flecs.With(healthID)}, func(fw *flecs.Writer, e flecs.ID, entered bool) {
		eventsA = append(eventsA, monEvent{e: e, entered: entered})
	})
	// Monitor B: requires Health AND Speed
	flecs.Monitor(w, []flecs.Term{flecs.With(healthID), flecs.With(speedID)}, func(fw *flecs.Writer, e flecs.ID, entered bool) {
		eventsB = append(eventsB, monEvent{e: e, entered: entered})
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// Add Health → A fires (entered=true), B does not.
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monHealth{HP: 10}) })
	if len(eventsA) != 1 || !eventsA[0].entered {
		t.Errorf("after Add Health: A want [entered=true], got %+v", eventsA)
	}
	if len(eventsB) != 0 {
		t.Errorf("after Add Health: B should not have fired, got %+v", eventsB)
	}

	// Add Speed → B fires (entered=true), A does not.
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monSpeed{V: 1.5}) })
	if len(eventsA) != 1 {
		t.Errorf("after Add Speed: A should not fire again, got %+v", eventsA)
	}
	if len(eventsB) != 1 || !eventsB[0].entered {
		t.Errorf("after Add Speed: B want [entered=true], got %+v", eventsB)
	}

	// Remove Health → A fires (entered=false), B fires (entered=false).
	w.Write(func(fw *flecs.Writer) { flecs.Remove[monHealth](fw, e) })
	if len(eventsA) != 2 || eventsA[1].entered {
		t.Errorf("after Remove Health: A want [entered=false], got %+v", eventsA[1:])
	}
	if len(eventsB) != 2 || eventsB[1].entered {
		t.Errorf("after Remove Health: B want [entered=false], got %+v", eventsB[1:])
	}
}

// ---- Test 7: re-entrant mutation ----

func TestMonitorReentrant(t *testing.T) {
	w := newMonWorld()
	healthID := flecs.RegisterComponent[monHealth](w)
	speedID := flecs.RegisterComponent[monSpeed](w)

	var speedEntered []flecs.ID

	// When an entity enters the health monitor, add Speed to another entity.
	flecs.Monitor(w, []flecs.Term{flecs.With(speedID)}, func(fw *flecs.Writer, e flecs.ID, entered bool) {
		if entered {
			speedEntered = append(speedEntered, e)
		}
	})

	var target flecs.ID
	w.Write(func(fw *flecs.Writer) { target = fw.NewEntity() })

	flecs.Monitor(w, []flecs.Term{flecs.With(healthID)}, func(fw *flecs.Writer, e flecs.ID, entered bool) {
		if entered {
			// Mutate target inside monitor callback → must be deferred.
			flecs.Set(fw, target, monSpeed{V: 2.0})
		}
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, monHealth{HP: 1})
	})
	_ = e

	// The deferred Set(target, Speed) should have fired the speed monitor.
	if len(speedEntered) != 1 || speedEntered[0] != target {
		t.Fatalf("re-entrant: speed monitor want [target], got %v", speedEntered)
	}
}

// ---- Test 8: marshal round-trip preserves world state, not monitors ----

func TestMonitorMarshalRoundTrip(t *testing.T) {
	w := newMonWorld()
	healthID := flecs.RegisterComponent[monHealth](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, monHealth{HP: 42})
	})

	// Register a monitor before marshaling.
	var events []monEvent
	flecs.Monitor(w, []flecs.Term{flecs.With(healthID)}, func(fw *flecs.Writer, ee flecs.ID, entered bool) {
		events = append(events, monEvent{e: ee, entered: entered})
	})

	// Marshal + unmarshal.
	data, err := w.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	w2 := flecs.New()
	flecs.RegisterComponent[monHealth](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	// World state is preserved.
	w2.Read(func(fr *flecs.Reader) {
		q := flecs.NewQuery(w2, healthID)
		count := 0
		q.Each(func(it *flecs.QueryIter) {
			vs := flecs.Field[monHealth](it, healthID)
			for i := range vs {
				if vs[i].HP != 42 {
					t.Errorf("HP after round-trip: got %d, want 42", vs[i].HP)
				}
				count++
			}
		})
		if count != 1 {
			t.Errorf("entity count after round-trip: got %d, want 1", count)
		}
	})

	// The monitor from w is NOT transferred to w2 — it's process-local.
	// Verify by adding a new entity in w2 and confirming events in w are unaffected.
	w2.Write(func(fw *flecs.Writer) {
		e2 := fw.NewEntity()
		flecs.Set(fw, e2, monHealth{HP: 99})
	})
	// No new events should have been fired on the w monitor.
	if len(events) != 0 {
		t.Fatalf("marshal: monitor should be process-local; unexpected events: %+v", events)
	}
}

// ---- Test 9: sparse (DontFragment) component term ----

func TestMonitorSparseComponent(t *testing.T) {
	w := newMonWorld()
	hpID := flecs.RegisterComponent[monDontFragHP](w)
	flecs.SetDontFragment(w, hpID)

	var events []monEvent
	flecs.Monitor(w, []flecs.Term{flecs.With(hpID)}, func(fw *flecs.Writer, e flecs.ID, entered bool) {
		events = append(events, monEvent{e: e, entered: entered})
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// Add DontFragment component → entered=true via hook-fallback path.
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monDontFragHP{HP: 7}) })
	if len(events) != 1 || !events[0].entered || events[0].e != e {
		t.Fatalf("after DontFragment add: want [entered=true], got %+v", events)
	}

	// Remove → entered=false.
	w.Write(func(fw *flecs.Writer) { flecs.Remove[monDontFragHP](fw, e) })
	if len(events) != 2 || events[1].entered {
		t.Fatalf("after DontFragment remove: want [entered=false], got %+v", events[1:])
	}

	// Add again → entered=true.
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monDontFragHP{HP: 3}) })
	if len(events) != 3 || !events[2].entered {
		t.Fatalf("after DontFragment re-add: want [entered=true], got %+v", events[2:])
	}
}

// ---- Test 10: unsubscribe during dispatch ----

func TestMonitorUnsubscribeDuringDispatch(t *testing.T) {
	w := newMonWorld()
	healthID := flecs.RegisterComponent[monHealth](w)

	var obsA, obsB *flecs.Observer
	var firedA, firedB int

	obsA = flecs.Monitor(w, []flecs.Term{flecs.With(healthID)}, func(fw *flecs.Writer, e flecs.ID, entered bool) {
		firedA++
		// Unsubscribe obsA from within its own callback.
		obsA.Unsubscribe()
	})
	obsB = flecs.Monitor(w, []flecs.Term{flecs.With(healthID)}, func(fw *flecs.Writer, e flecs.ID, entered bool) {
		firedB++
	})
	_ = obsB

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// First add: both A and B fire.
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monHealth{HP: 1}) })
	if firedA != 1 {
		t.Errorf("A: want 1 fire, got %d", firedA)
	}
	if firedB != 1 {
		t.Errorf("B: want 1 fire after first add, got %d", firedB)
	}

	// Second add (remove then re-add): only B fires (A is unsubscribed).
	w.Write(func(fw *flecs.Writer) { flecs.Remove[monHealth](fw, e) })
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monHealth{HP: 2}) })
	if firedA != 1 {
		t.Errorf("A: after unsubscribe, should not fire again; got %d", firedA)
	}
	if firedB != 3 { // 1 enter + 1 exit + 1 re-enter
		t.Errorf("B: want 3 total fires, got %d", firedB)
	}
}

// ---- Test 11: entity delete fires monitor exit ----

func TestMonitorEntityDelete(t *testing.T) {
	w := newMonWorld()
	healthID := flecs.RegisterComponent[monHealth](w)

	var events []monEvent
	flecs.Monitor(w, []flecs.Term{flecs.With(healthID)}, func(fw *flecs.Writer, e flecs.ID, entered bool) {
		events = append(events, monEvent{e: e, entered: entered})
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, monHealth{HP: 1})
	})

	if len(events) != 1 || !events[0].entered {
		t.Fatalf("after add: want [entered=true], got %+v", events)
	}

	// Delete entity → monitor fires entered=false.
	w.Write(func(fw *flecs.Writer) { fw.Delete(e) })
	if len(events) != 2 || events[1].entered {
		t.Fatalf("after delete: want [entered=false], got %+v", events[1:])
	}
}

// ---- Test 12: EventMonitor accessor ----

func TestMonitorEventMonitorAccessor(t *testing.T) {
	w := newMonWorld()
	id := w.EventMonitor()
	if id == 0 {
		t.Fatal("EventMonitor() returned zero ID")
	}
	// Index should be 46 (one past DependsOn at 45).
	if id.Index() != 46 {
		t.Errorf("EventMonitor() index: got %d, want 46", id.Index())
	}
}

// ---- Test 13: EventMonitor EventKind constant and String ----

func TestMonitorEventKindConstant(t *testing.T) {
	if flecs.EventMonitor != 5 {
		t.Errorf("EventMonitor constant: got %d, want 5", flecs.EventMonitor)
	}
	if s := flecs.EventMonitor.String(); s != "Monitor" {
		t.Errorf("EventMonitor.String(): got %q, want %q", s, "Monitor")
	}
	// EventMonitor() accessor returns the built-in entity at index 46
	w := newMonWorld()
	id := w.EventMonitor()
	if id.Index() != 46 {
		t.Errorf("EventMonitor() index: got %d, want 46", id.Index())
	}
}

// ---- Test 14: OR-group terms ----
// Exercises monitorMatchesTable OR-group path and entityMatchesMonitorExcluding OR-group path.

func TestMonitorOrGroup(t *testing.T) {
	w := newMonWorld()
	healthID := flecs.RegisterComponent[monHealth](w)
	frozenID := flecs.RegisterComponent[monFrozen](w)
	speedID := flecs.RegisterComponent[monSpeed](w)

	// Monitor: has Health AND (Frozen OR Speed).
	var events []monEvent
	flecs.Monitor(w,
		[]flecs.Term{flecs.With(healthID), flecs.Or(frozenID), flecs.Or(speedID)},
		func(fw *flecs.Writer, e flecs.ID, entered bool) {
			events = append(events, monEvent{e: e, entered: entered})
		},
	)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// Add Health only → no match (OR group not satisfied).
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monHealth{HP: 1}) })
	if len(events) != 0 {
		t.Fatalf("health only: want 0 events, got %+v", events)
	}

	// Add Speed → now matches (Health + Speed satisfies the OR group).
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monSpeed{V: 1}) })
	if len(events) != 1 || !events[0].entered {
		t.Fatalf("add speed: want [entered=true], got %+v", events)
	}

	// Remove Speed → exits (OR group no longer satisfied).
	w.Write(func(fw *flecs.Writer) { flecs.Remove[monSpeed](fw, e) })
	if len(events) != 2 || events[1].entered {
		t.Fatalf("remove speed: want [entered=false], got %+v", events[1:])
	}

	// Add Frozen → enters again (OR group satisfied via Frozen).
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monFrozen{}) })
	if len(events) != 3 || !events[2].entered {
		t.Fatalf("add frozen: want [entered=true], got %+v", events[2:])
	}
}

// ---- Test 15: OR-group yield_existing ----
// Exercises monitorMatchesTable OR-group path during the initial sweep.

func TestMonitorOrGroupYieldExisting(t *testing.T) {
	w := newMonWorld()
	healthID := flecs.RegisterComponent[monHealth](w)
	frozenID := flecs.RegisterComponent[monFrozen](w)
	speedID := flecs.RegisterComponent[monSpeed](w)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, monHealth{HP: 1})
		flecs.Set(fw, e1, monSpeed{V: 2}) // satisfies Health AND (Frozen OR Speed)
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, monHealth{HP: 3}) // Health only — OR group unsatisfied
	})

	var entered []flecs.ID
	flecs.MonitorWithOptions(w,
		[]flecs.Term{flecs.With(healthID), flecs.Or(frozenID), flecs.Or(speedID)},
		flecs.WithYieldExisting(),
		func(fw *flecs.Writer, e flecs.ID, in bool) {
			if in {
				entered = append(entered, e)
			}
		},
	)

	// Only e1 should be swept (health + speed satisfies Health AND (Frozen OR Speed)).
	if len(entered) != 1 || entered[0] != e1 {
		t.Fatalf("yield OR existing: want [e1], got %v", entered)
	}
}

// ---- Test 16: sparse delete fires monitor exit ----
// Exercises the sparse branch in fireMonitorsOnDelete.

func TestMonitorDeleteSparse(t *testing.T) {
	w := newMonWorld()
	hpID := flecs.RegisterComponent[monDontFragHP](w)
	flecs.SetDontFragment(w, hpID)

	var events []monEvent
	flecs.Monitor(w, []flecs.Term{flecs.With(hpID)}, func(fw *flecs.Writer, e flecs.ID, entered bool) {
		events = append(events, monEvent{e: e, entered: entered})
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, monDontFragHP{HP: 7})
	})
	if len(events) != 1 || !events[0].entered {
		t.Fatalf("after add: want [entered=true], got %+v", events)
	}

	// Delete entity → sparse monitor fires entered=false.
	w.Write(func(fw *flecs.Writer) { fw.Delete(e) })
	if len(events) != 2 || events[1].entered || events[1].e != e {
		t.Fatalf("after delete sparse: want [entered=false], got %+v", events[1:])
	}
}

// ---- Test 17: yield_existing with sparse (DontFragment) monitor ----
// Exercises the sparse sweep path in monitorSweepExisting.

func TestMonitorYieldExistingSparse(t *testing.T) {
	w := newMonWorld()
	hpID := flecs.RegisterComponent[monDontFragHP](w)
	flecs.SetDontFragment(w, hpID)
	healthID := flecs.RegisterComponent[monHealth](w)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, monHealth{HP: 1})
		flecs.Set(fw, e1, monDontFragHP{HP: 10})
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, monHealth{HP: 2}) // has Health but not DontFragHP
	})

	var entered []flecs.ID
	flecs.MonitorWithOptions(w,
		[]flecs.Term{flecs.With(healthID), flecs.With(hpID)},
		flecs.WithYieldExisting(),
		func(fw *flecs.Writer, e flecs.ID, in bool) {
			if in {
				entered = append(entered, e)
			}
		},
	)

	// Only e1 satisfies both terms.
	if len(entered) != 1 || entered[0] != e1 {
		t.Fatalf("sparse yield existing: want [e1], got %v", entered)
	}
}

// ---- Test 18: Disabled table is skipped by monitor ----
// Exercises the skipDisabled path in monitorMatchesTable.

func TestMonitorSkipsDisabledTable(t *testing.T) {
	w := newMonWorld()
	healthID := flecs.RegisterComponent[monHealth](w)

	var events []monEvent
	flecs.Monitor(w, []flecs.Term{flecs.With(healthID)}, func(fw *flecs.Writer, e flecs.ID, entered bool) {
		events = append(events, monEvent{e: e, entered: entered})
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, monHealth{HP: 1})
	})
	if len(events) != 1 || !events[0].entered {
		t.Fatalf("before disable: want [entered=true], got %+v", events)
	}

	// Disable entity → archetype changes to include Disabled; monitor should fire exit.
	w.Write(func(fw *flecs.Writer) { flecs.DisableEntity(fw, e) })
	if len(events) != 2 || events[1].entered {
		t.Fatalf("after DisableEntity: want [entered=false], got %+v", events[1:])
	}

	// Re-enable → monitor should fire entry again.
	w.Write(func(fw *flecs.Writer) { flecs.EnableEntity(fw, e) })
	if len(events) != 3 || !events[2].entered {
		t.Fatalf("after EnableEntity: want [entered=true], got %+v", events[2:])
	}
}

// ---- Test 19: Not-term with sparse component ----
// Exercises TermNot path in entityMatchesMonitorExcluding.

func TestMonitorNotTermSparse(t *testing.T) {
	w := newMonWorld()
	hpID := flecs.RegisterComponent[monDontFragHP](w)
	flecs.SetDontFragment(w, hpID)
	frozenID := flecs.RegisterComponent[monFrozen](w)

	// Monitor: has DontFragHP AND NOT Frozen.
	var events []monEvent
	flecs.Monitor(w,
		[]flecs.Term{flecs.With(hpID), flecs.Without(frozenID)},
		func(fw *flecs.Writer, e flecs.ID, entered bool) {
			events = append(events, monEvent{e: e, entered: entered})
		},
	)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// Add DontFragHP (no Frozen) → entered=true.
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monDontFragHP{HP: 5}) })
	if len(events) != 1 || !events[0].entered {
		t.Fatalf("add HP: want [entered=true], got %+v", events)
	}

	// Add Frozen → exits (TermNot violated).
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monFrozen{}) })
	if len(events) != 2 || events[1].entered {
		t.Fatalf("add Frozen: want [entered=false], got %+v", events[1:])
	}

	// Remove Frozen → enters again.
	w.Write(func(fw *flecs.Writer) { flecs.Remove[monFrozen](fw, e) })
	if len(events) != 3 || !events[2].entered {
		t.Fatalf("remove Frozen: want [entered=true], got %+v", events[2:])
	}
}

// ---- Test 20: Clear entity fires monitor exit ----
// Exercises fireMonitorsOnDelete called from clearImmediate.

func TestMonitorEntityClear(t *testing.T) {
	w := newMonWorld()
	healthID := flecs.RegisterComponent[monHealth](w)

	var events []monEvent
	flecs.Monitor(w, []flecs.Term{flecs.With(healthID)}, func(fw *flecs.Writer, e flecs.ID, entered bool) {
		events = append(events, monEvent{e: e, entered: entered})
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, monHealth{HP: 10})
	})
	if len(events) != 1 || !events[0].entered {
		t.Fatalf("after add: want [entered=true], got %+v", events)
	}

	// Clear removes all components → monitor fires entered=false.
	w.Write(func(fw *flecs.Writer) { flecs.Clear(fw, e) })
	if len(events) != 2 || events[1].entered || events[1].e != e {
		t.Fatalf("after Clear: want [entered=false], got %+v", events[1:])
	}

	// Entity is still alive; add Health again → enters again.
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monHealth{HP: 5}) })
	if len(events) != 3 || !events[2].entered {
		t.Fatalf("after re-add: want [entered=true], got %+v", events[2:])
	}
}

// ---- Test 21: Sparse monitor with OR groups ----
// Exercises the OR-group branch inside entityMatchesMonitorExcluding (sparse mode).

func TestMonitorSparseWithOrGroup(t *testing.T) {
	w := newMonWorld()
	hpID := flecs.RegisterComponent[monDontFragHP](w)
	flecs.SetDontFragment(w, hpID)
	frozenID := flecs.RegisterComponent[monFrozen](w)
	speedID := flecs.RegisterComponent[monSpeed](w)

	// sparseMode = true (DontFragHP is DontFragment), with OR group (Frozen OR Speed).
	var events []monEvent
	flecs.Monitor(w,
		[]flecs.Term{flecs.With(hpID), flecs.Or(frozenID), flecs.Or(speedID)},
		func(fw *flecs.Writer, e flecs.ID, entered bool) {
			events = append(events, monEvent{e: e, entered: entered})
		},
	)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// Add DontFragHP only — OR group unsatisfied → no event.
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monDontFragHP{HP: 3}) })
	if len(events) != 0 {
		t.Fatalf("hp only: want 0 events, got %+v", events)
	}

	// Add Speed → now matched (DontFragHP AND (Frozen OR Speed)) → entered.
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monSpeed{V: 1}) })
	if len(events) != 1 || !events[0].entered {
		t.Fatalf("add speed: want [entered=true], got %+v", events)
	}

	// Remove DontFragHP → exits.
	w.Write(func(fw *flecs.Writer) { flecs.Remove[monDontFragHP](fw, e) })
	if len(events) != 2 || events[1].entered {
		t.Fatalf("remove hp: want [entered=false], got %+v", events[1:])
	}
}

// ---- Test 22: Union pair term ----
// Exercises the Union branch in entityHasComponentForMonitor.
// Exercises the Union branch in entityHasComponentForMonitor.

func TestMonitorUnionTerm(t *testing.T) {
	w := newMonWorld()

	var R, T1, T2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		R = fw.NewEntity()
		T1 = fw.NewEntity()
		T2 = fw.NewEntity()
	})
	flecs.SetUnion(w, R)

	pairT1 := flecs.MakePair(R, T1)
	pairT2 := flecs.MakePair(R, T2)

	// Monitor fires when entity has (R, T1).
	var events []monEvent
	flecs.Monitor(w, []flecs.Term{flecs.With(pairT1)}, func(fw *flecs.Writer, e flecs.ID, entered bool) {
		events = append(events, monEvent{e: e, entered: entered})
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// Add (R, T1) → monitor fires enter.
	w.Write(func(fw *flecs.Writer) { flecs.AddID(fw, e, pairT1) })
	if len(events) != 1 || !events[0].entered || events[0].e != e {
		t.Fatalf("add (R,T1): want [entered=true], got %+v", events)
	}

	// Switch to (R, T2) → old (R,T1) removed → monitor fires exit.
	w.Write(func(fw *flecs.Writer) { flecs.AddID(fw, e, pairT2) })
	if len(events) != 2 || events[1].entered || events[1].e != e {
		t.Fatalf("switch to (R,T2): want [entered=false], got %+v", events[1:])
	}

	// Switch back to (R, T1) → monitor fires enter.
	w.Write(func(fw *flecs.Writer) { flecs.AddID(fw, e, pairT1) })
	if len(events) != 3 || !events[2].entered || events[2].e != e {
		t.Fatalf("switch back to (R,T1): want [entered=true], got %+v", events[2:])
	}
}

// ---- Test 23: Sparse+Union monitor — entity lacks the union pair ----
// Exercises the "union store not found" and "entity not in union store" paths
// inside entityHasComponentForMonitor.

func TestMonitorSparseUnionMissingPair(t *testing.T) {
	w := newMonWorld()

	hpID := flecs.RegisterComponent[monDontFragHP](w)
	flecs.SetDontFragment(w, hpID)

	var R, T1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		R = fw.NewEntity()
		T1 = fw.NewEntity()
	})
	flecs.SetUnion(w, R)
	pairT1 := flecs.MakePair(R, T1)

	// Monitor: sparseMode=true (DontFragHP) AND has Union pair (R,T1).
	var events []monEvent
	flecs.Monitor(w,
		[]flecs.Term{flecs.With(hpID), flecs.With(pairT1)},
		func(fw *flecs.Writer, e flecs.ID, entered bool) {
			events = append(events, monEvent{e: e, entered: entered})
		},
	)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// Add DontFragHP only — Union pair absent; monitor should NOT fire.
	// entityHasComponentForMonitor hits the "not in union store" path.
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monDontFragHP{HP: 9}) })
	if len(events) != 0 {
		t.Fatalf("hp only, no union: want 0 events, got %+v", events)
	}

	// Now add the Union pair → both terms satisfied → monitor enters.
	w.Write(func(fw *flecs.Writer) { flecs.AddID(fw, e, pairT1) })
	if len(events) != 1 || !events[0].entered {
		t.Fatalf("add union pair: want [entered=true], got %+v", events)
	}
}

// ---- Test 24: Prefab entity skipped by monitor ----
// Exercises the skipPrefab path in monitorMatchesTable.

func TestMonitorSkipsPrefabTable(t *testing.T) {
	w := newMonWorld()
	healthID := flecs.RegisterComponent[monHealth](w)

	var events []monEvent
	flecs.Monitor(w, []flecs.Term{flecs.With(healthID)}, func(fw *flecs.Writer, e flecs.ID, entered bool) {
		events = append(events, monEvent{e: e, entered: entered})
	})

	// Normal entity: monitor fires.
	var normal flecs.ID
	w.Write(func(fw *flecs.Writer) {
		normal = fw.NewEntity()
		flecs.Set(fw, normal, monHealth{HP: 1})
	})
	if len(events) != 1 || !events[0].entered {
		t.Fatalf("normal entity: want [entered=true], got %+v", events)
	}

	// Prefab entity: marked as prefab first, then Health added.
	// The prefab table is excluded by skipPrefab, so no monitor event.
	var prefab flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.MarkPrefab(fw, prefab)
		flecs.Set(fw, prefab, monHealth{HP: 2})
	})
	if len(events) != 1 {
		t.Fatalf("prefab entity: want 0 new events (still 1 total), got %+v", events)
	}
	_ = prefab
}

// ---- Test 25: Sparse OR group with DontFragment component in OR ----
// Exercises the DontFragment (sparseStorage) branch in entityMatchesMonitorExcluding OR group.

type monDontFragSpeed struct{ V float32 }

func TestMonitorSparseOrGroupDontFrag(t *testing.T) {
	w := newMonWorld()
	hpID := flecs.RegisterComponent[monDontFragHP](w)
	flecs.SetDontFragment(w, hpID)
	dfSpeedID := flecs.RegisterComponent[monDontFragSpeed](w)
	flecs.SetDontFragment(w, dfSpeedID)
	frozenID := flecs.RegisterComponent[monFrozen](w)

	// sparseMode = true (hpID is DontFragment).
	// OR group: (Frozen OR dfSpeed) — dfSpeed is also DontFragment, so the
	// sparseStorage path inside entityMatchesMonitorExcluding is exercised.
	var events []monEvent
	flecs.Monitor(w,
		[]flecs.Term{flecs.With(hpID), flecs.Or(frozenID), flecs.Or(dfSpeedID)},
		func(fw *flecs.Writer, e flecs.ID, entered bool) {
			events = append(events, monEvent{e: e, entered: entered})
		},
	)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// Add DontFragHP only — OR group not satisfied.
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monDontFragHP{HP: 1}) })
	if len(events) != 0 {
		t.Fatalf("hp only: want 0 events, got %+v", events)
	}

	// Add dfSpeed (DontFragment) → OR group now satisfied via DontFragment path.
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, e, monDontFragSpeed{V: 5}) })
	if len(events) != 1 || !events[0].entered {
		t.Fatalf("add dfSpeed: want [entered=true], got %+v", events)
	}

	// Remove dfSpeed → exits (OR group no longer satisfied).
	w.Write(func(fw *flecs.Writer) { flecs.Remove[monDontFragSpeed](fw, e) })
	if len(events) != 2 || events[1].entered {
		t.Fatalf("remove dfSpeed: want [entered=false], got %+v", events[1:])
	}
}

// ---- Test 26: MonitorWithOptions with logger set ----
// Exercises the w.logger != nil debug-log branch in MonitorWithOptions.

func TestMonitorWithLogger(t *testing.T) {
	w := newMonWorld()
	w.SetLogger(slog.Default())
	healthID := flecs.RegisterComponent[monHealth](w)

	var fired bool
	obs := flecs.Monitor(w, []flecs.Term{flecs.With(healthID)}, func(fw *flecs.Writer, e flecs.ID, entered bool) {
		fired = true
	})
	if obs == nil {
		t.Fatal("Monitor returned nil observer")
	}
	// Sanity-check: the monitor fires.
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, monHealth{HP: 1})
	})
	if !fired {
		t.Fatal("monitor did not fire after Set")
	}
	w.SetLogger(nil)
}

// ---- Test 27: all-DontFragment sweep with non-TermAnd term ----
// Exercises the non-TermAnd continue and seedID==0 early-return in monitorSweepExisting.

func TestMonitorYieldExistingAllDontFrag(t *testing.T) {
	w := newMonWorld()
	hpID := flecs.RegisterComponent[monDontFragHP](w)
	flecs.SetDontFragment(w, hpID)
	frozenID := flecs.RegisterComponent[monFrozen](w)

	// Only TermAnd term is DontFragment + a TermNot archetype term.
	// monitorSweepExisting: all TermAnd are DontFragment → seedID==0 → return.
	var events []monEvent
	flecs.MonitorWithOptions(w,
		[]flecs.Term{flecs.With(hpID), flecs.Without(frozenID)},
		flecs.WithYieldExisting(),
		func(fw *flecs.Writer, e flecs.ID, entered bool) {
			events = append(events, monEvent{e: e, entered: entered})
		},
	)
	// Sweep returns early; no events fired for existing entities.
	if len(events) != 0 {
		t.Fatalf("all-DontFrag yield_existing: want 0 events, got %+v", events)
	}
}

// ---- Test 28: Sparse monitor delete — entity NOT in matched set ----
// Exercises the m.matched[e] not-found path in fireMonitorsOnDelete.

func TestMonitorDeleteSparseNotMatched(t *testing.T) {
	w := newMonWorld()
	hpID := flecs.RegisterComponent[monDontFragHP](w)
	flecs.SetDontFragment(w, hpID)

	var events []monEvent
	flecs.Monitor(w, []flecs.Term{flecs.With(hpID)}, func(fw *flecs.Writer, e flecs.ID, entered bool) {
		events = append(events, monEvent{e: e, entered: entered})
	})

	// Entity does NOT have DontFragHP — never added to m.matched.
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// Delete entity without adding DontFragHP → fireMonitorsOnDelete skips it silently.
	w.Write(func(fw *flecs.Writer) { fw.Delete(e) })
	if len(events) != 0 {
		t.Fatalf("delete unmatched sparse: want 0 events, got %+v", events)
	}
}

// ---- Test 29: Disabled monitor on entity delete ----
// Exercises the !m.observer.enabled branch inside fireMonitorsOnDelete.

func TestMonitorDisabledOnDelete(t *testing.T) {
	w := newMonWorld()
	healthID := flecs.RegisterComponent[monHealth](w)

	var events []monEvent
	obs := flecs.Monitor(w, []flecs.Term{flecs.With(healthID)}, func(fw *flecs.Writer, e flecs.ID, entered bool) {
		events = append(events, monEvent{e: e, entered: entered})
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, monHealth{HP: 1})
	})
	if len(events) != 1 {
		t.Fatalf("before disable: want 1 event, got %+v", events)
	}

	// Disable the monitor, then delete — no exit event should fire.
	obs.SetEnabled(false)
	w.Write(func(fw *flecs.Writer) { fw.Delete(e) })
	if len(events) != 1 {
		t.Fatalf("after delete with disabled monitor: want still 1 event, got %+v", events)
	}
}

// ---- Test 30: fw.Clear (Writer method form) fires monitor exit ----
// Exercises the (fw *Writer).Clear method on scope.go which is otherwise uncovered.

func TestMonitorFwClearMethod(t *testing.T) {
	w := newMonWorld()
	healthID := flecs.RegisterComponent[monHealth](w)

	var events []monEvent
	flecs.Monitor(w, []flecs.Term{flecs.With(healthID)}, func(fw *flecs.Writer, e flecs.ID, entered bool) {
		events = append(events, monEvent{e: e, entered: entered})
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, monHealth{HP: 1})
	})
	if len(events) != 1 || !events[0].entered {
		t.Fatalf("after add: want [entered=true], got %+v", events)
	}

	// Use the Writer method form fw.Clear instead of flecs.Clear.
	w.Write(func(fw *flecs.Writer) { fw.Clear(e) })
	if len(events) != 2 || events[1].entered {
		t.Fatalf("after fw.Clear: want [entered=false], got %+v", events[1:])
	}
}

// ---- Test 31: TermNot DontFragment in monitorMatchesTable ----
// Exercises the TermNot DontFragment/Union continue path in monitorMatchesTable.

func TestMonitorNotTermDontFragInTable(t *testing.T) {
	w := newMonWorld()
	healthID := flecs.RegisterComponent[monHealth](w)
	hpID := flecs.RegisterComponent[monDontFragHP](w)
	flecs.SetDontFragment(w, hpID)

	// sparseMode=true (TermNot DontFragment), seedID=healthID.
	// monitorSweepExisting calls monitorMatchesTable, hitting TermNot DontFragment → continue.
	var entered []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, monHealth{HP: 1}) // has Health, no DontFragHP → matches
	})
	flecs.MonitorWithOptions(w,
		[]flecs.Term{flecs.With(healthID), flecs.Without(hpID)},
		flecs.WithYieldExisting(),
		func(fw *flecs.Writer, e flecs.ID, in bool) {
			if in {
				entered = append(entered, e)
			}
		},
	)
	if len(entered) != 1 {
		t.Fatalf("TermNot DontFrag sweep: want 1 entity, got %v", entered)
	}
}
