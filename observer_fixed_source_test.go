package flecs_test

import (
	"testing"
	"unsafe"

	"github.com/snichols/flecs"
)

// component types local to fixed-source observer tests
type fsPosition struct{ X, Y float32 }
type fsVelocity struct{ VX, VY float32 }

// TestFixedSourceOnSet verifies that a fixed-source OnSet observer fires only
// for the named entity, not for other entities.
func TestFixedSourceOnSet(t *testing.T) {
	w := flecs.New()

	var player, other flecs.ID
	w.Write(func(fw *flecs.Writer) {
		player = fw.NewEntity()
		other = fw.NewEntity()
	})

	var fired []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveWithOptions[fsPosition](w,
			flecs.WithSource(player),
			[]flecs.EventKind{flecs.EventOnSet},
			func(fw *flecs.Writer, _ flecs.EventKind, e flecs.ID, _ fsPosition) {
				fired = append(fired, e)
			},
		)
	})

	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, other, fsPosition{1, 2}) })
	if len(fired) != 0 {
		t.Fatalf("observer fired for unrelated entity; want 0 fires, got %d", len(fired))
	}

	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, player, fsPosition{3, 4}) })
	if len(fired) != 1 || fired[0] != player {
		t.Fatalf("observer did not fire for source entity; got %v", fired)
	}

	// second set on player fires again
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, player, fsPosition{5, 6}) })
	if len(fired) != 2 {
		t.Fatalf("expected 2 fires total, got %d", len(fired))
	}
}

// TestFixedSourceOnAdd verifies OnAdd + WithSource filtering.
func TestFixedSourceOnAdd(t *testing.T) {
	w := flecs.New()
	var player, other flecs.ID
	w.Write(func(fw *flecs.Writer) {
		player = fw.NewEntity()
		other = fw.NewEntity()
	})

	var fired []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveWithOptions[fsPosition](w,
			flecs.WithSource(player),
			[]flecs.EventKind{flecs.EventOnAdd},
			func(fw *flecs.Writer, _ flecs.EventKind, e flecs.ID, _ fsPosition) {
				fired = append(fired, e)
			},
		)
	})

	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, other, fsPosition{}) })
	if len(fired) != 0 {
		t.Fatalf("OnAdd fired for unrelated entity")
	}
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, player, fsPosition{}) })
	if len(fired) != 1 || fired[0] != player {
		t.Fatalf("OnAdd did not fire for source entity; got %v", fired)
	}
}

// TestFixedSourceOnRemove verifies OnRemove + WithSource filtering.
func TestFixedSourceOnRemove(t *testing.T) {
	w := flecs.New()
	var player, other flecs.ID
	w.Write(func(fw *flecs.Writer) {
		player = fw.NewEntity()
		other = fw.NewEntity()
		flecs.Set(fw, player, fsPosition{})
		flecs.Set(fw, other, fsPosition{})
	})

	var fired []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveWithOptions[fsPosition](w,
			flecs.WithSource(player),
			[]flecs.EventKind{flecs.EventOnRemove},
			func(fw *flecs.Writer, _ flecs.EventKind, e flecs.ID, _ fsPosition) {
				fired = append(fired, e)
			},
		)
	})

	w.Write(func(fw *flecs.Writer) { flecs.Remove[fsPosition](fw, other) })
	if len(fired) != 0 {
		t.Fatalf("OnRemove fired for unrelated entity")
	}
	w.Write(func(fw *flecs.Writer) { flecs.Remove[fsPosition](fw, player) })
	if len(fired) != 1 || fired[0] != player {
		t.Fatalf("OnRemove did not fire for source entity; got %v", fired)
	}
}

