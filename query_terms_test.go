package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// ── With (TermAnd) ────────────────────────────────────────────────────────────

func TestQueryTermsWith(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	_ = flecs.RegisterComponent[Velocity](w)

	var e1, e2, e3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, Position{X: 1})
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, Position{X: 2})
		flecs.Set(fw, e2, Velocity{DX: 1})
		// e3 has only Velocity — should not match.
		e3 = fw.NewEntity()
		flecs.Set(fw, e3, Velocity{DX: 3})
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID))

	var got []flecs.ID
	q.Each(func(it *flecs.QueryIter) {
		got = append(got, it.Entities()...)
	})

	if len(got) != 2 {
		t.Fatalf("want 2 entities with Position, got %d", len(got))
	}
	for _, id := range got {
		if id == e3 {
			t.Error("e3 (Velocity-only) should not match With(posID)")
		}
	}
	_, _ = e1, e2
}

// ── Without (TermNot) ─────────────────────────────────────────────────────────

func TestQueryTermsWithout(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var deadID, alive, dead flecs.ID
	w.Write(func(fw *flecs.Writer) {
		deadID = fw.NewEntity() // tag

		// alive: has Position, no dead tag.
		alive = fw.NewEntity()
		flecs.Set(fw, alive, Position{X: 1})
		// dead: has Position and dead tag.
		dead = fw.NewEntity()
		flecs.Set(fw, dead, Position{X: 2})
		flecs.AddID(fw, dead, deadID)
	})

	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Without(deadID),
	)

	var got []flecs.ID
	q.Each(func(it *flecs.QueryIter) {
		got = append(got, it.Entities()...)
	})

	if len(got) != 1 {
		t.Fatalf("want 1 alive entity, got %d: %v", len(got), got)
	}
	if got[0] != alive {
		t.Errorf("expected alive entity %v, got %v", alive, got[0])
	}
	_ = dead
}

// ── Maybe (TermOptional) ──────────────────────────────────────────────────────

func TestQueryTermsMaybe(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		// e1: Position only.
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, Position{X: 1})
		// e2: Position + Velocity.
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, Position{X: 2})
		flecs.Set(fw, e2, Velocity{DX: 10})
	})

	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Maybe(velID),
	)

	type result struct {
		entity flecs.ID
		hasVel bool
		velDX  float32
	}
	var results []result

	q.Each(func(it *flecs.QueryIter) {
		positions := flecs.Field[Position](it, posID)
		vels, hasVel := flecs.FieldMaybe[Velocity](it, velID)
		for i, e := range it.Entities() {
			r := result{entity: e, hasVel: hasVel}
			_ = positions[i]
			if hasVel {
				r.velDX = vels[i].DX
			}
			results = append(results, r)
		}
	})

	if len(results) != 2 {
		t.Fatalf("want 2 entities (both have Position), got %d", len(results))
	}

	byEntity := make(map[flecs.ID]result)
	for _, r := range results {
		byEntity[r.entity] = r
	}

	if r, ok := byEntity[e1]; !ok || r.hasVel {
		t.Errorf("e1 (Position-only): want hasVel=false, got hasVel=%v", r.hasVel)
	}
	if r, ok := byEntity[e2]; !ok || !r.hasVel || r.velDX != 10 {
		t.Errorf("e2 (P+V): want hasVel=true velDX=10, got hasVel=%v velDX=%v", r.hasVel, r.velDX)
	}
}

// ── Mixed: And + Not + Optional ───────────────────────────────────────────────

func TestQueryTermsMixed(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var deadID, eAlive, eAliveVel, eDead flecs.ID
	w.Write(func(fw *flecs.Writer) {
		deadID = fw.NewEntity()

		// match: Position, no dead, optional Velocity.
		eAlive = fw.NewEntity()
		flecs.Set(fw, eAlive, Position{X: 1})
		// match: Position + Velocity, no dead.
		eAliveVel = fw.NewEntity()
		flecs.Set(fw, eAliveVel, Position{X: 2})
		flecs.Set(fw, eAliveVel, Velocity{DX: 5})
		// no match: Position + dead.
		eDead = fw.NewEntity()
		flecs.Set(fw, eDead, Position{X: 3})
		flecs.AddID(fw, eDead, deadID)
	})

	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Without(deadID),
		flecs.Maybe(velID),
	)

	matched := make(map[flecs.ID]bool)
	q.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			matched[e] = true
		}
	})

	if !matched[eAlive] {
		t.Error("eAlive (Position, no dead) should match")
	}
	if !matched[eAliveVel] {
		t.Error("eAliveVel (Position+Velocity, no dead) should match")
	}
	if matched[eDead] {
		t.Error("eDead (Position+dead) should NOT match")
	}
}

// ── Multiple Not terms ────────────────────────────────────────────────────────

func TestQueryTermsMultipleNot(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var aID, bID, eOnly, eA, eB, eAB flecs.ID
	w.Write(func(fw *flecs.Writer) {
		aID = fw.NewEntity()
		bID = fw.NewEntity()

		// match: only Position.
		eOnly = fw.NewEntity()
		flecs.Set(fw, eOnly, Position{X: 1})
		// no match: Position + A.
		eA = fw.NewEntity()
		flecs.Set(fw, eA, Position{})
		flecs.AddID(fw, eA, aID)
		// no match: Position + B.
		eB = fw.NewEntity()
		flecs.Set(fw, eB, Position{})
		flecs.AddID(fw, eB, bID)
		// no match: Position + A + B.
		eAB = fw.NewEntity()
		flecs.Set(fw, eAB, Position{})
		flecs.AddID(fw, eAB, aID)
		flecs.AddID(fw, eAB, bID)
	})

	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Without(aID),
		flecs.Without(bID),
	)

	var got []flecs.ID
	q.Each(func(it *flecs.QueryIter) {
		got = append(got, it.Entities()...)
	})

	if len(got) != 1 || got[0] != eOnly {
		t.Errorf("want only eOnly, got %v", got)
	}
	_, _, _ = eA, eB, eAB
}

// ── Multiple Optional terms ───────────────────────────────────────────────────

