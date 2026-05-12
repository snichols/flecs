package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// Test 1: Default behavior unchanged — a non-Transitive relationship does not chain.
func TestTransitive_DefaultNoChain(t *testing.T) {
	w := flecs.New()
	var r, a, b, c flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		c = fw.NewEntity()
	})
	// a R b, b R c — R is NOT transitive
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, b))
		fw.AddID(b, flecs.MakePair(r, c))
	})

	// Query for (R, c) should match only b (direct), not a.
	var matched []flecs.ID
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(r, c)))
	it := q.Iter()
	for it.Next() {
		matched = append(matched, it.Entities()...)
	}
	if len(matched) != 1 {
		t.Fatalf("expected 1 match (b only), got %d: %v", len(matched), matched)
	}
	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(matched[0], flecs.MakePair(r, c)) {
			t.Error("expected the matched entity to directly have (R, c)")
		}
	})
}

// Test 2: Simple chain — a R b, b R c; mark R Transitive; query (R,c) matches a AND b.
func TestTransitive_SimpleChain(t *testing.T) {
	w := flecs.New()
	var r, a, b, c flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		c = fw.NewEntity()
	})
	flecs.SetTransitive(w, r)

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, b))
		fw.AddID(b, flecs.MakePair(r, c))
	})

	// Query for (R, c): b matches directly, a matches transitively via b.
	matched := map[flecs.ID]bool{}
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(r, c)))
	it := q.Iter()
	for it.Next() {
		for _, e := range it.Entities() {
			matched[e] = true
		}
	}
	if !matched[a] {
		t.Error("expected a to match (R,c) transitively via b")
	}
	if !matched[b] {
		t.Error("expected b to match (R,c) directly")
	}
	if matched[c] {
		t.Error("expected c not to match (c has no (R,*) pair)")
	}
}

// Test 3: Longer chain — a R b R c R d; query (R,d) matches a, b, and c.
func TestTransitive_LongerChain(t *testing.T) {
	w := flecs.New()
	var r, a, b, c, d flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		c = fw.NewEntity()
		d = fw.NewEntity()
	})
	flecs.SetTransitive(w, r)

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, b))
		fw.AddID(b, flecs.MakePair(r, c))
		fw.AddID(c, flecs.MakePair(r, d))
	})

	matched := map[flecs.ID]bool{}
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(r, d)))
	it := q.Iter()
	for it.Next() {
		for _, e := range it.Entities() {
			matched[e] = true
		}
	}
	if !matched[a] {
		t.Error("expected a to match (R,d) via 3-hop chain")
	}
	if !matched[b] {
		t.Error("expected b to match (R,d) via 2-hop chain")
	}
	if !matched[c] {
		t.Error("expected c to match (R,d) directly")
	}
	if matched[d] {
		t.Error("expected d not to match (d has no (R,*) pair)")
	}
}

// Test 4: Branching chain — a has (R,b) and (R,c); b has (R,d).
// Query for (R,d) matches a and b. Query for (R,c) matches only a.
func TestTransitive_BranchingChain(t *testing.T) {
	w := flecs.New()
	var r, a, b, c, d flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		c = fw.NewEntity()
		d = fw.NewEntity()
	})
	flecs.SetTransitive(w, r)

	// a has (R,b) and (R,c); b has (R,d)
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, b))
		fw.AddID(a, flecs.MakePair(r, c))
		fw.AddID(b, flecs.MakePair(r, d))
	})

	// Query for (R,d): a (via b) and b (directly) match.
	matched := map[flecs.ID]bool{}
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(r, d)))
	it := q.Iter()
	for it.Next() {
		for _, e := range it.Entities() {
			matched[e] = true
		}
	}
	if !matched[a] {
		t.Error("expected a to match (R,d) via branch through b")
	}
	if !matched[b] {
		t.Error("expected b to match (R,d) directly")
	}
	if matched[c] {
		t.Error("expected c not to match (R,d)")
	}

	// Query for (R,c): only a matches (direct), b has no path to c.
	matched = map[flecs.ID]bool{}
	q2 := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(r, c)))
	it2 := q2.Iter()
	for it2.Next() {
		for _, e := range it2.Entities() {
			matched[e] = true
		}
	}
	if !matched[a] {
		t.Error("expected a to match (R,c) directly")
	}
	if matched[b] {
		t.Error("expected b not to match (R,c) — b has no path to c")
	}
}

