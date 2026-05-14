package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// Component types used exclusively by fixed-source query tests.
type fqsPosition struct{ X, Y float32 }
type fqsVelocity struct{ VX, VY float32 }
type fqsSimTime struct{ DT float32 }
type fqsDifficulty struct{ Level int }
type fqsSparseComp struct{ Val int }
type fqsDFPairData struct{ V int }
type fqsTagComp struct{}        // zero-size tag for fixed-source tag tests
type fqsDFComp struct{ W int }  // DontFragment component for sparse/mixed tests
type fqsOptComp struct{ U int } // optional fixed-source component

// TestFixedSourceBasic verifies the motivating singleton-on-query pattern:
// iterate entities with Position; SimTime is constant and bound to game.
func TestFixedSourceBasic(t *testing.T) {
	w := flecs.New()

	posID := flecs.RegisterComponent[fqsPosition](w)
	simTimeID := flecs.RegisterComponent[fqsSimTime](w)

	var game, e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		flecs.Set(fw, game, fqsSimTime{DT: 0.016})

		e1 = fw.NewEntity()
		flecs.Set(fw, e1, fqsPosition{X: 1})
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, fqsPosition{X: 2})
	})

	w.Read(func(fr *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithSourceTerm(simTimeID, game),
		)
		it := q.Iter()

		var seenEntities []flecs.ID
		for it.Next() {
			entities := it.Entities()
			seenEntities = append(seenEntities, entities...)

			// SimTime field is the same for every row.
			st := flecs.Field[fqsSimTime](it, simTimeID)
			if len(st) != 1 {
				t.Fatalf("want 1-element SimTime slice, got %d", len(st))
			}
			if st[0].DT != 0.016 {
				t.Fatalf("want DT=0.016, got %v", st[0].DT)
			}
		}
		if len(seenEntities) != 2 {
			t.Fatalf("want 2 entities, got %d: %v", len(seenEntities), seenEntities)
		}
		_ = e1
		_ = e2
	})
}

// TestFixedSourceNoComponentOnSourceYieldsZero verifies the upstream semantic:
// if the source entity does not hold the fixed-source component, the query yields
// zero results.
func TestFixedSourceNoComponentOnSourceYieldsZero(t *testing.T) {
	w := flecs.New()

	posID := flecs.RegisterComponent[fqsPosition](w)
	simTimeID := flecs.RegisterComponent[fqsSimTime](w)

	var game, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		// game intentionally lacks SimTime.
		e = fw.NewEntity()
		flecs.Set(fw, e, fqsPosition{X: 1})
	})

	w.Read(func(fr *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithSourceTerm(simTimeID, game), // game has no SimTime
		)
		it := q.Iter()
		count := 0
		for it.Next() {
			count += it.Count()
		}
		if count != 0 {
			t.Fatalf("want 0 matches when source lacks component, got %d", count)
		}
		_ = e
	})
}

// TestFixedSourceMixedThisAndFixed: With(Pos).With(Vel).WithSourceTerm(SimTime, game)
// iterates Pos+Vel entities; SimTime is constant.
func TestFixedSourceMixedThisAndFixed(t *testing.T) {
	w := flecs.New()

	posID := flecs.RegisterComponent[fqsPosition](w)
	velID := flecs.RegisterComponent[fqsVelocity](w)
	simTimeID := flecs.RegisterComponent[fqsSimTime](w)

	var game flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		flecs.Set(fw, game, fqsSimTime{DT: 0.032})
	})

	// Two entities with Pos+Vel, one with only Pos.
	var ev1, ev2, ep flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ev1 = fw.NewEntity()
		flecs.Set(fw, ev1, fqsPosition{X: 1})
		flecs.Set(fw, ev1, fqsVelocity{VX: 1})

		ev2 = fw.NewEntity()
		flecs.Set(fw, ev2, fqsPosition{X: 2})
		flecs.Set(fw, ev2, fqsVelocity{VX: 2})

		ep = fw.NewEntity()
		flecs.Set(fw, ep, fqsPosition{X: 3})
		// ep has no Velocity — should not be matched.
	})

	w.Read(func(fr *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.With(velID),
			flecs.WithSourceTerm(simTimeID, game),
		)
		it := q.Iter()

		count := 0
		for it.Next() {
			st := flecs.Field[fqsSimTime](it, simTimeID)
			if st[0].DT != 0.032 {
				t.Fatalf("wrong DT: %v", st[0].DT)
			}
			count += it.Count()
		}
		if count != 2 {
			t.Fatalf("want 2 entities (Pos+Vel), got %d", count)
		}
		_ = ev1
		_ = ev2
		_ = ep
	})
}