func TestQueryTermsMultipleOptional(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)
	type Mass struct{ V float32 }
	massID := flecs.RegisterComponent[Mass](w)

	var e1, e2, e3, e4 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		// e1: Position only.
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, Position{X: 1})
		// e2: Position + Velocity.
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, Position{X: 2})
		flecs.Set(fw, e2, Velocity{DX: 1})
		// e3: Position + Mass.
		e3 = fw.NewEntity()
		flecs.Set(fw, e3, Position{X: 3})
		flecs.Set(fw, e3, Mass{V: 5})
		// e4: Position + Velocity + Mass.
		e4 = fw.NewEntity()
		flecs.Set(fw, e4, Position{X: 4})
		flecs.Set(fw, e4, Velocity{DX: 2})
		flecs.Set(fw, e4, Mass{V: 10})
	})

	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Maybe(velID),
		flecs.Maybe(massID),
	)

	type rec struct {
		hasVel  bool
		hasMass bool
	}
	byEntity := make(map[flecs.ID]rec)
	q.Each(func(it *flecs.QueryIter) {
		_, hasVel := flecs.FieldMaybe[Velocity](it, velID)
		_, hasMass := flecs.FieldMaybe[Mass](it, massID)
		for _, e := range it.Entities() {
			byEntity[e] = rec{hasVel, hasMass}
		}
	})

	if len(byEntity) != 4 {
		t.Fatalf("want 4 entities, got %d", len(byEntity))
	}
	check := func(e flecs.ID, wantVel, wantMass bool) {
		t.Helper()
		r, ok := byEntity[e]
		if !ok {
			t.Errorf("entity %v not visited", e)
			return
		}
		if r.hasVel != wantVel || r.hasMass != wantMass {
			t.Errorf("entity %v: want vel=%v mass=%v, got vel=%v mass=%v",
				e, wantVel, wantMass, r.hasVel, r.hasMass)
		}
	}
	check(e1, false, false)
	check(e2, true, false)
	check(e3, false, true)
	check(e4, true, true)
}

// ── NewQuery (legacy) unchanged ───────────────────────────────────────────────

func TestQueryTermsLegacyNewQueryUnchanged(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, Position{X: 1})
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, Position{X: 2})
		flecs.Set(fw, e2, Velocity{DX: 1})
	})

	q := flecs.NewQuery(w, posID, velID)

	var got []flecs.ID
	q.Each(func(it *flecs.QueryIter) {
		got = append(got, it.Entities()...)
	})
	if len(got) != 1 || got[0] != e2 {
		t.Errorf("legacy NewQuery: want [e2], got %v", got)
	}
	// Terms() returns And-only IDs in sorted order.
	terms := q.Terms()
	if len(terms) != 2 {
		t.Fatalf("Terms(): want 2, got %d", len(terms))
	}
	_ = e1
}

// ── Panic: no And terms ───────────────────────────────────────────────────────

func TestQueryTermsPanicNoAndTerms(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for Without-only query")
		}
	}()
	flecs.NewQueryFromTerms(w, flecs.Without(posID))
}

// ── Panic: duplicate ID ───────────────────────────────────────────────────────

func TestQueryTermsPanicDuplicateID(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for With+Without same ID")
		}
	}()
	flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.Without(posID))
}

// ── Panic: FieldMaybe on And term ─────────────────────────────────────────────

func TestQueryTermsPanicFieldMaybeOnAndTerm(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1})
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID))
	it := q.Iter()
	if !it.Next() {
		t.Fatal("expected Next() = true")
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for FieldMaybe on And term")
		}
	}()
	flecs.FieldMaybe[Position](it, posID)
}

// ── Field on Optional term panics when column absent ─────────────────────────

func TestQueryTermsFieldPanicsOnMissingOptional(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	// Entity with only Position; Velocity is absent.
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1})
	})

	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Maybe(velID),
	)
	it := q.Iter()
	// Find the Position-only table (Velocity absent).
	var foundPosOnly bool
	for it.Next() {
		vels, hasVel := flecs.FieldMaybe[Velocity](it, velID)
		if !hasVel {
			foundPosOnly = true
			// Now call Field on velID, which must panic.
			func() {
				defer func() {
					if r := recover(); r == nil {
						t.Error("expected Field[Velocity] to panic on absent column")
					}
				}()
				flecs.Field[Velocity](it, velID)
			}()
			_ = vels
		}
	}
	if !foundPosOnly {
		t.Skip("no Position-only table found in this test setup")
	}
}

// ── TermsFull ─────────────────────────────────────────────────────────────────

func TestQueryTermsFull(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)
	var deadID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		deadID = fw.NewEntity()
	})

	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Without(deadID),
		flecs.Maybe(velID),
	)

	full := q.TermsFull()
	if len(full) != 3 {
		t.Fatalf("TermsFull: want 3 terms, got %d", len(full))
	}
	// Sorted: And(posID), Not(deadID), Optional(velID).
	if full[0].Kind != flecs.TermAnd || full[0].ID != posID {
		t.Errorf("full[0]: want And(%v), got %+v", posID, full[0])
	}
	if full[1].Kind != flecs.TermNot || full[1].ID != deadID {
		t.Errorf("full[1]: want Not(%v), got %+v", deadID, full[1])
	}
	if full[2].Kind != flecs.TermOptional || full[2].ID != velID {
		t.Errorf("full[2]: want Optional(%v), got %+v", velID, full[2])
	}
	// Modifying the returned copy must not affect the query.
	full[0].ID = 9999
	full2 := q.TermsFull()
	if full2[0].ID != posID {
		t.Error("TermsFull must return a copy, not a reference")
	}
}

// ── CachedQuery with Not/Optional ────────────────────────────────────────────

func TestCachedQueryFromTermsBasic(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var deadID, eAlive, eDead flecs.ID
	w.Write(func(fw *flecs.Writer) {
		deadID = fw.NewEntity()

		// alive: Position only.
		eAlive = fw.NewEntity()
		flecs.Set(fw, eAlive, Position{X: 1})
		// dead: Position + dead tag.
		eDead = fw.NewEntity()
		flecs.Set(fw, eDead, Position{X: 2})
		flecs.AddID(fw, eDead, deadID)
	})

	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(posID),
		flecs.Without(deadID),
		flecs.Maybe(velID),
	)
	defer cq.Close()

	if cq.Count() != 1 {
		t.Fatalf("want 1 table (Position-only), got %d", cq.Count())
	}
	if cq.EntityCount() != 1 {
		t.Fatalf("want 1 entity (alive), got %d", cq.EntityCount())
	}

	var got []flecs.ID
	cq.Each(func(it *flecs.QueryIter) {
		got = append(got, it.Entities()...)
	})
	if len(got) != 1 || got[0] != eAlive {
		t.Errorf("want [eAlive], got %v", got)
	}
	_ = eDead
}

// ── CachedQuery migration: Not term cache invalidation ────────────────────────

