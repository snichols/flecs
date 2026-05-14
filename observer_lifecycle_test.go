package flecs_test

import (
	"sort"
	"testing"

	"github.com/snichols/flecs"
)

type obsPos struct{ X, Y float32 }

// ── Observer disabling ──────────────────────────────────────────────────────

// Test 1: Basic observer fires on Set.
func TestObserverDisable_BasicFires(t *testing.T) {
	w := flecs.New()
	var count int
	w.Write(func(fw *flecs.Writer) {
		flecs.Observe[obsPos](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v obsPos) {
			count++
		})
		e := fw.NewEntity()
		flecs.Set(fw, e, obsPos{X: 1, Y: 2})
	})
	if count != 1 {
		t.Fatalf("expected 1 invocation, got %d", count)
	}
}

// Test 2: SetEnabled(false) suppresses subsequent Set.
func TestObserverDisable_DisabledSkips(t *testing.T) {
	w := flecs.New()
	var count int
	var obs *flecs.Observer
	w.Write(func(fw *flecs.Writer) {
		obs = flecs.Observe[obsPos](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v obsPos) {
			count++
		})
		e := fw.NewEntity()
		flecs.Set(fw, e, obsPos{X: 1, Y: 2})
	})
	obs.SetEnabled(false)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, obsPos{X: 3, Y: 4})
	})
	if count != 1 {
		t.Fatalf("expected 1 invocation (second suppressed), got %d", count)
	}
}

// Test 3: Re-enabling fires again.
func TestObserverDisable_ReEnableFires(t *testing.T) {
	w := flecs.New()
	var count int
	var obs *flecs.Observer
	w.Write(func(fw *flecs.Writer) {
		obs = flecs.Observe[obsPos](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v obsPos) {
			count++
		})
		e := fw.NewEntity()
		flecs.Set(fw, e, obsPos{X: 1, Y: 2})
	})
	obs.SetEnabled(false)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, obsPos{X: 3, Y: 4})
	})
	obs.SetEnabled(true)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, obsPos{X: 5, Y: 6})
	})
	if count != 2 {
		t.Fatalf("expected 2 invocations, got %d", count)
	}
}

// Test 4: IsEnabled round-trip.
func TestObserverDisable_IsEnabledRoundTrip(t *testing.T) {
	w := flecs.New()
	var obs *flecs.Observer
	w.Write(func(fw *flecs.Writer) {
		obs = flecs.Observe[obsPos](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v obsPos) {})
	})
	if !obs.IsEnabled() {
		t.Fatal("new observer should be enabled by default")
	}
	obs.SetEnabled(false)
	if obs.IsEnabled() {
		t.Fatal("observer should be disabled after SetEnabled(false)")
	}
	obs.SetEnabled(true)
	if !obs.IsEnabled() {
		t.Fatal("observer should be re-enabled after SetEnabled(true)")
	}
}

// Test 5: Two observers on the same event; disable one; the other still fires.
func TestObserverDisable_TwoObserversDisableOne(t *testing.T) {
	w := flecs.New()
	var countA, countB int
	var obsA *flecs.Observer
	w.Write(func(fw *flecs.Writer) {
		obsA = flecs.Observe[obsPos](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v obsPos) { countA++ })
		flecs.Observe[obsPos](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v obsPos) { countB++ })
		e := fw.NewEntity()
		flecs.Set(fw, e, obsPos{X: 1, Y: 2})
	})
	obsA.SetEnabled(false)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, obsPos{X: 3, Y: 4})
	})
	if countA != 1 {
		t.Fatalf("obsA: expected 1 invocation, got %d", countA)
	}
	if countB != 2 {
		t.Fatalf("obsB: expected 2 invocations, got %d", countB)
	}
}

// Test 6: Disable mid-dispatch — observer A disables observer B (later in list) during A's callback.
// B should be skipped for this dispatch because its enabled flag is read per-node.
func TestObserverDisable_MidDispatch(t *testing.T) {
	w := flecs.New()
	var countB int
	var obsB *flecs.Observer
	w.Write(func(fw *flecs.Writer) {
		flecs.Observe[obsPos](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v obsPos) {
			obsB.SetEnabled(false)
		})
		obsB = flecs.Observe[obsPos](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v obsPos) {
			countB++
		})
	})
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, obsPos{X: 1, Y: 2})
	})
	if countB != 0 {
		t.Fatalf("obsB disabled mid-dispatch before being visited; expected 0 invocations, got %d", countB)
	}
}

