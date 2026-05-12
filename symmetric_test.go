package flecs_test

import (
	"testing"
	"unsafe"

	"github.com/snichols/flecs"
)

// Test 1: Default behavior unchanged — non-Symmetric relationship does not auto-mirror.
func TestSymmetric_DefaultNoMirror(t *testing.T) {
	w := flecs.New()
	var r, a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
	})

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, b))
	})

	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(a, flecs.MakePair(r, b)) {
			t.Error("expected (R, b) on a")
		}
		if fr.HasID(b, flecs.MakePair(r, a)) {
			t.Error("expected no mirror (R, a) on b for non-Symmetric relationship")
		}
	})
}

// Test 2: Mark + Add mirrors — AddID(a, MakePair(R, b)) causes HasID(b, MakePair(R, a)) == true.
func TestSymmetric_AddMirrors(t *testing.T) {
	w := flecs.New()
	var r, a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
	})
	flecs.SetSymmetric(w, r)

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, b))
	})

	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(a, flecs.MakePair(r, b)) {
			t.Error("expected (R, b) on a")
		}
		if !fr.HasID(b, flecs.MakePair(r, a)) {
			t.Error("expected mirrored (R, a) on b")
		}
	})
}

// Test 3: Idempotent — adding the same pair twice fires OnAdd exactly once per side.
func TestSymmetric_IdempotentAdd(t *testing.T) {
	w := flecs.New()
	var r, a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
	})
	flecs.SetSymmetric(w, r)

	pairRB := flecs.MakePair(r, b)
	pairRA := flecs.MakePair(r, a)

	addCountRB := 0
	addCountRA := 0
	flecs.ObserveID(w, pairRB, flecs.EventOnAdd, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
		addCountRB++
	})
	flecs.ObserveID(w, pairRA, flecs.EventOnAdd, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
		addCountRA++
	})

	// First add — mirrors to b.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, pairRB)
	})
	// Second add — already present on both sides; no-op.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, pairRB)
	})

	if addCountRB != 1 {
		t.Errorf("expected (R,b) OnAdd to fire once on a, got %d", addCountRB)
	}
	if addCountRA != 1 {
		t.Errorf("expected (R,a) OnAdd to fire once on b (mirror), got %d", addCountRA)
	}
}

// Test 4: Remove mirrors — RemoveID(a, MakePair(R, b)) causes HasID(b, MakePair(R, a)) == false.
func TestSymmetric_RemoveMirrors(t *testing.T) {
	w := flecs.New()
	var r, a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
	})
	flecs.SetSymmetric(w, r)

	// Establish the pair.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, b))
	})
	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(b, flecs.MakePair(r, a)) {
			t.Fatal("precondition: expected mirror (R, a) on b after add")
		}
	})

	// Remove from a — should also remove mirror from b.
	w.Write(func(fw *flecs.Writer) {
		fw.RemoveID(a, flecs.MakePair(r, b))
	})

	w.Read(func(fr *flecs.Reader) {
		if fr.HasID(a, flecs.MakePair(r, b)) {
			t.Error("expected (R, b) removed from a")
		}
		if fr.HasID(b, flecs.MakePair(r, a)) {
			t.Error("expected mirrored (R, a) removed from b")
		}
	})
}

// Test 5: Self-relationship — AddID(a, MakePair(R, a)) results in a single pair, not two.
func TestSymmetric_SelfRelationship(t *testing.T) {
	w := flecs.New()
	var r, a flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
	})
	flecs.SetSymmetric(w, r)

	addCount := 0
	pairSelf := flecs.MakePair(r, a)
	flecs.ObserveID(w, pairSelf, flecs.EventOnAdd, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
		addCount++
	})

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, pairSelf)
	})

	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(a, pairSelf) {
			t.Error("expected (R, a) on a after self-add")
		}
	})
	if addCount != 1 {
		t.Errorf("expected OnAdd to fire exactly once for self-pair, got %d", addCount)
	}

	// Remove cleans up.
	w.Write(func(fw *flecs.Writer) {
		fw.RemoveID(a, pairSelf)
	})
	w.Read(func(fr *flecs.Reader) {
		if fr.HasID(a, pairSelf) {
			t.Error("expected (R, a) removed from a after self-remove")
		}
	})
}

