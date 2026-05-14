package flecs_test

import (
	"sort"
	"testing"

	"github.com/snichols/flecs"
)

// Component and tag types used exclusively by variable query tests.
type qvSpaceShip struct{}
type qvPlanet struct{ Name string }
type qvStar struct{}
type qvSimTime2 struct{ DT float32 }

// qvBuildScene constructs the canonical spaceships-and-planets scene.
//
//	spaceships: A (→P1), B (→P1, →P2), C (→P2)
//	planets:    P1, P2
//
// All component registrations happen before the Write scope so that the world
// receives the registration correctly.
func qvBuildScene(w *flecs.World) (
	shipID, planetID, dockedToID flecs.ID,
	A, B, C, P1, P2 flecs.ID,
) {
	shipID = flecs.RegisterComponent[qvSpaceShip](w)
	planetID = flecs.RegisterComponent[qvPlanet](w)
	w.Write(func(fw *flecs.Writer) {
		dockedToID = fw.NewEntity() // relationship entity (tag-style pairs)

		P1 = fw.NewEntity()
		flecs.Set(fw, P1, qvPlanet{Name: "P1"})
		P2 = fw.NewEntity()
		flecs.Set(fw, P2, qvPlanet{Name: "P2"})

		A = fw.NewEntity()
		flecs.Set(fw, A, qvSpaceShip{})
		flecs.AddID(fw, A, flecs.MakePair(dockedToID, P1))

		B = fw.NewEntity()
		flecs.Set(fw, B, qvSpaceShip{})
		flecs.AddID(fw, B, flecs.MakePair(dockedToID, P1))
		flecs.AddID(fw, B, flecs.MakePair(dockedToID, P2))

		C = fw.NewEntity()
		flecs.Set(fw, C, qvSpaceShip{})
		flecs.AddID(fw, C, flecs.MakePair(dockedToID, P2))
	})
	return
}

// TestVarSingleVarJoin is the canonical motivating example from the issue:
//
//	SpaceShip, DockedTo($this, $planet), Planet($planet)
//
// Expects exactly 4 rows: A→P1, B→P1, B→P2, C→P2.
func TestVarSingleVarJoin(t *testing.T) {
	w := flecs.New()
	shipID, planetID, dockedToID, A, B, C, P1, P2 := qvBuildScene(w)

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(shipID),
			flecs.WithPairTgtVar(dockedToID, "planet"),
			flecs.WithVar(planetID, "planet"),
		)
		it := q.Iter()

		type row struct{ ship, planet flecs.ID }
		var got []row
		for it.Next() {
			ents := it.Entities()
			if len(ents) != 1 {
				t.Fatalf("want 1 entity per step, got %d", len(ents))
			}
			got = append(got, row{ship: ents[0], planet: it.Var("planet")})
		}
		if len(got) != 4 {
			t.Fatalf("want 4 rows, got %d: %v", len(got), got)
		}
		want := map[[2]flecs.ID]bool{
			{A, P1}: true,
			{B, P1}: true,
			{B, P2}: true,
			{C, P2}: true,
		}
		for _, r := range got {
			if !want[[2]flecs.ID{r.ship, r.planet}] {
				t.Errorf("unexpected row ship=%v planet=%v", r.ship, r.planet)
			}
		}
	})
}

// TestVarTargetBoundOnly queries with only WithPairTgtVar (no Planet constraint).
// Every dock target is allowed; rows = all docking pairs (4 total).
func TestVarTargetBoundOnly(t *testing.T) {
	w := flecs.New()
	_, _, dockedToID, A, B, C, P1, P2 := qvBuildScene(w)

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.WithPairTgtVar(dockedToID, "planet"),
		)
		it := q.Iter()

		type row struct{ ship, planet flecs.ID }
		var got []row
		for it.Next() {
			ents := it.Entities()
			if len(ents) != 1 {
				t.Fatalf("want 1 entity per step, got %d", len(ents))
			}
			got = append(got, row{ship: ents[0], planet: it.Var("planet")})
		}
		if len(got) != 4 {
			t.Fatalf("want 4 rows (all docking pairs), got %d: %v", len(got), got)
		}
		want := map[[2]flecs.ID]bool{
			{A, P1}: true,
			{B, P1}: true,
			{B, P2}: true,
			{C, P2}: true,
		}
		for _, r := range got {
			if !want[[2]flecs.ID{r.ship, r.planet}] {
				t.Errorf("unexpected row ship=%v planet=%v", r.ship, r.planet)
			}
		}
	})
}

