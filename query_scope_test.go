package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// Component types used exclusively by query scope tests.
type qsPosition struct{ X, Y float32 }
type qsVelocity struct{ VX, VY float32 }
type qsSpeed struct{ S float32 }
type qsFrozen struct{}
type qsAlpha struct{}
type qsBeta struct{}
type qsGamma struct{}
type qsConfig struct{ Value int }
type qsSparseComp struct{ Val int }
type qsDFComp struct{ W int } // DontFragment component

// helpers to collect entity IDs from a Query / CachedQuery iterator.
func qsCollect(q *flecs.Query) []flecs.ID {
	it := q.Iter()
	var out []flecs.ID
	for it.Next() {
		out = append(out, it.Entities()...)
	}
	return out
}

func qsCollectCQ(cq *flecs.CachedQuery) []flecs.ID {
	it := cq.Iter()
	var out []flecs.ID
	for it.Next() {
		out = append(out, it.Entities()...)
	}
	return out
}

func contains(ids []flecs.ID, id flecs.ID) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

// Test 1: Position AND NOT (Velocity OR Speed).
// b.With(vel).Or(speed) → sub-terms [TermAnd(vel), TermOr(speed)] = vel OR speed.
// NOT(vel OR speed) = !vel AND !speed.
func TestWithoutScope_NotVelocityOrSpeed(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qsPosition](w)
	velID := flecs.RegisterComponent[qsVelocity](w)
	spdID := flecs.RegisterComponent[qsSpeed](w)

	var ePosOnly, ePosVel, ePosSpd, ePosVelSpd flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ePosOnly = fw.NewEntity()
		flecs.Set(fw, ePosOnly, qsPosition{})

		ePosVel = fw.NewEntity()
		flecs.Set(fw, ePosVel, qsPosition{})
		flecs.Set(fw, ePosVel, qsVelocity{})

		ePosSpd = fw.NewEntity()
		flecs.Set(fw, ePosSpd, qsPosition{})
		flecs.Set(fw, ePosSpd, qsSpeed{})

		ePosVelSpd = fw.NewEntity()
		flecs.Set(fw, ePosVelSpd, qsPosition{})
		flecs.Set(fw, ePosVelSpd, qsVelocity{})
		flecs.Set(fw, ePosVelSpd, qsSpeed{})
	})

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
				b.With(velID).Or(spdID)
			}),
		)
		got := qsCollect(q)

		// Only ePosOnly should match: has Position, has neither Velocity nor Speed.
		if !contains(got, ePosOnly) {
			t.Errorf("expected ePosOnly in results")
		}
		if contains(got, ePosVel) {
			t.Errorf("did not expect ePosVel (has Velocity)")
		}
		if contains(got, ePosSpd) {
			t.Errorf("did not expect ePosSpd (has Speed)")
		}
		if contains(got, ePosVelSpd) {
			t.Errorf("did not expect ePosVelSpd (has both)")
		}
	})
}

// Test 2: De-Morgan equivalence — NOT(V OR S) == Without(V) + Without(S).
func TestWithoutScope_DeMorganEquivalence(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qsPosition](w)
	velID := flecs.RegisterComponent[qsVelocity](w)
	spdID := flecs.RegisterComponent[qsSpeed](w)

	var ePosOnly, ePosVel, ePosSpd, ePosVelSpd flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ePosOnly = fw.NewEntity()
		flecs.Set(fw, ePosOnly, qsPosition{})

		ePosVel = fw.NewEntity()
		flecs.Set(fw, ePosVel, qsPosition{})
		flecs.Set(fw, ePosVel, qsVelocity{})

		ePosSpd = fw.NewEntity()
		flecs.Set(fw, ePosSpd, qsPosition{})
		flecs.Set(fw, ePosSpd, qsSpeed{})

		ePosVelSpd = fw.NewEntity()
		flecs.Set(fw, ePosVelSpd, qsPosition{})
		flecs.Set(fw, ePosVelSpd, qsVelocity{})
		flecs.Set(fw, ePosVelSpd, qsSpeed{})
	})

	w.Read(func(_ *flecs.Reader) {
		scope := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
				b.With(velID).Or(spdID)
			}),
		)
		flat := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.Without(velID),
			flecs.Without(spdID),
		)
		gotScope := qsCollect(scope)
		gotFlat := qsCollect(flat)

		// Both should yield the same set.
		if len(gotScope) != len(gotFlat) {
			t.Errorf("scope len=%d, flat len=%d; expected equal", len(gotScope), len(gotFlat))
		}
		for _, id := range []flecs.ID{ePosOnly, ePosVel, ePosSpd, ePosVelSpd} {
			inScope := contains(gotScope, id)
			inFlat := contains(gotFlat, id)
			if inScope != inFlat {
				t.Errorf("entity %d: scope=%v flat=%v; must agree", id, inScope, inFlat)
			}
		}
	})
}