// Test 6: Symmetric + Exclusive (15.2 interaction) — pair replacement propagates through mirror.
func TestSymmetric_ExclusiveInteraction(t *testing.T) {
	w := flecs.New()
	var r, a, b, x flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		x = fw.NewEntity()
	})
	flecs.SetSymmetric(w, r)
	flecs.SetExclusive(w, r)

	// Give a an existing target x.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, x))
	})
	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(a, flecs.MakePair(r, x)) {
			t.Fatal("precondition: a should have (R, x)")
		}
		if !fr.HasID(x, flecs.MakePair(r, a)) {
			t.Fatal("precondition: x should have mirrored (R, a)")
		}
	})

	// Add (R, b) to a — exclusive replaces (R, x), then symmetric mirrors (R, a) onto b.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, b))
	})

	w.Read(func(fr *flecs.Reader) {
		if fr.HasID(a, flecs.MakePair(r, x)) {
			t.Error("expected (R, x) replaced on a by exclusive enforcement")
		}
		if !fr.HasID(a, flecs.MakePair(r, b)) {
			t.Error("expected (R, b) on a after exclusive replace")
		}
		if !fr.HasID(b, flecs.MakePair(r, a)) {
			t.Error("expected mirrored (R, a) on b after symmetric mirror")
		}
	})
}

// Test 7: IsSymmetric round-trip — SetSymmetric then IsSymmetric returns true; never-set returns false.
func TestSymmetric_IsSymmetricRoundTrip(t *testing.T) {
	w := flecs.New()
	var r, s flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		s = fw.NewEntity()
	})

	if flecs.IsSymmetric(w, r) {
		t.Error("expected IsSymmetric(r) = false before SetSymmetric")
	}
	flecs.SetSymmetric(w, r)
	if !flecs.IsSymmetric(w, r) {
		t.Error("expected IsSymmetric(r) = true after SetSymmetric")
	}
	if flecs.IsSymmetric(w, s) {
		t.Error("expected IsSymmetric(s) = false (not marked symmetric)")
	}

	// Bare-tag form also sets the flag.
	var t2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		t2 = fw.NewEntity()
		fw.AddID(t2, w.Symmetric())
	})
	if !flecs.IsSymmetric(w, t2) {
		t.Error("expected IsSymmetric(t2) = true after fw.AddID(t2, w.Symmetric())")
	}
}

// Test 8: Loop guard correctness — Add and Remove mirror without infinite recursion.
// Combined Add then Remove returns the world to baseline state.
func TestSymmetric_LoopGuardAndRoundTrip(t *testing.T) {
	w := flecs.New()
	var r, a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
	})
	flecs.SetSymmetric(w, r)

	// If loop guard is broken, Add would recurse infinitely and hang/stack-overflow.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, b))
	})
	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(a, flecs.MakePair(r, b)) {
			t.Error("expected (R, b) on a")
		}
		if !fr.HasID(b, flecs.MakePair(r, a)) {
			t.Error("expected (R, a) on b")
		}
	})

	// If loop guard is broken, Remove would recurse infinitely and hang/stack-overflow.
	w.Write(func(fw *flecs.Writer) {
		fw.RemoveID(a, flecs.MakePair(r, b))
	})
	w.Read(func(fr *flecs.Reader) {
		if fr.HasID(a, flecs.MakePair(r, b)) {
			t.Error("expected (R, b) removed from a")
		}
		if fr.HasID(b, flecs.MakePair(r, a)) {
			t.Error("expected (R, a) removed from b")
		}
	})
}

// Test 9a: Batched add — two commands for the same entity in one Write scope trigger
// batchForEntity, which must fire the symmetric mirror via the post-commitBatch loop.
func TestSymmetric_BatchedAdd(t *testing.T) {
	w := flecs.New()
	type Tag struct{}
	tagID := flecs.RegisterComponent[Tag](w)
	var r, a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
	})
	flecs.SetSymmetric(w, r)

	// Two commands for a → triggers batchForEntity (multi-cmd chain).
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, tagID)
		fw.AddID(a, flecs.MakePair(r, b))
	})

	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(a, flecs.MakePair(r, b)) {
			t.Error("expected (R, b) on a")
		}
		if !fr.HasID(b, flecs.MakePair(r, a)) {
			t.Error("expected mirrored (R, a) on b via batchForEntity path")
		}
	})
}