// TestFixedSourceYieldExistingHas verifies that yield_existing + WithSource fires
// once when the source entity already has the component at registration time.
func TestFixedSourceYieldExistingHas(t *testing.T) {
	w := flecs.New()
	var player flecs.ID
	w.Write(func(fw *flecs.Writer) {
		player = fw.NewEntity()
		flecs.Set(fw, player, fsPosition{X: 7, Y: 8})
	})

	var calls []fsPosition
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveWithOptions[fsPosition](w,
			flecs.WithYieldExisting().AndSource(player),
			[]flecs.EventKind{flecs.EventOnSet},
			func(fw *flecs.Writer, _ flecs.EventKind, _ flecs.ID, v fsPosition) {
				calls = append(calls, v)
			},
		)
	})

	if len(calls) != 1 {
		t.Fatalf("yield_existing+WithSource: want 1 fire, got %d", len(calls))
	}
	if calls[0].X != 7 || calls[0].Y != 8 {
		t.Fatalf("yield_existing value mismatch: %+v", calls[0])
	}
}

// TestFixedSourceYieldExistingMissing verifies that yield_existing + WithSource
// does NOT fire when the source entity does not have the component.
func TestFixedSourceYieldExistingMissing(t *testing.T) {
	w := flecs.New()
	var player flecs.ID
	w.Write(func(fw *flecs.Writer) { player = fw.NewEntity() })

	var calls int
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveWithOptions[fsPosition](w,
			flecs.WithYieldExisting().AndSource(player),
			[]flecs.EventKind{flecs.EventOnSet},
			func(fw *flecs.Writer, _ flecs.EventKind, _ flecs.ID, _ fsPosition) {
				calls++
			},
		)
	})

	if calls != 0 {
		t.Fatalf("yield_existing+WithSource: expected 0 fires when component absent, got %d", calls)
	}
}

// TestFixedSourceMultipleObservers verifies that multiple fixed-source observers
// on the same source all fire in registration order.
func TestFixedSourceMultipleObservers(t *testing.T) {
	w := flecs.New()
	var player flecs.ID
	w.Write(func(fw *flecs.Writer) { player = fw.NewEntity() })

	var order []int
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveWithOptions[fsPosition](w,
			flecs.WithSource(player),
			[]flecs.EventKind{flecs.EventOnSet},
			func(fw *flecs.Writer, _ flecs.EventKind, _ flecs.ID, _ fsPosition) {
				order = append(order, 1)
			},
		)
		flecs.ObserveWithOptions[fsPosition](w,
			flecs.WithSource(player),
			[]flecs.EventKind{flecs.EventOnSet},
			func(fw *flecs.Writer, _ flecs.EventKind, _ flecs.ID, _ fsPosition) {
				order = append(order, 2)
			},
		)
	})

	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, player, fsPosition{}) })
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("registration order not preserved: %v", order)
	}
}

// TestFixedSourceMixed verifies that an any-entity observer and a fixed-source
// observer on the same component both fire when the fixed source is set
// (any-entity fires first), and only the any-entity observer fires for other entities.
func TestFixedSourceMixed(t *testing.T) {
	w := flecs.New()
	var player, other flecs.ID
	w.Write(func(fw *flecs.Writer) {
		player = fw.NewEntity()
		other = fw.NewEntity()
	})

	var anyFired []flecs.ID
	var fixedFired []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		flecs.Observe[fsPosition](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, _ fsPosition) {
			anyFired = append(anyFired, e)
		})
		flecs.ObserveWithOptions[fsPosition](w,
			flecs.WithSource(player),
			[]flecs.EventKind{flecs.EventOnSet},
			func(fw *flecs.Writer, _ flecs.EventKind, e flecs.ID, _ fsPosition) {
				fixedFired = append(fixedFired, e)
			},
		)
	})

	// Set on fixed source → both fire; any-entity appears first in anyFired,
	// confirming it fires before the fixed-source callback.
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, player, fsPosition{}) })
	if len(anyFired) != 1 || anyFired[0] != player {
		t.Fatalf("any-entity did not fire for player; got %v", anyFired)
	}
	if len(fixedFired) != 1 || fixedFired[0] != player {
		t.Fatalf("fixed-source did not fire for player; got %v", fixedFired)
	}

	anyFired = anyFired[:0]
	fixedFired = fixedFired[:0]

	// Set on other entity → only any-entity fires.
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, other, fsPosition{}) })
	if len(anyFired) != 1 || anyFired[0] != other {
		t.Fatalf("any-entity did not fire for other; got %v", anyFired)
	}
	if len(fixedFired) != 0 {
		t.Fatalf("fixed-source fired for non-source entity; got %v", fixedFired)
	}
}

