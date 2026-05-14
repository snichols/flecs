package flecs_test

import (
	"testing"
	"time"
	"unsafe"

	"github.com/snichols/flecs"
)

// ── 1. Single inheritor: OnSet fires for source then inheritor ──────────────

func TestPropagationSingleInheritor(t *testing.T) {
	w := flecs.New()
	var order []flecs.ID

	_ = flecs.Observe[Position](w, flecs.EventOnSet, func(_ *flecs.Writer, e flecs.ID, _ Position) {
		order = append(order, e)
	})

	var p, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), p))
	})
	order = nil // reset after setup

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{1, 2})
	})

	if len(order) != 2 {
		t.Fatalf("want 2 fires (source + 1 inheritor), got %d", len(order))
	}
	if order[0] != p {
		t.Fatalf("first fire should be source p=%v, got %v", p, order[0])
	}
	if order[1] != inst {
		t.Fatalf("second fire should be inheritor inst=%v, got %v", inst, order[1])
	}
}

// ── 2. Multiple inheritors: fires N+1 times ──────────────────────────────────

func TestPropagationMultipleInheritors(t *testing.T) {
	const N = 5
	w := flecs.New()
	var fires int

	_ = flecs.Observe[Position](w, flecs.EventOnSet, func(_ *flecs.Writer, _ flecs.ID, _ Position) {
		fires++
	})

	var p flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		for i := 0; i < N; i++ {
			inst := fw.NewEntity()
			flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), p))
		}
	})
	fires = 0 // reset after setup

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{3, 4})
	})

	if fires != N+1 {
		t.Fatalf("want %d fires (1 source + %d inheritors), got %d", N+1, N, fires)
	}
}

// ── 3. Recursive chain: BFS order P → B → A ──────────────────────────────────

func TestPropagationRecursiveChain(t *testing.T) {
	w := flecs.New()
	var order []flecs.ID

	_ = flecs.Observe[Position](w, flecs.EventOnSet, func(_ *flecs.Writer, e flecs.ID, _ Position) {
		order = append(order, e)
	})

	// A IsA B IsA P
	var p, b, a flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		b = fw.NewEntity()
		a = fw.NewEntity()
		flecs.AddID(fw, b, flecs.MakePair(w.IsA(), p))
		flecs.AddID(fw, a, flecs.MakePair(w.IsA(), b))
	})
	order = nil // reset after setup

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{1, 0})
	})

	if len(order) != 3 {
		t.Fatalf("want 3 fires (P, B, A), got %d: %v", len(order), order)
	}
	if order[0] != p {
		t.Fatalf("want P first, got %v", order[0])
	}
	if order[1] != b {
		t.Fatalf("want B second (BFS level 1), got %v", order[1])
	}
	if order[2] != a {
		t.Fatalf("want A third (BFS level 2), got %v", order[2])
	}
}

// ── 4. Diamond inheritance: A IsA B and A IsA C ──────────────────────────────

func TestPropagationDiamond(t *testing.T) {
	w := flecs.New()

	var bFires, cFires []flecs.ID

	_ = flecs.Observe[Position](w, flecs.EventOnSet, func(_ *flecs.Writer, e flecs.ID, _ Position) {
		// We'll track fires per-source externally; just track all fires.
		_ = e
	})

	// B and C are separate prefabs; A inherits from both.
	var b, c, a flecs.ID
	w.Write(func(fw *flecs.Writer) {
		b = fw.NewEntity()
		c = fw.NewEntity()
		a = fw.NewEntity()
		flecs.AddID(fw, a, flecs.MakePair(w.IsA(), b))
		flecs.AddID(fw, a, flecs.MakePair(w.IsA(), c))
	})

	// Observe separately to know which events fired for which source.
	_ = flecs.Observe[Position](w, flecs.EventOnSet, func(_ *flecs.Writer, e flecs.ID, _ Position) {
		bFires = append(bFires, e)
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, b, Position{1, 0})
	})
	// bFires should contain b and a (A inherits from B)
	found_b := false
	found_a_via_b := false
	for _, e := range bFires {
		if e == b {
			found_b = true
		}
		if e == a {
			found_a_via_b = true
		}
		if e == c {
			t.Fatalf("Set(B) must not fire for C")
		}
	}
	if !found_b {
		t.Fatalf("Set(B) must fire for B")
	}
	if !found_a_via_b {
		t.Fatalf("Set(B) must fire for A (A IsA B)")
	}

	bFires = nil
	_ = flecs.Observe[Position](w, flecs.EventOnSet, func(_ *flecs.Writer, e flecs.ID, _ Position) {
		cFires = append(cFires, e)
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, c, Position{2, 0})
	})
	// cFires should contain c and a (A inherits from C)
	found_c := false
	found_a_via_c := false
	for _, e := range cFires {
		if e == c {
			found_c = true
		}
		if e == a {
			found_a_via_c = true
		}
		if e == b {
			t.Fatalf("Set(C) must not fire for B")
		}
	}
	if !found_c {
		t.Fatalf("Set(C) must fire for C")
	}
	if !found_a_via_c {
		t.Fatalf("Set(C) must fire for A (A IsA C)")
	}
}

