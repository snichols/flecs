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

	e1 := w.NewEntity()
	flecs.Set(w, e1, Position{X: 1})
	e2 := w.NewEntity()
	flecs.Set(w, e2, Position{X: 2})
	flecs.Set(w, e2, Velocity{DX: 1})
	// e3 has only Velocity — should not match.
	e3 := w.NewEntity()
	flecs.Set(w, e3, Velocity{DX: 3})

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
}

// ── Without (TermNot) ─────────────────────────────────────────────────────────

func TestQueryTermsWithout(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	deadID := w.NewEntity() // tag

	// alive: has Position, no dead tag.
	alive := w.NewEntity()
	flecs.Set(w, alive, Position{X: 1})
	// dead: has Position and dead tag.
	dead := w.NewEntity()
	flecs.Set(w, dead, Position{X: 2})
	flecs.AddID(w, dead, deadID)

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
}

// ── Maybe (TermOptional) ──────────────────────────────────────────────────────

func TestQueryTermsMaybe(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	// e1: Position only.
	e1 := w.NewEntity()
	flecs.Set(w, e1, Position{X: 1})
	// e2: Position + Velocity.
	e2 := w.NewEntity()
	flecs.Set(w, e2, Position{X: 2})
	flecs.Set(w, e2, Velocity{DX: 10})

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
	deadID := w.NewEntity()

	// match: Position, no dead, optional Velocity.
	eAlive := w.NewEntity()
	flecs.Set(w, eAlive, Position{X: 1})
	// match: Position + Velocity, no dead.
	eAliveVel := w.NewEntity()
	flecs.Set(w, eAliveVel, Position{X: 2})
	flecs.Set(w, eAliveVel, Velocity{DX: 5})
	// no match: Position + dead.
	eDead := w.NewEntity()
	flecs.Set(w, eDead, Position{X: 3})
	flecs.AddID(w, eDead, deadID)

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
	aID := w.NewEntity()
	bID := w.NewEntity()

	// match: only Position.
	eOnly := w.NewEntity()
	flecs.Set(w, eOnly, Position{X: 1})
	// no match: Position + A.
	eA := w.NewEntity()
	flecs.Set(w, eA, Position{})
	flecs.AddID(w, eA, aID)
	// no match: Position + B.
	eB := w.NewEntity()
	flecs.Set(w, eB, Position{})
	flecs.AddID(w, eB, bID)
	// no match: Position + A + B.
	eAB := w.NewEntity()
	flecs.Set(w, eAB, Position{})
	flecs.AddID(w, eAB, aID)
	flecs.AddID(w, eAB, bID)

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
}

// ── Multiple Optional terms ───────────────────────────────────────────────────