// TestFixedSourceDisabled verifies that SetEnabled(false) silences a fixed-source observer.
func TestFixedSourceDisabled(t *testing.T) {
	w := flecs.New()
	var player flecs.ID
	w.Write(func(fw *flecs.Writer) { player = fw.NewEntity() })

	var fired int
	var obs *flecs.Observer
	w.Write(func(fw *flecs.Writer) {
		obs = flecs.ObserveWithOptions[fsPosition](w,
			flecs.WithSource(player),
			[]flecs.EventKind{flecs.EventOnSet},
			func(fw *flecs.Writer, _ flecs.EventKind, _ flecs.ID, _ fsPosition) { fired++ },
		)
	})

	obs.SetEnabled(false)
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, player, fsPosition{}) })
	if fired != 0 {
		t.Fatalf("disabled fixed-source observer fired; got %d fires", fired)
	}

	obs.SetEnabled(true)
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, player, fsPosition{}) })
	if fired != 1 {
		t.Fatalf("re-enabled fixed-source observer did not fire; got %d fires", fired)
	}
}

// TestFixedSourceZeroPanics verifies that WithSource(0) panics at construction.
func TestFixedSourceZeroPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for WithSource(0), but no panic occurred")
		}
	}()
	flecs.WithSource(0)
}

// TestFixedSourceStaleIDNeverFires registers a fixed-source observer for an entity
// that is subsequently deleted, then verifies no fires occur for the dead ID.
func TestFixedSourceStaleIDNeverFires(t *testing.T) {
	w := flecs.New()
	var player flecs.ID
	w.Write(func(fw *flecs.Writer) { player = fw.NewEntity() })

	var fired int
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveWithOptions[fsPosition](w,
			flecs.WithSource(player),
			[]flecs.EventKind{flecs.EventOnSet},
			func(fw *flecs.Writer, _ flecs.EventKind, _ flecs.ID, _ fsPosition) { fired++ },
		)
	})

	// Delete the source entity.
	w.Write(func(fw *flecs.Writer) { fw.Delete(player) })

	// Any future set on a different entity must not fire the observer.
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, fsPosition{})
	})
	if fired != 0 {
		t.Fatalf("fixed-source observer fired after source was deleted; got %d fires", fired)
	}
}

// TestFixedSourceOnTableCreatePanics verifies that combining WithSource with
// EventOnTableCreate panics at registration time.
func TestFixedSourceOnTableCreatePanics(t *testing.T) {
	w := flecs.New()
	var sentinel flecs.ID
	w.Write(func(fw *flecs.Writer) { sentinel = fw.NewEntity() })

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for OnTableCreate+WithSource, but no panic occurred")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveIDWithOptions(w,
			flecs.RegisterComponent[fsPosition](w),
			flecs.WithSource(sentinel),
			[]flecs.EventKind{flecs.EventOnTableCreate},
			func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {},
		)
	})
}

// TestFixedSourceCustomEvent verifies that ObserveEventWithOptions + WithSource
// fires via Emit only for the named entity.
func TestFixedSourceCustomEvent(t *testing.T) {
	w := flecs.New()
	var eventID, player, other flecs.ID
	w.Write(func(fw *flecs.Writer) {
		eventID = flecs.RegisterEvent(fw, "fsTestEvent")
		player = fw.NewEntity()
		other = fw.NewEntity()
	})

	var received []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveEventWithOptions(w, eventID, flecs.WithSource(player),
			func(fw *flecs.Writer, e flecs.ID, _ interface{}) {
				received = append(received, e)
			},
		)
	})

	w.Write(func(fw *flecs.Writer) { flecs.Emit(fw, eventID, other, nil) })
	if len(received) != 0 {
		t.Fatalf("custom event observer fired for non-source entity")
	}

	w.Write(func(fw *flecs.Writer) { flecs.Emit(fw, eventID, player, nil) })
	if len(received) != 1 || received[0] != player {
		t.Fatalf("custom event observer did not fire for source entity; got %v", received)
	}
}

