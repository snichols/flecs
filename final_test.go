package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// Test 1: Default behavior unchanged — entity not marked Final accepts (IsA, target) adds.
func TestFinal_DefaultAllowsIsA(t *testing.T) {
	w := flecs.New()
	var base, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		base = fw.NewEntity()
		child = fw.NewEntity()
		fw.AddID(child, flecs.MakePair(w.IsA(), base))
	})
	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(child, flecs.MakePair(w.IsA(), base)) {
			t.Error("expected (IsA, base) to be present on child when base is not Final")
		}
	})
}

// Test 2: Immediate path panics — SetFinal(w, base); AddID(child, MakePair(IsA, base)) panics.
func TestFinal_ImmediatePathPanics(t *testing.T) {
	w := flecs.New()
	var base, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		base = fw.NewEntity()
		child = fw.NewEntity()
	})
	flecs.SetFinal(w, base)

	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic when adding (IsA, Final-entity) on immediate path")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(child, flecs.MakePair(w.IsA(), base))
	})
}

// Test 3: Non-IsA pairs to a Final entity are fine — Final only gates IsA targets.
func TestFinal_NonIsAPairAllowed(t *testing.T) {
	w := flecs.New()
	var base, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		base = fw.NewEntity()
		child = fw.NewEntity()
	})
	flecs.SetFinal(w, base)

	// Adding (ChildOf, base) must not panic — Final only blocks IsA.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(child, flecs.MakePair(w.ChildOf(), base))
	})
	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(child, flecs.MakePair(w.ChildOf(), base)) {
			t.Error("expected (ChildOf, base) to be present on child — Final does not block ChildOf")
		}
	})
}

// Test 4: Non-Final target is fine — negative control for IsA enforcement.
func TestFinal_NonFinalTargetAllowed(t *testing.T) {
	w := flecs.New()
	var nonFinalBase, final_, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		nonFinalBase = fw.NewEntity()
		final_ = fw.NewEntity()
		child = fw.NewEntity()
	})
	flecs.SetFinal(w, final_)

	// Must not panic: nonFinalBase is not marked Final.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(child, flecs.MakePair(w.IsA(), nonFinalBase))
	})
	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(child, flecs.MakePair(w.IsA(), nonFinalBase)) {
			t.Error("expected (IsA, nonFinalBase) to be present — nonFinalBase is not Final")
		}
	})
}

// Test 5: IsFinal round-trip — SetFinal then IsFinal returns true; works via *Reader and *Writer.
func TestFinal_IsFinalRoundTrip(t *testing.T) {
	w := flecs.New()
	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
	})

	// Before SetFinal.
	w.Read(func(fr *flecs.Reader) {
		if flecs.IsFinal(fr, e1) {
			t.Error("expected IsFinal(e1) = false before SetFinal")
		}
	})

	flecs.SetFinal(w, e1)

	// Via *Reader.
	w.Read(func(fr *flecs.Reader) {
		if !flecs.IsFinal(fr, e1) {
			t.Error("expected IsFinal(e1) = true via *Reader after SetFinal")
		}
		if flecs.IsFinal(fr, e2) {
			t.Error("expected IsFinal(e2) = false — not marked")
		}
	})

	// Via *Writer (scope interface — no AsReader() needed per Phase 15.8).
	w.Write(func(fw *flecs.Writer) {
		if !flecs.IsFinal(fw, e1) {
			t.Error("expected IsFinal(e1) = true via *Writer after SetFinal")
		}
		if flecs.IsFinal(fw, e2) {
			t.Error("expected IsFinal(e2) = false via *Writer — not marked")
		}
	})
}

// Test 6: Final + Reflexive composition — marking a Reflexive relationship's target Final
// must not cause a spurious panic, because the reflexive self-match is implicit (no
// (IsA, self) pair is actually added).
func TestFinal_ReflexiveComposition(t *testing.T) {
	w := flecs.New()
	var rel, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetReflexive(w, rel)
	flecs.SetFinal(w, e)

	// Reflexive self-match is implicit; no add of (rel, e) is performed, so no panic.
	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(e, flecs.MakePair(rel, e)) {
			t.Error("expected reflexive self-match to hold for e on rel")
		}
	})
}

// Test 7: Self-IsA-add to a Final entity panics — src == tgt case, matching C semantics.
func TestFinal_SelfIsAPanics(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})
	flecs.SetFinal(w, e)

	defer func() {
		if recover() == nil {
			t.Error("expected panic: AddID(e, MakePair(IsA, e)) where e is Final")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, flecs.MakePair(w.IsA(), e))
	})
}

// Test 8: Deferred path panics — panic surfaces when the Write scope flushes through addIDImmediate.
func TestFinal_DeferredPathPanics(t *testing.T) {
	w := flecs.New()
	var base, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		base = fw.NewEntity()
		child = fw.NewEntity()
	})
	flecs.SetFinal(w, base)

	defer func() {
		if recover() == nil {
			t.Error("expected panic on deferred flush when adding (IsA, Final-entity)")
		}
	}()
	// AddID queues a cmdAddID (deferDepth > 0). The panic fires during flush at
	// Write scope close when addIDImmediate calls checkFinal.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(child, flecs.MakePair(w.IsA(), base))
	})
}