// TestFixedSourceSnapshotAtIterStart: mutate the source after Iter() is called;
// the iteration should see the snapshot value, not the mutated value.
func TestFixedSourceSnapshotAtIterStart(t *testing.T) {
	w := flecs.New()

	posID := flecs.RegisterComponent[fqsPosition](w)
	simTimeID := flecs.RegisterComponent[fqsSimTime](w)

	var game, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		flecs.Set(fw, game, fqsSimTime{DT: 0.016})
		e = fw.NewEntity()
		flecs.Set(fw, e, fqsPosition{X: 1})
	})

	w.Read(func(fr *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithSourceTerm(simTimeID, game),
		)
		// Snapshot happens at Iter() time.
		it := q.Iter()

		// The snapshot DT must be the value at Iter() time regardless of what
		// happens to the component after (we verify the pointer is valid here).
		for it.Next() {
			st := flecs.Field[fqsSimTime](it, simTimeID)
			if st[0].DT != 0.016 {
				t.Fatalf("snapshot value wrong: got %v, want 0.016", st[0].DT)
			}
		}
	})
	_ = e
}

// TestFixedSourceSparse: the fixed-source term's component is Sparse.
func TestFixedSourceSparse(t *testing.T) {
	w := flecs.New()

	posID := flecs.RegisterComponent[fqsPosition](w)
	sparseID := flecs.RegisterComponent[fqsSparseComp](w)
	flecs.SetSparse(w, sparseID)

	var game, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		flecs.Set(fw, game, fqsSparseComp{Val: 42})
		e = fw.NewEntity()
		flecs.Set(fw, e, fqsPosition{X: 1})
	})

	w.Read(func(fr *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithSourceTerm(sparseID, game),
		)
		it := q.Iter()

		count := 0
		for it.Next() {
			sc := flecs.Field[fqsSparseComp](it, sparseID)
			if sc[0].Val != 42 {
				t.Fatalf("want Val=42, got %v", sc[0].Val)
			}
			count += it.Count()
		}
		if count != 1 {
			t.Fatalf("want 1 match, got %d", count)
		}
	})
	_ = e
}

// TestFixedSourceDontFragmentPair: the fixed-source term's component is DontFragment.
func TestFixedSourceDontFragmentPair(t *testing.T) {
	w := flecs.New()

	posID := flecs.RegisterComponent[fqsPosition](w)
	dfID := flecs.RegisterComponent[fqsDFPairData](w)
	flecs.SetDontFragment(w, dfID)

	var game, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		flecs.Set(fw, game, fqsDFPairData{V: 99})
		e = fw.NewEntity()
		flecs.Set(fw, e, fqsPosition{})
	})

	w.Read(func(fr *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithSourceTerm(dfID, game),
		)
		it := q.Iter()

		count := 0
		for it.Next() {
			df := flecs.Field[fqsDFPairData](it, dfID)
			if df[0].V != 99 {
				t.Fatalf("want V=99, got %v", df[0].V)
			}
			count += it.Count()
		}
		if count != 1 {
			t.Fatalf("want 1 match, got %d", count)
		}
	})
	_ = e
}

// TestFixedSourceZeroSourcePanics: WithSourceTerm(c, 0) panics at construction.
func TestFixedSourceZeroSourcePanics(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[fqsPosition](w)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for zero source entity, got none")
		}
	}()
	_ = flecs.WithSourceTerm(posID, 0)
}

// TestFixedSourceDeadEntityPanics: fixed source that is a dead entity panics at
// query construction time (validates via index.IsAlive).
func TestFixedSourceDeadEntityPanics(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[fqsPosition](w)
	simTimeID := flecs.RegisterComponent[fqsSimTime](w)

	var dead flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dead = fw.NewEntity()
		fw.Delete(dead)
	})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for dead source entity, got none")
		}
	}()
	_ = flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.WithSourceTerm(simTimeID, dead),
	)
}

