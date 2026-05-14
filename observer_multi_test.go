package flecs_test

import (
	"io"
	"log/slog"
	"testing"
	"time"
	"unsafe"

	"github.com/snichols/flecs"
)

// ── 1. Basic And filter: fires when trigger present AND filter component present ──

func TestObserveQueryAndFilter(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var fired []flecs.ID
	_ = flecs.ObserveQuery(w, flecs.EventOnSet, []flecs.Term{
		flecs.With(posID),
		flecs.With(velID),
	}, func(fw *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {
		fired = append(fired, e)
	})

	var withVel, withoutVel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		withVel = fw.NewEntity()
		flecs.AddID(fw, withVel, velID)
		flecs.Set[Position](fw, withVel, Position{1, 2}) // should fire

		withoutVel = fw.NewEntity()
		flecs.Set[Position](fw, withoutVel, Position{3, 4}) // should NOT fire
	})

	if len(fired) != 1 || fired[0] != withVel {
		t.Fatalf("want [%v], got %v", withVel, fired)
	}
}

// ── 2. Without (TermNot) filter: does not fire when excluded component present ──

func TestObserveQueryNotFilter(t *testing.T) {
	type Frozen struct{}
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	frozenID := flecs.RegisterComponent[Frozen](w)

	var fired []flecs.ID
	_ = flecs.ObserveQuery(w, flecs.EventOnSet, []flecs.Term{
		flecs.With(posID),
		flecs.Without(frozenID),
	}, func(fw *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {
		fired = append(fired, e)
	})

	var unfrozen, frozen flecs.ID
	w.Write(func(fw *flecs.Writer) {
		unfrozen = fw.NewEntity()
		flecs.Set[Position](fw, unfrozen, Position{1, 1}) // fires: no Frozen

		frozen = fw.NewEntity()
		flecs.AddID(fw, frozen, frozenID)
		flecs.Set[Position](fw, frozen, Position{2, 2}) // does NOT fire: has Frozen
	})

	if len(fired) != 1 || fired[0] != unfrozen {
		t.Fatalf("want [%v], got %v", unfrozen, fired)
	}
}

// ── 3. Or filter: fires when entity has at least one of the Or terms ──

func TestObserveQueryOrFilter(t *testing.T) {
	type Acceleration struct{ AX, AY float32 }
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)
	accID := flecs.RegisterComponent[Acceleration](w)

	var fired []flecs.ID
	_ = flecs.ObserveQuery(w, flecs.EventOnSet, []flecs.Term{
		flecs.With(posID),
		flecs.Or(velID),
		flecs.Or(accID),
	}, func(fw *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {
		fired = append(fired, e)
	})

	var hasVel, hasAcc, hasNeither flecs.ID
	w.Write(func(fw *flecs.Writer) {
		hasVel = fw.NewEntity()
		flecs.AddID(fw, hasVel, velID)
		flecs.Set[Position](fw, hasVel, Position{}) // fires: has Velocity

		hasAcc = fw.NewEntity()
		flecs.AddID(fw, hasAcc, accID)
		flecs.Set[Position](fw, hasAcc, Position{}) // fires: has Acceleration

		hasNeither = fw.NewEntity()
		flecs.Set[Position](fw, hasNeither, Position{}) // does NOT fire
	})

	if len(fired) != 2 {
		t.Fatalf("want 2 firings, got %d: %v", len(fired), fired)
	}
	firedSet := map[flecs.ID]bool{fired[0]: true, fired[1]: true}
	if !firedSet[hasVel] || !firedSet[hasAcc] {
		t.Fatalf("want hasVel and hasAcc, got %v", fired)
	}
	if firedSet[hasNeither] {
		t.Fatal("hasNeither should not fire")
	}
}

// ── 4. Pair filter: fires for entities with a specific pair relationship ──

func TestObserveQueryPairFilter(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	childOfPair := flecs.MakePair(w.ChildOf(), w.Wildcard())

	var fired []flecs.ID
	_ = flecs.ObserveQuery(w, flecs.EventOnSet, []flecs.Term{
		flecs.With(posID),
		flecs.With(childOfPair),
	}, func(fw *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {
		fired = append(fired, e)
	})

	var withParent, withoutParent flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent := fw.NewEntity()

		withParent = fw.NewEntity()
		fw.AddID(withParent, flecs.MakePair(w.ChildOf(), parent))
		flecs.Set[Position](fw, withParent, Position{1, 1}) // fires: has ChildOf

		withoutParent = fw.NewEntity()
		flecs.Set[Position](fw, withoutParent, Position{2, 2}) // does NOT fire
	})

	if len(fired) != 1 || fired[0] != withParent {
		t.Fatalf("want [%v], got %v", withParent, fired)
	}
}