// ── 5. DontInherit blocks propagation ────────────────────────────────────────

func TestPropagationDontInheritBlocks(t *testing.T) {
	w := flecs.New()

	// Register Position and mark it DontInherit.
	posID := flecs.RegisterComponent[Position](w)
	flecs.SetInstantiatePolicy(w, posID, w.DontInherit())

	var fires []flecs.ID
	_ = flecs.Observe[Position](w, flecs.EventOnSet, func(_ *flecs.Writer, e flecs.ID, _ Position) {
		fires = append(fires, e)
	})

	var p, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), p))
	})
	fires = nil

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{1, 2})
	})

	if len(fires) != 1 || fires[0] != p {
		t.Fatalf("DontInherit: want only P to fire, got %v", fires)
	}
}

// ── 6. Override blocks propagation for the specific inheritor ─────────────────

func TestPropagationOverrideBlocks(t *testing.T) {
	w := flecs.New()
	var fires []flecs.ID

	_ = flecs.Observe[Position](w, flecs.EventOnSet, func(_ *flecs.Writer, e flecs.ID, _ Position) {
		fires = append(fires, e)
	})

	var p, overrider, inheritor flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		overrider = fw.NewEntity()
		inheritor = fw.NewEntity()
		flecs.AddID(fw, overrider, flecs.MakePair(w.IsA(), p))
		flecs.AddID(fw, inheritor, flecs.MakePair(w.IsA(), p))
		// overrider has its own Position (local copy masks the prefab's value)
		flecs.Set[Position](fw, overrider, Position{99, 99})
	})
	fires = nil // reset after setup (which fired for p, overrider during Set above)

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{1, 2})
	})

	// p fires, inheritor fires, but overrider does NOT fire (has local copy)
	gotP := false
	gotInheritor := false
	gotOverrider := false
	for _, e := range fires {
		switch e {
		case p:
			gotP = true
		case inheritor:
			gotInheritor = true
		case overrider:
			gotOverrider = true
		}
	}
	if !gotP {
		t.Fatal("Set(P) must fire for P")
	}
	if !gotInheritor {
		t.Fatal("Set(P) must fire for inheritor (no local copy)")
	}
	if gotOverrider {
		t.Fatal("Set(P) must NOT fire for overrider (has its own local copy)")
	}
}

// ── 7. OnAdd propagation ──────────────────────────────────────────────────────

func TestPropagationOnAdd(t *testing.T) {
	w := flecs.New()
	var fires []flecs.ID

	_ = flecs.Observe[Position](w, flecs.EventOnAdd, func(_ *flecs.Writer, e flecs.ID, _ Position) {
		fires = append(fires, e)
	})

	var p, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), p))
	})
	fires = nil // reset after instance setup

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{5, 6}) // Set triggers OnAdd on first add
	})

	found_p := false
	found_inst := false
	for _, e := range fires {
		if e == p {
			found_p = true
		}
		if e == inst {
			found_inst = true
		}
	}
	if !found_p {
		t.Fatal("OnAdd must fire for prefab P")
	}
	if !found_inst {
		t.Fatal("OnAdd must propagate to inheritor inst")
	}
}