// Test 3: Position AND NOT (Velocity AND Speed). Result set differs from test 2.
// b.With(vel).With(speed) → NOT(vel AND speed) = !vel OR !speed.
// Entities with only vel or only spd should match; entity with both should NOT match.
func TestWithoutScope_NotVelocityAndSpeed(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qsPosition](w)
	velID := flecs.RegisterComponent[qsVelocity](w)
	spdID := flecs.RegisterComponent[qsSpeed](w)

	var ePosOnly, ePosVel, ePosSpd, ePosVelSpd flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ePosOnly = fw.NewEntity()
		flecs.Set(fw, ePosOnly, qsPosition{})

		ePosVel = fw.NewEntity()
		flecs.Set(fw, ePosVel, qsPosition{})
		flecs.Set(fw, ePosVel, qsVelocity{})

		ePosSpd = fw.NewEntity()
		flecs.Set(fw, ePosSpd, qsPosition{})
		flecs.Set(fw, ePosSpd, qsSpeed{})

		ePosVelSpd = fw.NewEntity()
		flecs.Set(fw, ePosVelSpd, qsPosition{})
		flecs.Set(fw, ePosVelSpd, qsVelocity{})
		flecs.Set(fw, ePosVelSpd, qsSpeed{})
	})

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
				b.With(velID).With(spdID)
			}),
		)
		got := qsCollect(q)

		// ePosOnly, ePosVel, ePosSpd should match (they lack at least one of vel/spd).
		if !contains(got, ePosOnly) {
			t.Errorf("expected ePosOnly (has neither)")
		}
		if !contains(got, ePosVel) {
			t.Errorf("expected ePosVel (lacks Speed)")
		}
		if !contains(got, ePosSpd) {
			t.Errorf("expected ePosSpd (lacks Velocity)")
		}
		// ePosVelSpd has both → NOT(vel AND speed) = false → should NOT match.
		if contains(got, ePosVelSpd) {
			t.Errorf("did not expect ePosVelSpd (has both Velocity and Speed)")
		}
	})
}

// Test 4: Nested scope — Position AND NOT (Velocity AND NOT Frozen).
// Means: match entities with Position that either lack Velocity OR have Frozen.
func TestWithoutScope_NestedScope(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qsPosition](w)
	velID := flecs.RegisterComponent[qsVelocity](w)
	frozenID := flecs.RegisterComponent[qsFrozen](w)

	var ePosOnly, ePosVel, ePosVelFrozen, ePosFrozen flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ePosOnly = fw.NewEntity()
		flecs.Set(fw, ePosOnly, qsPosition{})

		ePosVel = fw.NewEntity()
		flecs.Set(fw, ePosVel, qsPosition{})
		flecs.Set(fw, ePosVel, qsVelocity{})

		ePosVelFrozen = fw.NewEntity()
		flecs.Set(fw, ePosVelFrozen, qsPosition{})
		flecs.Set(fw, ePosVelFrozen, qsVelocity{})
		flecs.AddID(fw, ePosVelFrozen, frozenID)

		ePosFrozen = fw.NewEntity()
		flecs.Set(fw, ePosFrozen, qsPosition{})
		flecs.AddID(fw, ePosFrozen, frozenID)
	})

	w.Read(func(_ *flecs.Reader) {
		// NOT(Velocity AND NOT Frozen) = !vel OR frozen
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
				b.With(velID).WithoutScope(func(b2 *flecs.ScopeBuilder) {
					b2.With(frozenID)
				})
			}),
		)
		got := qsCollect(q)

		// ePosOnly: !vel → matches.
		if !contains(got, ePosOnly) {
			t.Errorf("expected ePosOnly (lacks Velocity)")
		}
		// ePosVel: vel AND NOT frozen → NOT(vel AND NOT frozen) = false → no match.
		if contains(got, ePosVel) {
			t.Errorf("did not expect ePosVel (has Vel, no Frozen)")
		}
		// ePosVelFrozen: vel AND frozen → NOT(vel AND NOT frozen) = NOT(true AND false) = NOT(false) = true → matches.
		if !contains(got, ePosVelFrozen) {
			t.Errorf("expected ePosVelFrozen (has Vel and Frozen)")
		}
		// ePosFrozen: !vel → NOT(vel AND ...) = NOT(false) = true → matches.
		if !contains(got, ePosFrozen) {
			t.Errorf("expected ePosFrozen (lacks Velocity)")
		}
	})
}