// ── 5. WithYieldExisting + multi-term: sweeps existing matches ──

func TestObserveQueryYieldExisting(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var preExist, noVel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		// Entity that matches: has Position AND Velocity
		preExist = fw.NewEntity()
		flecs.AddID(fw, preExist, velID)
		flecs.Set[Position](fw, preExist, Position{1, 2})

		// Entity that does NOT match: has Position only
		noVel = fw.NewEntity()
		flecs.Set[Position](fw, noVel, Position{3, 4})
	})

	var yielded []flecs.ID
	_ = flecs.ObserveQueryWithOptions(w, flecs.WithYieldExisting(), []flecs.EventKind{flecs.EventOnSet}, []flecs.Term{
		flecs.With(posID),
		flecs.With(velID),
	}, func(fw *flecs.Writer, _ flecs.EventKind, e flecs.ID, _ unsafe.Pointer) {
		yielded = append(yielded, e)
	})

	if len(yielded) != 1 || yielded[0] != preExist {
		t.Fatalf("want [%v], got %v", preExist, yielded)
	}
}

// ── 6. SetEnabled(false): no fire while disabled; re-enabling resumes ──

func TestObserveQuerySetEnabled(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	fired := 0
	obs := flecs.ObserveQuery(w, flecs.EventOnSet, []flecs.Term{
		flecs.With(posID),
		flecs.With(velID),
	}, func(fw *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {
		fired++
	})

	obs.SetEnabled(false)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.AddID(fw, e, velID)
		flecs.Set[Position](fw, e, Position{}) // disabled: should NOT fire
	})
	if fired != 0 {
		t.Fatalf("want 0 fires while disabled, got %d", fired)
	}

	obs.SetEnabled(true)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.AddID(fw, e, velID)
		flecs.Set[Position](fw, e, Position{}) // re-enabled: should fire
	})
	if fired != 1 {
		t.Fatalf("want 1 fire after re-enable, got %d", fired)
	}
}

// ── 7. WithSource + multi-term: fires only for the named entity AND filter passes ──

func TestObserveQueryWithSource(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var target, other flecs.ID
	w.Write(func(fw *flecs.Writer) {
		target = fw.NewEntity()
		flecs.AddID(fw, target, velID)
		other = fw.NewEntity()
		flecs.AddID(fw, other, velID)
	})

	fired := 0
	_ = flecs.ObserveQueryWithOptions(w, flecs.WithSource(target), []flecs.EventKind{flecs.EventOnSet}, []flecs.Term{
		flecs.With(posID),
		flecs.With(velID),
	}, func(fw *flecs.Writer, _ flecs.EventKind, e flecs.ID, _ unsafe.Pointer) {
		fired++
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, other, Position{1, 1})  // wrong entity → no fire
		flecs.Set[Position](fw, target, Position{2, 2}) // correct entity + filter passes → fire
	})

	if fired != 1 {
		t.Fatalf("want 1 fire, got %d", fired)
	}

	// Remove Velocity from target; filter no longer passes
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[Velocity](fw, target)
		flecs.Set[Position](fw, target, Position{3, 3}) // filter fails now → no fire
	})
	if fired != 1 {
		t.Fatalf("want still 1 fire after filter fails, got %d", fired)
	}
}

// ── 8. ObserveQueryEvents [OnAdd, OnSet]: both events fire for qualifying entities ──

