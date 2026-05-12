package flecs_test

import (
	"fmt"
	"testing"

	"github.com/snichols/flecs"
)

// Test 1: Wildcard target — With(MakePair(Likes, Wildcard)) matches all entities
// with any Likes pair, yielding one iterator row per concrete target.
func TestWildcard_WildcardTarget(t *testing.T) {
	w := flecs.New()
	var likes, a, bob, alice flecs.ID
	w.Write(func(fw *flecs.Writer) {
		likes = fw.NewEntity()
		bob = fw.NewEntity()
		alice = fw.NewEntity()
		a = fw.NewEntity()
		fw.AddID(a, flecs.MakePair(likes, bob))
		fw.AddID(a, flecs.MakePair(likes, alice))
	})

	// Query for (Likes, Wildcard): should yield two rows for entity a.
	type row struct{ entity, target flecs.ID }
	var rows []row

	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(likes, w.Wildcard())))
	it := q.Iter()
	for it.Next() {
		target := flecs.MatchedTarget(it, 0)
		for _, e := range it.Entities() {
			rows = append(rows, row{e, target})
		}
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (one per concrete target), got %d", len(rows))
	}
	targets := map[flecs.ID]bool{}
	for _, r := range rows {
		if r.entity != a {
			t.Errorf("unexpected entity %v; expected %v", r.entity, a)
		}
		targets[r.target] = true
	}
	if !targets[bob] {
		t.Error("expected Bob as a matched target")
	}
	if !targets[alice] {
		t.Error("expected Alice as a matched target")
	}
}

// Test 2: Wildcard relationship — With(MakePair(Wildcard, Bob)) matches all
// entities that have any relationship to Bob, one row per concrete relationship.
func TestWildcard_WildcardRelationship(t *testing.T) {
	w := flecs.New()
	var likes, knows, bob, e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		likes = fw.NewEntity()
		knows = fw.NewEntity()
		bob = fw.NewEntity()
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		fw.AddID(e1, flecs.MakePair(likes, bob))
		fw.AddID(e1, flecs.MakePair(knows, bob))
		fw.AddID(e2, flecs.MakePair(likes, bob))
	})

	type row struct{ entity, matchedID flecs.ID }
	var rows []row
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(w.Wildcard(), bob)))
	it := q.Iter()
	for it.Next() {
		mid := flecs.MatchedID(it, 0)
		for _, e := range it.Entities() {
			rows = append(rows, row{e, mid})
		}
	}

	// e1 should appear twice (Likes+Bob and Knows+Bob), e2 once (Likes+Bob).
	countByEntity := map[flecs.ID]int{}
	for _, r := range rows {
		countByEntity[r.entity]++
	}
	if countByEntity[e1] != 2 {
		t.Errorf("e1: expected 2 rows, got %d", countByEntity[e1])
	}
	if countByEntity[e2] != 1 {
		t.Errorf("e2: expected 1 row, got %d", countByEntity[e2])
	}
}

// Test 3: Both wildcard — With(MakePair(Wildcard, Wildcard)) matches every entity
// that has any pair, emitting one row per concrete pair.
func TestWildcard_BothWildcard(t *testing.T) {
	w := flecs.New()
	var r1, r2, t1, t2, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r1 = fw.NewEntity()
		r2 = fw.NewEntity()
		t1 = fw.NewEntity()
		t2 = fw.NewEntity()
		e = fw.NewEntity()
		fw.AddID(e, flecs.MakePair(r1, t1))
		fw.AddID(e, flecs.MakePair(r2, t2))
	})

	matched := map[flecs.ID]bool{}
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(w.Wildcard(), w.Wildcard())))
	it := q.Iter()
	for it.Next() {
		mid := flecs.MatchedID(it, 0)
		for _, ent := range it.Entities() {
			if ent == e {
				matched[mid] = true
			}
		}
	}

	if !matched[flecs.MakePair(r1, t1)] {
		t.Error("expected (r1,t1) pair to be matched")
	}
	if !matched[flecs.MakePair(r2, t2)] {
		t.Error("expected (r2,t2) pair to be matched")
	}
}