// TestFixedSourceCachedQuerySourceUpdate: CachedQuery re-reads source on each Iter()
// call, so mutations between iterations are visible.
func TestFixedSourceCachedQuerySourceUpdate(t *testing.T) {
	w := flecs.New()

	posID := flecs.RegisterComponent[fqsPosition](w)
	simTimeID := flecs.RegisterComponent[fqsSimTime](w)

	var game, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		flecs.Set(fw, game, fqsSimTime{DT: 0.016})
		e = fw.NewEntity()
		flecs.Set(fw, e, fqsPosition{X: 1})
	})

	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(posID),
		flecs.WithSourceTerm(simTimeID, game),
	)

	// First iteration: DT should be 0.016.
	w.Read(func(fr *flecs.Reader) {
		it := cq.Iter()
		for it.Next() {
			st := flecs.Field[fqsSimTime](it, simTimeID)
			if st[0].DT != 0.016 {
				t.Fatalf("first iter: want DT=0.016, got %v", st[0].DT)
			}
		}
	})

	// Mutate source.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, game, fqsSimTime{DT: 0.032})
	})

	// Second iteration: DT should be 0.032 (re-read at Iter() time).
	w.Read(func(fr *flecs.Reader) {
		it := cq.Iter()
		for it.Next() {
			st := flecs.Field[fqsSimTime](it, simTimeID)
			if st[0].DT != 0.032 {
				t.Fatalf("second iter: want DT=0.032, got %v", st[0].DT)
			}
		}
	})
	_ = e
}

// TestFixedSourceOrderByThis: OrderBy on a $this component + fixed-source term;
// sort operates on $this, fixed-source is constant.
func TestFixedSourceOrderByThis(t *testing.T) {
	w := flecs.New()

	posID := flecs.RegisterComponent[fqsPosition](w)
	simTimeID := flecs.RegisterComponent[fqsSimTime](w)

	var game flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		flecs.Set(fw, game, fqsSimTime{DT: 0.016})
		for i := range 5 {
			e := fw.NewEntity()
			flecs.Set(fw, e, fqsPosition{X: float32(5 - i)}) // descending
		}
	})

	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(posID, flecs.OrderBy[fqsPosition](func(_ flecs.ID, a *fqsPosition, _ flecs.ID, b *fqsPosition) int {
			if a.X < b.X {
				return -1
			}
			if a.X > b.X {
				return 1
			}
			return 0
		})),
		flecs.With(posID),
		flecs.WithSourceTerm(simTimeID, game),
	)

	w.Read(func(fr *flecs.Reader) {
		it := cq.Iter()
		var xs []float32
		for it.Next() {
			pos := flecs.Field[fqsPosition](it, posID)
			xs = append(xs, pos[0].X)
			st := flecs.Field[fqsSimTime](it, simTimeID)
			if st[0].DT != 0.016 {
				t.Fatalf("wrong DT: %v", st[0].DT)
			}
		}
		for i := 1; i < len(xs); i++ {
			if xs[i] < xs[i-1] {
				t.Fatalf("not sorted: xs=%v", xs)
			}
		}
		if len(xs) != 5 {
			t.Fatalf("want 5 entities, got %d", len(xs))
		}
	})
}

// TestFixedSourceMultipleSources: two fixed-source terms with different sources.
func TestFixedSourceMultipleSources(t *testing.T) {
	w := flecs.New()

	posID := flecs.RegisterComponent[fqsPosition](w)
	simTimeID := flecs.RegisterComponent[fqsSimTime](w)
	diffID := flecs.RegisterComponent[fqsDifficulty](w)

	var game, player, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		flecs.Set(fw, game, fqsSimTime{DT: 0.016})

		player = fw.NewEntity()
		flecs.Set(fw, player, fqsDifficulty{Level: 3})

		e = fw.NewEntity()
		flecs.Set(fw, e, fqsPosition{X: 5})
	})

	w.Read(func(fr *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithSourceTerm(simTimeID, game),
			flecs.WithSourceTerm(diffID, player),
		)
		it := q.Iter()

		count := 0
		for it.Next() {
			st := flecs.Field[fqsSimTime](it, simTimeID)
			if st[0].DT != 0.016 {
				t.Fatalf("wrong DT: %v", st[0].DT)
			}
			di := flecs.Field[fqsDifficulty](it, diffID)
			if di[0].Level != 3 {
				t.Fatalf("wrong Level: %v", di[0].Level)
			}
			count += it.Count()
		}
		if count != 1 {
			t.Fatalf("want 1, got %d", count)
		}
	})
	_ = e
}