func TestObserveQueryEvents(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	type entry struct {
		ev flecs.EventKind
		e  flecs.ID
	}
	var log []entry
	_ = flecs.ObserveQueryEvents(w, []flecs.EventKind{flecs.EventOnAdd, flecs.EventOnSet}, []flecs.Term{
		flecs.With(posID),
		flecs.With(velID),
	}, func(fw *flecs.Writer, ev flecs.EventKind, e flecs.ID, _ unsafe.Pointer) {
		log = append(log, entry{ev, e})
	})

	var qualifying, nonQualifying flecs.ID
	w.Write(func(fw *flecs.Writer) {
		qualifying = fw.NewEntity()
		flecs.AddID(fw, qualifying, velID)
		flecs.Set[Position](fw, qualifying, Position{1, 1}) // OnAdd + OnSet → 2 fires (filter passes)

		nonQualifying = fw.NewEntity()
		flecs.Set[Position](fw, nonQualifying, Position{2, 2}) // filter fails → 0 fires
	})

	addCount, setCount := 0, 0
	for _, entry := range log {
		if entry.e == nonQualifying {
			t.Fatalf("nonQualifying entity should never fire")
		}
		switch entry.ev {
		case flecs.EventOnAdd:
			addCount++
		case flecs.EventOnSet:
			setCount++
		}
	}
	if addCount != 1 {
		t.Fatalf("want 1 OnAdd, got %d", addCount)
	}
	if setCount != 1 {
		t.Fatalf("want 1 OnSet, got %d", setCount)
	}
}

// ── 9. Sparse component as trigger ──

func TestObserveQuerySparseTrigger(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)
	// Make Velocity a sparse (DontFragment) component so it doesn't appear in the archetype.
	flecs.SetDontFragment(w, velID)

	fired := 0
	_ = flecs.ObserveQuery(w, flecs.EventOnSet, []flecs.Term{
		flecs.With(posID),
	}, func(fw *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {
		fired++
	})

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set[Position](fw, e, Position{}) // trigger fires regardless of sparse Velocity
	})
	if fired != 1 {
		t.Fatalf("want 1 fire, got %d", fired)
	}
}

// ── 10. DontFragment component as filter ──

func TestObserveQueryDontFragmentFilter(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)
	flecs.SetDontFragment(w, velID) // Velocity is DontFragment: not in archetype

	var fired []flecs.ID
	_ = flecs.ObserveQuery(w, flecs.EventOnSet, []flecs.Term{
		flecs.With(posID),
		flecs.With(velID),
	}, func(fw *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {
		fired = append(fired, e)
	})

	var hasVelDF, noVelDF flecs.ID
	w.Write(func(fw *flecs.Writer) {
		hasVelDF = fw.NewEntity()
		flecs.Set[Velocity](fw, hasVelDF, Velocity{1, 2}) // add DontFragment Velocity
		flecs.Set[Position](fw, hasVelDF, Position{})     // should fire: has sparse Velocity

		noVelDF = fw.NewEntity()
		flecs.Set[Position](fw, noVelDF, Position{}) // should NOT fire: no sparse Velocity
	})

	if len(fired) != 1 || fired[0] != hasVelDF {
		t.Fatalf("want [%v], got %v", hasVelDF, fired)
	}
}

// ── 11. Performance: 100 multi-term observers on same trigger, O(N) dispatch ──

func TestObserveQueryPerformance100Observers(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	const N = 100
	counts := make([]int, N)
	for i := range counts {
		i := i
		_ = flecs.ObserveQuery(w, flecs.EventOnSet, []flecs.Term{
			flecs.With(posID),
			flecs.With(velID),
		}, func(fw *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {
			counts[i]++
		})
	}

	const entities = 500
	start := time.Now()
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < entities; i++ {
			e := fw.NewEntity()
			flecs.AddID(fw, e, velID)
			flecs.Set[Position](fw, e, Position{float32(i), float32(i)})
		}
	})
	elapsed := time.Since(start)

	// Each observer should have fired once per entity.
	for i, c := range counts {
		if c != entities {
			t.Fatalf("observer %d: want %d fires, got %d", i, entities, c)
		}
	}

	// Wall-clock budget: 500 entities × 100 observers × filter eval should complete
	// well within 2 seconds. A quadratic blow-up would take tens of seconds.
	if elapsed > 2*time.Second {
		t.Fatalf("performance regression: 100 observers × %d entities took %v (budget: 2s)", entities, elapsed)
	}
}