// Test 5: Multi-Or inside scope — NOT (A OR B OR C).
func TestWithoutScope_MultiOr(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qsPosition](w)
	aID := flecs.RegisterComponent[qsAlpha](w)
	bID := flecs.RegisterComponent[qsBeta](w)
	gID := flecs.RegisterComponent[qsGamma](w)

	var ePos, ePosA, ePosB, ePosG, ePosAB flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ePos = fw.NewEntity()
		flecs.Set(fw, ePos, qsPosition{})

		ePosA = fw.NewEntity()
		flecs.Set(fw, ePosA, qsPosition{})
		flecs.AddID(fw, ePosA, aID)

		ePosB = fw.NewEntity()
		flecs.Set(fw, ePosB, qsPosition{})
		flecs.AddID(fw, ePosB, bID)

		ePosG = fw.NewEntity()
		flecs.Set(fw, ePosG, qsPosition{})
		flecs.AddID(fw, ePosG, gID)

		ePosAB = fw.NewEntity()
		flecs.Set(fw, ePosAB, qsPosition{})
		flecs.AddID(fw, ePosAB, aID)
		flecs.AddID(fw, ePosAB, bID)
	})

	w.Read(func(_ *flecs.Reader) {
		// NOT (A OR B OR C) = !A AND !B AND !C
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
				b.With(aID).Or(bID).Or(gID)
			}),
		)
		got := qsCollect(q)

		if !contains(got, ePos) {
			t.Errorf("expected ePos (has none of A/B/C)")
		}
		if contains(got, ePosA) {
			t.Errorf("did not expect ePosA")
		}
		if contains(got, ePosB) {
			t.Errorf("did not expect ePosB")
		}
		if contains(got, ePosG) {
			t.Errorf("did not expect ePosG")
		}
		if contains(got, ePosAB) {
			t.Errorf("did not expect ePosAB")
		}
	})
}

// Test 6: Empty scope panics at construction.
func TestWithoutScope_EmptyPanic(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("expected panic for empty scope; did not panic")
		}
	}()
	flecs.WithoutScope(func(_ *flecs.ScopeBuilder) {})
}

// Test 7: Sparse component inside scope.
func TestWithoutScope_SparseInner(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qsPosition](w)
	sparseID := flecs.RegisterComponent[qsSparseComp](w)

	flecs.SetSparse(w, sparseID)

	var ePos, ePosSpMissing, ePosSp flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ePos = fw.NewEntity()
		flecs.Set(fw, ePos, qsPosition{})

		// ePosSpMissing: has Position, no sparse component → scope inner (has sparse) = false → NOT = true → matches.
		ePosSpMissing = fw.NewEntity()
		flecs.Set(fw, ePosSpMissing, qsPosition{})

		// ePosSp: has Position + sparse → scope inner = true → NOT = false → no match.
		ePosSp = fw.NewEntity()
		flecs.Set(fw, ePosSp, qsPosition{})
		flecs.Set(fw, ePosSp, qsSparseComp{Val: 1})
	})

	w.Read(func(_ *flecs.Reader) {
		// NOT (has SparseComp): complex scope (Sparse inner term).
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
				b.With(sparseID)
			}),
		)
		got := qsCollect(q)

		if !contains(got, ePos) {
			t.Errorf("expected ePos (no sparse)")
		}
		if !contains(got, ePosSpMissing) {
			t.Errorf("expected ePosSpMissing (no sparse)")
		}
		if contains(got, ePosSp) {
			t.Errorf("did not expect ePosSp (has sparse)")
		}
	})
}

// Test 8: DontFragment component inside scope.
func TestWithoutScope_DontFragmentInner(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qsPosition](w)
	dfID := flecs.RegisterComponent[qsDFComp](w)

	flecs.SetDontFragment(w, dfID)

	var ePos, ePosDF flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ePos = fw.NewEntity()
		flecs.Set(fw, ePos, qsPosition{})

		ePosDF = fw.NewEntity()
		flecs.Set(fw, ePosDF, qsPosition{})
		flecs.Set(fw, ePosDF, qsDFComp{W: 42})
	})

	w.Read(func(_ *flecs.Reader) {
		// NOT (has DontFragment component): complex scope.
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
				b.With(dfID)
			}),
		)
		got := qsCollect(q)

		if !contains(got, ePos) {
			t.Errorf("expected ePos (no DontFragment)")
		}
		if contains(got, ePosDF) {
			t.Errorf("did not expect ePosDF (has DontFragment)")
		}
	})
}