// TestFixedSourceObserveIDWithOptions verifies that ObserveIDWithOptions + WithSource
// filters by entity correctly.
func TestFixedSourceObserveIDWithOptions(t *testing.T) {
	w := flecs.New()
	var player, other flecs.ID
	w.Write(func(fw *flecs.Writer) {
		player = fw.NewEntity()
		other = fw.NewEntity()
	})

	id := flecs.RegisterComponent[fsVelocity](w)

	var fired []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveIDWithOptions(w, id, flecs.WithSource(player), []flecs.EventKind{flecs.EventOnSet},
			func(fw *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {
				fired = append(fired, e)
			},
		)
	})

	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, other, fsVelocity{}) })
	if len(fired) != 0 {
		t.Fatalf("ObserveIDWithOptions+WithSource fired for unrelated entity")
	}
	w.Write(func(fw *flecs.Writer) { flecs.Set(fw, player, fsVelocity{}) })
	if len(fired) != 1 || fired[0] != player {
		t.Fatalf("ObserveIDWithOptions+WithSource did not fire for source; got %v", fired)
	}
}

// TestFixedSourceObserveIDWithOptionsYieldExisting verifies that
// ObserveIDWithOptions with yieldExisting fires for entities carrying the
// component at registration time (any-entity path).
func TestFixedSourceObserveIDWithOptionsYieldExisting(t *testing.T) {
	w := flecs.New()
	id := flecs.RegisterComponent[fsPosition](w)

	var entity flecs.ID
	w.Write(func(fw *flecs.Writer) {
		entity = fw.NewEntity()
		flecs.Set(fw, entity, fsPosition{X: 1, Y: 2})
	})

	var yields []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveIDWithOptions(w, id, flecs.WithYieldExisting(), []flecs.EventKind{flecs.EventOnSet},
			func(fw *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {
				yields = append(yields, e)
			},
		)
	})

	if len(yields) != 1 || yields[0] != entity {
		t.Fatalf("ObserveIDWithOptions yield_existing: want 1 entity, got %v", yields)
	}
}

// TestFixedSourceObserveIDWithOptionsYieldExistingSource verifies that
// ObserveIDWithOptions with yieldExisting + WithSource fires exactly once for the
// named source entity when it already holds the component.
func TestFixedSourceObserveIDWithOptionsYieldExistingSource(t *testing.T) {
	w := flecs.New()
	id := flecs.RegisterComponent[fsPosition](w)

	var player flecs.ID
	w.Write(func(fw *flecs.Writer) {
		player = fw.NewEntity()
		flecs.Set(fw, player, fsPosition{X: 9, Y: 9})
	})

	var yields int
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveIDWithOptions(w, id, flecs.WithYieldExisting().AndSource(player), []flecs.EventKind{flecs.EventOnSet},
			func(fw *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
				yields++
			},
		)
	})

	if yields != 1 {
		t.Fatalf("ObserveIDWithOptions yield_existing+source: want 1 fire, got %d", yields)
	}
}

// TestFixedSourceObserveIDWithOptionsOnlyRemovePanics verifies the panic when
// yieldExisting is combined with an OnRemove-only event list.
func TestFixedSourceObserveIDWithOptionsOnlyRemovePanics(t *testing.T) {
	w := flecs.New()
	id := flecs.RegisterComponent[fsPosition](w)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for yieldExisting+OnRemove-only in ObserveIDWithOptions")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveIDWithOptions(w, id, flecs.WithYieldExisting(), []flecs.EventKind{flecs.EventOnRemove},
			func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {},
		)
	})
}

// TestFixedSourceAndSourceZeroPanics verifies that AndSource(0) panics.
func TestFixedSourceAndSourceZeroPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for AndSource(0), but no panic occurred")
		}
	}()
	flecs.WithYieldExisting().AndSource(0)
}

// TestFixedSourceCustomEventPayload verifies that ObserveEventWithOptions delivers
// a non-nil payload to the callback.
func TestFixedSourceCustomEventPayload(t *testing.T) {
	w := flecs.New()
	var eventID, player flecs.ID
	w.Write(func(fw *flecs.Writer) {
		eventID = flecs.RegisterEvent(fw, "fsPayloadEvent")
		player = fw.NewEntity()
	})

	var got interface{}
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveEventWithOptions(w, eventID, flecs.WithSource(player),
			func(_ *flecs.Writer, _ flecs.ID, payload interface{}) {
				got = payload
			},
		)
	})

	w.Write(func(fw *flecs.Writer) { flecs.Emit(fw, eventID, player, 42) })
	if got != 42 {
		t.Fatalf("expected payload 42, got %v", got)
	}
}