// Test 4: Any target — With(MakePair(Likes, Any)) matches entities that have at
// least one Likes pair, emitting exactly one row per entity regardless of target count.
func TestWildcard_AnyTarget(t *testing.T) {
	w := flecs.New()
	var likes, a, bob, alice flecs.ID
	w.Write(func(fw *flecs.Writer) {
		likes = fw.NewEntity()
		bob = fw.NewEntity()
		alice = fw.NewEntity()
		a = fw.NewEntity()
		fw.AddID(a, flecs.MakePair(likes, bob))
		fw.AddID(a, flecs.MakePair(likes, alice))
	})

	// Query with Any: should yield exactly one row for entity a.
	var entities []flecs.ID
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(likes, w.Any())))
	it := q.Iter()
	for it.Next() {
		entities = append(entities, it.Entities()...)
	}

	if len(entities) != 1 {
		t.Fatalf("expected exactly 1 entity row (Any short-circuits), got %d", len(entities))
	}
	if entities[0] != a {
		t.Errorf("expected entity a, got %v", entities[0])
	}
}

// Test 5: MatchedTarget returns the concrete target for the current wildcard row.
func TestWildcard_MatchedTarget(t *testing.T) {
	w := flecs.New()
	var likes, a, bob flecs.ID
	w.Write(func(fw *flecs.Writer) {
		likes = fw.NewEntity()
		bob = fw.NewEntity()
		a = fw.NewEntity()
		fw.AddID(a, flecs.MakePair(likes, bob))
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(likes, w.Wildcard())))
	it := q.Iter()
	found := false
	for it.Next() {
		tgt := flecs.MatchedTarget(it, 0)
		if tgt == bob {
			found = true
		}
	}
	if !found {
		t.Error("MatchedTarget did not return bob")
	}
}

// Test 6: MatchedID returns the full concrete pair ID for the current wildcard row.
func TestWildcard_MatchedID(t *testing.T) {
	w := flecs.New()
	var likes, a, bob flecs.ID
	w.Write(func(fw *flecs.Writer) {
		likes = fw.NewEntity()
		bob = fw.NewEntity()
		a = fw.NewEntity()
		fw.AddID(a, flecs.MakePair(likes, bob))
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(likes, w.Wildcard())))
	it := q.Iter()
	found := false
	for it.Next() {
		mid := flecs.MatchedID(it, 0)
		if mid == flecs.MakePair(likes, bob) {
			found = true
		}
	}
	if !found {
		t.Error("MatchedID did not return MakePair(likes, bob)")
	}
}

// Test 7: Combined wildcard + non-wildcard — With[Position] With(MakePair(Likes, Wildcard)).
// Only entities that have both Position AND at least one Likes pair are matched.
func TestWildcard_MixedTerms(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	var likes, bob, alice flecs.ID
	var withPos, withoutPos flecs.ID
	w.Write(func(fw *flecs.Writer) {
		likes = fw.NewEntity()
		bob = fw.NewEntity()
		alice = fw.NewEntity()
		// withPos has Position + (Likes, Bob) + (Likes, Alice)
		withPos = fw.NewEntity()
		flecs.Set(fw, withPos, Position{X: 1, Y: 2})
		fw.AddID(withPos, flecs.MakePair(likes, bob))
		fw.AddID(withPos, flecs.MakePair(likes, alice))
		// withoutPos has (Likes, Bob) but no Position → should not match
		withoutPos = fw.NewEntity()
		fw.AddID(withoutPos, flecs.MakePair(likes, bob))
	})

	type row struct {
		entity flecs.ID
		target flecs.ID
	}
	var rows []row
	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.With(flecs.MakePair(likes, w.Wildcard())),
	)
	it := q.Iter()
	for it.Next() {
		target := flecs.MatchedTarget(it, 1) // wildcard is the second term (index 1)
		for _, e := range it.Entities() {
			rows = append(rows, row{e, target})
		}
	}

	// withPos should appear twice (bob + alice), withoutPos should not appear.
	entities := map[flecs.ID]int{}
	for _, r := range rows {
		entities[r.entity]++
		if r.entity == withoutPos {
			t.Error("withoutPos (no Position) should not have matched")
		}
	}
	if entities[withPos] != 2 {
		t.Errorf("withPos: expected 2 wildcard rows, got %d", entities[withPos])
	}
	_ = posID
}