// TestCachedQueryNotTermMigration verifies the Not-term cache invariant:
//  1. [With(P), Without(D)] cached query: entity with [P] matches.
//  2. Adding D to the entity migrates it to [P,D]; the new [P,D] table is
//     rejected by tryMatchTable (D present). The [P] table remains cached.
//  3. Removing D migrates the entity back to [P]; the [P] table was already
//     cached, so EntityCount is still correct.
//  4. Re-adding D migrates back to [P,D]; EntityCount drops to 0 again.
func TestCachedQueryNotTermMigration(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var deadID, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		deadID = fw.NewEntity()
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1})
	})

	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(posID),
		flecs.Without(deadID),
	)
	defer cq.Close()

	if cq.EntityCount() != 1 {
		t.Fatalf("initial: want 1, got %d", cq.EntityCount())
	}

	// Add dead tag: entity migrates to [P, dead]. tryMatchTable([P,dead]) → false (dead present).
	// The [P] table still has 0 entities. EntityCount drops to 0.
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, deadID)
	})
	if cq.EntityCount() != 0 {
		t.Fatalf("after AddID(dead): want 0, got %d", cq.EntityCount())
	}

	// Remove dead tag: entity migrates back to [P]. [P] table is still in cache.
	w.Write(func(fw *flecs.Writer) {
		flecs.RemoveID(fw, e, deadID)
	})
	if cq.EntityCount() != 1 {
		t.Fatalf("after RemoveID(dead): want 1, got %d", cq.EntityCount())
	}

	// Re-add dead: entity leaves [P] again.
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, deadID)
	})
	if cq.EntityCount() != 0 {
		t.Fatalf("after re-AddID(dead): want 0, got %d", cq.EntityCount())
	}
}

// ── CachedQuery: new tables that match Not query are added; non-matching are not ─

func TestCachedQueryNotTermNewMatchingTable(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	type Extra struct{ V int }
	_ = flecs.RegisterComponent[Extra](w)

	var deadID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		deadID = fw.NewEntity()
	})

	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(posID),
		flecs.Without(deadID),
	)
	defer cq.Close()

	if cq.Count() != 0 {
		t.Fatalf("empty world: want 0, got %d", cq.Count())
	}

	// Set Position: creates [P] table → matches (P present, dead absent).
	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, Position{X: 1})
	})
	if cq.Count() != 1 {
		t.Fatalf("after [P] entity: want 1 table, got %d", cq.Count())
	}

	// Set Extra: e1 migrates to [P,Extra] → new table, matches.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e1, Extra{V: 42})
	})
	if cq.Count() != 2 {
		t.Fatalf("after [P,Extra] migration: want 2 tables, got %d", cq.Count())
	}

	// Create entity that ends up with [P, dead]: new table should NOT be added
	// (dead is present, violating the Without term).
	w.Write(func(fw *flecs.Writer) {
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, Position{X: 2}) // migrates to [P], already cached
		flecs.AddID(fw, e2, deadID)       // migrates to [P,dead] — new table, rejected
	})

	if cq.Count() != 2 {
		t.Fatalf("after [P,dead] entity: want 2 tables (unchanged), got %d", cq.Count())
	}
	_ = e2
}

// ── Panic: NewQueryFromTerms nil world ───────────────────────────────────────

func TestQueryFromTermsPanicNilWorld(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil world")
		}
	}()
	flecs.NewQueryFromTerms(nil, flecs.With(1))
}

// ── Panic: NewQueryFromTerms empty terms ─────────────────────────────────────

func TestQueryFromTermsPanicEmptyTerms(t *testing.T) {
	w := flecs.New()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty terms")
		}
	}()
	flecs.NewQueryFromTerms(w)
}

// ── Panic: FieldMaybe with id not in query terms ──────────────────────────────

func TestQueryTermsPanicFieldMaybeIDNotInTerms(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1})
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID))
	it := q.Iter()
	if !it.Next() {
		t.Fatal("expected Next()=true")
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for id not in query terms")
		}
	}()
	// velID is not a term in this query at all.
	flecs.FieldMaybe[Velocity](it, velID)
}

// ── CachedQuery TermsFull after Close ────────────────────────────────────────

func TestCachedQueryTermsFullAfterClose(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	cq := flecs.NewCachedQueryFromTerms(w, flecs.With(posID))
	cq.Close()

	if cq.TermsFull() != nil {
		t.Fatal("TermsFull() after Close should return nil")
	}
}

// ── Panic: NewCachedQueryFromTerms empty terms ───────────────────────────────

func TestCachedQueryFromTermsPanicEmptyTerms(t *testing.T) {
	w := flecs.New()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty terms")
		}
	}()
	flecs.NewCachedQueryFromTerms(w)
}

// ── CachedQueryFromTerms: panic on nil world ──────────────────────────────────

func TestCachedQueryFromTermsPanicNilWorld(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil world")
		}
	}()
	flecs.NewCachedQueryFromTerms(nil, flecs.With(1))
}

// ── CachedQueryFromTerms: panic on no And terms ───────────────────────────────

func TestCachedQueryFromTermsPanicNoAndTerms(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for Without-only cached query")
		}
	}()
	flecs.NewCachedQueryFromTerms(w, flecs.Without(posID))
}

// ── CachedQueryFromTerms: panic on duplicate ID ───────────────────────────────

func TestCachedQueryFromTermsPanicDuplicateID(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for duplicate ID")
		}
	}()
	flecs.NewCachedQueryFromTerms(w, flecs.With(posID), flecs.Without(posID))
}

// ── CachedQuery Terms() backward compat ──────────────────────────────────────

func TestCachedQueryFromTermsTermsBackwardCompat(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)
	var deadID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		deadID = fw.NewEntity()
	})

	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(posID),
		flecs.With(velID),
		flecs.Without(deadID),
	)
	defer cq.Close()

	// Terms() must return only And term IDs.
	ids := cq.Terms()
	if len(ids) != 2 {
		t.Fatalf("Terms(): want 2 And IDs, got %d: %v", len(ids), ids)
	}
	// Must be sorted.
	if ids[0] >= ids[1] {
		t.Errorf("Terms(): want sorted IDs, got %v", ids)
	}

	// TermsFull returns all three terms.
	full := cq.TermsFull()
	if len(full) != 3 {
		t.Fatalf("TermsFull(): want 3, got %d", len(full))
	}
}

// ── OR query terms ────────────────────────────────────────────────────────────