// ── 8. OnRemove propagation ───────────────────────────────────────────────────

func TestPropagationOnRemove(t *testing.T) {
	w := flecs.New()
	var fires []flecs.ID

	_ = flecs.Observe[Position](w, flecs.EventOnRemove, func(_ *flecs.Writer, e flecs.ID, _ Position) {
		fires = append(fires, e)
	})

	var p, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), p))
		flecs.Set[Position](fw, p, Position{1, 2})
	})
	fires = nil // reset after setup

	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[Position](fw, p)
	})

	found_p := false
	found_inst := false
	for _, e := range fires {
		if e == p {
			found_p = true
		}
		if e == inst {
			found_inst = true
		}
	}
	if !found_p {
		t.Fatal("OnRemove must fire for prefab P")
	}
	if !found_inst {
		t.Fatal("OnRemove must propagate to inheritor inst")
	}
}

// ── 9. OnReplace hook propagation ────────────────────────────────────────────

func TestPropagationOnReplace(t *testing.T) {
	w := flecs.New()
	type replaceRecord struct {
		e      flecs.ID
		old    Position
		newVal Position
	}
	var records []replaceRecord

	flecs.OnReplace[Position](w, func(_ *flecs.Writer, e flecs.ID, old, nv Position) {
		records = append(records, replaceRecord{e, old, nv})
	})

	var p, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), p))
		flecs.Set[Position](fw, p, Position{1, 2}) // first Set: OnAdd + OnSet, no OnReplace
	})
	records = nil // reset: first Set doesn't fire OnReplace

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{3, 4}) // second Set: OnReplace fires
	})

	found_p := false
	found_inst := false
	for _, r := range records {
		switch r.e {
		case p:
			found_p = true
			if r.old != (Position{1, 2}) {
				t.Fatalf("P old want {1,2}, got %v", r.old)
			}
			if r.newVal != (Position{3, 4}) {
				t.Fatalf("P new want {3,4}, got %v", r.newVal)
			}
		case inst:
			found_inst = true
			if r.old != (Position{1, 2}) {
				t.Fatalf("inst old want {1,2}, got %v", r.old)
			}
			if r.newVal != (Position{3, 4}) {
				t.Fatalf("inst new want {3,4}, got %v", r.newVal)
			}
		}
	}
	if !found_p {
		t.Fatal("OnReplace must fire for prefab P")
	}
	if !found_inst {
		t.Fatal("OnReplace must propagate hook to inheritor inst")
	}
}

// ── 10. Pair components propagate ────────────────────────────────────────────

func TestPropagationPairComponents(t *testing.T) {
	w := flecs.New()

	var relID, tgtID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		relID = fw.NewEntity()
		tgtID = fw.NewEntity()
	})
	pairID := flecs.MakePair(relID, tgtID)

	var fires []flecs.ID
	_ = flecs.ObserveID(w, pairID, flecs.EventOnSet, func(_ *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {
		fires = append(fires, e)
	})

	var p, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), p))
	})
	fires = nil

	w.Write(func(fw *flecs.Writer) {
		flecs.SetPair[Velocity](fw, p, relID, tgtID, Velocity{1, 2})
	})

	found_p := false
	found_inst := false
	for _, e := range fires {
		if e == p {
			found_p = true
		}
		if e == inst {
			found_inst = true
		}
	}
	if !found_p {
		t.Fatal("pair OnSet must fire for prefab P")
	}
	if !found_inst {
		t.Fatal("pair OnSet must propagate to inheritor inst")
	}
}

// ── 11. Multi-term observer: filter evaluated per-inheritor ───────────────────

