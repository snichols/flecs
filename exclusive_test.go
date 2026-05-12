package flecs_test

import (
	"testing"
	"unsafe"

	"github.com/snichols/flecs"
)

// Test 1: Default non-Exclusive relationship allows multiple targets.
func TestExclusive_DefaultAllowsMultipleTargets(t *testing.T) {
	w := flecs.New()
	var r, a, b, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		e = fw.NewEntity()
		fw.AddID(e, flecs.MakePair(r, a))
		fw.AddID(e, flecs.MakePair(r, b))
	})

	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(e, flecs.MakePair(r, a)) {
			t.Error("expected (R, A) to still be present on non-exclusive relationship")
		}
		if !fr.HasID(e, flecs.MakePair(r, b)) {
			t.Error("expected (R, B) to be present alongside (R, A) on non-exclusive relationship")
		}
	})
}

// Test 2: SetExclusive + add second target replaces first; hooks fire correctly.
// Uses two separate Write scopes so pairA is committed to the entity's table
// before pairB is added; this lets us verify OnRemove fires for pairA.
func TestExclusive_ReplaceOnAdd(t *testing.T) {
	w := flecs.New()
	var r, a, b, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetExclusive(w, r)

	removeCount := 0
	addCount := 0

	pairA := flecs.MakePair(r, a)
	pairB := flecs.MakePair(r, b)

	flecs.ObserveID(w, pairA, flecs.EventOnRemove, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
		removeCount++
	})
	flecs.ObserveID(w, pairB, flecs.EventOnAdd, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
		addCount++
	})

	// Scope 1: commit pairA to e's table.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, pairA)
	})
	// Scope 2: add pairB — exclusive enforcement replaces pairA.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, pairB)
	})

	w.Read(func(fr *flecs.Reader) {
		if fr.HasID(e, pairA) {
			t.Error("expected (R, A) to be removed after exclusive replace")
		}
		if !fr.HasID(e, pairB) {
			t.Error("expected (R, B) to be present after exclusive replace")
		}
	})
	if removeCount != 1 {
		t.Errorf("expected OnRemove to fire once for (R,A), got %d", removeCount)
	}
	if addCount != 1 {
		t.Errorf("expected OnAdd to fire once for (R,B), got %d", addCount)
	}
}

// Test 2b: Exclusive replace within a single deferred Write scope — the net
// result is correct (only the final target survives) even though pairA was
// never committed to the entity's table, so OnRemove for pairA does not fire
// (consistent with how any other component add/remove coalescing works).
func TestExclusive_ReplaceOnAddDeferred(t *testing.T) {
	w := flecs.New()
	var r, a, b, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetExclusive(w, r)

	pairA := flecs.MakePair(r, a)
	pairB := flecs.MakePair(r, b)

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, pairA)
		fw.AddID(e, pairB) // batched replace: pairA never hits the table
	})

	w.Read(func(fr *flecs.Reader) {
		if fr.HasID(e, pairA) {
			t.Error("expected (R, A) to not be present (replaced by (R, B) in batch)")
		}
		if !fr.HasID(e, pairB) {
			t.Error("expected (R, B) to be the sole target")
		}
	})
}

// Test 3: Re-adding the same target is a no-op (HasComponent guard).
func TestExclusive_ReAddSameTargetIsNoOp(t *testing.T) {
	w := flecs.New()
	var r, a, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetExclusive(w, r)

	pairA := flecs.MakePair(r, a)
	addCount := 0
	flecs.ObserveID(w, pairA, flecs.EventOnAdd, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
		addCount++
	})

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, pairA)
		fw.AddID(e, pairA) // second add: no-op, HasComponent guard returns false
	})

	if addCount != 1 {
		t.Errorf("expected OnAdd to fire exactly once, got %d", addCount)
	}
}

// Test 4: ChildOf is exclusive after bootstrap — adding a second parent replaces the first.
func TestExclusive_ChildOfIsExclusiveAfterBootstrap(t *testing.T) {
	w := flecs.New()
	var parent1, parent2, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent1 = fw.NewEntity()
		parent2 = fw.NewEntity()
		child = fw.NewEntity()
		fw.AddID(child, flecs.MakePair(w.ChildOf(), parent1))
		fw.AddID(child, flecs.MakePair(w.ChildOf(), parent2)) // replaces parent1
	})

	w.Read(func(fr *flecs.Reader) {
		if fr.HasID(child, flecs.MakePair(w.ChildOf(), parent1)) {
			t.Error("expected (ChildOf, parent1) to be replaced")
		}
		if !fr.HasID(child, flecs.MakePair(w.ChildOf(), parent2)) {
			t.Error("expected (ChildOf, parent2) to be the sole parent")
		}
		got, ok := w.ParentOf(child)
		if !ok || got != parent2 {
			t.Errorf("ParentOf(child) = %v, %v; want %v, true", got, ok, parent2)
		}
	})
}