// ── 12. Re-entry: handler mutates world via deferred write; no infinite loop ──

func TestObserveQueryReEntry(t *testing.T) {
	type Added struct{}
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)
	addedID := flecs.RegisterComponent[Added](w)

	fired := 0
	_ = flecs.ObserveQuery(w, flecs.EventOnSet, []flecs.Term{
		flecs.With(posID),
		flecs.With(velID),
	}, func(fw *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {
		fired++
		// Mutate the world in the callback — should be deferred, not recursive.
		fw.AddID(e, addedID)
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.AddID(fw, e, velID)
		flecs.Set[Position](fw, e, Position{1, 1})
	})

	if fired != 1 {
		t.Fatalf("want exactly 1 fire, got %d (re-entry detected)", fired)
	}
	// The Added component should have been applied by the deferred queue.
	w.Read(func(r *flecs.Reader) {
		if !flecs.Has[Added](r, e) {
			t.Fatal("deferred Add[Added] was not applied")
		}
	})
}

// ── 13. ObserveQueryID: explicit trigger ID with separate filter terms ──

func TestObserveQueryID(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var fired []flecs.ID
	_ = flecs.ObserveQueryID(w, posID, flecs.EventOnSet, []flecs.Term{
		flecs.With(velID), // filter only: velID must be present at fire time
	}, func(fw *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {
		fired = append(fired, e)
	})

	var matching, notMatching flecs.ID
	w.Write(func(fw *flecs.Writer) {
		matching = fw.NewEntity()
		flecs.AddID(fw, matching, velID)
		flecs.Set[Position](fw, matching, Position{}) // fires: filter passes

		notMatching = fw.NewEntity()
		flecs.Set[Position](fw, notMatching, Position{}) // does NOT fire
	})

	if len(fired) != 1 || fired[0] != matching {
		t.Fatalf("want [%v], got %v", matching, fired)
	}
}

// ── 14. YieldExisting + WithSource + multi-term ──

func TestObserveQueryYieldExistingWithSource(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var source flecs.ID
	w.Write(func(fw *flecs.Writer) {
		source = fw.NewEntity()
		flecs.AddID(fw, source, velID)
		flecs.Set[Position](fw, source, Position{5, 5})
	})

	var yielded []flecs.ID
	_ = flecs.ObserveQueryWithOptions(w,
		flecs.WithYieldExisting().AndSource(source),
		[]flecs.EventKind{flecs.EventOnSet},
		[]flecs.Term{
			flecs.With(posID),
			flecs.With(velID),
		},
		func(fw *flecs.Writer, _ flecs.EventKind, e flecs.ID, _ unsafe.Pointer) {
			yielded = append(yielded, e)
		},
	)

	if len(yielded) != 1 || yielded[0] != source {
		t.Fatalf("want [%v], got %v", source, yielded)
	}
}

// ── 15. YieldExisting + multi-term: filter excludes non-matching pre-existing entities ──

func TestObserveQueryYieldExistingFilter(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var match, noMatch flecs.ID
	w.Write(func(fw *flecs.Writer) {
		match = fw.NewEntity()
		flecs.AddID(fw, match, velID)
		flecs.Set[Position](fw, match, Position{})

		noMatch = fw.NewEntity()
		flecs.Set[Position](fw, noMatch, Position{})
	})

	var yielded []flecs.ID
	_ = flecs.ObserveQueryWithOptions(w, flecs.WithYieldExisting(), []flecs.EventKind{flecs.EventOnAdd, flecs.EventOnSet}, []flecs.Term{
		flecs.With(posID),
		flecs.With(velID),
	}, func(fw *flecs.Writer, _ flecs.EventKind, e flecs.ID, _ unsafe.Pointer) {
		yielded = append(yielded, e)
	})

	// OnAdd + OnSet both yielded for match (2 entries), noMatch yielded for neither
	for _, y := range yielded {
		if y == noMatch {
			t.Fatal("noMatch entity should not be yielded")
		}
	}
	if len(yielded) < 1 {
		t.Fatal("match entity should be yielded at least once")
	}
}

// ── 21. TermNot wildcard pair as filter: Without(MakePair(R, Wildcard)) ──

func TestObserveQueryNotWildcardFilter(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	childOfWild := flecs.MakePair(w.ChildOf(), w.Wildcard())

	var fired []flecs.ID
	_ = flecs.ObserveQuery(w, flecs.EventOnSet, []flecs.Term{
		flecs.With(posID),
		flecs.Without(childOfWild), // must NOT have any ChildOf pair
	}, func(fw *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {
		fired = append(fired, e)
	})

	var noParent, hasParent flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent := fw.NewEntity()

		noParent = fw.NewEntity()
		flecs.Set[Position](fw, noParent, Position{}) // fires: no ChildOf

		hasParent = fw.NewEntity()
		fw.AddID(hasParent, flecs.MakePair(w.ChildOf(), parent))
		flecs.Set[Position](fw, hasParent, Position{}) // does NOT fire: has ChildOf
	})

	if len(fired) != 1 || fired[0] != noParent {
		t.Fatalf("want [%v], got %v", noParent, fired)
	}
}