func TestPropagationMultiTermPerInheritor(t *testing.T) {
	w := flecs.New()
	var fires []flecs.ID

	// ObserveQuery: trigger on Position, filter requires Velocity.
	// Only inheritors that ALSO have Velocity should fire.
	_ = flecs.ObserveQuery(w, flecs.EventOnSet,
		[]flecs.Term{flecs.With(flecs.RegisterComponent[Position](w)), flecs.With(flecs.RegisterComponent[Velocity](w))},
		func(_ *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {
			fires = append(fires, e)
		},
	)

	var p, withVel, withoutVel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		withVel = fw.NewEntity()
		withoutVel = fw.NewEntity()
		flecs.AddID(fw, withVel, flecs.MakePair(w.IsA(), p))
		flecs.AddID(fw, withoutVel, flecs.MakePair(w.IsA(), p))
		// Give withVel its own Velocity (so it matches the Velocity filter).
		flecs.Set[Velocity](fw, withVel, Velocity{1, 0})
	})
	fires = nil

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{1, 2})
	})

	// p itself doesn't have Velocity, so it doesn't fire.
	// withVel has Velocity, so it fires.
	// withoutVel doesn't have Velocity, so it doesn't fire.
	found_withVel := false
	for _, e := range fires {
		if e == p {
			t.Fatal("P has no Velocity, multi-term observer must not fire for P")
		}
		if e == withoutVel {
			t.Fatal("withoutVel has no Velocity, must not fire")
		}
		if e == withVel {
			found_withVel = true
		}
	}
	if !found_withVel {
		t.Fatal("withVel has Velocity, multi-term observer must fire for it")
	}
}

// ── 12. Disabled observer: no propagation ────────────────────────────────────

func TestPropagationDisabledObserver(t *testing.T) {
	w := flecs.New()
	var fires int

	obs := flecs.Observe[Position](w, flecs.EventOnSet, func(_ *flecs.Writer, _ flecs.ID, _ Position) {
		fires++
	})
	obs.SetEnabled(false) // disable the observer

	var p, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), p))
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{1, 2})
	})

	_ = inst
	if fires != 0 {
		t.Fatalf("disabled observer must not fire (got %d fires including propagated)", fires)
	}
}

// ── 13. Performance: 1000 inheritors completes in linear time ─────────────────

func TestPropagationPerformance1000Inheritors(t *testing.T) {
	const N = 1000
	w := flecs.New()
	var count int

	_ = flecs.Observe[Position](w, flecs.EventOnSet, func(_ *flecs.Writer, _ flecs.ID, _ Position) {
		count++
	})

	var p flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		for i := 0; i < N; i++ {
			inst := fw.NewEntity()
			flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), p))
		}
	})

	start := time.Now()
	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{1, 2})
	})
	elapsed := time.Since(start)

	if count != N+1 {
		t.Fatalf("want %d fires, got %d", N+1, count)
	}
	// Linear time budget: 1000 inheritors should complete well within 500ms even
	// on a heavily loaded CI host. Quadratic would be ~1M operations and much slower.
	if elapsed > 500*time.Millisecond {
		t.Fatalf("propagation to %d inheritors took %v; expected linear O(N) not O(N²)", N, elapsed)
	}
}

// ── 14. Marshal round-trip: post-restore propagation works ────────────────────
//
// The test registers Position BEFORE creating any user entities in both worlds
// so that the component entity ID (48) is the same in both worlds and the
// restored entity IDs align with those captured in w.

func TestPropagationMarshalRoundTrip(t *testing.T) {
	w := flecs.New()
	// Register Position first so it lands at entity 48 in both worlds.
	posID := flecs.RegisterComponent[Position](w)

	var p, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()    // entity 49
		inst = fw.NewEntity() // entity 50
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), p))
		flecs.Set[Position](fw, p, Position{7, 8})
	})
	_ = posID

	data, err := w.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	w2 := flecs.New()
	// Register Position first (same ID as w) before unmarshal allocates user entities.
	flecs.RegisterComponent[Position](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	// Register the observer AFTER unmarshal so its registration does not
	// consume any entity IDs that would shift the restored p / inst IDs.
	var fires2 []flecs.ID
	_ = flecs.Observe[Position](w2, flecs.EventOnSet, func(_ *flecs.Writer, e flecs.ID, _ Position) {
		fires2 = append(fires2, e)
	})

	// p and inst have the same IDs in w2 (same sequential allocation order).
	w2.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{9, 10})
	})

	found_p2 := false
	found_inst2 := false
	for _, e := range fires2 {
		if e == p {
			found_p2 = true
		}
		if e == inst {
			found_inst2 = true
		}
	}
	if !found_p2 {
		t.Fatal("post-restore: Set(P) must fire for P")
	}
	if !found_inst2 {
		t.Fatal("post-restore: Set(P) must propagate to inst")
	}
}