// TestVarMultiHopDeferred documents the deferral of multi-hop relational joins
// (requires multi-variable support; Phase 16.25.x).
func TestVarMultiHopDeferred(t *testing.T) {
	t.Skip("multi-hop relational joins require multi-variable support (Phase 16.25.x); see issue #231")
}

// TestVarNoMatches verifies that WithPairTgtVar yields zero results when no entity
// holds the queried relationship pair.
func TestVarNoMatches(t *testing.T) {
	w := flecs.New()
	var dockedToID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dockedToID = fw.NewEntity()
		// No entity has any DockedTo pair.
		_ = fw.NewEntity()
	})

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.WithPairTgtVar(dockedToID, "planet"),
		)
		it := q.Iter()
		count := 0
		for it.Next() {
			count++
		}
		if count != 0 {
			t.Fatalf("want 0 rows when no DockedTo pairs exist, got %d", count)
		}
	})
}

// TestVarEmptyDriverDomain verifies that WithVar yields zero results when no entity
// has the constraining component (empty driver domain).
func TestVarEmptyDriverDomain(t *testing.T) {
	w := flecs.New()
	planetID := flecs.RegisterComponent[qvPlanet](w)
	var dockedToID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dockedToID = fw.NewEntity()
		// No entity has Planet component.
		_ = fw.NewEntity()
	})

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.WithPairTgtVar(dockedToID, "planet"),
			flecs.WithVar(planetID, "planet"),
		)
		it := q.Iter()
		count := 0
		for it.Next() {
			count++
		}
		if count != 0 {
			t.Fatalf("want 0 rows when driver domain is empty, got %d", count)
		}
	})
}

// TestVarCombinedCanonical is the full three-term canonical query from the spec:
//
//	With(SpaceShip), WithPairTgtVar(DockedTo, "planet"), WithVar(Planet, "planet")
func TestVarCombinedCanonical(t *testing.T) {
	w := flecs.New()
	shipID, planetID, dockedToID, A, B, C, P1, P2 := qvBuildScene(w)

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(shipID),
			flecs.WithPairTgtVar(dockedToID, "planet"),
			flecs.WithVar(planetID, "planet"),
		)
		it := q.Iter()
		type row struct{ ship, planet flecs.ID }
		var got []row
		for it.Next() {
			got = append(got, row{ship: it.Entities()[0], planet: it.Var("planet")})
		}
		if len(got) != 4 {
			t.Fatalf("want 4 rows, got %d", len(got))
		}
		want := [][2]flecs.ID{{A, P1}, {B, P1}, {B, P2}, {C, P2}}
		seen := make(map[[2]flecs.ID]int)
		for _, r := range got {
			seen[[2]flecs.ID{r.ship, r.planet}]++
		}
		for _, w2 := range want {
			if seen[w2] != 1 {
				t.Errorf("expected row %v exactly once, saw %d times", w2, seen[w2])
			}
		}
	})
}

// TestVarCorrectBinding verifies that Var("planet") returns the correct binding
// for each iteration step.
func TestVarCorrectBinding(t *testing.T) {
	w := flecs.New()
	shipID, planetID, dockedToID, A, _, _, P1, P2 := qvBuildScene(w)

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(shipID),
			flecs.WithPairTgtVar(dockedToID, "planet"),
			flecs.WithVar(planetID, "planet"),
		)
		it := q.Iter()
		for it.Next() {
			ship := it.Entities()[0]
			planet := it.Var("planet")

			if ship == A && planet != P1 {
				t.Errorf("ship A: want planet=P1, got %v", planet)
			}
			if planet != P1 && planet != P2 {
				t.Errorf("planet binding %v is not P1=%v or P2=%v", planet, P1, P2)
			}
		}
	})
}