// ── 22. YieldExisting with OR group in filter ──

func TestObserveQueryYieldExistingOrFilter(t *testing.T) {
	type Heavy struct{}
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)
	heavyID := flecs.RegisterComponent[Heavy](w)

	var hasVel, hasHeavy, hasNeither flecs.ID
	w.Write(func(fw *flecs.Writer) {
		hasVel = fw.NewEntity()
		flecs.AddID(fw, hasVel, velID)
		flecs.Set[Position](fw, hasVel, Position{})

		hasHeavy = fw.NewEntity()
		flecs.AddID(fw, hasHeavy, heavyID)
		flecs.Set[Position](fw, hasHeavy, Position{})

		hasNeither = fw.NewEntity()
		flecs.Set[Position](fw, hasNeither, Position{})
	})

	var yielded []flecs.ID
	_ = flecs.ObserveQueryWithOptions(w, flecs.WithYieldExisting(), []flecs.EventKind{flecs.EventOnSet}, []flecs.Term{
		flecs.With(posID),
		flecs.Or(velID),
		flecs.Or(heavyID),
	}, func(fw *flecs.Writer, _ flecs.EventKind, e flecs.ID, _ unsafe.Pointer) {
		yielded = append(yielded, e)
	})

	yieldedSet := make(map[flecs.ID]bool)
	for _, y := range yielded {
		yieldedSet[y] = true
	}
	if !yieldedSet[hasVel] {
		t.Errorf("hasVel should be yielded")
	}
	if !yieldedSet[hasHeavy] {
		t.Errorf("hasHeavy should be yielded")
	}
	if yieldedSet[hasNeither] {
		t.Errorf("hasNeither should not be yielded")
	}
}

// ── 23. YieldExisting with TermNot wildcard in filter ──

func TestObserveQueryYieldExistingNotWildcardFilter(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	childOfWild := flecs.MakePair(w.ChildOf(), w.Wildcard())

	var noParent, hasParent flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent := fw.NewEntity()

		noParent = fw.NewEntity()
		flecs.Set[Position](fw, noParent, Position{1, 1})

		hasParent = fw.NewEntity()
		fw.AddID(hasParent, flecs.MakePair(w.ChildOf(), parent))
		flecs.Set[Position](fw, hasParent, Position{2, 2})
	})

	var yielded []flecs.ID
	_ = flecs.ObserveQueryWithOptions(w, flecs.WithYieldExisting(), []flecs.EventKind{flecs.EventOnSet}, []flecs.Term{
		flecs.With(posID),
		flecs.Without(childOfWild),
	}, func(fw *flecs.Writer, _ flecs.EventKind, e flecs.ID, _ unsafe.Pointer) {
		yielded = append(yielded, e)
	})

	if len(yielded) != 1 || yielded[0] != noParent {
		t.Fatalf("want [%v], got %v", noParent, yielded)
	}
}