// ── yield_existing ────────────────────────────────────────────────────────────

// Test 7: 100 entities with obsPos; OnAdd observer with WithYieldExisting fires 100 times.
func TestYieldExisting_OnAdd100Entities(t *testing.T) {
	w := flecs.New()
	var created []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < 100; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, obsPos{X: float32(i), Y: 0})
			created = append(created, e)
		}
	})
	var visited []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveWithOptions[obsPos](w, flecs.WithYieldExisting(), []flecs.EventKind{flecs.EventOnAdd},
			func(fw *flecs.Writer, ev flecs.EventKind, e flecs.ID, v obsPos) {
				visited = append(visited, e)
			})
	})
	if len(visited) != 100 {
		t.Fatalf("expected 100 yield invocations, got %d", len(visited))
	}
	sort.Slice(created, func(i, j int) bool { return created[i] < created[j] })
	sort.Slice(visited, func(i, j int) bool { return visited[i] < visited[j] })
	for i := range created {
		if created[i] != visited[i] {
			t.Fatalf("entity mismatch at index %d: created=%v visited=%v", i, created[i], visited[i])
		}
	}
}

// Test 8: Without WithYieldExisting, existing entities are NOT yielded; new Set DOES fire.
func TestYieldExisting_NoYieldByDefault(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, obsPos{X: 1, Y: 2})
	})
	var count int
	var newE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		flecs.Observe[obsPos](w, flecs.EventOnSet, func(fw *flecs.Writer, e flecs.ID, v obsPos) { count++ })
		newE = fw.NewEntity()
		flecs.Set(fw, newE, obsPos{X: 3, Y: 4})
	})
	if count != 1 {
		t.Fatalf("expected 1 invocation (only for newE), got %d", count)
	}
}

// Test 9: yield_existing with OnSet delivers OnSet event kind to callback.
func TestYieldExisting_OnSetEventKind(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, obsPos{X: 1, Y: 2})
	})
	var seenEvents []flecs.EventKind
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveWithOptions[obsPos](w, flecs.WithYieldExisting(), []flecs.EventKind{flecs.EventOnSet},
			func(fw *flecs.Writer, ev flecs.EventKind, e flecs.ID, v obsPos) {
				seenEvents = append(seenEvents, ev)
			})
	})
	if len(seenEvents) != 1 || seenEvents[0] != flecs.EventOnSet {
		t.Fatalf("expected [OnSet], got %v", seenEvents)
	}
}

// Test 10: yield_existing sweep fires only the newly-registered observer; peer observers are NOT re-fired.
func TestYieldExisting_PeerNotReFired(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, obsPos{X: 1, Y: 2})
	})
	var peerCount int
	w.Write(func(fw *flecs.Writer) {
		flecs.Observe[obsPos](w, flecs.EventOnAdd, func(fw *flecs.Writer, e flecs.ID, v obsPos) { peerCount++ })
	})
	var newCount int
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveWithOptions[obsPos](w, flecs.WithYieldExisting(), []flecs.EventKind{flecs.EventOnAdd},
			func(fw *flecs.Writer, ev flecs.EventKind, e flecs.ID, v obsPos) { newCount++ })
	})
	if peerCount != 0 {
		t.Fatalf("peer observer should not fire during yield sweep; got %d", peerCount)
	}
	if newCount != 1 {
		t.Fatalf("yield observer should fire once for existing entity; got %d", newCount)
	}
}

// Test 11: yield_existing skips entities carrying the Disabled tag.
func TestYieldExisting_SkipsDisabledEntities(t *testing.T) {
	w := flecs.New()
	var normalE, disabledE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		normalE = fw.NewEntity()
		flecs.Set(fw, normalE, obsPos{X: 1, Y: 2})

		disabledE = fw.NewEntity()
		flecs.Set(fw, disabledE, obsPos{X: 3, Y: 4})
		flecs.DisableEntity(fw, disabledE)
	})
	var visited []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveWithOptions[obsPos](w, flecs.WithYieldExisting(), []flecs.EventKind{flecs.EventOnAdd},
			func(fw *flecs.Writer, ev flecs.EventKind, e flecs.ID, v obsPos) {
				visited = append(visited, e)
			})
	})
	for _, e := range visited {
		if e == disabledE {
			t.Fatal("yield sweep should not have visited disabled entity")
		}
	}
	found := false
	for _, e := range visited {
		if e == normalE {
			found = true
		}
	}
	if !found {
		t.Fatal("yield sweep should have visited normal entity")
	}
}