// Test 5: IsA is NOT exclusive — multiple prefab bases are allowed.
func TestExclusive_IsANotExclusive(t *testing.T) {
	w := flecs.New()
	var p1, p2, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		p2 = fw.NewEntity()
		e = fw.NewEntity()
		fw.AddID(e, flecs.MakePair(w.IsA(), p1))
		fw.AddID(e, flecs.MakePair(w.IsA(), p2))
	})

	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(e, flecs.MakePair(w.IsA(), p1)) {
			t.Error("expected (IsA, p1) to still be present (IsA is not exclusive)")
		}
		if !fr.HasID(e, flecs.MakePair(w.IsA(), p2)) {
			t.Error("expected (IsA, p2) to be present alongside (IsA, p1)")
		}
	})
}

// Test 6: IsExclusive round-trip.
func TestExclusive_IsExclusiveRoundTrip(t *testing.T) {
	w := flecs.New()
	var r, s flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		s = fw.NewEntity()
	})

	if flecs.IsExclusive(w, r) {
		t.Error("expected IsExclusive(r) = false before SetExclusive")
	}
	flecs.SetExclusive(w, r)
	if !flecs.IsExclusive(w, r) {
		t.Error("expected IsExclusive(r) = true after SetExclusive")
	}
	if flecs.IsExclusive(w, s) {
		t.Error("expected IsExclusive(s) = false (not marked exclusive)")
	}

	// Built-in relationships are exclusive by bootstrap.
	if !flecs.IsExclusive(w, w.ChildOf()) {
		t.Error("expected ChildOf to be exclusive by default")
	}
	if flecs.IsExclusive(w, w.IsA()) {
		t.Error("expected IsA to NOT be exclusive")
	}
}

// Test 7: Exclusive + cleanup interaction — replacing a target does not delete
// the old target entity; cleanup only fires when the old target is deleted.
func TestExclusive_DoesNotDeleteOldTarget(t *testing.T) {
	w := flecs.New()
	var r, a, b, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetExclusive(w, r)
	flecs.SetCleanupPolicy(w, r, w.OnDeleteTarget(), w.DeleteAction())

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, flecs.MakePair(r, a))
		fw.AddID(e, flecs.MakePair(r, b)) // replaces (R, A); must NOT delete entity a
	})

	if !w.IsAlive(a) {
		t.Error("entity a should still be alive after exclusive pair replace (only deleted when a is explicitly deleted)")
	}
	if !w.IsAlive(b) {
		t.Error("entity b should be alive")
	}
	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(e, flecs.MakePair(r, b)) {
			t.Error("expected (R, B) to be on e")
		}
	})
}

// Test 8: fw.AddID(myRel, w.Exclusive()) sets the flag, parallel to pair-add form.
func TestExclusive_BareTagFormSetsFlag(t *testing.T) {
	w := flecs.New()
	var myRel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		myRel = fw.NewEntity()
		fw.AddID(myRel, w.Exclusive()) // bare-tag form
	})

	if !flecs.IsExclusive(w, myRel) {
		t.Error("expected IsExclusive(myRel) = true after fw.AddID(myRel, w.Exclusive())")
	}

	// Confirm the exclusive behavior actually works.
	var a, b, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a = fw.NewEntity()
		b = fw.NewEntity()
		e = fw.NewEntity()
		fw.AddID(e, flecs.MakePair(myRel, a))
		fw.AddID(e, flecs.MakePair(myRel, b))
	})

	w.Read(func(fr *flecs.Reader) {
		if fr.HasID(e, flecs.MakePair(myRel, a)) {
			t.Error("expected (myRel, A) to be replaced by exclusive enforce")
		}
		if !fr.HasID(e, flecs.MakePair(myRel, b)) {
			t.Error("expected (myRel, B) to be the current target")
		}
	})
}