// Test 9: Fixed-source term inside scope.
// WithoutScope(b => b.With(configID).Source(globalEntity)): passes when globalEntity
// does NOT have configID.
func TestWithoutScope_FixedSourceInner(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qsPosition](w)
	cfgID := flecs.RegisterComponent[qsConfig](w)

	var globalWith, globalWithout, eA, eB flecs.ID
	w.Write(func(fw *flecs.Writer) {
		globalWith = fw.NewEntity()
		flecs.Set(fw, globalWith, qsConfig{Value: 1})

		globalWithout = fw.NewEntity() // no Config

		eA = fw.NewEntity()
		flecs.Set(fw, eA, qsPosition{})

		eB = fw.NewEntity()
		flecs.Set(fw, eB, qsPosition{})
	})

	w.Read(func(_ *flecs.Reader) {
		// Scope inner expr: globalWith HAS Config → true → NOT = false → query yields nothing.
		qFail := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
				b.With(cfgID).Source(globalWith)
			}),
		)
		gotFail := qsCollect(qFail)
		if len(gotFail) != 0 {
			t.Errorf("expected 0 results when fixed-source has Config; got %d", len(gotFail))
		}

		// Scope inner expr: globalWithout has NO Config → false → NOT = true → all Position entities match.
		qPass := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
				b.With(cfgID).Source(globalWithout)
			}),
		)
		gotPass := qsCollect(qPass)
		if !contains(gotPass, eA) || !contains(gotPass, eB) {
			t.Errorf("expected eA and eB when fixed-source lacks Config; got %v", gotPass)
		}
	})
}

// Test 10: CachedQuery with scope — Changed() reflects mutations to scope-matched components.
func TestWithoutScope_CachedQueryChanged(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qsPosition](w)
	velID := flecs.RegisterComponent[qsVelocity](w)

	var ePosOnly, ePosVel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ePosOnly = fw.NewEntity()
		flecs.Set(fw, ePosOnly, qsPosition{X: 1})

		ePosVel = fw.NewEntity()
		flecs.Set(fw, ePosVel, qsPosition{X: 2})
		flecs.Set(fw, ePosVel, qsVelocity{VX: 1})
	})

	// Build cached query: Position AND NOT Velocity (simple scope, table-level fast path).
	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(posID),
		flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
			b.With(velID)
		}),
	)

	// First Changed() call → always true (initial state).
	if !cq.Changed() {
		t.Errorf("first Changed() should return true")
	}
	// Second call without mutation → false.
	if cq.Changed() {
		t.Errorf("second Changed() without mutation should return false")
	}

	// Verify only ePosOnly is matched.
	got := qsCollectCQ(cq)
	if !contains(got, ePosOnly) {
		t.Errorf("expected ePosOnly in cached query results")
	}
	if contains(got, ePosVel) {
		t.Errorf("did not expect ePosVel in cached query results")
	}

	// Mutate Position of ePosOnly → table's ChangeCount bumps → Changed() should flip.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, ePosOnly, qsPosition{X: 99})
	})
	if !cq.Changed() {
		t.Errorf("Changed() should return true after mutation")
	}
	if cq.Changed() {
		t.Errorf("Changed() should return false after being drained")
	}

	// Add Velocity to ePosOnly → it migrates out of the matched table; a new entity-less
	// Position-only table may remain. Either way, Changed() must report true.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, ePosOnly, qsVelocity{VX: 5})
	})
	if !cq.Changed() {
		t.Errorf("Changed() should return true after structural change")
	}
}

// Test 11: Simple scope used with CachedQuery — table-level fast path.
func TestWithoutScope_CachedQuerySimpleScope(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qsPosition](w)
	velID := flecs.RegisterComponent[qsVelocity](w)

	var ePos, ePosVel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ePos = fw.NewEntity()
		flecs.Set(fw, ePos, qsPosition{})

		ePosVel = fw.NewEntity()
		flecs.Set(fw, ePosVel, qsPosition{})
		flecs.Set(fw, ePosVel, qsVelocity{})
	})

	// NOT(Velocity) is a simple scope (single TermAnd, archetype, no Or).
	// Should be handled at the table-level fast path.
	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(posID),
		flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
			b.With(velID)
		}),
	)
	got := qsCollectCQ(cq)
	if !contains(got, ePos) {
		t.Errorf("expected ePos")
	}
	if contains(got, ePosVel) {
		t.Errorf("did not expect ePosVel")
	}
}