// ── 15. Cache invalidation: adding/removing inheritors updates propagation ─────

func TestPropagationCacheInvalidation(t *testing.T) {
	w := flecs.New()
	var fires []flecs.ID

	_ = flecs.Observe[Position](w, flecs.EventOnSet, func(_ *flecs.Writer, e flecs.ID, _ Position) {
		fires = append(fires, e)
	})

	var p flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		flecs.Set[Position](fw, p, Position{1, 2})
	})
	fires = nil // reset after initial set

	// Add a new inheritor after the first Set (cache must be invalidated).
	var newInst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		newInst = fw.NewEntity()
		flecs.AddID(fw, newInst, flecs.MakePair(w.IsA(), p))
	})
	fires = nil

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{3, 4})
	})

	found_new := false
	for _, e := range fires {
		if e == newInst {
			found_new = true
		}
	}
	if !found_new {
		t.Fatal("newly added inheritor must receive propagated event after cache invalidation")
	}

	// Remove the inheritor and verify it no longer fires.
	w.Write(func(fw *flecs.Writer) {
		flecs.RemoveID(fw, newInst, flecs.MakePair(w.IsA(), p))
	})
	fires = nil

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{5, 6})
	})

	for _, e := range fires {
		if e == newInst {
			t.Fatal("removed inheritor must NOT receive propagated event")
		}
	}
}

// ── 16. Custom event propagates to inheritors ─────────────────────────────────

func TestPropagationCustomEvent(t *testing.T) {
	w := flecs.New()

	var evID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		evID = flecs.RegisterEvent(fw, "TestEvent")
	})

	var fires []flecs.ID
	_ = flecs.ObserveEvent(w, evID, func(_ *flecs.Writer, e flecs.ID, _ interface{}) {
		fires = append(fires, e)
	})

	var p, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), p))
	})
	fires = nil

	w.Write(func(fw *flecs.Writer) {
		flecs.Emit(fw, evID, p, nil)
	})

	found_p := false
	found_inst := false
	for _, e := range fires {
		if e == p {
			found_p = true
		}
		if e == inst {
			found_inst = true
		}
	}
	if !found_p {
		t.Fatal("custom Emit must fire for source P")
	}
	if !found_inst {
		t.Fatal("custom Emit must propagate to inheritor inst")
	}
}

// ── 17. TermNot filter in propagated dispatch ─────────────────────────────────

func TestPropagationMultiTermNotFilter(t *testing.T) {
	w := flecs.New()
	var fires []flecs.ID

	// ObserveQuery: trigger Position, filter NOT Velocity.
	// Inheritors WITHOUT Velocity should fire; inheritors WITH Velocity should not.
	_ = flecs.ObserveQuery(w, flecs.EventOnSet,
		[]flecs.Term{
			flecs.With(flecs.RegisterComponent[Position](w)),
			flecs.Without(flecs.RegisterComponent[Velocity](w)),
		},
		func(_ *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {
			fires = append(fires, e)
		},
	)

	var p, withVel, withoutVel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		withVel = fw.NewEntity()
		withoutVel = fw.NewEntity()
		flecs.AddID(fw, withVel, flecs.MakePair(w.IsA(), p))
		flecs.AddID(fw, withoutVel, flecs.MakePair(w.IsA(), p))
		flecs.Set[Velocity](fw, withVel, Velocity{1, 0})
	})
	fires = nil

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{1, 2})
	})

	// withoutVel has no Velocity → should fire.
	// withVel has Velocity → should NOT fire (TermNot blocks it).
	found_withoutVel := false
	for _, e := range fires {
		if e == withVel {
			t.Fatal("withVel has Velocity → TermNot must suppress it")
		}
		if e == withoutVel {
			found_withoutVel = true
		}
	}
	if !found_withoutVel {
		t.Fatal("withoutVel has no Velocity → TermNot must allow it")
	}
}

// ── 18. DontInherit suppresses OnReplace propagation ─────────────────────────