// Test 5: Cycle safety — a R b, b R a; no infinite loop; querying (R,a) matches b and vice versa.
func TestTransitive_CycleSafety(t *testing.T) {
	w := flecs.New()
	var r, a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
	})
	flecs.SetTransitive(w, r)

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, b))
		fw.AddID(b, flecs.MakePair(r, a))
	})

	// Query for (R,a): a matches (direct), b matches (via a which has (R,b)... but we want (R,a)).
	// Actually: b has (R,a) directly → b matches.
	// a has (R,b) → walk b: b has (R,a) → a matches transitively!
	matchedA := map[flecs.ID]bool{}
	qA := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(r, a)))
	itA := qA.Iter()
	for itA.Next() {
		for _, e := range itA.Entities() {
			matchedA[e] = true
		}
	}
	if !matchedA[b] {
		t.Error("expected b to match (R,a) directly")
	}
	if !matchedA[a] {
		t.Error("expected a to match (R,a) transitively via b (b has (R,a))")
	}

	// Query for (R,b): a matches directly, b matches transitively via a.
	matchedB := map[flecs.ID]bool{}
	qB := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(r, b)))
	itB := qB.Iter()
	for itB.Next() {
		for _, e := range itB.Entities() {
			matchedB[e] = true
		}
	}
	if !matchedB[a] {
		t.Error("expected a to match (R,b) directly")
	}
	if !matchedB[b] {
		t.Error("expected b to match (R,b) transitively via a (a has (R,b))")
	}
}

// Test 6: Cache interaction — CachedQuery with transitive term pre-computes at construction
// and re-evaluates on table creation.
func TestTransitive_CacheInteraction(t *testing.T) {
	w := flecs.New()
	var r, a, b, c flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		c = fw.NewEntity()
	})
	flecs.SetTransitive(w, r)

	// a R b, b R c
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, b))
		fw.AddID(b, flecs.MakePair(r, c))
	})

	// Build cached query for (R,c) — pre-computes at construction.
	cq := flecs.NewCachedQueryFromTerms(w, flecs.With(flecs.MakePair(r, c)))
	defer cq.Close()

	matched := map[flecs.ID]bool{}
	cq.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			matched[e] = true
		}
	})
	if !matched[a] {
		t.Error("pre-computed cache: expected a to match (R,c) transitively")
	}
	if !matched[b] {
		t.Error("pre-computed cache: expected b to match (R,c) directly")
	}

	// Now add a new entity d that has (R,b) — creates a new table → cache re-evaluates.
	var d flecs.ID
	w.Write(func(fw *flecs.Writer) {
		d = fw.NewEntity()
		fw.AddID(d, flecs.MakePair(r, b))
	})

	matched2 := map[flecs.ID]bool{}
	cq.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			matched2[e] = true
		}
	})
	if !matched2[d] {
		t.Error("cache re-eval on table-create: expected d to match (R,c) transitively via b")
	}
}

// Test 7: Depth limit — a very long chain (> maxTraversalDepth) terminates gracefully.
// Entities within reach DO match; the function must not panic or deadlock.
func TestTransitive_DepthLimit(t *testing.T) {
	w := flecs.New()
	// maxTraversalDepth = 64; use 100 hops to exceed it comfortably.
	const chainLen = 100
	entities := make([]flecs.ID, chainLen+1)
	var r flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		for i := range entities {
			entities[i] = fw.NewEntity()
		}
	})
	flecs.SetTransitive(w, r)

	// Build linear chain: entities[0] R entities[1] R ... R entities[chainLen]
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < chainLen; i++ {
			fw.AddID(entities[i], flecs.MakePair(r, entities[i+1]))
		}
	})

	target := entities[chainLen]

	// This must not panic. The depth limit prevents traversing the full chain,
	// so entities[0] may not match — that is acceptable. Direct neighbor DOES match.
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(r, target)))
	_ = q.Iter() // must not panic

	// Direct neighbor of target (entities[chainLen-1]) must match.
	directMatched := false
	it := q.Iter()
	for it.Next() {
		for _, e := range it.Entities() {
			if e == entities[chainLen-1] {
				directMatched = true
			}
		}
	}
	if !directMatched {
		t.Error("expected direct neighbor of target to match")
	}
}