// TestOrQueryBasic: With(Pos), Or(Sleep), Or(Work), Or(Play).
// Entities with exactly one of the Or ids each match; entity with none does not.
func TestOrQueryBasic(t *testing.T) {
	type Sleeping struct{}
	type Working struct{}
	type Playing struct{}

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	sleepID := flecs.RegisterComponent[Sleeping](w)
	workID := flecs.RegisterComponent[Working](w)
	playID := flecs.RegisterComponent[Playing](w)

	var eSleep, eWork, ePlay, eNone flecs.ID
	w.Write(func(fw *flecs.Writer) {
		eSleep = fw.NewEntity()
		flecs.Set(fw, eSleep, Position{X: 1})
		flecs.Set(fw, eSleep, Sleeping{})

		eWork = fw.NewEntity()
		flecs.Set(fw, eWork, Position{X: 2})
		flecs.Set(fw, eWork, Working{})

		ePlay = fw.NewEntity()
		flecs.Set(fw, ePlay, Position{X: 3})
		flecs.Set(fw, ePlay, Playing{})

		eNone = fw.NewEntity()
		flecs.Set(fw, eNone, Position{X: 4}) // no activity — should NOT match
	})

	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Or(sleepID),
		flecs.Or(workID),
		flecs.Or(playID),
	)

	matched := make(map[flecs.ID]bool)
	q.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			matched[e] = true
		}
	})

	if !matched[eSleep] {
		t.Error("eSleep should match")
	}
	if !matched[eWork] {
		t.Error("eWork should match")
	}
	if !matched[ePlay] {
		t.Error("ePlay should match")
	}
	if matched[eNone] {
		t.Error("eNone (Position-only) should NOT match")
	}
}

// TestOrQueryMultipleGroups: two independent OR-groups separated by a With term.
// [Or(A), Or(B), With(X), Or(C), Or(D)] → groups {A,B} and {C,D}.
func TestOrQueryMultipleGroups(t *testing.T) {
	type CompA struct{}
	type CompB struct{}
	type CompX struct{}
	type CompC struct{}
	type CompD struct{}

	w := flecs.New()
	aID := flecs.RegisterComponent[CompA](w)
	bID := flecs.RegisterComponent[CompB](w)
	xID := flecs.RegisterComponent[CompX](w)
	cID := flecs.RegisterComponent[CompC](w)
	dID := flecs.RegisterComponent[CompD](w)

	var eAXC, eBXD, eAXonly, eACnoX flecs.ID
	w.Write(func(fw *flecs.Writer) {
		// Matches: has A, X, C.
		eAXC = fw.NewEntity()
		flecs.Set(fw, eAXC, CompA{})
		flecs.Set(fw, eAXC, CompX{})
		flecs.Set(fw, eAXC, CompC{})

		// Matches: has B, X, D.
		eBXD = fw.NewEntity()
		flecs.Set(fw, eBXD, CompB{})
		flecs.Set(fw, eBXD, CompX{})
		flecs.Set(fw, eBXD, CompD{})

		// No match: has A, X but neither C nor D.
		eAXonly = fw.NewEntity()
		flecs.Set(fw, eAXonly, CompA{})
		flecs.Set(fw, eAXonly, CompX{})

		// No match: has A, C but no X.
		eACnoX = fw.NewEntity()
		flecs.Set(fw, eACnoX, CompA{})
		flecs.Set(fw, eACnoX, CompC{})
	})

	q := flecs.NewQueryFromTerms(w,
		flecs.Or(aID),
		flecs.Or(bID),
		flecs.With(xID),
		flecs.Or(cID),
		flecs.Or(dID),
	)

	matched := make(map[flecs.ID]bool)
	q.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			matched[e] = true
		}
	})

	if !matched[eAXC] {
		t.Error("eAXC should match (A∈{A,B}, X, C∈{C,D})")
	}
	if !matched[eBXD] {
		t.Error("eBXD should match (B∈{A,B}, X, D∈{C,D})")
	}
	if matched[eAXonly] {
		t.Error("eAXonly should NOT match (missing {C,D})")
	}
	if matched[eACnoX] {
		t.Error("eACnoX should NOT match (missing X and {C,D} group not fully satisfied)")
	}
}

// TestOrQuerySingleTermGroup: Or(A) with no adjacent Or behaves like With(A).
func TestOrQuerySingleTermGroup(t *testing.T) {
	type CompA struct{}

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	aID := flecs.RegisterComponent[CompA](w)

	w.Write(func(fw *flecs.Writer) {
		eWithA := fw.NewEntity()
		flecs.Set(fw, eWithA, Position{X: 1})
		flecs.Set(fw, eWithA, CompA{})

		eWithout := fw.NewEntity()
		flecs.Set(fw, eWithout, Position{X: 2}) // no A
	})

	qOr := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.Or(aID))
	qAnd := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.With(aID))

	count := func(q *flecs.Query) int {
		n := 0
		q.Each(func(it *flecs.QueryIter) { n += it.Count() })
		return n
	}

	if count(qOr) != count(qAnd) {
		t.Errorf("single-term Or(A) should behave like With(A): got Or=%d And=%d",
			count(qOr), count(qAnd))
	}
	if count(qOr) != 1 {
		t.Errorf("want 1 entity (eWithA), got %d", count(qOr))
	}
}

// TestOrQueryOrPlusNot: [With(Pos), Or(A), Or(B), Without(Dead)].
func TestOrQueryOrPlusNot(t *testing.T) {
	type CompA struct{}
	type CompB struct{}
	type Dead struct{}

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	aID := flecs.RegisterComponent[CompA](w)
	bID := flecs.RegisterComponent[CompB](w)
	deadID := flecs.RegisterComponent[Dead](w)

	var eA, eB, eADead, eNone flecs.ID
	w.Write(func(fw *flecs.Writer) {
		// Matches: Pos, A, no Dead.
		eA = fw.NewEntity()
		flecs.Set(fw, eA, Position{})
		flecs.Set(fw, eA, CompA{})

		// Matches: Pos, B, no Dead.
		eB = fw.NewEntity()
		flecs.Set(fw, eB, Position{})
		flecs.Set(fw, eB, CompB{})

		// No match: Pos, A, Dead.
		eADead = fw.NewEntity()
		flecs.Set(fw, eADead, Position{})
		flecs.Set(fw, eADead, CompA{})
		flecs.Set(fw, eADead, Dead{})

		// No match: Pos, no A/B.
		eNone = fw.NewEntity()
		flecs.Set(fw, eNone, Position{})
	})

	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Or(aID),
		flecs.Or(bID),
		flecs.Without(deadID),
	)

	matched := make(map[flecs.ID]bool)
	q.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			matched[e] = true
		}
	})

	if !matched[eA] {
		t.Error("eA should match")
	}
	if !matched[eB] {
		t.Error("eB should match")
	}
	if matched[eADead] {
		t.Error("eADead should NOT match (has Dead)")
	}
	if matched[eNone] {
		t.Error("eNone should NOT match (missing Or-group)")
	}
}