// TestVarUndefinedPanics verifies that Var("undefined") panics with a clear error.
func TestVarUndefinedPanics(t *testing.T) {
	w := flecs.New()
	shipID, planetID, dockedToID, _, _, _, _, _ := qvBuildScene(w)

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(shipID),
			flecs.WithPairTgtVar(dockedToID, "planet"),
			flecs.WithVar(planetID, "planet"),
		)
		it := q.Iter()
		if !it.Next() {
			t.Fatal("expected at least one result row")
		}
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for undefined variable name, got none")
			}
		}()
		it.Var("undefined") // should panic
	})
}

// TestVarCachedQuery verifies that consecutive Iter() calls on a CachedQuery with
// variables produce identical result sets (bindings are re-executed each call).
func TestVarCachedQuery(t *testing.T) {
	w := flecs.New()
	shipID, planetID, dockedToID, _, _, _, _, _ := qvBuildScene(w)

	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(shipID),
		flecs.WithPairTgtVar(dockedToID, "planet"),
		flecs.WithVar(planetID, "planet"),
	)

	collectRows := func() [][2]flecs.ID {
		it := cq.Iter()
		var rows [][2]flecs.ID
		for it.Next() {
			rows = append(rows, [2]flecs.ID{it.Entities()[0], it.Var("planet")})
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i][0] != rows[j][0] {
				return rows[i][0] < rows[j][0]
			}
			return rows[i][1] < rows[j][1]
		})
		return rows
	}

	first := collectRows()
	second := collectRows()

	if len(first) != 4 {
		t.Fatalf("first iter: want 4 rows, got %d", len(first))
	}
	if len(first) != len(second) {
		t.Fatalf("row count mismatch: first=%d second=%d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("row %d differs: first=%v second=%v", i, first[i], second[i])
		}
	}
}

// TestVarMixedWithFixedSource verifies that a variable query works correctly when
// combined with a fixed-source term (Phase 16.18 + Phase 16.25 composition).
func TestVarMixedWithFixedSource(t *testing.T) {
	w := flecs.New()
	simTimeID := flecs.RegisterComponent[qvSimTime2](w)
	shipID, planetID, dockedToID, _, _, _, _, _ := qvBuildScene(w)
	var game flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		flecs.Set(fw, game, qvSimTime2{DT: 0.016})
	})

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(shipID),
			flecs.WithSourceTerm(simTimeID, game), // fixed-source
			flecs.WithPairTgtVar(dockedToID, "planet"),
			flecs.WithVar(planetID, "planet"),
		)
		it := q.Iter()
		count := 0
		for it.Next() {
			count++
			st := flecs.Field[qvSimTime2](it, simTimeID)
			if len(st) != 1 || st[0].DT != 0.016 {
				t.Errorf("step %d: unexpected SimTime %v", count, st)
			}
			planet := it.Var("planet")
			if planet == 0 {
				t.Errorf("step %d: zero planet binding", count)
			}
		}
		if count != 4 {
			t.Fatalf("want 4 rows (4 docking pairs), got %d", count)
		}
	})
}

