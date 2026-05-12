package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// Test 1: Default behavior unchanged — non-Acyclic relationship allows what would otherwise be cycles.
func TestAcyclic_DefaultAllowsCycles(t *testing.T) {
	w := flecs.New()
	var r, a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		// R is NOT acyclic; both directions must be storable without panic.
		fw.AddID(a, flecs.MakePair(r, b))
		fw.AddID(b, flecs.MakePair(r, a))
	})
	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(a, flecs.MakePair(r, b)) {
			t.Error("expected (a, R, b) to be present")
		}
		if !fr.HasID(b, flecs.MakePair(r, a)) {
			t.Error("expected (b, R, a) to be present")
		}
	})
}

// Test 2: Direct cycle prevented — adding (b, R, a) when (a, R, b) already exists must panic.
func TestAcyclic_DirectCyclePrevented(t *testing.T) {
	w := flecs.New()
	var r, a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
	})
	flecs.SetAcyclic(w, r)

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, b)) // OK: no cycle yet
	})

	defer func() {
		if recover() == nil {
			t.Error("expected panic when adding the return edge (b, R, a)")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(b, flecs.MakePair(r, a)) // must panic: cycle a→b→a
	})
}

// Test 3: Transitive cycle prevented — chain a→b→c then (c, R, a) must panic.
func TestAcyclic_TransitiveCyclePrevented(t *testing.T) {
	w := flecs.New()
	var r, a, b, c flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		c = fw.NewEntity()
	})
	flecs.SetAcyclic(w, r)

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, b))
		fw.AddID(b, flecs.MakePair(r, c))
	})

	defer func() {
		if recover() == nil {
			t.Error("expected panic when adding (c, R, a) which forms a transitive cycle c→a→b→c")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(c, flecs.MakePair(r, a)) // must panic: c→a→b→c
	})
}

// Test 4: Self-pair (a, R, a) is allowed — Acyclic does not reject self-pairs.
func TestAcyclic_SelfPairAllowed(t *testing.T) {
	w := flecs.New()
	var r, a flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
	})
	flecs.SetAcyclic(w, r)

	// Must not panic.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, a))
	})
	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(a, flecs.MakePair(r, a)) {
			t.Error("expected self-pair (a, R, a) to be stored on acyclic R")
		}
	})
}

// Test 5: ChildOf bootstrap regression — existing cascade tests still pass; cycle attempts are rejected.
func TestAcyclic_ChildOfBootstrapRegression(t *testing.T) {
	w := flecs.New()
	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		child = fw.NewEntity()
		fw.AddID(child, flecs.MakePair(w.ChildOf(), parent))
	})

	// Verify the normal parent→child relationship was stored.
	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(child, flecs.MakePair(w.ChildOf(), parent)) {
			t.Error("expected child to have (ChildOf, parent)")
		}
	})

	// Verify that attempting to add (parent, ChildOf, child) panics (would create a cycle).
	defer func() {
		if recover() == nil {
			t.Error("expected panic when adding (parent, ChildOf, child) — ChildOf is bootstrapped as acyclic")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(parent, flecs.MakePair(w.ChildOf(), child)) // must panic
	})
}

// Test 6: IsAcyclic round-trip — SetAcyclic and bare-tag form both set the flag.
func TestAcyclic_IsAcyclicRoundTrip(t *testing.T) {
	w := flecs.New()
	var r1, r2, r3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r1 = fw.NewEntity()
		r2 = fw.NewEntity()
		r3 = fw.NewEntity()
	})

	if flecs.IsAcyclic(w, r1) {
		t.Error("expected IsAcyclic(r1) = false before SetAcyclic")
	}
	flecs.SetAcyclic(w, r1)
	if !flecs.IsAcyclic(w, r1) {
		t.Error("expected IsAcyclic(r1) = true after SetAcyclic")
	}
	if flecs.IsAcyclic(w, r2) {
		t.Error("expected IsAcyclic(r2) = false (not marked)")
	}

	// Bare-tag form: fw.AddID(r3, w.Acyclic())
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(r3, w.Acyclic())
	})
	if !flecs.IsAcyclic(w, r3) {
		t.Error("expected IsAcyclic(r3) = true after fw.AddID(r3, w.Acyclic())")
	}
}

// Test 7: Acyclic + Transitive composition — Acyclic prevents write-time cycles while Transitive
// still resolves chains at query time. These traits act at different layers.
func TestAcyclic_TransitiveComposition(t *testing.T) {
	w := flecs.New()
	var r, a, b, c flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		c = fw.NewEntity()
	})
	flecs.SetAcyclic(w, r)
	flecs.SetTransitive(w, r)

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, b))
		fw.AddID(b, flecs.MakePair(r, c))
	})

	// Transitive query: (R, c) should match a and b.
	matched := map[flecs.ID]bool{}
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(r, c)))
	it := q.Iter()
	for it.Next() {
		for _, e := range it.Entities() {
			matched[e] = true
		}
	}
	if !matched[b] {
		t.Error("expected b (direct) to be matched by transitive query")
	}
	if !matched[a] {
		t.Error("expected a (transitive) to be matched by transitive query")
	}

	// Verify that a cycle is still rejected at write time even with Transitive.
	defer func() {
		if recover() == nil {
			t.Error("expected panic when adding (c, R, a) — Acyclic prevents write-time cycles")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(c, flecs.MakePair(r, a))
	})
}

// Test 8: Acyclic + Symmetric — Symmetric adds the mirror pair; if the mirror would create a cycle,
// the original add must fail before the mirror is attempted (C semantics: cycle check precedes mirror).
func TestAcyclic_SymmetricEdgeCase(t *testing.T) {
	w := flecs.New()
	var r, a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
	})
	flecs.SetAcyclic(w, r)
	flecs.SetSymmetric(w, r)

	// Add (a, R, b). Symmetric will also add (b, R, a) as a mirror.
	// Since R is also acyclic, adding (b, R, a) after (a, R, b) is stored would
	// be a cycle. The cycle check in addIDImmediate runs before the mirror add,
	// so this must panic (the check sees a→b, then tries b→a which reaches a).
	defer func() {
		if recover() == nil {
			t.Error("expected panic: Symmetric mirror (b, R, a) forms a cycle when R is also Acyclic")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, b))
	})
}

// Test 9: Bare-tag self-pair form — fw.AddID(relID, w.Acyclic()) sets the acyclic policy.
// Mirrors Phase 15.7's AddID(relID, Reflexive()) pattern verified in id_ops.go.
func TestAcyclic_BareTagSelfPairForm(t *testing.T) {
	w := flecs.New()
	var r, a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		// Register r as acyclic via bare-tag form.
		fw.AddID(r, w.Acyclic())
	})

	if !flecs.IsAcyclic(w, r) {
		t.Error("expected IsAcyclic(r) = true after fw.AddID(r, w.Acyclic())")
	}

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, b))
	})

	defer func() {
		if recover() == nil {
			t.Error("expected panic when adding return edge (b, R, a) after bare-tag SetAcyclic")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(b, flecs.MakePair(r, a))
	})
}