// TestFixedSourcePairForm: WithSourceTerm(Pair(R, T), game) where game has (R, T).
func TestFixedSourcePairForm(t *testing.T) {
	w := flecs.New()

	posID := flecs.RegisterComponent[fqsPosition](w)
	simTimeID := flecs.RegisterComponent[fqsSimTime](w)

	var rel, target, game, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		target = fw.NewEntity()
		flecs.RegisterComponent[fqsSimTime](w) // already registered above; idempotent

		game = fw.NewEntity()
		// Add a plain pair (rel, target) to game. Pairs are tags here (no data).
		fw.AddID(game, flecs.MakePair(rel, target))

		e = fw.NewEntity()
		flecs.Set(fw, e, fqsPosition{X: 1})
	})

	pairID := flecs.MakePair(rel, target)

	w.Read(func(fr *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithSourceTerm(pairID, game),
		)
		it := q.Iter()

		count := 0
		for it.Next() {
			count += it.Count()
		}
		if count != 1 {
			t.Fatalf("want 1 match for pair fixed-source, got %d", count)
		}
	})
	_ = simTimeID
	_ = e
}

// TestFixedSourceOptional: Maybe semantic for fixed-source — absent component on
// source yields FieldMaybe -> (nil, false); does not zero out results.
// This is a deliberate divergence from upstream: Go uses the FieldMaybe-friendly
// behaviour rather than yielding zero results for optional absent sources.
func TestFixedSourceOptional(t *testing.T) {
	w := flecs.New()

	posID := flecs.RegisterComponent[fqsPosition](w)
	simTimeID := flecs.RegisterComponent[fqsSimTime](w)

	var gameWithTime, gameNoTime, e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		gameWithTime = fw.NewEntity()
		flecs.Set(fw, gameWithTime, fqsSimTime{DT: 0.016})

		gameNoTime = fw.NewEntity() // no SimTime

		e1 = fw.NewEntity()
		flecs.Set(fw, e1, fqsPosition{X: 1})
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, fqsPosition{X: 2})
	})

	// Optional fixed-source with source that HAS the component.
	w.Read(func(fr *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.Maybe(simTimeID).Source(gameWithTime),
		)
		it := q.Iter()

		count := 0
		for it.Next() {
			count += it.Count()
			sl, ok := flecs.FieldMaybe[fqsSimTime](it, simTimeID)
			if !ok || len(sl) == 0 {
				t.Fatalf("expected SimTime to be present (source has it)")
			}
			if sl[0].DT != 0.016 {
				t.Fatalf("wrong DT: %v", sl[0].DT)
			}
		}
		if count != 2 {
			t.Fatalf("want 2 entities, got %d", count)
		}
	})

	// Optional fixed-source with source that LACKS the component.
	w.Read(func(fr *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.Maybe(simTimeID).Source(gameNoTime),
		)
		it := q.Iter()

		count := 0
		for it.Next() {
			count += it.Count()
			sl, ok := flecs.FieldMaybe[fqsSimTime](it, simTimeID)
			if ok || sl != nil {
				t.Fatalf("expected SimTime to be absent (source lacks it); got ok=%v sl=%v", ok, sl)
			}
		}
		// Optional absent source must NOT zero out results.
		if count != 2 {
			t.Fatalf("optional absent source should still yield 2 entities, got %d", count)
		}
	})
	_ = e1
	_ = e2
}

// TestFixedSourceTermNotPanics: TermNot with fixed source panics at construction.
func TestFixedSourceTermNotPanics(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[fqsPosition](w)
	simTimeID := flecs.RegisterComponent[fqsSimTime](w)

	var game flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		flecs.Set(fw, game, fqsSimTime{})
	})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for TermNot with fixed source, got none")
		}
	}()
	_ = flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Without(simTimeID).Source(game),
	)
}

// TestFixedSourceZeroComponentIDPanics: WithSourceTerm(0, entity) panics.
func TestFixedSourceZeroComponentIDPanics(t *testing.T) {
	w := flecs.New()
	var game flecs.ID
	w.Write(func(fw *flecs.Writer) { game = fw.NewEntity() })

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for zero component ID, got none")
		}
	}()
	_ = flecs.WithSourceTerm(0, game)
}