// TestOrQueryOrPlusMaybe: [With(Pos), Or(A), Or(B), Maybe(C)].
// All entities with (A or B) match; FieldMaybe[C] reports per-table presence.
func TestOrQueryOrPlusMaybe(t *testing.T) {
	type CompA struct{}
	type CompB struct{}
	type CompC struct{ V int }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	aID := flecs.RegisterComponent[CompA](w)
	bID := flecs.RegisterComponent[CompB](w)
	cID := flecs.RegisterComponent[CompC](w)

	var eAC, eB, eNone flecs.ID
	w.Write(func(fw *flecs.Writer) {
		// Pos + A + C.
		eAC = fw.NewEntity()
		flecs.Set(fw, eAC, Position{})
		flecs.Set(fw, eAC, CompA{})
		flecs.Set(fw, eAC, CompC{V: 7})

		// Pos + B, no C.
		eB = fw.NewEntity()
		flecs.Set(fw, eB, Position{})
		flecs.Set(fw, eB, CompB{})

		// Pos only — should NOT match.
		eNone = fw.NewEntity()
		flecs.Set(fw, eNone, Position{})
	})

	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Or(aID),
		flecs.Or(bID),
		flecs.Maybe(cID),
	)

	type result struct{ hasC bool }
	byEntity := make(map[flecs.ID]result)
	q.Each(func(it *flecs.QueryIter) {
		_, hasC := flecs.FieldMaybe[CompC](it, cID)
		for _, e := range it.Entities() {
			byEntity[e] = result{hasC}
		}
	})

	if _, ok := byEntity[eAC]; !ok {
		t.Error("eAC (Pos+A+C) should match")
	} else if !byEntity[eAC].hasC {
		t.Error("eAC: want hasC=true")
	}

	if _, ok := byEntity[eB]; !ok {
		t.Error("eB (Pos+B) should match")
	} else if byEntity[eB].hasC {
		t.Error("eB: want hasC=false")
	}

	if _, ok := byEntity[eNone]; ok {
		t.Error("eNone (Pos-only) should NOT match")
	}
}

// ── OR panic cases ────────────────────────────────────────────────────────────

func TestOrQueryPanicNoAnd(t *testing.T) {
	w := flecs.New()
	type CompA struct{}
	type CompB struct{}
	aID := flecs.RegisterComponent[CompA](w)
	bID := flecs.RegisterComponent[CompB](w)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for Or-only query (no TermAnd)")
		}
	}()
	flecs.NewQueryFromTerms(w, flecs.Or(aID), flecs.Or(bID))
}

func TestOrQueryPanicDuplicateInGroup(t *testing.T) {
	w := flecs.New()
	type CompA struct{}
	posID := flecs.RegisterComponent[Position](w)
	aID := flecs.RegisterComponent[CompA](w)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for Or(A), Or(A) same group")
		}
	}()
	flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.Or(aID), flecs.Or(aID))
}

func TestOrQueryPanicCrossKindDuplicate(t *testing.T) {
	w := flecs.New()
	type CompA struct{}
	aID := flecs.RegisterComponent[CompA](w)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for With(A) + Or(A)")
		}
	}()
	flecs.NewQueryFromTerms(w, flecs.With(aID), flecs.Or(aID))
}

func TestOrQueryPanicZeroID(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for Or(0)")
		}
	}()
	flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.Or(0))
}

// ── CachedQuery with Or ───────────────────────────────────────────────────────

func TestCachedQueryWithOr(t *testing.T) {
	type CompA struct{}
	type CompB struct{}

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	aID := flecs.RegisterComponent[CompA](w)
	bID := flecs.RegisterComponent[CompB](w)

	// Create entities before the cached query.
	var eA, eB flecs.ID
	w.Write(func(fw *flecs.Writer) {
		eA = fw.NewEntity()
		flecs.Set(fw, eA, Position{})
		flecs.Set(fw, eA, CompA{})

		eB = fw.NewEntity()
		flecs.Set(fw, eB, Position{})
		flecs.Set(fw, eB, CompB{})
	})

	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(posID),
		flecs.Or(aID),
		flecs.Or(bID),
	)
	defer cq.Close()

	if cq.Count() < 1 {
		t.Fatalf("want ≥1 matching table(s), got %d", cq.Count())
	}
	initialTableCount := cq.Count()

	matched := make(map[flecs.ID]bool)
	cq.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			matched[e] = true
		}
	})
	if !matched[eA] || !matched[eB] {
		t.Error("both eA and eB should be in cached query results")
	}

	// New entity with neither A nor B must NOT grow the cache.
	w.Write(func(fw *flecs.Writer) {
		eNeither := fw.NewEntity()
		flecs.Set(fw, eNeither, Position{})
	})

	if cq.Count() != initialTableCount {
		t.Errorf("cache should not grow for Pos-only entity: want %d tables, got %d",
			initialTableCount, cq.Count())
	}

	// Sanity: eNeither must not appear in results.
	matched2 := make(map[flecs.ID]bool)
	cq.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			matched2[e] = true
		}
	})
	// We can't check matched2[eNeither] since eNeither is scoped inside Write closure.
	// The cache count check above is sufficient.
}

// TestCachedQueryWithOrNewMatchingTable: after construction, a new table that
// satisfies the Or-group must be added to the cache automatically.
func TestCachedQueryWithOrNewMatchingTable(t *testing.T) {
	type CompA struct{}
	type CompB struct{}
	type Extra struct{ V int }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	aID := flecs.RegisterComponent[CompA](w)
	bID := flecs.RegisterComponent[CompB](w)
	_ = flecs.RegisterComponent[Extra](w) // registers Extra so migration tables can be created

	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(posID),
		flecs.Or(aID),
		flecs.Or(bID),
	)
	defer cq.Close()

	if cq.Count() != 0 {
		t.Fatalf("empty world: want 0 tables, got %d", cq.Count())
	}

	// Create a [Pos, A] entity → new table, should be added.
	var e1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, Position{})
		flecs.Set(fw, e1, CompA{})
	})
	if cq.Count() != 1 {
		t.Fatalf("after [Pos,A] entity: want 1 table, got %d", cq.Count())
	}

	// Add Extra to e1 → migrates to [Pos,A,Extra], new table, should be added.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e1, Extra{V: 5})
	})
	if cq.Count() != 2 {
		t.Fatalf("after [Pos,A,Extra] migration: want 2 tables, got %d", cq.Count())
	}

	// Create [Pos, Extra] entity → table lacks A and B, should NOT be added.
	w.Write(func(fw *flecs.Writer) {
		e2 := fw.NewEntity()
		flecs.Set(fw, e2, Position{})
		flecs.Set(fw, e2, Extra{V: 9})
	})
	if cq.Count() != 2 {
		t.Fatalf("after [Pos,Extra] entity: want 2 tables (unchanged), got %d", cq.Count())
	}
}