// Test 12: yield_existing skips entities carrying the Prefab tag.
func TestYieldExisting_SkipsPrefabEntities(t *testing.T) {
	w := flecs.New()
	var normalE, prefabE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		normalE = fw.NewEntity()
		flecs.Set(fw, normalE, obsPos{X: 1, Y: 2})

		prefabE = fw.NewEntity()
		flecs.Set(fw, prefabE, obsPos{X: 3, Y: 4})
		flecs.MarkPrefab(fw, prefabE)
	})
	var visited []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveWithOptions[obsPos](w, flecs.WithYieldExisting(), []flecs.EventKind{flecs.EventOnAdd},
			func(fw *flecs.Writer, ev flecs.EventKind, e flecs.ID, v obsPos) {
				visited = append(visited, e)
			})
	})
	for _, e := range visited {
		if e == prefabE {
			t.Fatal("yield sweep should not have visited prefab entity")
		}
	}
	found := false
	for _, e := range visited {
		if e == normalE {
			found = true
		}
	}
	if !found {
		t.Fatal("yield sweep should have visited normal entity")
	}
}

// Test 13: yield_existing with OnAdd + OnSet fires 2×N for N existing entities.
func TestYieldExisting_MultiEvent(t *testing.T) {
	w := flecs.New()
	const N = 5
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < N; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, obsPos{X: float32(i), Y: 0})
		}
	})
	var addCount, setCount int
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveWithOptions[obsPos](w, flecs.WithYieldExisting(), []flecs.EventKind{flecs.EventOnAdd, flecs.EventOnSet},
			func(fw *flecs.Writer, ev flecs.EventKind, e flecs.ID, v obsPos) {
				switch ev {
				case flecs.EventOnAdd:
					addCount++
				case flecs.EventOnSet:
					setCount++
				}
			})
	})
	if addCount != N {
		t.Fatalf("expected %d OnAdd invocations, got %d", N, addCount)
	}
	if setCount != N {
		t.Fatalf("expected %d OnSet invocations, got %d", N, setCount)
	}
}

// Test 14: yield_existing with only OnRemove panics.
func TestYieldExisting_OnlyOnRemovePanics(t *testing.T) {
	w := flecs.New()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for OnRemove-only with yieldExisting")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveWithOptions[obsPos](w, flecs.WithYieldExisting(), []flecs.EventKind{flecs.EventOnRemove},
			func(fw *flecs.Writer, ev flecs.EventKind, e flecs.ID, v obsPos) {})
	})
}

// Test 15: yield_existing sweep is synchronous — all invocations complete before ObserveWithOptions returns.
func TestYieldExisting_Synchronous(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < 10; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, obsPos{X: float32(i), Y: 0})
		}
	})
	var count int
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveWithOptions[obsPos](w, flecs.WithYieldExisting(), []flecs.EventKind{flecs.EventOnAdd},
			func(fw *flecs.Writer, ev flecs.EventKind, e flecs.ID, v obsPos) {
				count++
			})
		if count != 10 {
			t.Errorf("expected count=10 immediately after ObserveWithOptions (synchronous sweep), got %d", count)
		}
	})
}

// Test 16: Entities added via AddID (no Set value) are also visited by yield sweep.
func TestYieldExisting_AddIDEntities(t *testing.T) {
	w := flecs.New()
	id := flecs.RegisterComponent[obsPos](w)
	var created []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < 20; i++ {
			e := fw.NewEntity()
			fw.AddID(e, id)
			created = append(created, e)
		}
	})
	var visited []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		flecs.ObserveWithOptions[obsPos](w, flecs.WithYieldExisting(), []flecs.EventKind{flecs.EventOnAdd},
			func(fw *flecs.Writer, ev flecs.EventKind, e flecs.ID, v obsPos) {
				visited = append(visited, e)
			})
	})
	if len(visited) != 20 {
		t.Fatalf("expected 20 yield invocations for AddID entities, got %d", len(visited))
	}
}