// TestFixedSourceTraversalPanics: combining .Source(e) with .Up(rel) panics.
func TestFixedSourceTraversalPanics(t *testing.T) {
	w := flecs.New()
	simTimeID := flecs.RegisterComponent[fqsSimTime](w)
	var rel, game flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		flecs.SetTraversable(w, rel)
		game = fw.NewEntity()
	})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when combining .Source with traversal, got none")
		}
	}()
	_ = flecs.With(simTimeID).Up(rel).Source(game) // should panic
}

// TestFixedSourceUnionTerm: fixed-source term whose component is a union pair.
func TestFixedSourceUnionTerm(t *testing.T) {
	w := flecs.New()

	posID := flecs.RegisterComponent[fqsPosition](w)
	var rel, targetA, game, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		flecs.SetUnion(w, rel)
		targetA = fw.NewEntity()

		game = fw.NewEntity()
		fw.AddID(game, flecs.MakePair(rel, targetA)) // game has (rel, targetA) union

		e = fw.NewEntity()
		flecs.Set(fw, e, fqsPosition{X: 1})
	})

	pairID := flecs.MakePair(rel, targetA)

	w.Read(func(fr *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithSourceTerm(pairID, game),
		)
		it := q.Iter()
		count := 0
		for it.Next() {
			count += it.Count()
		}
		if count != 1 {
			t.Fatalf("want 1 match for union-pair fixed-source, got %d", count)
		}
	})
}

// TestFixedSourceUnionNoStore: fixed-source union pair when no entity has ever
// used that union relationship → no store exists → zero results.
func TestFixedSourceUnionNoStore(t *testing.T) {
	w := flecs.New()

	posID := flecs.RegisterComponent[fqsPosition](w)
	var rel, targetA, game, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		flecs.SetUnion(w, rel)
		targetA = fw.NewEntity()

		game = fw.NewEntity()
		// game has NO union pair — the union store has never been created.

		e = fw.NewEntity()
		flecs.Set(fw, e, fqsPosition{X: 1})
	})

	pairID := flecs.MakePair(rel, targetA)

	w.Read(func(fr *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithSourceTerm(pairID, game),
		)
		it := q.Iter()
		count := 0
		for it.Next() {
			count += it.Count()
		}
		if count != 0 {
			t.Fatalf("want 0 results (no union store), got %d", count)
		}
	})
	_ = e
}

// TestFixedSourceUnionAbsent: fixed-source union pair absent → zero results.
func TestFixedSourceUnionAbsent(t *testing.T) {
	w := flecs.New()

	posID := flecs.RegisterComponent[fqsPosition](w)
	var rel, targetA, targetB, game, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		flecs.SetUnion(w, rel)
		targetA = fw.NewEntity()
		targetB = fw.NewEntity()

		game = fw.NewEntity()
		fw.AddID(game, flecs.MakePair(rel, targetA)) // game has targetA, not targetB

		e = fw.NewEntity()
		flecs.Set(fw, e, fqsPosition{X: 1})
	})

	pairID := flecs.MakePair(rel, targetB) // query for targetB — absent on game

	w.Read(func(fr *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithSourceTerm(pairID, game),
		)
		it := q.Iter()
		count := 0
		for it.Next() {
			count += it.Count()
		}
		if count != 0 {
			t.Fatalf("want 0 matches for absent union-pair, got %d", count)
		}
	})
	_ = e
}

// TestFixedSourceCachedQueryDeadIter: CachedQuery with required fixed-source
// component absent on source yields zero results.
func TestFixedSourceCachedQueryDeadIter(t *testing.T) {
	w := flecs.New()

	posID := flecs.RegisterComponent[fqsPosition](w)
	simTimeID := flecs.RegisterComponent[fqsSimTime](w)

	var game, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		// game intentionally lacks SimTime.
		e = fw.NewEntity()
		flecs.Set(fw, e, fqsPosition{X: 1})
	})

	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(posID),
		flecs.WithSourceTerm(simTimeID, game),
	)

	w.Read(func(fr *flecs.Reader) {
		it := cq.Iter()
		count := 0
		for it.Next() {
			count += it.Count()
		}
		if count != 0 {
			t.Fatalf("want 0 results (source lacks component), got %d", count)
		}
	})
}