// Test 12: ScopeBuilder methods are chainable and all term kinds are supported.
func TestWithoutScope_ScopeBuilderMethods(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qsPosition](w)
	velID := flecs.RegisterComponent[qsVelocity](w)
	frozenID := flecs.RegisterComponent[qsFrozen](w)

	var ePos, ePosVel, ePosFrozen flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ePos = fw.NewEntity()
		flecs.Set(fw, ePos, qsPosition{})

		ePosVel = fw.NewEntity()
		flecs.Set(fw, ePosVel, qsPosition{})
		flecs.Set(fw, ePosVel, qsVelocity{})

		ePosFrozen = fw.NewEntity()
		flecs.Set(fw, ePosFrozen, qsPosition{})
		flecs.AddID(fw, ePosFrozen, frozenID)
	})

	w.Read(func(_ *flecs.Reader) {
		// NOT (Velocity AND NOT Frozen): chained with Without on ScopeBuilder.
		// = !vel OR frozen
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
				b.With(velID).Without(frozenID)
			}),
		)
		got := qsCollect(q)

		// ePos: !vel → matches.
		if !contains(got, ePos) {
			t.Errorf("expected ePos (no Velocity)")
		}
		// ePosVel: has vel and NOT frozen → inner = vel AND NOT frozen = true → NOT = false → no match.
		if contains(got, ePosVel) {
			t.Errorf("did not expect ePosVel")
		}
		// ePosFrozen: !vel → matches.
		if !contains(got, ePosFrozen) {
			t.Errorf("expected ePosFrozen (no Velocity)")
		}
	})
}

// Test 13 (extra): Union relationship inside scope.
// WithoutScope(b => b.With(MakePair(rel, target))): passes when entity does NOT
// have the union pair (rel, target).
func TestWithoutScope_UnionInner(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qsPosition](w)
	var rel, targetA, targetB flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		flecs.SetUnion(w, rel)
		targetA = fw.NewEntity()
		targetB = fw.NewEntity()
	})
	pairA := flecs.MakePair(rel, targetA)
	pairB := flecs.MakePair(rel, targetB)

	var ePos, ePosA, ePosB flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ePos = fw.NewEntity()
		flecs.Set(fw, ePos, qsPosition{})

		ePosA = fw.NewEntity()
		flecs.Set(fw, ePosA, qsPosition{})
		fw.AddID(ePosA, pairA) // has union (rel, targetA)

		ePosB = fw.NewEntity()
		flecs.Set(fw, ePosB, qsPosition{})
		fw.AddID(ePosB, pairB) // has union (rel, targetB)
	})

	w.Read(func(_ *flecs.Reader) {
		// NOT (has (rel, targetA)): ePos and ePosB match; ePosA does not.
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
				b.With(pairA)
			}),
		)
		got := qsCollect(q)

		if !contains(got, ePos) {
			t.Errorf("expected ePos (no union)")
		}
		if contains(got, ePosA) {
			t.Errorf("did not expect ePosA (has (rel,targetA))")
		}
		if !contains(got, ePosB) {
			t.Errorf("expected ePosB (has different target)")
		}
	})
}

// Test 15 (extra): Union wildcard target inside scope — hits the "return true" wildcard path.
func TestWithoutScope_UnionWildcardInner(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qsPosition](w)
	var rel, targetA flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		flecs.SetUnion(w, rel)
		targetA = fw.NewEntity()
	})
	wildcardPair := flecs.MakePair(rel, w.Wildcard()) // matches any target

	var ePos, ePosA flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ePos = fw.NewEntity()
		flecs.Set(fw, ePos, qsPosition{})

		ePosA = fw.NewEntity()
		flecs.Set(fw, ePosA, qsPosition{})
		fw.AddID(ePosA, flecs.MakePair(rel, targetA))
	})

	w.Read(func(_ *flecs.Reader) {
		// NOT (has ANY (rel, *) union pair): ePos matches; ePosA does not.
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
				b.With(wildcardPair)
			}),
		)
		got := qsCollect(q)
		if !contains(got, ePos) {
			t.Errorf("expected ePos (no union)")
		}
		if contains(got, ePosA) {
			t.Errorf("did not expect ePosA (has union pair)")
		}
	})
}