// TestFixedSourceYieldExistingDisabledSource verifies that yield_existing +
// WithSource does NOT fire when the source entity's table carries the Disabled tag.
func TestFixedSourceYieldExistingDisabledSource(t *testing.T) {
	w := flecs.New()
	var player flecs.ID
	w.Write(func(fw *flecs.Writer) {
		player = fw.NewEntity()
		flecs.Set(fw, player, fsPosition{X: 1, Y: 2})
		// Mark the entity as disabled so it mirrors a prefab/disabled archetype.
		flecs.AddID(fw, player, w.Disabled())
	})

	var calls int
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveWithOptions[fsPosition](w,
			flecs.WithYieldExisting().AndSource(player),
			[]flecs.EventKind{flecs.EventOnSet},
			func(fw *flecs.Writer, _ flecs.EventKind, _ flecs.ID, _ fsPosition) {
				calls++
			},
		)
	})

	if calls != 0 {
		t.Fatalf("yield_existing+WithSource should skip disabled source; got %d fires", calls)
	}
}

// TestFixedSourceSparseYieldExisting verifies that yield_existing + WithSource works
// for sparse components (entityRawPtrForYield sparse-set path).
func TestFixedSourceSparseYieldExisting(t *testing.T) {
	type SparseComp struct{ Val int }

	w := flecs.New()
	id := flecs.RegisterComponent[SparseComp](w)
	flecs.SetSparse(w, id)

	var player flecs.ID
	w.Write(func(fw *flecs.Writer) {
		player = fw.NewEntity()
		flecs.Set(fw, player, SparseComp{Val: 55})
	})

	var yields int
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveIDWithOptions(w, id, flecs.WithYieldExisting().AndSource(player), []flecs.EventKind{flecs.EventOnSet},
			func(fw *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
				yields++
			},
		)
	})

	if yields != 1 {
		t.Fatalf("sparse yield_existing+WithSource: want 1 fire, got %d", yields)
	}
}

// TestFixedSourceSparseMissingYieldExisting verifies that yield_existing + WithSource
// does NOT fire when the source entity does not hold a sparse component.
func TestFixedSourceSparseMissingYieldExisting(t *testing.T) {
	type SparseComp2 struct{ Val int }

	w := flecs.New()
	id := flecs.RegisterComponent[SparseComp2](w)
	flecs.SetSparse(w, id)

	var player flecs.ID
	w.Write(func(fw *flecs.Writer) { player = fw.NewEntity() })

	var yields int
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveIDWithOptions(w, id, flecs.WithYieldExisting().AndSource(player), []flecs.EventKind{flecs.EventOnSet},
			func(fw *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
				yields++
			},
		)
	})

	if yields != 0 {
		t.Fatalf("sparse yield_existing: expected 0 fires, got %d", yields)
	}
}

// TestFixedSourceSparseDisabledYieldExisting verifies that yield_existing + WithSource
// skips a sparse component on a disabled source entity.
func TestFixedSourceSparseDisabledYieldExisting(t *testing.T) {
	type SparseComp3 struct{ Val int }

	w := flecs.New()
	id := flecs.RegisterComponent[SparseComp3](w)
	flecs.SetSparse(w, id)

	var player flecs.ID
	w.Write(func(fw *flecs.Writer) {
		player = fw.NewEntity()
		flecs.Set(fw, player, SparseComp3{Val: 99})
		flecs.AddID(fw, player, w.Disabled())
	})

	var yields int
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveIDWithOptions(w, id, flecs.WithYieldExisting().AndSource(player), []flecs.EventKind{flecs.EventOnSet},
			func(fw *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
				yields++
			},
		)
	})

	if yields != 0 {
		t.Fatalf("sparse yield_existing on disabled entity: expected 0 fires, got %d", yields)
	}
}
