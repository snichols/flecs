package flecs_test

import (
	"strings"
	"testing"

	"github.com/snichols/flecs"
)

// Marker is a zero-size tag component used in traversal query tests.
type TraversableMarker struct{}

// Test 1: SetTraversable(w, R) then a query with .Up(R) succeeds.
func TestTraversable_UpSucceeds(t *testing.T) {
	w := flecs.New()
	var rel, parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		parent = fw.NewEntity()
		child = fw.NewEntity()
	})
	flecs.SetTraversable(w, rel)
	markerID := flecs.RegisterComponent[TraversableMarker](w)

	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, parent, markerID)
		flecs.AddID(fw, child, flecs.MakePair(rel, parent))
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(markerID).Up(rel))
	var matched []flecs.ID
	q.Each(func(it *flecs.QueryIter) {
		matched = append(matched, it.Entities()...)
	})
	if len(matched) == 0 {
		t.Error("expected at least one entity matched via .Up(rel), got none")
	}
}

// Test 2: Query with .Up(R) on a non-Traversable R panics with a message
// naming both the traversal modifier (.Up()) and the relationship.
func TestTraversable_NonTraversable_Up_Panics(t *testing.T) {
	w := flecs.New()
	var rel flecs.ID
	w.Write(func(fw *flecs.Writer) { rel = fw.NewEntity() })
	markerID := flecs.RegisterComponent[TraversableMarker](w)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for non-traversable .Up(), got none")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T: %v", r, r)
		}
		if !strings.Contains(msg, ".Up()") {
			t.Errorf("panic message does not name .Up(): %q", msg)
		}
		if !strings.Contains(msg, "SetTraversable") {
			t.Errorf("panic message does not mention SetTraversable: %q", msg)
		}
	}()

	flecs.NewQueryFromTerms(w, flecs.With(markerID).Up(rel))
}

// Test 3a: Query with .SelfUp(R) on a non-Traversable R panics naming .SelfUp().
func TestTraversable_NonTraversable_SelfUp_Panics(t *testing.T) {
	w := flecs.New()
	var rel flecs.ID
	w.Write(func(fw *flecs.Writer) { rel = fw.NewEntity() })
	markerID := flecs.RegisterComponent[TraversableMarker](w)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for non-traversable .SelfUp(), got none")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T: %v", r, r)
		}
		if !strings.Contains(msg, ".SelfUp()") {
			t.Errorf("panic message does not name .SelfUp(): %q", msg)
		}
	}()

	flecs.NewQueryFromTerms(w, flecs.With(markerID).SelfUp(rel))
}

// Test 3b: Query with .Cascade(R) on a non-Traversable R panics naming .Cascade().
func TestTraversable_NonTraversable_Cascade_Panics(t *testing.T) {
	w := flecs.New()
	var rel flecs.ID
	w.Write(func(fw *flecs.Writer) { rel = fw.NewEntity() })
	markerID := flecs.RegisterComponent[TraversableMarker](w)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for non-traversable .Cascade(), got none")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T: %v", r, r)
		}
		if !strings.Contains(msg, ".Cascade()") {
			t.Errorf("panic message does not name .Cascade(): %q", msg)
		}
	}()

	flecs.NewCachedQueryFromTerms(w, flecs.With(markerID).Cascade(rel))
}

// Test 4: IsTraversable returns true for IsA and ChildOf at world init (bootstrap check).
func TestTraversable_Bootstrap_IsAAndChildOf(t *testing.T) {
	w := flecs.New()
	w.Read(func(fr *flecs.Reader) {
		if !flecs.IsTraversable(fr, w.IsA()) {
			t.Error("IsTraversable(IsA) should be true after world init")
		}
		if !flecs.IsTraversable(fr, w.ChildOf()) {
			t.Error("IsTraversable(ChildOf) should be true after world init")
		}
	})
}

// Test 5: IsTraversable on a vanilla user entity returns false.
func TestTraversable_VanillaEntityReturnsFalse(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	w.Read(func(fr *flecs.Reader) {
		if flecs.IsTraversable(fr, e) {
			t.Errorf("IsTraversable(vanilla entity) should be false, got true")
		}
	})
}

// Test 6: SetTraversable(w, R) causes IsAcyclic(w, R) to return true (Acyclic implication).
func TestTraversable_ImpliesAcyclic(t *testing.T) {
	w := flecs.New()
	var rel flecs.ID
	w.Write(func(fw *flecs.Writer) { rel = fw.NewEntity() })

	if flecs.IsAcyclic(w, rel) {
		t.Fatal("expected IsAcyclic to be false before SetTraversable")
	}

	flecs.SetTraversable(w, rel)

	if !flecs.IsAcyclic(w, rel) {
		t.Error("SetTraversable should imply IsAcyclic; IsAcyclic returned false")
	}
}

// Test 7: SetTraversable round-trip + idempotence: set twice still true; bare-tag
// form fw.AddID(R, w.Traversable()) is equivalent.
func TestTraversable_IdempotentAndBareTag(t *testing.T) {
	w := flecs.New()
	var rel1, rel2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel1 = fw.NewEntity()
		rel2 = fw.NewEntity()
	})

	// Function form — set twice (idempotent).
	flecs.SetTraversable(w, rel1)
	flecs.SetTraversable(w, rel1)
	w.Read(func(fr *flecs.Reader) {
		if !flecs.IsTraversable(fr, rel1) {
			t.Error("IsTraversable(rel1) should be true after two SetTraversable calls")
		}
	})

	// Bare-tag form: fw.AddID(rel2, w.Traversable()).
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, rel2, w.Traversable())
	})
	w.Read(func(fr *flecs.Reader) {
		if !flecs.IsTraversable(fr, rel2) {
			t.Error("IsTraversable(rel2) should be true after bare-tag AddID form")
		}
	})
}