// Test 16: Union scope term where the union store does not exist yet.
// When no entity has ever added the union pair, the store is nil → scope inner = false → NOT = true → all match.
func TestWithoutScope_UnionNoStore(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qsPosition](w)
	var rel, target flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		flecs.SetUnion(w, rel)
		target = fw.NewEntity()
	})
	pairID := flecs.MakePair(rel, target)

	var eA, eB flecs.ID
	w.Write(func(fw *flecs.Writer) {
		eA = fw.NewEntity()
		flecs.Set(fw, eA, qsPosition{})
		eB = fw.NewEntity()
		flecs.Set(fw, eB, qsPosition{})
		// Intentionally no entity adds the union pair → union store never created.
	})

	w.Read(func(_ *flecs.Reader) {
		// NOT (has union pair): since no store exists, inner = false → NOT = true for all.
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
				b.With(pairID)
			}),
		)
		got := qsCollect(q)
		if !contains(got, eA) {
			t.Errorf("expected eA (union store absent)")
		}
		if !contains(got, eB) {
			t.Errorf("expected eB (union store absent)")
		}
	})
}

// Test 16: DontFragment outer term with archetype scope inner term (t==nil path).
// In allSparse mode the iterator has no current table; entityHasTermInScope
// falls back to the world index to look up the entity's own table.
func TestWithoutScope_AllSparseWithArchetypeScopeInner(t *testing.T) {
	w := flecs.New()
	dfID := flecs.RegisterComponent[qsDFComp](w)
	posID := flecs.RegisterComponent[qsPosition](w)
	velID := flecs.RegisterComponent[qsVelocity](w)
	flecs.SetDontFragment(w, dfID)

	var eDFNoVel, eDFVel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		eDFNoVel = fw.NewEntity()
		flecs.Set(fw, eDFNoVel, qsDFComp{W: 1}) // DontFragment; no Position or Velocity

		eDFVel = fw.NewEntity()
		flecs.Set(fw, eDFVel, qsDFComp{W: 2})
		flecs.Set(fw, eDFVel, qsVelocity{}) // also has archetype Velocity
	})
	_ = posID // posID not used in this query

	w.Read(func(_ *flecs.Reader) {
		// Outer: DontFragment only → allSparse mode, it.current == nil.
		// Scope: NOT(Velocity) — archetype inner term, simple scope.
		// In allSparse mode the scope must still be evaluated per-entity (t==nil path).
		q := flecs.NewQueryFromTerms(w,
			flecs.With(dfID),
			flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
				b.With(velID)
			}),
		)
		got := qsCollect(q)
		if !contains(got, eDFNoVel) {
			t.Errorf("expected eDFNoVel (no Velocity)")
		}
		if contains(got, eDFVel) {
			t.Errorf("did not expect eDFVel (has Velocity)")
		}
	})
}

// Test 18: Sparse outer And term + simple scope — exercises the matchesSparseTerms
// "continue" fast path (it.current != nil && isScopeTableSimple → skip per-entity).
func TestWithoutScope_SparseOuterSimpleScope(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qsPosition](w)
	sparseID := flecs.RegisterComponent[qsSparseComp](w)
	velID := flecs.RegisterComponent[qsVelocity](w)

	// Make sparseID Sparse-only (not DontFragment) so entities with it are in archetype.
	flecs.SetSparse(w, sparseID)

	var eSpNoVel, eSpVel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		eSpNoVel = fw.NewEntity()
		flecs.Set(fw, eSpNoVel, qsPosition{})
		flecs.Set(fw, eSpNoVel, qsSparseComp{Val: 1}) // has sparse; no Velocity

		eSpVel = fw.NewEntity()
		flecs.Set(fw, eSpVel, qsPosition{})
		flecs.Set(fw, eSpVel, qsSparseComp{Val: 2})
		flecs.Set(fw, eSpVel, qsVelocity{}) // has sparse + Velocity
	})

	w.Read(func(_ *flecs.Reader) {
		// With(sparseID) → hasSparseTerms=true → mixed mode → matchesSparseTerms called.
		// WithoutScope(b.With(velID)) → simple scope (archetype, non-sparse, no Or) →
		// matchesSparseTerms skips it (it.current != nil && isScopeTableSimple).
		// matchesTable handles the simple scope at the table level.
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.With(sparseID),
			flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
				b.With(velID)
			}),
		)
		got := qsCollect(q)
		if !contains(got, eSpNoVel) {
			t.Errorf("expected eSpNoVel (has sparse, no velocity)")
		}
		if contains(got, eSpVel) {
			t.Errorf("did not expect eSpVel (has velocity)")
		}
	})
}