// Test 8: IsTransitive round-trip — SetTransitive and bare-tag form both set the flag.
func TestTransitive_IsTransitiveRoundTrip(t *testing.T) {
	w := flecs.New()
	var r1, r2, r3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r1 = fw.NewEntity()
		r2 = fw.NewEntity()
		r3 = fw.NewEntity()
	})

	if flecs.IsTransitive(w, r1) {
		t.Error("expected IsTransitive(r1) = false before SetTransitive")
	}

	// SetTransitive form.
	flecs.SetTransitive(w, r1)
	if !flecs.IsTransitive(w, r1) {
		t.Error("expected IsTransitive(r1) = true after SetTransitive")
	}
	if flecs.IsTransitive(w, r2) {
		t.Error("expected IsTransitive(r2) = false (not marked)")
	}

	// Bare-tag form via AddID.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(r3, w.Transitive())
	})
	if !flecs.IsTransitive(w, r3) {
		t.Error("expected IsTransitive(r3) = true after fw.AddID(r3, w.Transitive())")
	}
}

// Test 9: Transitive + Symmetric composition — both traits on the same R.
// Symmetric mirrors at write time; Transitive chains at query time.
func TestTransitive_SymmetricComposition(t *testing.T) {
	w := flecs.New()
	var r, a, b, c flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		c = fw.NewEntity()
	})
	flecs.SetTransitive(w, r)
	flecs.SetSymmetric(w, r)

	// Add (R,b) to a: Symmetric mirrors (R,a) onto b.
	// Add (R,c) to b: Symmetric mirrors (R,b) onto c.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, b))
	})
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(b, flecs.MakePair(r, c))
	})

	// Verify symmetric mirrors fired.
	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(b, flecs.MakePair(r, a)) {
			t.Fatal("Symmetric: expected b to have (R,a) after adding (R,b) to a")
		}
		if !fr.HasID(c, flecs.MakePair(r, b)) {
			t.Fatal("Symmetric: expected c to have (R,b) after adding (R,c) to b")
		}
	})

	// Query for (R,c) with Transitive:
	// b has (R,c) directly → b matches.
	// a has (R,b) → walk b: b has (R,c) → a matches.
	// c has (R,b) → walk b: b has (R,c) → c also matches via b!
	matched := map[flecs.ID]bool{}
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(r, c)))
	it := q.Iter()
	for it.Next() {
		for _, e := range it.Entities() {
			matched[e] = true
		}
	}
	if !matched[b] {
		t.Error("expected b to match (R,c) directly")
	}
	if !matched[a] {
		t.Error("expected a to match (R,c) transitively via b")
	}
	// c has (R,b) (from symmetric mirror), and b has (R,c) → c transitively reaches c
	// via b. This is a degenerate self-match via the chain c→b→c, which the cycle
	// detection handles; c is in visited after the first step so no infinite recursion.
	// Transitive does not imply Reflexive, so (R,c) on c itself is not auto-added.
	// The result for c depends on whether c→b→c is treated as reaching target c.
	// Since c's walk from b finds (R,c) which == target, c DOES match transitively.
	if !matched[c] {
		t.Error("expected c to match (R,c) transitively (c has (R,b) via symmetric; b has (R,c))")
	}
}

// TestTransitive_CycleDead verifies the visited-set guard in transitiveWalk when
// a cycle exists but the target is not reachable: walk terminates, no panic.
func TestTransitive_CycleDead(t *testing.T) {
	w := flecs.New()
	// a→b→a (cycle), query for unreachable target c.
	var r, a, b, c flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		c = fw.NewEntity()
	})
	flecs.SetTransitive(w, r)

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(a, flecs.MakePair(r, b))
		fw.AddID(b, flecs.MakePair(r, a))
	})

	// Query for (R, c) — neither a nor b reaches c; the cycle guard must fire.
	matched := false
	q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(r, c)))
	it := q.Iter()
	for it.Next() {
		for _, e := range it.Entities() {
			if e == a || e == b {
				matched = true
			}
		}
	}
	if matched {
		t.Error("expected neither a nor b to match (R,c) — c is not reachable via the cycle")
	}
}

// BenchmarkTransitiveQuery_ChainLen10 measures the walk cost for a 10-entity
// chain with a transitive relationship.
func BenchmarkTransitiveQuery_ChainLen10(b *testing.B) {
	const chainLen = 10
	w := flecs.New()
	entities := make([]flecs.ID, chainLen+1)
	var r flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		for i := range entities {
			entities[i] = fw.NewEntity()
		}
	})
	flecs.SetTransitive(w, r)

	// Build chain: entities[0] R entities[1] R ... R entities[chainLen]
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < chainLen; i++ {
			fw.AddID(entities[i], flecs.MakePair(r, entities[i+1]))
		}
	})

	target := entities[chainLen]
	b.ResetTimer()
	for range b.N {
		count := 0
		q := flecs.NewQueryFromTerms(w, flecs.With(flecs.MakePair(r, target)))
		it := q.Iter()
		for it.Next() {
			count += it.Count()
		}
		_ = count
	}
}