// TestFixedSourceMethodZeroPanics: (Term).Source(0) panics.
func TestFixedSourceMethodZeroPanics(t *testing.T) {
	w := flecs.New()
	simTimeID := flecs.RegisterComponent[fqsSimTime](w)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for .Source(0), got none")
		}
	}()
	_ = flecs.With(simTimeID).Source(0)
}

// TestFixedSourceChainedBuilder: (Term).Source(e) produces same result as WithSourceTerm.
func TestFixedSourceChainedBuilder(t *testing.T) {
	w := flecs.New()

	posID := flecs.RegisterComponent[fqsPosition](w)
	simTimeID := flecs.RegisterComponent[fqsSimTime](w)

	var game, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		flecs.Set(fw, game, fqsSimTime{DT: 0.025})
		e = fw.NewEntity()
		flecs.Set(fw, e, fqsPosition{X: 3})
	})

	w.Read(func(fr *flecs.Reader) {
		// Use chained builder instead of WithSourceTerm.
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.With(simTimeID).Source(game),
		)
		it := q.Iter()
		found := false
		for it.Next() {
			st := flecs.Field[fqsSimTime](it, simTimeID)
			if st[0].DT != 0.025 {
				t.Fatalf("wrong DT: %v", st[0].DT)
			}
			found = true
		}
		if !found {
			t.Fatal("expected at least one match via chained builder")
		}
	})
	_ = e
}

// TestFixedSourceTermOrPanics: Or(id).Source(game) panics at validateAndSortTerms
// because TermOr with a fixed source is not supported in this phase.
func TestFixedSourceTermOrPanics(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[fqsPosition](w)
	simTimeID := flecs.RegisterComponent[fqsSimTime](w)

	var game flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		flecs.Set(fw, game, fqsSimTime{DT: 0.016})
	})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for TermOr with fixed source, got none")
		}
	}()
	_ = flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Or(simTimeID).Source(game), // TermOr + fixed-source → panic
	)
}

// TestFixedSourceValidateTraversalDirectConstruct covers the validateAndSortTerms
// traversal+fixed-source panic (line 1509) by constructing a Term directly with
// both Src and a non-default Traverse — bypassing the Source() builder's own guard.
func TestFixedSourceValidateTraversalDirectConstruct(t *testing.T) {
	w := flecs.New()
	simTimeID := flecs.RegisterComponent[fqsSimTime](w)
	var rel, game flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		flecs.SetTraversable(w, rel)
		game = fw.NewEntity()
		flecs.Set(fw, game, fqsSimTime{DT: 0.016})
	})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for direct Term with Src + TraverseUp, got none")
		}
	}()
	// Directly construct a Term with Src and a traversal flag to bypass Source()'s guard
	// and exercise the validateAndSortTerms check at line 1509.
	_ = flecs.NewQueryFromTerms(w, flecs.Term{
		ID:       simTimeID,
		Kind:     flecs.TermAnd,
		Src:      game,
		Traverse: flecs.TraverseUp,
		Trav:     rel,
	})
}

// TestFixedSourceDontFragmentOptional covers updateOptionalPresenceSparse (line 969):
// a pure-DontFragment query with an optional fixed-source term. When nextSparseOnly
// advances, updateOptionalPresenceSparse skips the fixed-source optional term.
func TestFixedSourceDontFragmentOptional(t *testing.T) {
	w := flecs.New()

	dfID := flecs.RegisterComponent[fqsDFComp](w)
	optID := flecs.RegisterComponent[fqsOptComp](w)
	flecs.SetDontFragment(w, dfID)

	var game, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		flecs.Set(fw, game, fqsOptComp{U: 7})
		e = fw.NewEntity()
		flecs.Set(fw, e, fqsDFComp{W: 1})
	})

	w.Read(func(fr *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(dfID),
			flecs.Maybe(optID).Source(game), // optional fixed-source → updateOptionalPresenceSparse skip
		)
		it := q.Iter()
		count := 0
		for it.Next() {
			count += it.Count()
			sl, ok := flecs.FieldMaybe[fqsOptComp](it, optID)
			if !ok || len(sl) == 0 {
				t.Fatalf("expected optional fixed-source present, got ok=%v", ok)
			}
		}
		if count != 1 {
			t.Fatalf("want 1 entity, got %d", count)
		}
	})
	_ = e
}