// Test 19: ScopeBuilder.Maybe — optional term inside scope does not affect matching.
// Maybe(id) adds a TermOptional sub-term; entities match regardless of whether id is present.
func TestWithoutScope_MaybeInsideScope(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qsPosition](w)
	velID := flecs.RegisterComponent[qsVelocity](w)
	frozenID := flecs.RegisterComponent[qsFrozen](w)

	var ePos, ePosVel, ePosFrozen flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ePos = fw.NewEntity()
		flecs.Set(fw, ePos, qsPosition{})

		ePosVel = fw.NewEntity()
		flecs.Set(fw, ePosVel, qsPosition{})
		flecs.Set(fw, ePosVel, qsVelocity{})

		ePosFrozen = fw.NewEntity()
		flecs.Set(fw, ePosFrozen, qsPosition{})
		flecs.AddID(fw, ePosFrozen, frozenID)
	})

	w.Read(func(_ *flecs.Reader) {
		// NOT (Velocity AND Maybe(Frozen)): the Maybe term does not constrain the scope.
		// Scope inner = vel (frozen is optional, doesn't affect evaluation).
		// NOT(vel) → ePosVel excluded; ePos and ePosFrozen included.
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
				b.With(velID).Maybe(frozenID)
			}),
		)
		got := qsCollect(q)
		if !contains(got, ePos) {
			t.Errorf("expected ePos (no Velocity)")
		}
		if contains(got, ePosVel) {
			t.Errorf("did not expect ePosVel (scope inner satisfied by Velocity)")
		}
		if !contains(got, ePosFrozen) {
			t.Errorf("expected ePosFrozen (no Velocity)")
		}
	})
}

// Test 20: CachedQuery with a complex (OR-group) scope.
// Covers the cached_query.go hasSparseTerms=true path in Iter() and the
// "complex scope: continue" branch in tryMatchTable — both gated on
// !isScopeTableSimple(term.Sub).
func TestWithoutScope_CachedQueryComplexScope(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qsPosition](w)
	velID := flecs.RegisterComponent[qsVelocity](w)
	spdID := flecs.RegisterComponent[qsSpeed](w)

	var ePos, ePosVel, ePosSpd flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ePos = fw.NewEntity()
		flecs.Set(fw, ePos, qsPosition{})

		ePosVel = fw.NewEntity()
		flecs.Set(fw, ePosVel, qsPosition{})
		flecs.Set(fw, ePosVel, qsVelocity{})

		ePosSpd = fw.NewEntity()
		flecs.Set(fw, ePosSpd, qsPosition{})
		flecs.Set(fw, ePosSpd, qsSpeed{})
	})

	// OR-group scope → !isScopeTableSimple → complex scope path in Iter() and tryMatchTable.
	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(posID),
		flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
			b.With(velID).Or(spdID) // OR-group: complex scope
		}),
	)
	got := qsCollectCQ(cq)

	if !contains(got, ePos) {
		t.Errorf("expected ePos (has neither vel nor spd)")
	}
	if contains(got, ePosVel) {
		t.Errorf("did not expect ePosVel (has vel)")
	}
	if contains(got, ePosSpd) {
		t.Errorf("did not expect ePosSpd (has spd)")
	}
}

// Test 21: TermNot inside scope where entity HAS the forbidden component.
// Exercises the `case TermNot: if entityHasTermInScope(...) { return false }` branch
// in evalScopeSubTerms — the entity has both the required And-term AND the forbidden
// Not-term, so evalScopeSubTerms returns false via the TermNot case.
func TestWithoutScope_TermNotInsideScopeHit(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qsPosition](w)
	velID := flecs.RegisterComponent[qsVelocity](w)
	frozenID := flecs.RegisterComponent[qsFrozen](w)

	var ePos, ePosVelFrozen, ePosVelOnly flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ePos = fw.NewEntity()
		flecs.Set(fw, ePos, qsPosition{})

		// Entity with BOTH vel and frozen.
		// Scope: b.With(vel).Without(frozen) — inner = vel AND NOT frozen.
		// This entity: vel=true, frozen=true → TermNot(frozen) hits the return-false branch
		// → evalScopeSubTerms returns false → NOT(false) → entity MATCHES query.
		ePosVelFrozen = fw.NewEntity()
		flecs.Set(fw, ePosVelFrozen, qsPosition{})
		flecs.Set(fw, ePosVelFrozen, qsVelocity{})
		flecs.AddID(fw, ePosVelFrozen, frozenID)

		// Entity with vel only (no frozen).
		// Inner: vel=true, NOT frozen=true → evalScopeSubTerms returns true → NOT(true) → excluded.
		ePosVelOnly = fw.NewEntity()
		flecs.Set(fw, ePosVelOnly, qsPosition{})
		flecs.Set(fw, ePosVelOnly, qsVelocity{})
	})

	w.Read(func(_ *flecs.Reader) {
		// NOT (Velocity AND NOT Frozen).
		q := flecs.NewQueryFromTerms(w,
			flecs.With(posID),
			flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
				b.With(velID).Without(frozenID)
			}),
		)
		got := qsCollect(q)

		if !contains(got, ePos) {
			t.Errorf("expected ePos (no velocity)")
		}
		// ePosVelFrozen: NOT(vel AND NOT frozen) = NOT(vel=T, frozen hit TermNot→return false) = NOT(false) = true → match
		if !contains(got, ePosVelFrozen) {
			t.Errorf("expected ePosVelFrozen (vel+frozen: inner TermNot triggers return false)")
		}
		// ePosVelOnly: NOT(vel AND NOT frozen) = NOT(true) → excluded
		if contains(got, ePosVelOnly) {
			t.Errorf("did not expect ePosVelOnly (vel only: inner scope satisfied)")
		}
	})
}