// Test 8: Cached query interaction — wildcard terms in CachedQuery; new concrete
// pair on a new table is picked up after construction.
func TestWildcard_CachedQuery(t *testing.T) {
	w := flecs.New()
	var likes, bob, alice flecs.ID
	var a flecs.ID
	w.Write(func(fw *flecs.Writer) {
		likes = fw.NewEntity()
		bob = fw.NewEntity()
		alice = fw.NewEntity()
		a = fw.NewEntity()
		fw.AddID(a, flecs.MakePair(likes, bob))
	})

	cq := flecs.NewCachedQueryFromTerms(w, flecs.With(flecs.MakePair(likes, w.Wildcard())))

	// Before adding alice: one row for a/bob.
	var rows1 []flecs.ID
	it1 := cq.Iter()
	for it1.Next() {
		rows1 = append(rows1, flecs.MatchedTarget(it1, 0))
	}
	if len(rows1) != 1 {
		t.Fatalf("before alice: expected 1 row, got %d", len(rows1))
	}
	if rows1[0] != bob {
		t.Errorf("before alice: expected bob target, got %v", rows1[0])
	}

	// Add (Likes, Alice) to a — this migrates a to a new table, which notifies
	// the cached query via notifyTableCreated.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(likes, alice))
	})

	// After adding alice: two rows for the new table.
	targets := map[flecs.ID]bool{}
	it2 := cq.Iter()
	for it2.Next() {
		targets[flecs.MatchedTarget(it2, 0)] = true
	}
	if !targets[bob] {
		t.Error("after alice: expected bob target")
	}
	if !targets[alice] {
		t.Error("after alice: expected alice target")
	}

	cq.Close()
}

// Test 9: FieldByMatch returns per-target component data for wildcard pair queries.
func TestWildcard_FieldByMatch(t *testing.T) {
	type Distance struct{ Meters float32 }

	w := flecs.New()
	flecs.RegisterComponent[Distance](w)
	var likes, a, bob, alice flecs.ID
	w.Write(func(fw *flecs.Writer) {
		likes = fw.NewEntity()
		bob = fw.NewEntity()
		alice = fw.NewEntity()
		a = fw.NewEntity()
		flecs.SetPair[Distance](fw, a, likes, bob, Distance{Meters: 10})
		flecs.SetPair[Distance](fw, a, likes, alice, Distance{Meters: 20})
	})

	distByTarget := map[flecs.ID]float32{}
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(likes, w.Wildcard())))
	it := q.Iter()
	for it.Next() {
		target := flecs.MatchedTarget(it, 0)
		col := flecs.FieldByMatch[Distance](it, 0)
		for i := range it.Entities() {
			distByTarget[target] = col[i].Meters
		}
	}

	if distByTarget[bob] != 10 {
		t.Errorf("expected Distance to Bob == 10, got %v", distByTarget[bob])
	}
	if distByTarget[alice] != 20 {
		t.Errorf("expected Distance to Alice == 20, got %v", distByTarget[alice])
	}
}

// Test: FieldByMatch with a tag pair (no component data) returns zero-value slice.
func TestWildcard_FieldByMatch_TagPair(t *testing.T) {
	w := flecs.New()
	var likes, a, bob flecs.ID
	w.Write(func(fw *flecs.Writer) {
		likes = fw.NewEntity()
		bob = fw.NewEntity()
		a = fw.NewEntity()
		fw.AddID(a, flecs.MakePair(likes, bob)) // tag pair: no data
	})

	type Empty struct{}
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(likes, w.Wildcard())))
	it := q.Iter()
	for it.Next() {
		col := flecs.FieldByMatch[Empty](it, 0)
		if len(col) != it.Count() {
			t.Errorf("expected tag slice length %d, got %d", it.Count(), len(col))
		}
	}
}

// Test: MatchedTarget panics when the term index doesn't match the wildcard term.
func TestWildcard_MatchedTarget_WrongTermIdx(t *testing.T) {
	w := flecs.New()
	var likes, a, bob flecs.ID
	w.Write(func(fw *flecs.Writer) {
		likes = fw.NewEntity()
		bob = fw.NewEntity()
		a = fw.NewEntity()
		fw.AddID(a, flecs.MakePair(likes, bob))
	})
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(likes, w.Wildcard())))
	it := q.Iter()
	for it.Next() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected MatchedTarget to panic for wrong termIdx")
			}
		}()
		_ = flecs.MatchedTarget(it, 99) // wrong term index → panic
	}
}

// Test: MatchedID panics when the term index doesn't match the wildcard term.
func TestWildcard_MatchedID_WrongTermIdx(t *testing.T) {
	w := flecs.New()
	var likes, a, bob flecs.ID
	w.Write(func(fw *flecs.Writer) {
		likes = fw.NewEntity()
		bob = fw.NewEntity()
		a = fw.NewEntity()
		fw.AddID(a, flecs.MakePair(likes, bob))
	})
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(likes, w.Wildcard())))
	it := q.Iter()
	for it.Next() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected MatchedID to panic for wrong termIdx")
			}
		}()
		_ = flecs.MatchedID(it, 99) // wrong term index → panic
	}
}