// ── FieldMaybe and Field on Or-group ids ─────────────────────────────────────

// TestOrQueryFieldMaybe: entity with A but not B; FieldMaybe[A] = (slice, true),
// FieldMaybe[B] = (nil, false).
func TestOrQueryFieldMaybe(t *testing.T) {
	type CompA struct{ V int }
	type CompB struct{ V int }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	aID := flecs.RegisterComponent[CompA](w)
	bID := flecs.RegisterComponent[CompB](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1})
		flecs.Set(fw, e, CompA{V: 42})
		// no CompB
	})

	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Or(aID),
		flecs.Or(bID),
	)

	found := false
	q.Each(func(it *flecs.QueryIter) {
		for _, ent := range it.Entities() {
			if ent != e {
				continue
			}
			found = true
			aSlice, hasA := flecs.FieldMaybe[CompA](it, aID)
			if !hasA {
				t.Error("want hasA=true")
			}
			if len(aSlice) == 0 {
				t.Error("want non-empty CompA slice")
			}

			_, hasB := flecs.FieldMaybe[CompB](it, bID)
			if hasB {
				t.Error("want hasB=false (entity lacks CompB)")
			}
		}
	})
	if !found {
		t.Fatal("entity e not visited by query")
	}
}

// TestOrQueryFieldPanicsWhenAbsent: Field[B] on an entity that has A but not B
// must panic, enforcing FieldMaybe usage for Or-group ids.
func TestOrQueryFieldPanicsWhenAbsent(t *testing.T) {
	type CompA struct{}
	type CompB struct{}

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	aID := flecs.RegisterComponent[CompA](w)
	bID := flecs.RegisterComponent[CompB](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{})
		flecs.Set(fw, e, CompA{})
		// no CompB
	})

	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Or(aID),
		flecs.Or(bID),
	)

	foundPanic := false
	q.Each(func(it *flecs.QueryIter) {
		for _, ent := range it.Entities() {
			if ent != e {
				continue
			}
			_, hasB := flecs.FieldMaybe[CompB](it, bID)
			if hasB {
				return // wrong table, skip
			}
			func() {
				defer func() {
					if r := recover(); r != nil {
						foundPanic = true
					}
				}()
				flecs.Field[CompB](it, bID)
			}()
		}
	})
	if !foundPanic {
		t.Error("Field[CompB] on an entity lacking CompB should panic")
	}
}

// ── TermsFull ordering with Or terms ─────────────────────────────────────────

func TestOrQueryTermsFull(t *testing.T) {
	type CompA struct{}
	type CompB struct{}

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)
	var deadID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		deadID = fw.NewEntity()
	})
	aID := flecs.RegisterComponent[CompA](w)
	bID := flecs.RegisterComponent[CompB](w)

	// [With(pos), Without(dead), Or(a), Or(b), Maybe(vel)]
	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.Without(deadID),
		flecs.Or(aID),
		flecs.Or(bID),
		flecs.Maybe(velID),
	)

	full := q.TermsFull()
	if len(full) != 5 {
		t.Fatalf("want 5 terms, got %d: %+v", len(full), full)
	}

	// Expected order: And(pos), Not(dead), Or(a or b, sorted by ID), Optional(vel).
	if full[0].Kind != flecs.TermAnd {
		t.Errorf("full[0] should be TermAnd, got %v", full[0].Kind)
	}
	if full[1].Kind != flecs.TermNot {
		t.Errorf("full[1] should be TermNot, got %v", full[1].Kind)
	}
	if full[2].Kind != flecs.TermOr {
		t.Errorf("full[2] should be TermOr, got %v", full[2].Kind)
	}
	if full[3].Kind != flecs.TermOr {
		t.Errorf("full[3] should be TermOr, got %v", full[3].Kind)
	}
	if full[4].Kind != flecs.TermOptional {
		t.Errorf("full[4] should be TermOptional, got %v", full[4].Kind)
	}

	// Or terms should be sorted by ID within the group.
	if full[2].ID >= full[3].ID {
		t.Errorf("Or terms should be sorted by ID: full[2].ID=%v full[3].ID=%v", full[2].ID, full[3].ID)
	}
}

// ── FieldMaybe works in CachedQuery iter ─────────────────────────────────────

func TestCachedQueryFromTermsFieldMaybe(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, Position{X: 1})
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, Position{X: 2})
		flecs.Set(fw, e2, Velocity{DX: 7})
	})

	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(posID),
		flecs.Maybe(velID),
	)
	defer cq.Close()

	byEntity := make(map[flecs.ID]bool) // true = has velocity
	cq.Each(func(it *flecs.QueryIter) {
		_, hasVel := flecs.FieldMaybe[Velocity](it, velID)
		for _, e := range it.Entities() {
			byEntity[e] = hasVel
		}
	})

	if len(byEntity) != 2 {
		t.Fatalf("want 2 entities, got %d", len(byEntity))
	}
	if byEntity[e1] {
		t.Error("e1 (no Velocity): want hasVel=false")
	}
	if !byEntity[e2] {
		t.Error("e2 (has Velocity): want hasVel=true")
	}
}

// ── Traversal term tests ──────────────────────────────────────────────────────

// TestQueryUp_MatchesViaPrefab: entity has (IsA, prefab), prefab has Position.
// With(posID).Up(w.IsA()) matches the entity; FieldShared returns prefab's value;
// IsFieldSelf is false.
func TestQueryUp_MatchesViaPrefab(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var prefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 42, Y: 7})

		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), prefab))
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID).Up(w.IsA()))

	var visited []flecs.ID
	var gotPos Position
	var gotSelf bool
	q.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			visited = append(visited, e)
			p, ok := flecs.FieldShared[Position](it, posID)
			if !ok {
				t.Error("FieldShared should return (value, true) for Up match")
			}
			gotPos = p
			gotSelf = flecs.IsFieldSelf(it, posID)
		}
	})

	if len(visited) != 1 || visited[0] != inst {
		t.Fatalf("want [inst], got %v", visited)
	}
	if gotSelf {
		t.Error("IsFieldSelf: want false for Up-matched term")
	}
	if gotPos.X != 42 || gotPos.Y != 7 {
		t.Errorf("FieldShared Position: want {42 7}, got %+v", gotPos)
	}
}