// ── 24. Logger path: world with slog logger registers correctly ──

func TestObserveQueryWithLogger(t *testing.T) {
	w := flecs.New()
	w.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	fired := 0
	_ = flecs.ObserveQueryEvents(w, []flecs.EventKind{flecs.EventOnSet}, []flecs.Term{
		flecs.With(posID),
		flecs.With(velID),
	}, func(fw *flecs.Writer, _ flecs.EventKind, e flecs.ID, _ unsafe.Pointer) {
		fired++
	})

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.AddID(fw, e, velID)
		flecs.Set[Position](fw, e, Position{1, 1})
	})
	if fired != 1 {
		t.Fatalf("want 1 fire, got %d", fired)
	}
}

// ── Panic: ObserveQueryID with zero triggerID ──

func TestObserveQueryIDPanicZeroID(t *testing.T) {
	w := flecs.New()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for zero triggerID")
		}
	}()
	_ = flecs.ObserveQueryID(w, 0, flecs.EventOnSet, nil, func(fw *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {})
}

// ── Panic: empty terms ──

func TestObserveQueryPanicEmptyTerms(t *testing.T) {
	w := flecs.New()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty terms")
		}
	}()
	_ = flecs.ObserveQuery(w, flecs.EventOnSet, nil, func(fw *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {})
}

// ── Panic: first term is not TermAnd ──

func TestObserveQueryPanicFirstTermNotAnd(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for non-TermAnd first term")
		}
	}()
	_ = flecs.ObserveQuery(w, flecs.EventOnSet, []flecs.Term{
		flecs.Without(posID),
	}, func(fw *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {})
}

// ── 16. YieldExisting with TermNot filter: table excluded by Not term ──

func TestObserveQueryYieldExistingNotFilter(t *testing.T) {
	type Frozen struct{}
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	frozenID := flecs.RegisterComponent[Frozen](w)

	var match, frozen flecs.ID
	w.Write(func(fw *flecs.Writer) {
		match = fw.NewEntity()
		flecs.Set[Position](fw, match, Position{1, 1}) // matches: no Frozen

		frozen = fw.NewEntity()
		flecs.AddID(fw, frozen, frozenID)
		flecs.Set[Position](fw, frozen, Position{2, 2}) // excluded: has Frozen
	})

	var yielded []flecs.ID
	_ = flecs.ObserveQueryWithOptions(w, flecs.WithYieldExisting(), []flecs.EventKind{flecs.EventOnSet}, []flecs.Term{
		flecs.With(posID),
		flecs.Without(frozenID),
	}, func(fw *flecs.Writer, _ flecs.EventKind, e flecs.ID, _ unsafe.Pointer) {
		yielded = append(yielded, e)
	})

	for _, y := range yielded {
		if y == frozen {
			t.Fatal("frozen entity should be excluded from yield_existing")
		}
	}
	if len(yielded) < 1 {
		t.Fatal("match entity should be yielded")
	}
}

// ── 17. YieldExisting with DontFragment filter in sparse mode ──

func TestObserveQueryYieldExistingDontFragmentFilter(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)
	flecs.SetDontFragment(w, velID)

	var withVel, withoutVel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		withVel = fw.NewEntity()
		flecs.Set[Velocity](fw, withVel, Velocity{1, 0})
		flecs.Set[Position](fw, withVel, Position{})

		withoutVel = fw.NewEntity()
		flecs.Set[Position](fw, withoutVel, Position{})
	})

	var yielded []flecs.ID
	_ = flecs.ObserveQueryWithOptions(w, flecs.WithYieldExisting(), []flecs.EventKind{flecs.EventOnSet}, []flecs.Term{
		flecs.With(posID),
		flecs.With(velID),
	}, func(fw *flecs.Writer, _ flecs.EventKind, e flecs.ID, _ unsafe.Pointer) {
		yielded = append(yielded, e)
	})

	if len(yielded) != 1 || yielded[0] != withVel {
		t.Fatalf("want [%v], got %v", withVel, yielded)
	}
}