// TestVarWithoutOnVariable verifies that Without on an implicit $this term
// filters the driver domain when the query has no regular $this And terms.
func TestVarWithoutOnVariable(t *testing.T) {
	w := flecs.New()
	planetID := flecs.RegisterComponent[qvPlanet](w)
	starID := flecs.RegisterComponent[qvStar](w)
	var P1, P2, P3 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		P1 = fw.NewEntity()
		flecs.Set(fw, P1, qvPlanet{Name: "P1"})

		P2 = fw.NewEntity()
		flecs.Set(fw, P2, qvPlanet{Name: "P2"})
		flecs.Set(fw, P2, qvStar{}) // excluded by Without(Star)

		P3 = fw.NewEntity()
		flecs.Set(fw, P3, qvPlanet{Name: "P3"})
	})

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.WithVar(planetID, "planet"),
			flecs.Without(starID),
		)
		it := q.Iter()
		var planets []flecs.ID
		for it.Next() {
			planets = append(planets, it.Var("planet"))
		}
		if len(planets) != 2 {
			t.Fatalf("want 2 planets (P1, P3), got %d: %v", len(planets), planets)
		}
		for _, p := range planets {
			if p == P2 {
				t.Error("P2 (has Star) must be excluded by Without(Star)")
			}
			if p != P1 && p != P3 {
				t.Errorf("unexpected planet %v", p)
			}
		}
	})
}

// TestVarCountAndEntities verifies Count() and Entities() semantics in variable mode.
func TestVarCountAndEntities(t *testing.T) {
	w := flecs.New()
	shipID, planetID, dockedToID, _, _, _, _, _ := qvBuildScene(w)

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(shipID),
			flecs.WithPairTgtVar(dockedToID, "planet"),
			flecs.WithVar(planetID, "planet"),
		)
		it := q.Iter()
		steps := 0
		for it.Next() {
			steps++
			if it.Count() != 1 {
				t.Errorf("Count() must be 1 in variable mode, got %d", it.Count())
			}
			ents := it.Entities()
			if len(ents) != 1 {
				t.Errorf("Entities() must return 1 entity in variable mode, got %d", len(ents))
			}
		}
		if steps != 4 {
			t.Fatalf("want 4 steps, got %d", steps)
		}
	})
}

// TestVarFieldAccessSrcVar verifies that Field[T] correctly reads a component from
// the variable-bound source entity (SrcVar term semantics).
func TestVarFieldAccessSrcVar(t *testing.T) {
	w := flecs.New()
	shipID, planetID, dockedToID, _, _, _, _, _ := qvBuildScene(w)

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(shipID),
			flecs.WithPairTgtVar(dockedToID, "planet"),
			flecs.WithVar(planetID, "planet"),
		)
		it := q.Iter()
		count := 0
		for it.Next() {
			count++
			// Field[qvPlanet] reads from the $planet bound entity.
			planets := flecs.Field[qvPlanet](it, planetID)
			if len(planets) != 1 {
				t.Errorf("step %d: expected 1-element planet slice, got %d", count, len(planets))
			}
		}
		if count != 4 {
			t.Fatalf("want 4 rows, got %d", count)
		}
	})
}

// TestVarConstructorPanics verifies that invalid constructor calls panic.
func TestVarConstructorPanics(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"WithVar zero componentID", func() { flecs.WithVar(0, "planet") }},
		{"WithVar empty varName", func() { flecs.WithVar(1, "") }},
		{"WithPairTgtVar zero rel", func() { flecs.WithPairTgtVar(0, "planet") }},
		{"WithPairTgtVar empty varName", func() { flecs.WithPairTgtVar(1, "") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("expected panic for %s", tc.name)
				}
			}()
			tc.fn()
		})
	}
}

// TestVarNotPositionedPanics verifies that Var() panics when Next() has not been called.
func TestVarNotPositionedPanics(t *testing.T) {
	w := flecs.New()
	planetID := flecs.RegisterComponent[qvPlanet](w)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, qvPlanet{Name: "X"})
	})

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.WithVar(planetID, "planet"),
		)
		it := q.Iter()
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic when calling Var before Next")
			}
		}()
		it.Var("planet") // before any Next() call
	})
}

// TestVarNoQueryVarsPanics verifies that Var() panics on a non-variable query.
func TestVarNoQueryVarsPanics(t *testing.T) {
	w := flecs.New()
	planetID := flecs.RegisterComponent[qvPlanet](w)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, qvPlanet{Name: "X"})
	})

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w, flecs.With(planetID))
		it := q.Iter()
		if !it.Next() {
			t.Fatal("expected at least one result")
		}
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic when calling Var on non-variable query")
			}
		}()
		it.Var("planet")
	})
}