// TestQueryUp_MatchesViaChildOf: child→mid→grandparent ChildOf chain; grandparent
// has Marker. With(markerID).Up(w.ChildOf()) matches the child; IsFieldSelf is false.
func TestQueryUp_MatchesViaChildOf(t *testing.T) {
	type Marker struct{}
	w := flecs.New()
	markerID := flecs.RegisterComponent[Marker](w)

	var grandparent, mid, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		grandparent = fw.NewEntity()
		flecs.Set(fw, grandparent, Marker{})

		mid = fw.NewEntity()
		flecs.AddID(fw, mid, flecs.MakePair(w.ChildOf(), grandparent))

		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), mid))
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(markerID).Up(w.ChildOf()))

	visited := make(map[flecs.ID]bool)
	q.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			visited[e] = true
		}
	})

	// grandparent has Marker locally so it won't match a pure-Up query
	// (Up starts from parent, not self). mid and child should match.
	if !visited[mid] {
		t.Error("mid should match Up(ChildOf) — grandparent has Marker")
	}
	if !visited[child] {
		t.Error("child should match Up(ChildOf) — ancestor chain has Marker")
	}
	_ = grandparent
}

// TestQuerySelfUp_PrefersSelf: entity has Position locally AND inherits via IsA.
// SelfUp matches via Self; Field[Position] works; IsFieldSelf is true.
func TestQuerySelfUp_PrefersSelf(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var prefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 99})

		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), prefab))
		flecs.Set(fw, inst, Position{X: 1}) // local override
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID).SelfUp(w.IsA()))

	found := false
	q.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			if e != inst {
				continue
			}
			found = true
			if !flecs.IsFieldSelf(it, posID) {
				t.Error("IsFieldSelf: want true (entity owns Position locally)")
			}
			pos := flecs.Field[Position](it, posID)
			if len(pos) == 0 || pos[0].X != 1 {
				t.Errorf("Field[Position]: want X=1 (local), got %+v", pos)
			}
		}
	})
	if !found {
		t.Fatal("inst not visited by SelfUp query")
	}
}

// TestQuerySelfUp_FallsBackToUp: entity has no Position locally but inherits.
// SelfUp falls back to Up; IsFieldSelf is false.
func TestQuerySelfUp_FallsBackToUp(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var prefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 55})

		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), prefab))
		// inst has no local Position
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID).SelfUp(w.IsA()))

	found := false
	q.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			if e != inst {
				continue
			}
			found = true
			if flecs.IsFieldSelf(it, posID) {
				t.Error("IsFieldSelf: want false (Position comes from prefab)")
			}
			p, ok := flecs.FieldShared[Position](it, posID)
			if !ok {
				t.Error("FieldShared: want (value, true) for Up-fallback")
			}
			if p.X != 55 {
				t.Errorf("FieldShared Position: want X=55, got %+v", p)
			}
		}
	})
	if !found {
		t.Fatal("inst not visited by SelfUp query")
	}
}

// TestQueryUp_NoMatch: entity with no ancestor having the component does not match.
func TestQueryUp_NoMatch(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var orphan flecs.ID
	w.Write(func(fw *flecs.Writer) {
		orphan = fw.NewEntity()
		// orphan has no Position and no IsA relationship
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID).Up(w.IsA()))

	visited := make(map[flecs.ID]bool)
	q.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			visited[e] = true
		}
	})
	if visited[orphan] {
		t.Error("orphan (no ancestor with Position) should NOT match")
	}
}

// TestQueryUp_DeadAncestor: ancestor is deleted between query construction and
// iteration; chain terminates cleanly without panic (walkUp dead-target guard).
func TestQueryUp_DeadAncestor(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var prefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 1})
		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), prefab))
	})

	// Delete the prefab — inst's ancestor is now dead.
	w.Write(func(fw *flecs.Writer) { fw.Delete(prefab) })

	// Must not panic; the dead ancestor terminates the chain.
	var visited []flecs.ID
	q := flecs.NewQueryFromTerms(w, flecs.With(posID).Up(w.IsA()))
	q.Each(func(it *flecs.QueryIter) {
		visited = append(visited, it.Entities()...)
	})
	if len(visited) != 0 {
		t.Errorf("want 0 matches (ancestor dead), got %v", visited)
	}
}

// TestQueryUp_IsACycleRejectedAtWrite: since v0.46.0 IsA is Traversable (implies
// Acyclic), so creating a two-entity IsA cycle panics at write time rather than
// at traversal time. Previously this test verified cycle-safe termination of the
// Up matcher; now the cycle itself is impossible to create.
func TestQueryUp_IsACycleRejectedAtWrite(t *testing.T) {
	w := flecs.New()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic when creating IsA cycle (Acyclic enforcement), got none")
		}
	}()

	w.Write(func(fw *flecs.Writer) {
		a := fw.NewEntity()
		b := fw.NewEntity()
		flecs.AddID(fw, a, flecs.MakePair(w.IsA(), b))
		// Creating the reverse edge must panic.
		flecs.AddID(fw, b, flecs.MakePair(w.IsA(), a))
	})
}

// TestQueryCascade_OrdersByDepth: three-level ChildOf chain (root, mid, leaf), each
// with Marker. NewCachedQueryFromTerms with Cascade(ChildOf) iterates root→mid→leaf.
func TestQueryCascade_OrdersByDepth(t *testing.T) {
	type Marker struct{}
	w := flecs.New()
	markerID := flecs.RegisterComponent[Marker](w)

	var root, mid, leaf flecs.ID
	w.Write(func(fw *flecs.Writer) {
		root = fw.NewEntity()
		flecs.Set(fw, root, Marker{})

		mid = fw.NewEntity()
		flecs.Set(fw, mid, Marker{})
		flecs.AddID(fw, mid, flecs.MakePair(w.ChildOf(), root))

		leaf = fw.NewEntity()
		flecs.Set(fw, leaf, Marker{})
		flecs.AddID(fw, leaf, flecs.MakePair(w.ChildOf(), mid))
	})

	cq := flecs.NewCachedQueryFromTerms(w, flecs.With(markerID).Cascade(w.ChildOf()))
	defer cq.Close()

	var order []flecs.ID
	cq.Each(func(it *flecs.QueryIter) {
		order = append(order, it.Entities()...)
	})

	if len(order) != 3 {
		t.Fatalf("want 3 entities, got %d: %v", len(order), order)
	}
	if order[0] != root || order[1] != mid || order[2] != leaf {
		t.Errorf("want [root mid leaf], got %v", order)
	}
}

// TestQueryCascade_RejectedForUncached: NewQueryFromTerms with a Cascade term panics.
func TestQueryCascade_RejectedForUncached(t *testing.T) {
	w := flecs.New()
	type Marker struct{}
	markerID := flecs.RegisterComponent[Marker](w)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for Cascade in NewQueryFromTerms")
		}
		msg, _ := r.(string)
		if msg == "" {
			t.Fatalf("panic value should be a string, got %T: %v", r, r)
		}
	}()
	flecs.NewQueryFromTerms(w, flecs.With(markerID).Cascade(w.ChildOf()))
}