// Test: FieldByMatch panics when the term index doesn't match the wildcard term.
func TestWildcard_FieldByMatch_WrongTermIdx(t *testing.T) {
	type Distance struct{ Meters float32 }
	w := flecs.New()
	flecs.RegisterComponent[Distance](w)
	var likes, a, bob flecs.ID
	w.Write(func(fw *flecs.Writer) {
		likes = fw.NewEntity()
		bob = fw.NewEntity()
		a = fw.NewEntity()
		flecs.SetPair[Distance](fw, a, likes, bob, Distance{Meters: 5})
	})
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(likes, w.Wildcard())))
	it := q.Iter()
	for it.Next() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected FieldByMatch to panic for wrong termIdx")
			}
		}()
		_ = flecs.FieldByMatch[Distance](it, 99) // wrong term index → panic
	}
}

// Test: FieldByMatch panics when T doesn't match the registered type.
func TestWildcard_FieldByMatch_TypeMismatch(t *testing.T) {
	type Distance struct{ Meters float32 }
	type Wrong struct{ Val int }
	w := flecs.New()
	flecs.RegisterComponent[Distance](w)
	var likes, a, bob flecs.ID
	w.Write(func(fw *flecs.Writer) {
		likes = fw.NewEntity()
		bob = fw.NewEntity()
		a = fw.NewEntity()
		flecs.SetPair[Distance](fw, a, likes, bob, Distance{Meters: 5})
	})
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(likes, w.Wildcard())))
	it := q.Iter()
	for it.Next() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected FieldByMatch to panic for type mismatch")
			}
		}()
		_ = flecs.FieldByMatch[Wrong](it, 0) // wrong type → panic
	}
}

// Test: CachedQuery correctly rejects tables that don't match the wildcard term.
func TestWildcard_CachedQuery_NoMatch(t *testing.T) {
	w := flecs.New()
	var likes, knows, bob flecs.ID
	w.Write(func(fw *flecs.Writer) {
		likes = fw.NewEntity()
		knows = fw.NewEntity()
		bob = fw.NewEntity()
		e := fw.NewEntity()
		fw.AddID(e, flecs.MakePair(likes, bob)) // has Likes but NOT Knows
	})

	// Query for (Knows, Wildcard) — no entity has Knows, so zero matches.
	cq := flecs.NewCachedQueryFromTerms(w, flecs.With(flecs.MakePair(knows, w.Wildcard())))
	it := cq.Iter()
	count := 0
	for it.Next() {
		count++
	}
	if count != 0 {
		t.Errorf("expected 0 matches, got %d", count)
	}
	cq.Close()
}

// Test: Any in the relationship position matches once per entity.
func TestWildcard_AnyRelationship(t *testing.T) {
	w := flecs.New()
	var r1, r2, target, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r1 = fw.NewEntity()
		r2 = fw.NewEntity()
		target = fw.NewEntity()
		e = fw.NewEntity()
		fw.AddID(e, flecs.MakePair(r1, target))
		fw.AddID(e, flecs.MakePair(r2, target))
	})

	var entities []flecs.ID
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(w.Any(), target)))
	it := q.Iter()
	for it.Next() {
		entities = append(entities, it.Entities()...)
	}
	// Any short-circuits: e should appear exactly once.
	count := 0
	for _, ent := range entities {
		if ent == e {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected entity e exactly once with Any relationship, got %d rows", count)
	}
}

// BenchmarkWildcardQuery_PairsPerEntity measures per-pair iteration cost as the
// number of distinct Likes targets per entity scales from 1 to 16.
func BenchmarkWildcardQuery_PairsPerEntity(b *testing.B) {
	for _, n := range []int{1, 4, 8, 16} {
		pairs := n
		b.Run(fmt.Sprintf("pairs=%d", pairs), func(b *testing.B) {
			w := flecs.New()
			var likes flecs.ID
			w.Write(func(fw *flecs.Writer) {
				likes = fw.NewEntity()
				targets := make([]flecs.ID, pairs)
				for i := range targets {
					targets[i] = fw.NewEntity()
				}
				e := fw.NewEntity()
				for _, tgt := range targets {
					fw.AddID(e, flecs.MakePair(likes, tgt))
				}
			})
			q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(likes, w.Wildcard())))
			b.ResetTimer()
			for range b.N {
				it := q.Iter()
				for it.Next() {
					_ = flecs.MatchedTarget(it, 0)
				}
			}
		})
	}
}