// Test 22: CachedQuery with sparse component inside scope — exercises Changed() sparse
// version tracking for scope-internal sparse sub-terms (tablesAdded sync path and
// main sparse-version check path in Changed()).
func TestWithoutScope_CachedQuerySparseScope(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qsPosition](w)
	sparseID := flecs.RegisterComponent[qsSparseComp](w)
	flecs.SetSparse(w, sparseID)

	var ePos, ePosSpd flecs.ID
	w.Write(func(fw *flecs.Writer) {
		ePos = fw.NewEntity()
		flecs.Set(fw, ePos, qsPosition{})

		ePosSpd = fw.NewEntity()
		flecs.Set(fw, ePosSpd, qsPosition{})
		flecs.Set(fw, ePosSpd, qsSparseComp{Val: 1})
	})

	// Scope with sparse sub-term — complex scope → per-entity path.
	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(posID),
		flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
			b.With(sparseID) // sparse sub-term inside scope
		}),
	)

	// Drain initial tablesAdded (covers scope-sparse sync in tablesAdded path).
	_ = cq.Changed()

	got := qsCollectCQ(cq)
	if !contains(got, ePos) {
		t.Errorf("expected ePos (no sparse comp)")
	}
	if contains(got, ePosSpd) {
		t.Errorf("did not expect ePosSpd (has sparse comp)")
	}

	// Add new entity with Position only → new table; triggers tablesAdded.
	var eNew flecs.ID
	w.Write(func(fw *flecs.Writer) {
		eNew = fw.NewEntity()
		flecs.Set(fw, eNew, qsPosition{})
	})
	if !cq.Changed() {
		t.Errorf("expected Changed() after new table")
	}

	// Remove the sparse component from ePosSpd → bumps sparse version (remove is structural).
	// Changed() should detect the scope-internal sparse version change.
	_ = cq.Changed() // drain
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[qsSparseComp](fw, ePosSpd)
	})
	if !cq.Changed() {
		t.Errorf("expected Changed() after sparse scope-internal removal")
	}

	// Now ePosSpd no longer has the sparse component → scope NOT(sparse) satisfied → included.
	got2 := qsCollectCQ(cq)
	if !contains(got2, ePos) {
		t.Errorf("expected ePos after sparse removal")
	}
	if !contains(got2, eNew) {
		t.Errorf("expected eNew after sparse removal")
	}
	if !contains(got2, ePosSpd) {
		t.Errorf("expected ePosSpd after sparse removal (scope now satisfied)")
	}
}

// Test 24: NewEntityAfterScope — entity added after CachedQuery construction appears in results.
func TestWithoutScope_CachedQueryIncrementalTable(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qsPosition](w)
	velID := flecs.RegisterComponent[qsVelocity](w)

	var eExisting flecs.ID
	w.Write(func(fw *flecs.Writer) {
		eExisting = fw.NewEntity()
		flecs.Set(fw, eExisting, qsPosition{})
	})

	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(posID),
		flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
			b.With(velID)
		}),
	)
	_ = cq.Changed() // drain initial

	// Add a new entity with Position only → new table (if archetype differs) or same table.
	var eNew flecs.ID
	w.Write(func(fw *flecs.Writer) {
		eNew = fw.NewEntity()
		flecs.Set(fw, eNew, qsPosition{})
	})

	got := qsCollectCQ(cq)
	if !contains(got, eExisting) {
		t.Errorf("expected eExisting")
	}
	if !contains(got, eNew) {
		t.Errorf("expected eNew")
	}
}