// TestVarCapExceededPanics verifies that exceeding the 8-variable cap panics at
// query construction time.
func TestVarCapExceededPanics(t *testing.T) {
	w := flecs.New()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when variable count exceeds cap of 8")
		}
	}()
	// Build 9 distinct SrcVar terms to hit the cap.
	terms := make([]flecs.Term, 9)
	for i := 0; i < 9; i++ {
		name := string([]byte{byte('a' + i)}) // "a".."i"
		terms[i] = flecs.WithVar(flecs.ID(uint64(i+1)), name)
	}
	_ = flecs.NewQueryFromTerms(w, terms...)
}

// TestVarSrcVarMutualExclusivity verifies that combining SrcVar with a fixed Src panics.
func TestVarSrcVarMutualExclusivity(t *testing.T) {
	w := flecs.New()
	planetID := flecs.RegisterComponent[qvPlanet](w)
	var game flecs.ID
	w.Write(func(fw *flecs.Writer) {
		game = fw.NewEntity()
		flecs.Set(fw, game, qvPlanet{Name: "game"})
	})
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when combining SrcVar with fixed Src")
		}
	}()
	_ = flecs.NewQueryFromTerms(w,
		flecs.WithVar(planetID, "planet").Source(game), // both SrcVar and Src set
	)
}

// TestVarTgtVarWithPairIDPanics verifies that combining TgtVar with a pair ID panics.
func TestVarTgtVarWithPairIDPanics(t *testing.T) {
	w := flecs.New()
	var relID, targetID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		relID = fw.NewEntity()
		targetID = fw.NewEntity()
	})
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when using TgtVar with a pair ID")
		}
	}()
	pairID := flecs.MakePair(relID, targetID)
	_ = flecs.NewQueryFromTerms(w,
		flecs.WithPairTgtVar(pairID, "target"), // pairID.IsPair() == true → panic
	)
}

// TestVarSkipDisabledTable verifies that variable query nested-loop mode honours
// skipDisabled: $this candidate tables with the Disabled tag are excluded.
func TestVarSkipDisabledTable(t *testing.T) {
	w := flecs.New()
	shipID, planetID, dockedToID, _, _, _, P1, _ := qvBuildScene(w)
	var disabledShip flecs.ID
	w.Write(func(fw *flecs.Writer) {
		// Add a fourth ship that is disabled; it should never appear in results.
		disabledShip = fw.NewEntity()
		flecs.Set(fw, disabledShip, qvSpaceShip{})
		flecs.AddID(fw, disabledShip, flecs.MakePair(dockedToID, P1))
		flecs.DisableEntity(fw, disabledShip)
	})

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(shipID),
			flecs.WithPairTgtVar(dockedToID, "planet"),
			flecs.WithVar(planetID, "planet"),
		)
		it := q.Iter()
		for it.Next() {
			ship := it.Entities()[0]
			if ship == disabledShip {
				t.Error("disabled ship must not appear in variable query results")
			}
		}
	})
}

// TestVarSkipPrefabTable verifies that variable query nested-loop mode honours
// skipPrefab: $this candidate tables with the Prefab tag are excluded.
func TestVarSkipPrefabTable(t *testing.T) {
	w := flecs.New()
	shipID, planetID, dockedToID, _, _, _, P1, _ := qvBuildScene(w)
	var prefabShip flecs.ID
	w.Write(func(fw *flecs.Writer) {
		// Add a prefab ship; it should never appear in results.
		prefabShip = fw.NewEntity()
		flecs.Set(fw, prefabShip, qvSpaceShip{})
		flecs.AddID(fw, prefabShip, flecs.MakePair(dockedToID, P1))
		flecs.MarkPrefab(fw, prefabShip)
	})

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(shipID),
			flecs.WithPairTgtVar(dockedToID, "planet"),
			flecs.WithVar(planetID, "planet"),
		)
		it := q.Iter()
		for it.Next() {
			ship := it.Entities()[0]
			if ship == prefabShip {
				t.Error("prefab ship must not appear in variable query results")
			}
		}
	})
}