func TestPropagationOnReplaceDontInheritBlocks(t *testing.T) {
	w := flecs.New()

	posID := flecs.RegisterComponent[Position](w)
	flecs.SetInstantiatePolicy(w, posID, w.DontInherit())

	var replaceCount int
	flecs.OnReplace[Position](w, func(_ *flecs.Writer, _ flecs.ID, _, _ Position) {
		replaceCount++
	})

	var p, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), p))
		flecs.Set[Position](fw, p, Position{1, 2})
	})
	replaceCount = 0

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{3, 4})
	})

	// Only P fires OnReplace; inst is skipped due to DontInherit.
	if replaceCount != 1 {
		t.Fatalf("DontInherit: want 1 OnReplace (for P only), got %d", replaceCount)
	}
}

// ── 19. Override blocks OnReplace propagation ─────────────────────────────────

func TestPropagationOnReplaceOverrideBlocks(t *testing.T) {
	w := flecs.New()

	var records []flecs.ID
	flecs.OnReplace[Position](w, func(_ *flecs.Writer, e flecs.ID, _, _ Position) {
		records = append(records, e)
	})

	var p, overrider, inheritor flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		overrider = fw.NewEntity()
		inheritor = fw.NewEntity()
		flecs.AddID(fw, overrider, flecs.MakePair(w.IsA(), p))
		flecs.AddID(fw, inheritor, flecs.MakePair(w.IsA(), p))
		// overrider gets its own copy
		flecs.Set[Position](fw, overrider, Position{99, 99})
		flecs.Set[Position](fw, p, Position{1, 2}) // first set on P
	})
	records = nil

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{3, 4}) // triggers OnReplace
	})

	gotP := false
	gotInheritor := false
	gotOverrider := false
	for _, e := range records {
		switch e {
		case p:
			gotP = true
		case inheritor:
			gotInheritor = true
		case overrider:
			gotOverrider = true
		}
	}
	if !gotP {
		t.Fatal("OnReplace must fire for P")
	}
	if !gotInheritor {
		t.Fatal("OnReplace must propagate to inheritor (no local copy)")
	}
	if gotOverrider {
		t.Fatal("OnReplace must NOT propagate to overrider (has local copy)")
	}
}

// ── 20. Fixed-source propagated observer ──────────────────────────────────────

func TestPropagationFixedSourceObserver(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var p, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), p))
	})

	// Register a fixed-source observer for inst: fires only when the event's
	// entity is inst. In the propagated path, dispatchObserversForPropagation
	// looks up bucket.fixedSource[inh], so this covers that branch.
	var fixedFires int
	_ = flecs.ObserveIDWithOptions(w, posID, flecs.WithSource(inst),
		[]flecs.EventKind{flecs.EventOnSet},
		func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
			fixedFires++
		},
	)

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{1, 2})
	})

	// The fixed-source observer is for inst, but propagation dispatches with
	// entity=inst. The fixed-source bucket for inst should fire.
	if fixedFires != 1 {
		t.Fatalf("fixed-source observer for inst want 1 fire, got %d", fixedFires)
	}
}

// ── 21. OR group: table match fires; missing OR component suppresses ──────────

func TestPropagationOrGroupTableMatchAndFail(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var fires []flecs.ID
	// OR group [velID]: withVel satisfies via table; withoutVel has no Velocity → skipped.
	_ = flecs.ObserveQueryID(w, posID, flecs.EventOnSet,
		[]flecs.Term{flecs.Or(velID)},
		func(_ *flecs.Writer, e flecs.ID, _ unsafe.Pointer) { fires = append(fires, e) },
	)

	var p, withVel, withoutVel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		withVel = fw.NewEntity()
		withoutVel = fw.NewEntity()
		flecs.AddID(fw, withVel, flecs.MakePair(w.IsA(), p))
		flecs.AddID(fw, withoutVel, flecs.MakePair(w.IsA(), p))
		flecs.Set[Velocity](fw, withVel, Velocity{1, 0})
	})
	fires = nil

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{1, 2})
	})

	foundWithVel := false
	for _, e := range fires {
		if e == withVel {
			foundWithVel = true
		}
		if e == withoutVel {
			t.Fatal("withoutVel must not fire: Velocity missing, OR group fails")
		}
	}
	if !foundWithVel {
		t.Fatal("withVel must fire: Velocity present, OR group satisfied via table")
	}
}