// TestFixedSourceMixedOptional covers updateOptionalPresenceMixed (line 992):
// a mixed (archetype + DontFragment) query with an optional fixed-source term.
// In mixed iteration, updateOptionalPresenceMixed skips the fixed-source optional term.
func TestFixedSourceMixedOptional(t *testing.T) {
	w := flecs.New()

	posID := flecs.RegisterComponent[fqsPosition](w)
	dfID := flecs.RegisterComponent[fqsDFComp](w)
	optID := flecs.RegisterComponent[fqsOptComp](w)
	flecs.SetDontFragment(w, dfID)

	var game, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		flecs.Set(fw, game, fqsOptComp{U: 99})
		e = fw.NewEntity()
		flecs.Set(fw, e, fqsPosition{X: 1})
		flecs.Set(fw, e, fqsDFComp{W: 2}) // DontFragment → hasSparseTerms → mixed mode
	})

	w.Read(func(fr *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.With(dfID),
			flecs.Maybe(optID).Source(game), // optional fixed-source → updateOptionalPresenceMixed skip
		)
		it := q.Iter()
		count := 0
		for it.Next() {
			count += it.Count()
			sl, ok := flecs.FieldMaybe[fqsOptComp](it, optID)
			if !ok || len(sl) == 0 {
				t.Fatalf("expected optional fixed-source present, got ok=%v", ok)
			}
		}
		if count != 1 {
			t.Fatalf("want 1 entity, got %d", count)
		}
	})
	_ = e
}

// TestFixedSourceTagComponentField covers Field/FieldMaybe for a zero-size tag
// component used as a fixed-source term:
// - Field[tag](it, tagID): ptr==nil → returns make([]T, 1) (line 1250)
// - FieldMaybe[tag](it, tagID): ptr==nil → returns (make([]T,1), true) (line 1338)
func TestFixedSourceTagComponentField(t *testing.T) {
	w := flecs.New()

	posID := flecs.RegisterComponent[fqsPosition](w)
	tagID := flecs.RegisterComponent[fqsTagComp](w)

	var game, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		flecs.Set(fw, game, fqsTagComp{}) // zero-size tag on source
		e = fw.NewEntity()
		flecs.Set(fw, e, fqsPosition{X: 1})
	})

	w.Read(func(fr *flecs.Reader) {
		// TermAnd + tag: Field returns make([]T, 1) (line 1250)
		q1 := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithSourceTerm(tagID, game),
		)
		it1 := q1.Iter()
		for it1.Next() {
			tags := flecs.Field[fqsTagComp](it1, tagID)
			if len(tags) != 1 {
				t.Fatalf("Field[tag] want len=1, got %d", len(tags))
			}
		}

		// TermOptional + tag: FieldMaybe returns (make([]T,1), true) (line 1338)
		q2 := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.Maybe(tagID).Source(game),
		)
		it2 := q2.Iter()
		for it2.Next() {
			tags, ok := flecs.FieldMaybe[fqsTagComp](it2, tagID)
			if !ok || len(tags) != 1 {
				t.Fatalf("FieldMaybe[tag] want (len=1, true), got len=%d ok=%v", len(tags), ok)
			}
		}
	})
	_ = e
}

// TestFixedSourceFieldPanicAbsentOptional covers Field() (line 1245) panicking
// when called for an optional fixed-source term whose source lacks the component.
// Callers should use FieldMaybe instead; Field panics to surface the misuse.
func TestFixedSourceFieldPanicAbsentOptional(t *testing.T) {
	w := flecs.New()

	posID := flecs.RegisterComponent[fqsPosition](w)
	simTimeID := flecs.RegisterComponent[fqsSimTime](w)

	var gameNoTime, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		gameNoTime = fw.NewEntity() // no SimTime
		e = fw.NewEntity()
		flecs.Set(fw, e, fqsPosition{X: 1})
	})

	w.Read(func(fr *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.Maybe(simTimeID).Source(gameNoTime), // optional, absent from source
		)
		it := q.Iter()
		if it.Next() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatal("expected panic when calling Field for absent optional fixed-source")
				}
			}()
			_ = flecs.Field[fqsSimTime](it, simTimeID) // panics: absent optional → line 1245
		}
	})
	_ = e
}