// Test 9aa: Batched remove — batchForEntity must fire the symmetric remove mirror.
func TestSymmetric_BatchedRemove(t *testing.T) {
	w := flecs.New()
	type Tag2 struct{}
	tagID := flecs.RegisterComponent[Tag2](w)
	var r, a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
	})
	flecs.SetSymmetric(w, r)

	// Establish both pair and tag on a.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, tagID)
		fw.AddID(a, flecs.MakePair(r, b))
	})

	// Remove with two commands → triggers batchForEntity remove path.
	w.Write(func(fw *flecs.Writer) {
		fw.RemoveID(a, flecs.MakePair(r, b))
		fw.RemoveID(a, tagID)
	})

	w.Read(func(fr *flecs.Reader) {
		if fr.HasID(a, flecs.MakePair(r, b)) {
			t.Error("expected (R, b) removed from a")
		}
		if fr.HasID(b, flecs.MakePair(r, a)) {
			t.Error("expected (R, a) removed from b via batchForEntity remove path")
		}
	})
}

// Test 9b: Exclusive mirror chain — when the symmetric mirror target already has a conflicting
// exclusive pair, the mirror replaces it. This covers the exclusive+symmetric branch inside
// addIDImmediate that fires when the mirror call itself triggers exclusive enforcement.
func TestSymmetric_ExclusiveMirrorReplacesTarget(t *testing.T) {
	w := flecs.New()
	var r, a, b, x, y flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		x = fw.NewEntity()
		y = fw.NewEntity()
	})
	flecs.SetSymmetric(w, r)
	flecs.SetExclusive(w, r)

	// Establish a→(R,x) and b→(R,y) with their symmetric mirrors.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, x))
	})
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(b, flecs.MakePair(r, y))
	})

	// Add (R, b) to a:
	//   exclusive on a: replaces (R, x) → (R, b) [via batchForEntity sig sim]
	//   symmetric mirror via addIDImmediate(b, (R, a)):
	//     b already has (R, y) and R is exclusive → addIDImmediate's exclusive branch fires,
	//     replacing (R, y) with (R, a) on b, then the symmetric guard runs addIDImmediate(a, (R, b)),
	//     which short-circuits because a already has (R, b).
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, b))
	})

	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(a, flecs.MakePair(r, b)) {
			t.Error("expected (R, b) on a")
		}
		if !fr.HasID(b, flecs.MakePair(r, a)) {
			t.Error("expected (R, a) on b after exclusive+symmetric mirror")
		}
		if fr.HasID(a, flecs.MakePair(r, x)) {
			t.Error("expected (R, x) gone from a")
		}
		if fr.HasID(b, flecs.MakePair(r, y)) {
			t.Error("expected (R, y) gone from b (replaced by exclusive mirror)")
		}
	})
}

// Test 9: OnAdd/OnRemove hooks fire on both sides — observers on b see the mirrored pair;
// no double-fire (hook count == 1 per side per operation).
func TestSymmetric_HooksFireOnBothSides(t *testing.T) {
	w := flecs.New()
	var r, a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
	})
	flecs.SetSymmetric(w, r)

	pairRB := flecs.MakePair(r, b) // added to a
	pairRA := flecs.MakePair(r, a) // mirrored onto b

	addRB, removeRB := 0, 0
	addRA, removeRA := 0, 0

	flecs.ObserveID(w, pairRB, flecs.EventOnAdd, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
		addRB++
	})
	flecs.ObserveID(w, pairRB, flecs.EventOnRemove, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
		removeRB++
	})
	flecs.ObserveID(w, pairRA, flecs.EventOnAdd, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
		addRA++
	})
	flecs.ObserveID(w, pairRA, flecs.EventOnRemove, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
		removeRA++
	})

	// Add (R, b) to a — should fire OnAdd for (R,b) once and OnAdd for (R,a) once.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, pairRB)
	})

	if addRB != 1 {
		t.Errorf("expected OnAdd(R,b) on a to fire once, got %d", addRB)
	}
	if addRA != 1 {
		t.Errorf("expected OnAdd(R,a) on b (mirror) to fire once, got %d", addRA)
	}

	// Remove (R, b) from a — should fire OnRemove for (R,b) once and OnRemove for (R,a) once.
	w.Write(func(fw *flecs.Writer) {
		fw.RemoveID(a, pairRB)
	})

	if removeRB != 1 {
		t.Errorf("expected OnRemove(R,b) on a to fire once, got %d", removeRB)
	}
	if removeRA != 1 {
		t.Errorf("expected OnRemove(R,a) on b (mirror) to fire once, got %d", removeRA)
	}
}