// TestVarOrGroupConstraint verifies that an OR-group term is correctly evaluated
// in varCheckTable: $this must have at least one component from the OR-group.
func TestVarOrGroupConstraint(t *testing.T) {
	w := flecs.New()
	shipID, planetID, dockedToID, A, _, C, P1, P2 := qvBuildScene(w)
	starID := flecs.RegisterComponent[qvStar](w)

	// Give ships A and C the Star component; B does not have it.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, A, qvStar{})
		flecs.Set(fw, C, qvStar{})
	})

	w.Read(func(_ *flecs.Reader) {
		// OR(SpaceShip | Star) ensures every matched $this table has at least one.
		// Since all ships have SpaceShip, this passes for all; the OR-group "matched"
		// path is covered. Verify row count is still 4.
		q := flecs.NewQueryFromTerms(w,
			flecs.Or(shipID),
			flecs.Or(starID),
			flecs.WithPairTgtVar(dockedToID, "planet"),
			flecs.WithVar(planetID, "planet"),
		)
		it := q.Iter()
		count := 0
		for it.Next() {
			count++
		}
		// A (Ship+Star→P1), B (Ship→P1,P2), B (Ship→P2), C (Ship+Star→P2)
		// Tables that match OR(Ship|Star): all ship tables.
		// B has only SpaceShip; A and C have SpaceShip+Star.
		// We expect 4 rows.
		if count != 4 {
			t.Fatalf("want 4 rows with OR-group constraint, got %d", count)
		}
		_ = P1
		_ = P2
	})
}

// TestVarCollectDomainIntersection exercises the intersection path in
// collectVarDomain: two WithVar terms that both name the same variable produce
// a set intersection, not a union.
func TestVarCollectDomainIntersection(t *testing.T) {
	w := flecs.New()
	planetID := flecs.RegisterComponent[qvPlanet](w)
	starID := flecs.RegisterComponent[qvStar](w)
	var P1, P2, S1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		// P1: has both Planet and Star
		P1 = fw.NewEntity()
		flecs.Set(fw, P1, qvPlanet{Name: "P1"})
		flecs.Set(fw, P1, qvStar{})

		// P2: has only Planet
		P2 = fw.NewEntity()
		flecs.Set(fw, P2, qvPlanet{Name: "P2"})

		// S1: has only Star
		S1 = fw.NewEntity()
		flecs.Set(fw, S1, qvStar{})
	})

	w.Read(func(_ *flecs.Reader) {
		// Two SrcVar terms for the SAME variable: domain = Planet ∩ Star = {P1}
		q := flecs.NewQueryFromTerms(w,
			flecs.WithVar(planetID, "x"),
			flecs.WithVar(starID, "x"),
		)
		it := q.Iter()
		var got []flecs.ID
		for it.Next() {
			got = append(got, it.Var("x"))
		}
		if len(got) != 1 || got[0] != P1 {
			t.Fatalf("intersection: want [P1], got %v (P1=%v P2=%v S1=%v)", got, P1, P2, S1)
		}
	})
}

// TestVarEntitiesAfterExhaustion verifies that Entities() returns nil when
// called after Next() returns false (sparseEntity == 0 in var mode).
func TestVarEntitiesAfterExhaustion(t *testing.T) {
	w := flecs.New()
	planetID := flecs.RegisterComponent[qvPlanet](w)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, qvPlanet{Name: "X"})
	})

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.WithVar(planetID, "planet"),
		)
		it := q.Iter()
		for it.Next() {
			// consume all rows
		}
		// After exhaustion Next() set sparseEntity = 0
		if ents := it.Entities(); ents != nil {
			t.Errorf("Entities() after exhaustion: want nil, got %v", ents)
		}
	})
}