// ── 22. Wildcard TermAnd and TermNot in propagated dispatch ───────────────────

func TestPropagationWildcardTerms(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var rel1, rel2, tgt flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel1 = fw.NewEntity()
		rel2 = fw.NewEntity()
		tgt = fw.NewEntity()
	})

	var fires []flecs.ID
	// Trigger: Position. Filter: With(MakePair(rel1, *)) AND NOT(MakePair(rel2, *)).
	_ = flecs.ObserveQuery(w, flecs.EventOnSet,
		[]flecs.Term{
			flecs.With(posID),
			flecs.With(flecs.MakePair(rel1, w.Wildcard())),
			flecs.Without(flecs.MakePair(rel2, w.Wildcard())),
		},
		func(_ *flecs.Writer, e flecs.ID, _ unsafe.Pointer) { fires = append(fires, e) },
	)

	// hasRel1: has (rel1,tgt) only → TermAnd wildcard matches, TermNot wildcard passes.
	// noRel1: missing (rel1,*) → TermAnd wildcard fails (return false, covers 88.88).
	// hasBoth: has (rel1,tgt) and (rel2,tgt) → TermNot wildcard fails (return false, covers 100.87).
	var p, hasRel1, noRel1, hasBoth flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		hasRel1 = fw.NewEntity()
		noRel1 = fw.NewEntity()
		hasBoth = fw.NewEntity()
		flecs.AddID(fw, hasRel1, flecs.MakePair(w.IsA(), p))
		flecs.AddID(fw, noRel1, flecs.MakePair(w.IsA(), p))
		flecs.AddID(fw, hasBoth, flecs.MakePair(w.IsA(), p))
		flecs.AddID(fw, hasRel1, flecs.MakePair(rel1, tgt))
		flecs.AddID(fw, hasBoth, flecs.MakePair(rel1, tgt))
		flecs.AddID(fw, hasBoth, flecs.MakePair(rel2, tgt))
	})
	fires = nil

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{1, 2})
	})

	foundHasRel1 := false
	for _, e := range fires {
		switch e {
		case hasRel1:
			foundHasRel1 = true
		case noRel1:
			t.Fatal("noRel1 must not fire: TermAnd wildcard fails (no rel1 pair)")
		case hasBoth:
			t.Fatal("hasBoth must not fire: TermNot wildcard fails (has rel2 pair)")
		}
	}
	if !foundHasRel1 {
		t.Fatal("hasRel1 must fire: TermAnd wildcard OK, TermNot wildcard passes")
	}
}

// ── 23. Disabled fixed-source observer is skipped in propagation ──────────────

func TestPropagationFixedSourceDisabledSkipped(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var p, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), p))
	})

	var fires int
	obs := flecs.ObserveIDWithOptions(w, posID, flecs.WithSource(inst),
		[]flecs.EventKind{flecs.EventOnSet},
		func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) { fires++ },
	)
	obs.SetEnabled(false)

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{1, 2})
	})

	if fires != 0 {
		t.Fatalf("disabled fixed-source observer must not fire: got %d", fires)
	}
}

// ── 24. Fixed-source multi-term observer: multiFilter mismatch skipped ─────────

func TestPropagationFixedSourceMultiFilterMismatch(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var p, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p = fw.NewEntity()
		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), p))
		// inst does NOT have Velocity
	})

	var fires int
	// Fixed-source observer for inst with Velocity filter: inst has no Velocity,
	// so entityMatchesTermsForPropagation returns false → node is skipped (line 66-67).
	_ = flecs.ObserveQueryWithOptions(w,
		flecs.WithSource(inst),
		[]flecs.EventKind{flecs.EventOnSet},
		[]flecs.Term{flecs.With(posID), flecs.With(velID)},
		func(_ *flecs.Writer, _ flecs.EventKind, _ flecs.ID, _ unsafe.Pointer) { fires++ },
	)

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, p, Position{1, 2})
	})

	if fires != 0 {
		t.Fatalf("fixed-source multiFilter mismatch must not fire: got %d", fires)
	}
}