func TestQueryTermsMultipleOptional(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)
	type Mass struct{ V float32 }
	massID := flecs.RegisterComponent[Mass](w)

	// e1: Position only.
	e1 := w.NewEntity()
	flecs.Set(w, e1, Position{X: 1})
	// e2: Position + Velocity.
	e2 := w.NewEntity()
	flecs.Set(w, e2, Position{X: 2})
	flecs.Set(w, e2, Velocity{DX: 1})
	// e3: Position + Mass.
	e3 := w.NewEntity()
	flecs.Set(w, e3, Position{X: 3})
	flecs.Set(w, e3, Mass{V: 5})
	// e4: Position + Velocity + Mass.
	e4 := w.NewEntity()
	flecs.Set(w, e4, Position{X: 4})
	flecs.Set(w, e4, Velocity{DX: 2})
	flecs.Set(w, e4, Mass{V: 10})

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

	e1 := w.NewEntity()
	flecs.Set(w, e1, Position{X: 1})
	e2 := w.NewEntity()
	flecs.Set(w, e2, Position{X: 2})
	flecs.Set(w, e2, Velocity{DX: 1})

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

	e := w.NewEntity()
	flecs.Set(w, e, Position{X: 1})

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
	e := w.NewEntity()
	flecs.Set(w, e, Position{X: 1})

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
	deadID := w.NewEntity()

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
	deadID := w.NewEntity()

	// alive: Position only.
	eAlive := w.NewEntity()
	flecs.Set(w, eAlive, Position{X: 1})
	// dead: Position + dead tag.
	eDead := w.NewEntity()
	flecs.Set(w, eDead, Position{X: 2})
	flecs.AddID(w, eDead, deadID)

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
	deadID := w.NewEntity()

	e := w.NewEntity()
	flecs.Set(w, e, Position{X: 1})

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
	flecs.AddID(w, e, deadID)
	if cq.EntityCount() != 0 {
		t.Fatalf("after AddID(dead): want 0, got %d", cq.EntityCount())
	}

	// Remove dead tag: entity migrates back to [P]. [P] table is still in cache.
	flecs.RemoveID(w, e, deadID)
	if cq.EntityCount() != 1 {
		t.Fatalf("after RemoveID(dead): want 1, got %d", cq.EntityCount())
	}

	// Re-add dead: entity leaves [P] again.
	flecs.AddID(w, e, deadID)
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
	deadID := w.NewEntity()

	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(posID),
		flecs.Without(deadID),
	)
	defer cq.Close()

	if cq.Count() != 0 {
		t.Fatalf("empty world: want 0, got %d", cq.Count())
	}

	// Set Position: creates [P] table → matches (P present, dead absent).
	e1 := w.NewEntity()
	flecs.Set(w, e1, Position{X: 1})
	if cq.Count() != 1 {
		t.Fatalf("after [P] entity: want 1 table, got %d", cq.Count())
	}

	// Set Extra: e1 migrates to [P,Extra] → new table, matches.
	flecs.Set(w, e1, Extra{V: 42})
	if cq.Count() != 2 {
		t.Fatalf("after [P,Extra] migration: want 2 tables, got %d", cq.Count())
	}

	// Create entity that ends up with [P, dead]: new table should NOT be added
	// (dead is present, violating the Without term).
	e2 := w.NewEntity()
	flecs.Set(w, e2, Position{X: 2}) // migrates to [P], already cached
	flecs.AddID(w, e2, deadID)       // migrates to [P,dead] — new table, rejected

	if cq.Count() != 2 {
		t.Fatalf("after [P,dead] entity: want 2 tables (unchanged), got %d", cq.Count())
	}
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

	e := w.NewEntity()
	flecs.Set(w, e, Position{X: 1})

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
	deadID := w.NewEntity()

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

	eSleep := w.NewEntity()
	flecs.Set(w, eSleep, Position{X: 1})
	flecs.Set(w, eSleep, Sleeping{})

	eWork := w.NewEntity()
	flecs.Set(w, eWork, Position{X: 2})
	flecs.Set(w, eWork, Working{})

	ePlay := w.NewEntity()
	flecs.Set(w, ePlay, Position{X: 3})
	flecs.Set(w, ePlay, Playing{})

	eNone := w.NewEntity()
	flecs.Set(w, eNone, Position{X: 4}) // no activity — should NOT match

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

	// Matches: has A, X, C.
	eAXC := w.NewEntity()
	flecs.Set(w, eAXC, CompA{})
	flecs.Set(w, eAXC, CompX{})
	flecs.Set(w, eAXC, CompC{})

	// Matches: has B, X, D.
	eBXD := w.NewEntity()
	flecs.Set(w, eBXD, CompB{})
	flecs.Set(w, eBXD, CompX{})
	flecs.Set(w, eBXD, CompD{})

	// No match: has A, X but neither C nor D.
	eAXonly := w.NewEntity()
	flecs.Set(w, eAXonly, CompA{})
	flecs.Set(w, eAXonly, CompX{})

	// No match: has A, C but no X.
	eACnoX := w.NewEntity()
	flecs.Set(w, eACnoX, CompA{})
	flecs.Set(w, eACnoX, CompC{})

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

	eWithA := w.NewEntity()
	flecs.Set(w, eWithA, Position{X: 1})
	flecs.Set(w, eWithA, CompA{})

	eWithout := w.NewEntity()
	flecs.Set(w, eWithout, Position{X: 2}) // no A

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

	// Matches: Pos, A, no Dead.
	eA := w.NewEntity()
	flecs.Set(w, eA, Position{})
	flecs.Set(w, eA, CompA{})

	// Matches: Pos, B, no Dead.
	eB := w.NewEntity()
	flecs.Set(w, eB, Position{})
	flecs.Set(w, eB, CompB{})

	// No match: Pos, A, Dead.
	eADead := w.NewEntity()
	flecs.Set(w, eADead, Position{})
	flecs.Set(w, eADead, CompA{})
	flecs.Set(w, eADead, Dead{})

	// No match: Pos, no A/B.
	eNone := w.NewEntity()
	flecs.Set(w, eNone, Position{})

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

	// Pos + A + C.
	eAC := w.NewEntity()
	flecs.Set(w, eAC, Position{})
	flecs.Set(w, eAC, CompA{})
	flecs.Set(w, eAC, CompC{V: 7})

	// Pos + B, no C.
	eB := w.NewEntity()
	flecs.Set(w, eB, Position{})
	flecs.Set(w, eB, CompB{})

	// Pos only — should NOT match.
	eNone := w.NewEntity()
	flecs.Set(w, eNone, Position{})

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
	eA := w.NewEntity()
	flecs.Set(w, eA, Position{})
	flecs.Set(w, eA, CompA{})

	eB := w.NewEntity()
	flecs.Set(w, eB, Position{})
	flecs.Set(w, eB, CompB{})

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
	eNeither := w.NewEntity()
	flecs.Set(w, eNeither, Position{})

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
	if matched2[eNeither] {
		t.Error("eNeither (Pos-only) should not appear in Or-cached query results")
	}
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
	e1 := w.NewEntity()
	flecs.Set(w, e1, Position{})
	flecs.Set(w, e1, CompA{})
	if cq.Count() != 1 {
		t.Fatalf("after [Pos,A] entity: want 1 table, got %d", cq.Count())
	}

	// Add Extra to e1 → migrates to [Pos,A,Extra], new table, should be added.
	flecs.Set(w, e1, Extra{V: 5})
	if cq.Count() != 2 {
		t.Fatalf("after [Pos,A,Extra] migration: want 2 tables, got %d", cq.Count())
	}

	// Create [Pos, Extra] entity → table lacks A and B, should NOT be added.
	e2 := w.NewEntity()
	flecs.Set(w, e2, Position{})
	flecs.Set(w, e2, Extra{V: 9})
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

	e := w.NewEntity()
	flecs.Set(w, e, Position{X: 1})
	flecs.Set(w, e, CompA{V: 42})
	// no CompB

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

	e := w.NewEntity()
	flecs.Set(w, e, Position{})
	flecs.Set(w, e, CompA{})
	// no CompB

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
	deadID := w.NewEntity()
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

	e1 := w.NewEntity()
	flecs.Set(w, e1, Position{X: 1})
	e2 := w.NewEntity()
	flecs.Set(w, e2, Position{X: 2})
	flecs.Set(w, e2, Velocity{DX: 7})

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