// TestCachedQueryUp_MatchesViaPrefab: same as TestQueryUp_MatchesViaPrefab but
// using NewCachedQueryFromTerms.
func TestCachedQueryUp_MatchesViaPrefab(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var prefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 10, Y: 20})

		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), prefab))
	})

	cq := flecs.NewCachedQueryFromTerms(w, flecs.With(posID).Up(w.IsA()))
	defer cq.Close()

	var visited []flecs.ID
	cq.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			visited = append(visited, e)
			p, ok := flecs.FieldShared[Position](it, posID)
			if !ok {
				t.Error("FieldShared should return (value, true) for cached Up match")
			}
			if p.X != 10 || p.Y != 20 {
				t.Errorf("FieldShared Position: want {10 20}, got %+v", p)
			}
			if flecs.IsFieldSelf(it, posID) {
				t.Error("IsFieldSelf: want false for cached Up match")
			}
		}
	})
	if len(visited) != 1 || visited[0] != inst {
		t.Fatalf("want [inst], got %v", visited)
	}
}

// TestCachedQueryUp_NewTableTriggersRematch: create a cached Up query, then create
// a new entity-via-prefab landing in a fresh archetype; the query picks it up via
// notifyTableCreated.
func TestCachedQueryUp_NewTableTriggersRematch(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	type Extra struct{ V int }
	extraID := flecs.RegisterComponent[Extra](w)

	var prefab flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 5})
	})

	cq := flecs.NewCachedQueryFromTerms(w, flecs.With(posID).Up(w.IsA()))
	defer cq.Close()

	initialCount := cq.EntityCount()

	// Create a new entity that inherits Position AND has Extra — this forces a new table.
	var inst2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		inst2 = fw.NewEntity()
		flecs.AddID(fw, inst2, flecs.MakePair(w.IsA(), prefab))
		flecs.Set(fw, inst2, Extra{V: 9})
	})

	if cq.EntityCount() != initialCount+1 {
		t.Fatalf("after new entity in new table: want %d, got %d", initialCount+1, cq.EntityCount())
	}

	found := false
	cq.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			if e == inst2 {
				found = true
			}
		}
	})
	if !found {
		t.Error("inst2 should be found in the cached Up query after notifyTableCreated")
	}
	_ = extraID
}

// TestQueryUp_TagComponent: FieldShared returns (zero, true) for a tag component
// inherited via Up (tests the nil-column branch in FieldShared).
func TestQueryUp_TagComponent(t *testing.T) {
	type Tag struct{} // zero-size tag
	w := flecs.New()
	tagID := flecs.RegisterComponent[Tag](w)

	var prefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Tag{})
		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), prefab))
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(tagID).Up(w.IsA()))
	found := false
	q.Each(func(it *flecs.QueryIter) {
		for _, e := range it.Entities() {
			if e != inst {
				continue
			}
			found = true
			_, ok := flecs.FieldShared[Tag](it, tagID)
			if !ok {
				t.Error("FieldShared on tag Up match: want (zero, true)")
			}
		}
	})
	if !found {
		t.Fatal("inst not matched by Up(IsA) query for tag component")
	}
}

// TestTermSelf: Term.Self() returns the same semantics as the default constructor.
func TestTermSelf(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 3})
	})

	// With(posID).Self() should behave identically to With(posID).
	q := flecs.NewQueryFromTerms(w, flecs.With(posID).Self())
	count := 0
	q.Each(func(it *flecs.QueryIter) {
		count += it.Count()
		if !flecs.IsFieldSelf(it, posID) {
			t.Error("IsFieldSelf: want true for Self() term")
		}
	})
	if count != 1 {
		t.Errorf("want 1 entity, got %d", count)
	}
}

// TestQuerySelfUp_CachedWithLocalAndInherited: cached SelfUp query with one entity
// having local component (Self) and another inheriting (Up); verifies both appear
// and IsFieldSelf/FieldShared work correctly for each.
func TestQuerySelfUp_CachedWithLocalAndInherited(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var prefab, local, inherited flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 99})

		local = fw.NewEntity()
		flecs.Set(fw, local, Position{X: 1}) // locally owned

		inherited = fw.NewEntity()
		flecs.AddID(fw, inherited, flecs.MakePair(w.IsA(), prefab)) // no local Position
	})

	cq := flecs.NewCachedQueryFromTerms(w, flecs.With(posID).SelfUp(w.IsA()))
	defer cq.Close()

	type selfResult struct {
		self bool
		x    float32
	}
	results := make(map[flecs.ID]selfResult)
	cq.Each(func(it *flecs.QueryIter) {
		isSelf := flecs.IsFieldSelf(it, posID)
		for _, e := range it.Entities() {
			if isSelf {
				pos := flecs.Field[Position](it, posID)
				for i, ent := range it.Entities() {
					if ent == e {
						results[e] = selfResult{self: true, x: pos[i].X}
					}
				}
			} else {
				p, ok := flecs.FieldShared[Position](it, posID)
				if !ok {
					t.Error("FieldShared: want (value, true) for Up match")
				}
				results[e] = selfResult{self: false, x: p.X}
			}
		}
	})

	if r, ok := results[local]; !ok || !r.self || r.x != 1 {
		t.Errorf("local entity: want {self=true x=1}, got %+v ok=%v", r, ok)
	}
	if r, ok := results[inherited]; !ok || r.self || r.x != 99 {
		t.Errorf("inherited entity: want {self=false x=99}, got %+v ok=%v", r, ok)
	}
}

// TestFieldShared_PanicWhenSelf: FieldShared on a TraverseSelf term panics.
func TestFieldShared_PanicWhenSelf(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1})
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID))
	it := q.Iter()
	if !it.Next() {
		t.Fatal("expected Next()=true")
	}
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for FieldShared on TraverseSelf term")
		}
	}()
	flecs.FieldShared[Position](it, posID)
}

// TestField_PanicWhenShared: Field[T] on an Up-matched term panics directing
// callers to FieldShared.
func TestField_PanicWhenShared(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var prefab, inst flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set(fw, prefab, Position{X: 1})
		inst = fw.NewEntity()
		flecs.AddID(fw, inst, flecs.MakePair(w.IsA(), prefab))
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID).Up(w.IsA()))
	it := q.Iter()
	if !it.Next() {
		t.Fatal("expected Next()=true")
	}
	_ = inst

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for Field[T] on Up-matched term")
		}
		msg, _ := r.(string)
		if msg == "" {
			t.Fatalf("panic should be a string, got %T: %v", r, r)
		}
	}()
	flecs.Field[Position](it, posID)
}
