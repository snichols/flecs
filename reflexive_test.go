package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// Test 1: Default behavior unchanged — non-Reflexive relationship HasID self-pair returns false.
func TestReflexive_DefaultNoSelfPair(t *testing.T) {
	w := flecs.New()
	var r, a flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
	})

	// R is NOT reflexive; no pair stored on a.
	w.Read(func(fr *flecs.Reader) {
		if fr.HasID(a, flecs.MakePair(r, a)) {
			t.Error("expected HasID(a, (R,a)) = false for non-reflexive R without stored pair")
		}
	})
}

// Test 2: SetReflexive + HasID self-pair returns true without a stored pair.
func TestReflexive_HasIDSelfPairReturnsTrue(t *testing.T) {
	w := flecs.New()
	var r, a flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
	})
	flecs.SetReflexive(w, r)

	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(a, flecs.MakePair(r, a)) {
			t.Error("expected HasID(a, (R,a)) = true for reflexive R without stored pair")
		}
	})
}

// Test 3: HasID does not return true for non-self pairs even when R is reflexive.
func TestReflexive_HasIDNonSelfPairUnchanged(t *testing.T) {
	w := flecs.New()
	var r, a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
	})
	flecs.SetReflexive(w, r)

	// (R, b) is not a self-pair for a — no stored pair, so should be false.
	w.Read(func(fr *flecs.Reader) {
		if fr.HasID(a, flecs.MakePair(r, b)) {
			t.Error("expected HasID(a, (R,b)) = false for reflexive R when b != a and no pair stored")
		}
	})
}

// Test 4: Query self-match — With(MakePair(R, target)) includes target itself.
func TestReflexive_QuerySelfMatch(t *testing.T) {
	w := flecs.New()
	var r, target, other flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		target = fw.NewEntity()
		other = fw.NewEntity()
		// other has (R, target) stored directly; target does not.
		fw.AddID(other, flecs.MakePair(r, target))
	})
	flecs.SetReflexive(w, r)

	// Query for (R, target) should match both other (direct) and target (reflexive self-match).
	matched := map[flecs.ID]bool{}
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(r, target)))
	it := q.Iter()
	for it.Next() {
		for _, e := range it.Entities() {
			matched[e] = true
		}
	}
	if !matched[other] {
		t.Error("expected 'other' (direct pair holder) to be matched")
	}
	if !matched[target] {
		t.Error("expected 'target' itself to be matched (reflexive self-match)")
	}
}

// Test 5: Reflexive + Transitive composition — query yields both the starting entity and ancestors.
func TestReflexive_TransitiveComposition(t *testing.T) {
	w := flecs.New()
	var r, a, b, c flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		c = fw.NewEntity()
		// Chain: a→(R,b), b→(R,c)
		fw.AddID(a, flecs.MakePair(r, b))
		fw.AddID(b, flecs.MakePair(r, c))
	})
	flecs.SetTransitive(w, r)
	flecs.SetReflexive(w, r)

	// Query for (R, c): should match b (direct), a (transitive), and c (reflexive self-match).
	matched := map[flecs.ID]bool{}
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(r, c)))
	it := q.Iter()
	for it.Next() {
		for _, e := range it.Entities() {
			matched[e] = true
		}
	}
	if !matched[b] {
		t.Error("expected b (direct) to be matched")
	}
	if !matched[a] {
		t.Error("expected a (transitive) to be matched")
	}
	if !matched[c] {
		t.Error("expected c (reflexive self-match) to be matched")
	}
}

// Test 6: IsA is reflexive after bootstrap — HasID(a, MakePair(IsA, a)) returns true.
func TestReflexive_IsABootstrapReflexive(t *testing.T) {
	w := flecs.New()
	var a flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a = fw.NewEntity()
	})

	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(a, flecs.MakePair(w.IsA(), a)) {
			t.Error("expected HasID(a, MakePair(IsA, a)) = true (IsA is bootstrapped as reflexive)")
		}
	})
}

// Test 7: IsReflexive round-trip — SetReflexive and bare-tag form both set the flag.
func TestReflexive_IsReflexiveRoundTrip(t *testing.T) {
	w := flecs.New()
	var r1, r2, r3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r1 = fw.NewEntity()
		r2 = fw.NewEntity()
		r3 = fw.NewEntity()
	})

	if flecs.IsReflexive(w, r1) {
		t.Error("expected IsReflexive(r1) = false before SetReflexive")
	}
	flecs.SetReflexive(w, r1)
	if !flecs.IsReflexive(w, r1) {
		t.Error("expected IsReflexive(r1) = true after SetReflexive")
	}
	if flecs.IsReflexive(w, r2) {
		t.Error("expected IsReflexive(r2) = false (not marked)")
	}

	// Bare-tag form: fw.AddID(r3, w.Reflexive())
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(r3, w.Reflexive())
	})
	if !flecs.IsReflexive(w, r3) {
		t.Error("expected IsReflexive(r3) = true after fw.AddID(r3, w.Reflexive())")
	}
}

// Test 8: Reflexive on a non-relationship entity (a plain tag) is lenient — no panic.
// The tag just has no effect in query evaluation since it is never used as a pair-first.
// Mirrors C behavior: ecs_add_id(world, MyTag, EcsReflexive) does not error.
func TestReflexive_OnNonRelationshipEntityIsLenient(t *testing.T) {
	w := flecs.New()
	var tag flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tag = fw.NewEntity()
	})
	// Should not panic.
	flecs.SetReflexive(w, tag)
	if !flecs.IsReflexive(w, tag) {
		t.Error("expected IsReflexive(tag) = true after SetReflexive on non-relationship entity")
	}
}

// Test 9: Cached query — reflexive self-match is included at construction time.
func TestReflexive_CachedQuerySelfMatch(t *testing.T) {
	w := flecs.New()
	var r, target, other flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		target = fw.NewEntity()
		other = fw.NewEntity()
		fw.AddID(other, flecs.MakePair(r, target))
	})
	flecs.SetReflexive(w, r)

	cq := flecs.NewCachedQueryFromTerms(w, flecs.With(flecs.MakePair(r, target)))
	matched := map[flecs.ID]bool{}
	it := cq.Iter()
	for it.Next() {
		for _, e := range it.Entities() {
			matched[e] = true
		}
	}
	if !matched[other] {
		t.Error("cached query: expected 'other' (direct pair holder) to be matched")
	}
	if !matched[target] {
		t.Error("cached query: expected 'target' itself to be matched (reflexive self-match)")
	}
}