// ── 18. ObserveQueryID with no filter terms (single-trigger behavior) ──

func TestObserveQueryIDNoFilterTerms(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	fired := 0
	_ = flecs.ObserveQueryID(w, posID, flecs.EventOnSet, nil, func(fw *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {
		fired++
	})

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set[Position](fw, e, Position{1, 2})
	})
	if fired != 1 {
		t.Fatalf("want 1 fire, got %d", fired)
	}
}

// ── 19. YieldExisting: disabled/prefab entities are skipped ──

func TestObserveQueryYieldExistingSkipsDisabledPrefab(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var normal, disabled flecs.ID
	w.Write(func(fw *flecs.Writer) {
		normal = fw.NewEntity()
		flecs.AddID(fw, normal, velID)
		flecs.Set[Position](fw, normal, Position{1, 1})

		disabled = fw.NewEntity()
		flecs.AddID(fw, disabled, velID)
		flecs.Set[Position](fw, disabled, Position{2, 2})
		fw.AddID(disabled, w.Disabled()) // mark as disabled
	})

	var yielded []flecs.ID
	_ = flecs.ObserveQueryWithOptions(w, flecs.WithYieldExisting(), []flecs.EventKind{flecs.EventOnSet}, []flecs.Term{
		flecs.With(posID),
		flecs.With(velID),
	}, func(fw *flecs.Writer, _ flecs.EventKind, e flecs.ID, _ unsafe.Pointer) {
		yielded = append(yielded, e)
	})

	if len(yielded) != 1 || yielded[0] != normal {
		t.Fatalf("want only normal entity, got %v", yielded)
	}
}

// ── 20. TermNot filter evaluation (entity matching) ──

func TestObserveQueryTermNotEvaluation(t *testing.T) {
	type Frozen struct{}
	type Heavy struct{}
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	frozenID := flecs.RegisterComponent[Frozen](w)
	heavyID := flecs.RegisterComponent[Heavy](w)

	var fired []flecs.ID
	_ = flecs.ObserveQuery(w, flecs.EventOnSet, []flecs.Term{
		flecs.With(posID),
		flecs.Without(frozenID),
		flecs.Without(heavyID),
	}, func(fw *flecs.Writer, e flecs.ID, _ unsafe.Pointer) {
		fired = append(fired, e)
	})

	var ok1, hasFrozen, hasHeavy flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ok1 = fw.NewEntity()
		flecs.Set[Position](fw, ok1, Position{}) // fires: neither Frozen nor Heavy

		hasFrozen = fw.NewEntity()
		flecs.AddID(fw, hasFrozen, frozenID)
		flecs.Set[Position](fw, hasFrozen, Position{}) // does NOT fire: has Frozen

		hasHeavy = fw.NewEntity()
		flecs.AddID(fw, hasHeavy, heavyID)
		flecs.Set[Position](fw, hasHeavy, Position{}) // does NOT fire: has Heavy
	})

	if len(fired) != 1 || fired[0] != ok1 {
		t.Fatalf("want [%v], got %v", ok1, fired)
	}
}

// ── Panic: yieldExisting + OnRemove only ──

func TestObserveQueryPanicYieldRemoveOnly(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for yieldExisting + OnRemove only")
		}
	}()
	_ = flecs.ObserveQueryWithOptions(w, flecs.WithYieldExisting(), []flecs.EventKind{flecs.EventOnRemove}, []flecs.Term{
		flecs.With(posID),
		flecs.With(velID),
	}, func(fw *flecs.Writer, _ flecs.EventKind, e flecs.ID, _ unsafe.Pointer) {})
}