// Test 8: SetTraversable inside a Write block exercises the deferred path
// (cmdAddID → bare-tag dispatch in cmd_queue.go).
func TestTraversable_DeferredPath(t *testing.T) {
	w := flecs.New()
	var rel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		// Deferred: cmdAddID for bare Traversable tag is queued here,
		// dispatched to applyTraversablePolicy in cmd_queue.go batchForEntity.
		flecs.AddID(fw, rel, w.Traversable())
	})
	// After flush, rel should be traversable.
	w.Read(func(fr *flecs.Reader) {
		if !flecs.IsTraversable(fr, rel) {
			t.Error("deferred SetTraversable (bare-tag inside Write block) not applied after flush")
		}
	})
	// Acyclic implication should also be active.
	if !flecs.IsAcyclic(w, rel) {
		t.Error("Acyclic implication not applied for deferred Traversable tag")
	}
	// Should now accept .Up(rel) in query construction.
	markerID := flecs.RegisterComponent[TraversableMarker](w)
	_ = flecs.NewQueryFromTerms(w, flecs.With(markerID).Up(rel))
}

// Test 9: Pair-form traversal. The Traversable check validates against the raw
// Index() of t.Trav. In C, term->trav is always a single entity ID, never a
// pair (src/query/validator.c:639). Go follows the same convention: if a pair
// is passed to .Up(), its Index() encodes the second entity's index (not the
// relationship), so even when the relationship side R is traversable the check
// fails. Pairs are not valid traversal relationships in Go flecs.
func TestTraversable_PairFormTravPanics(t *testing.T) {
	w := flecs.New()
	var rel flecs.ID
	w.Write(func(fw *flecs.Writer) { rel = fw.NewEntity() })
	flecs.SetTraversable(w, rel)
	markerID := flecs.RegisterComponent[TraversableMarker](w)

	// MakePair(rel, w.Wildcard()) has its own pair-encoded ID. Even though rel
	// is traversable, the pair ID's Index() is the Wildcard index, which is not
	// in traversablePolicies, so the check rejects it.
	pairTrav := flecs.MakePair(rel, w.Wildcard())

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic when using a pair as traversal relationship, got none")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T: %v", r, r)
		}
		if !strings.Contains(msg, "SetTraversable") {
			t.Errorf("panic message does not mention SetTraversable: %q", msg)
		}
	}()

	// Passes pairTrav as the traversal relationship — must panic.
	flecs.NewQueryFromTerms(w, flecs.With(markerID).Up(pairTrav))
}

// Test 10: SetTransitive alone does NOT imply Traversable in this phase.
// C bootstrap.c:1299 has (EcsTransitive, EcsWith, EcsTraversable) which would
// make all transitive relationships also traversable (and therefore acyclic).
// Go flecs defers this implication to a follow-up phase: adding it now would
// break the existing cycle-safety tests in transitive_test.go that rely on the
// seen-map guard in transitiveWalk rather than write-time rejection.
// Users who want to traverse a transitive relationship with .Up(R) must call
// SetTraversable(w, R) explicitly in addition to SetTransitive(w, R).
func TestTraversable_TransitiveAloneDoesNotImplyTraversable(t *testing.T) {
	w := flecs.New()
	var rel flecs.ID
	w.Write(func(fw *flecs.Writer) { rel = fw.NewEntity() })

	flecs.SetTransitive(w, rel)

	// SetTransitive does NOT imply Traversable in this phase.
	w.Read(func(fr *flecs.Reader) {
		if flecs.IsTraversable(fr, rel) {
			t.Error("SetTransitive should NOT imply IsTraversable in this phase (deferred implication)")
		}
	})

	// .Up(rel) must be explicitly enabled with SetTraversable.
	flecs.SetTraversable(w, rel)
	markerID := flecs.RegisterComponent[TraversableMarker](w)
	_ = flecs.NewQueryFromTerms(w, flecs.With(markerID).Up(rel))
}

// Test 11: IsTraversable works inside Write and Read scopes (scope interface).
// Also exercises the TraverseSelf guard in validateAndSortTerms: a term whose
// Trav field is set directly but whose Traverse stays TraverseSelf (the default)
// must not trigger the traversable check, since TraverseSelf never traverses.
func TestTraversable_SelfTraverseWithNonZeroTravSkipsCheck(t *testing.T) {
	w := flecs.New()
	var rel flecs.ID
	w.Write(func(fw *flecs.Writer) { rel = fw.NewEntity() })
	markerID := flecs.RegisterComponent[TraversableMarker](w)

	// Construct a term with Trav=rel but Traverse=TraverseSelf (the zero default).
	// This is an unusual direct struct usage; the normal API always sets Traverse
	// correctly. The check in validateAndSortTerms skips this because TraverseSelf
	// means "no traversal" — the Trav field is irrelevant.
	term := flecs.With(markerID)
	term.Trav = rel // Traverse stays TraverseSelf (0)

	// Must not panic even though rel is not traversable.
	_ = flecs.NewQueryFromTerms(w, term)
}

func TestTraversable_IsTraversableInsideWrite(t *testing.T) {
	w := flecs.New()
	var rel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		flecs.SetTraversable(w, rel)
		if !flecs.IsTraversable(fw, rel) {
			t.Error("IsTraversable(fw, rel) should be true inside Write scope after SetTraversable")
		}
	})
	w.Read(func(fr *flecs.Reader) {
		if !flecs.IsTraversable(fr, rel) {
			t.Error("IsTraversable(fr, rel) should be true inside Read scope after SetTraversable")
		}
	})
}
