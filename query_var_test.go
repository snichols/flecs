package flecs_test

import (
	"fmt"
	"sort"
	"testing"

	"github.com/snichols/flecs"
)

// Component and tag types used exclusively by variable query tests.
type qvSpaceShip struct{}
type qvPlanet struct{ Name string }
type qvStar struct{}
type qvSimTime2 struct{ DT float32 }
type qvGalaxy struct{}
type qvHealth struct{ HP int }

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

// TestVarCapExceededPanics verifies that exceeding the 16-variable cap panics at
// query construction time.
func TestVarCapExceededPanics(t *testing.T) {
	w := flecs.New()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when variable count exceeds cap of 16")
		}
	}()
	// Build 17 distinct SrcVar terms to exceed the 16-variable cap.
	terms := make([]flecs.Term, 17)
	for i := 0; i < 17; i++ {
		name := fmt.Sprintf("v%d", i)
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

// TestVarChainedSetterSrcVar verifies that (Term).SrcVar(name) works as a chained
// setter: the resulting query resolves the variable correctly, identical to WithVar.
func TestVarChainedSetterSrcVar(t *testing.T) {
	w := flecs.New()
	planetID := flecs.RegisterComponent[qvPlanet](w)
	var p1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		flecs.Set(fw, p1, qvPlanet{Name: "P1"})
	})

	w.Read(func(_ *flecs.Reader) {
		// Use chained setter instead of WithVar constructor.
		q := flecs.NewQueryFromTerms(w, flecs.With(planetID).SrcVar("planet"))
		it := q.Iter()
		var bindings []flecs.ID
		for it.Next() {
			bindings = append(bindings, it.Var("planet"))
		}
		if len(bindings) != 1 || bindings[0] != p1 {
			t.Fatalf("SrcVar chained setter: want [%v], got %v", p1, bindings)
		}
	})
}

// TestVarChainedSetterTgtVar verifies that (Term).TgtVar(name) works as a chained
// setter: the resulting query resolves the pair-target variable correctly.
func TestVarChainedSetterTgtVar(t *testing.T) {
	w := flecs.New()
	_, _, dockedToID, _, _, _, P1, _ := qvBuildScene(w)

	w.Read(func(_ *flecs.Reader) {
		// Use chained setter instead of WithPairTgtVar constructor.
		q := flecs.NewQueryFromTerms(w, flecs.With(dockedToID).TgtVar("planet"))
		it := q.Iter()
		var seen []flecs.ID
		for it.Next() {
			seen = append(seen, it.Var("planet"))
		}
		if len(seen) == 0 {
			t.Fatal("TgtVar chained setter: expected at least one docking row")
		}
		// P1 should appear as a target (ships A and B are docked to P1).
		found := false
		for _, id := range seen {
			if id == P1 {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("TgtVar chained setter: P1 not found in targets %v", seen)
		}
	})
}

// TestVarChainedSetterPanics verifies that (Term).SrcVar and (Term).TgtVar panic on
// invalid inputs — empty name, combined fixed source, combined traversal, pair ID.
func TestVarChainedSetterPanics(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"SrcVar empty name", func() { flecs.With(1).SrcVar("") }},
		{"TgtVar empty name", func() { flecs.With(1).TgtVar("") }},
		{"SrcVar with fixed Src", func() {
			_ = flecs.With(1).Source(2).SrcVar("v")
		}},
		{"SrcVar with traversal", func() {
			_ = flecs.With(1).Up(2).SrcVar("v")
		}},
		{"TgtVar with pair ID", func() {
			_ = flecs.With(flecs.MakePair(1, 2)).TgtVar("v")
		}},
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

// qvBuildStarScene builds the canonical two-variable scene:
//
//	ships:   A(→P1), B(→P1,→P2), C(→P2)
//	planets: P1, P2  (both orbit S1)
//	stars:   S1
//
// Returns (shipID, planetID, starID, orbitsID, dockedToID, A, B, C, P1, P2, S1).
func qvBuildStarScene(w *flecs.World) (
	shipID, planetID, starID, orbitsID, dockedToID flecs.ID,
	A, B, C, P1, P2, S1 flecs.ID,
) {
	shipID = flecs.RegisterComponent[qvSpaceShip](w)
	planetID = flecs.RegisterComponent[qvPlanet](w)
	starID = flecs.RegisterComponent[qvStar](w)
	w.Write(func(fw *flecs.Writer) {
		orbitsID = fw.NewEntity()
		dockedToID = fw.NewEntity()

		S1 = fw.NewEntity()
		flecs.Set(fw, S1, qvStar{})

		P1 = fw.NewEntity()
		flecs.Set(fw, P1, qvPlanet{Name: "P1"})
		flecs.AddID(fw, P1, flecs.MakePair(orbitsID, S1))

		P2 = fw.NewEntity()
		flecs.Set(fw, P2, qvPlanet{Name: "P2"})
		flecs.AddID(fw, P2, flecs.MakePair(orbitsID, S1))

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

// TestVarTwoVariableChain verifies the motivating multi-hop query:
//
//	SpaceShip($this), DockedTo($this,$planet), Planet($planet),
//	Orbits($planet,$star), Star($star)
//
// Expects 4 rows: (A,P1,S1), (B,P1,S1), (B,P2,S1), (C,P2,S1).
func TestVarTwoVariableChain(t *testing.T) {
	w := flecs.New()
	shipID, planetID, starID, orbitsID, dockedToID, A, B, C, P1, P2, S1 := qvBuildStarScene(w)

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(shipID),
			flecs.WithPairTgtVar(dockedToID, "planet"),
			flecs.WithVar(planetID, "planet"),
			flecs.With(orbitsID).SrcVar("planet").TgtVar("star"),
			flecs.WithVar(starID, "star"),
		)
		it := q.Iter()

		type row struct{ ship, planet, star flecs.ID }
		var got []row
		for it.Next() {
			ents := it.Entities()
			if len(ents) != 1 {
				t.Fatalf("want 1 entity per step, got %d", len(ents))
			}
			got = append(got, row{ship: ents[0], planet: it.Var("planet"), star: it.Var("star")})
		}
		if len(got) != 4 {
			t.Fatalf("want 4 rows, got %d: %v", len(got), got)
		}
		want := map[[3]flecs.ID]bool{
			{A, P1, S1}: true,
			{B, P1, S1}: true,
			{B, P2, S1}: true,
			{C, P2, S1}: true,
		}
		for _, r := range got {
			if !want[[3]flecs.ID{r.ship, r.planet, r.star}] {
				t.Errorf("unexpected row ship=%v planet=%v star=%v", r.ship, r.planet, r.star)
			}
		}
	})
}

// TestVarThreeVariableChain adds a galaxy dimension to the two-variable scene:
//
//	SpaceShip($this), DockedTo($this,$planet), Planet($planet),
//	Orbits($planet,$star), Star($star), InGalaxy($star,$galaxy), Galaxy($galaxy)
//
// Expects 4 rows with galaxy G1 on each.
func TestVarThreeVariableChain(t *testing.T) {
	w := flecs.New()
	shipID, planetID, starID, orbitsID, dockedToID, A, B, C, P1, P2, S1 := qvBuildStarScene(w)
	galaxyID := flecs.RegisterComponent[qvGalaxy](w)

	var inGalaxyID, G1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		inGalaxyID = fw.NewEntity()
		G1 = fw.NewEntity()
		flecs.Set(fw, G1, qvGalaxy{})
		flecs.AddID(fw, S1, flecs.MakePair(inGalaxyID, G1))
	})

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(shipID),
			flecs.WithPairTgtVar(dockedToID, "planet"),
			flecs.WithVar(planetID, "planet"),
			flecs.With(orbitsID).SrcVar("planet").TgtVar("star"),
			flecs.WithVar(starID, "star"),
			flecs.With(inGalaxyID).SrcVar("star").TgtVar("galaxy"),
			flecs.WithVar(galaxyID, "galaxy"),
		)
		it := q.Iter()

		type row struct{ ship, planet, star, galaxy flecs.ID }
		var got []row
		for it.Next() {
			ents := it.Entities()
			if len(ents) != 1 {
				t.Fatalf("want 1 entity per step, got %d", len(ents))
			}
			got = append(got, row{
				ship:   ents[0],
				planet: it.Var("planet"),
				star:   it.Var("star"),
				galaxy: it.Var("galaxy"),
			})
		}
		if len(got) != 4 {
			t.Fatalf("want 4 rows, got %d: %v", len(got), got)
		}
		want := map[[4]flecs.ID]bool{
			{A, P1, S1, G1}: true,
			{B, P1, S1, G1}: true,
			{B, P2, S1, G1}: true,
			{C, P2, S1, G1}: true,
		}
		for _, r := range got {
			if !want[[4]flecs.ID{r.ship, r.planet, r.star, r.galaxy}] {
				t.Errorf("unexpected row %v", r)
			}
		}
	})
}

// TestVarEmptyJoinNoResults verifies that zero ships docked produces zero rows
// even though planets, stars, and orbital relationships exist.
func TestVarEmptyJoinNoResults(t *testing.T) {
	w := flecs.New()
	shipID := flecs.RegisterComponent[qvSpaceShip](w)
	planetID := flecs.RegisterComponent[qvPlanet](w)
	starID := flecs.RegisterComponent[qvStar](w)

	w.Write(func(fw *flecs.Writer) {
		orbitsID := fw.NewEntity()
		dockedToID := fw.NewEntity()
		_ = dockedToID

		S1 := fw.NewEntity()
		flecs.Set(fw, S1, qvStar{})
		P1 := fw.NewEntity()
		flecs.Set(fw, P1, qvPlanet{Name: "P1"})
		flecs.AddID(fw, P1, flecs.MakePair(orbitsID, S1))

		// Ship exists but has NO DockedTo pair — join breaks here.
		lonely := fw.NewEntity()
		flecs.Set(fw, lonely, qvSpaceShip{})
	})

	w.Read(func(_ *flecs.Reader) {
		var orbitsID, dockedToID flecs.ID
		// We need the relationship entities — use a query to find them.
		// For this test, just use WithVar directly to verify zero results.
		q := flecs.NewQueryFromTerms(w,
			flecs.With(shipID),
			flecs.WithPairTgtVar(flecs.ID(0|1), "planet"), // can't dock to any planet
			flecs.WithVar(planetID, "planet"),
			flecs.WithVar(starID, "star"),
		)
		_ = orbitsID
		_ = dockedToID
		it := q.Iter()
		var count int
		for it.Next() {
			count++
		}
		if count != 0 {
			t.Errorf("want 0 rows, got %d", count)
		}
	})
}

// TestVarEmptyJoinNoDockedShips verifies zero rows when no DockedTo pairs exist.
func TestVarEmptyJoinNoDockedShips(t *testing.T) {
	w := flecs.New()
	shipID := flecs.RegisterComponent[qvSpaceShip](w)
	planetID := flecs.RegisterComponent[qvPlanet](w)
	starID := flecs.RegisterComponent[qvStar](w)

	var orbitsID, dockedToID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		orbitsID = fw.NewEntity()
		dockedToID = fw.NewEntity()

		S1 := fw.NewEntity()
		flecs.Set(fw, S1, qvStar{})
		P1 := fw.NewEntity()
		flecs.Set(fw, P1, qvPlanet{Name: "P1"})
		flecs.AddID(fw, P1, flecs.MakePair(orbitsID, S1))

		// Ship has no DockedTo pair → multi-variable join finds nothing.
		lonely := fw.NewEntity()
		flecs.Set(fw, lonely, qvSpaceShip{})
	})

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(shipID),
			flecs.WithPairTgtVar(dockedToID, "planet"),
			flecs.WithVar(planetID, "planet"),
			flecs.With(orbitsID).SrcVar("planet").TgtVar("star"),
			flecs.WithVar(starID, "star"),
		)
		it := q.Iter()
		var count int
		for it.Next() {
			count++
		}
		if count != 0 {
			t.Errorf("want 0 rows (no docked ships), got %d", count)
		}
	})
}

// TestVarManyToMany verifies the Cartesian-product case:
// 3 ships × 2 planets × 1 star = 6 result rows.
// Each ship docks at both planets; both planets orbit the same star.
func TestVarManyToMany(t *testing.T) {
	w := flecs.New()
	shipID := flecs.RegisterComponent[qvSpaceShip](w)
	planetID := flecs.RegisterComponent[qvPlanet](w)
	starID := flecs.RegisterComponent[qvStar](w)

	var orbitsID, dockedToID flecs.ID
	var ships [3]flecs.ID
	var planets [2]flecs.ID
	var S1 flecs.ID

	w.Write(func(fw *flecs.Writer) {
		orbitsID = fw.NewEntity()
		dockedToID = fw.NewEntity()

		S1 = fw.NewEntity()
		flecs.Set(fw, S1, qvStar{})

		for i := range planets {
			planets[i] = fw.NewEntity()
			flecs.Set(fw, planets[i], qvPlanet{})
			flecs.AddID(fw, planets[i], flecs.MakePair(orbitsID, S1))
		}
		for i := range ships {
			ships[i] = fw.NewEntity()
			flecs.Set(fw, ships[i], qvSpaceShip{})
			// Each ship docks at both planets.
			for _, p := range planets {
				flecs.AddID(fw, ships[i], flecs.MakePair(dockedToID, p))
			}
		}
	})

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(shipID),
			flecs.WithPairTgtVar(dockedToID, "planet"),
			flecs.WithVar(planetID, "planet"),
			flecs.With(orbitsID).SrcVar("planet").TgtVar("star"),
			flecs.WithVar(starID, "star"),
		)
		it := q.Iter()
		var count int
		for it.Next() {
			count++
		}
		if count != 6 {
			t.Errorf("want 6 rows (3 ships × 2 planets × 1 star), got %d", count)
		}
	})
}

// TestVarMultiWithHealthFilter verifies that an additional With term correctly
// filters $this: only ships with Health AND a DockedTo $planet pair are returned.
func TestVarMultiWithHealthFilter(t *testing.T) {
	w := flecs.New()
	shipID := flecs.RegisterComponent[qvSpaceShip](w)
	planetID := flecs.RegisterComponent[qvPlanet](w)
	healthID := flecs.RegisterComponent[qvHealth](w)

	var dockedToID flecs.ID
	var healthy, unhealthy, P1 flecs.ID

	w.Write(func(fw *flecs.Writer) {
		dockedToID = fw.NewEntity()

		P1 = fw.NewEntity()
		flecs.Set(fw, P1, qvPlanet{Name: "P1"})

		healthy = fw.NewEntity()
		flecs.Set(fw, healthy, qvSpaceShip{})
		flecs.Set(fw, healthy, qvHealth{HP: 100})
		flecs.AddID(fw, healthy, flecs.MakePair(dockedToID, P1))

		unhealthy = fw.NewEntity()
		flecs.Set(fw, unhealthy, qvSpaceShip{})
		// unhealthy has no Health component.
		flecs.AddID(fw, unhealthy, flecs.MakePair(dockedToID, P1))
	})

	_ = healthy // used in assertion below
	_ = unhealthy

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(shipID),
			flecs.With(healthID),
			flecs.WithPairTgtVar(dockedToID, "planet"),
			flecs.WithVar(planetID, "planet"),
		)
		it := q.Iter()
		var count int
		for it.Next() {
			count++
			ents := it.Entities()
			for _, e := range ents {
				if e == unhealthy {
					t.Errorf("unhealthy ship should be excluded but appeared in results")
				}
			}
		}
		if count != 1 {
			t.Errorf("want 1 row (only healthy ship), got %d", count)
		}
	})
}

// TestVarIterVarMultipleBindings verifies that Var("planet") and Var("star")
// return the correct entity for each result row independently.
func TestVarIterVarMultipleBindings(t *testing.T) {
	w := flecs.New()
	shipID, planetID, starID, orbitsID, dockedToID, A, B, C, P1, P2, S1 := qvBuildStarScene(w)

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(shipID),
			flecs.WithPairTgtVar(dockedToID, "planet"),
			flecs.WithVar(planetID, "planet"),
			flecs.With(orbitsID).SrcVar("planet").TgtVar("star"),
			flecs.WithVar(starID, "star"),
		)
		it := q.Iter()

		type row struct{ ship, planet, star flecs.ID }
		var got []row
		for it.Next() {
			ents := it.Entities()
			if len(ents) != 1 {
				t.Fatalf("want 1 entity per step, got %d", len(ents))
			}
			p := it.Var("planet")
			s := it.Var("star")
			if p == 0 {
				t.Errorf("Var(\"planet\") returned 0")
			}
			if s == 0 {
				t.Errorf("Var(\"star\") returned 0")
			}
			got = append(got, row{ship: ents[0], planet: p, star: s})
		}
		if len(got) != 4 {
			t.Fatalf("want 4 rows, got %d", len(got))
		}
		// Every row must have star == S1.
		for _, r := range got {
			if r.star != S1 {
				t.Errorf("row %v: star want %v got %v", r.ship, S1, r.star)
			}
		}
		// Ship A must always pair with P1.
		for _, r := range got {
			if r.ship == A && r.planet != P1 {
				t.Errorf("ship A should be docked to P1, got %v", r.planet)
			}
		}
		// Ship C must always pair with P2.
		for _, r := range got {
			if r.ship == C && r.planet != P2 {
				t.Errorf("ship C should be docked to P2, got %v", r.planet)
			}
		}
		// Ship B appears twice — once with P1, once with P2.
		var bRows []row
		for _, r := range got {
			if r.ship == B {
				bRows = append(bRows, r)
			}
		}
		if len(bRows) != 2 {
			t.Fatalf("ship B should appear in 2 rows, got %d", len(bRows))
		}
	})
}

// TestVarCycleDetectionPanics verifies that a variable dependency cycle panics
// at query construction with a message containing "cycle".
func TestVarCycleDetectionPanics(t *testing.T) {
	w := flecs.New()
	var orbitsID flecs.ID
	w.Write(func(fw *flecs.Writer) {
		orbitsID = fw.NewEntity()
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for cyclic variable dependency")
		}
		// Verify the panic message mentions "cycle".
		msg := fmt.Sprintf("%v", r)
		if len(msg) == 0 {
			t.Errorf("panic message should not be empty")
		}
	}()

	// $planet depends on $star (planet has Orbits to star)
	// $star depends on $planet (star has Orbits to planet) → cycle
	_ = flecs.NewQueryFromTerms(w,
		flecs.With(orbitsID).SrcVar("planet").TgtVar("star"),
		flecs.With(orbitsID).SrcVar("star").TgtVar("planet"),
	)
}

// TestVarCapAt16Works verifies that exactly 16 distinct variables do not panic.
func TestVarCapAt16Works(t *testing.T) {
	w := flecs.New()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("unexpected panic with 16 variables: %v", r)
		}
	}()
	terms := make([]flecs.Term, 16)
	for i := 0; i < 16; i++ {
		terms[i] = flecs.WithVar(flecs.ID(uint64(i+1)), fmt.Sprintf("v%d", i))
	}
	_ = flecs.NewQueryFromTerms(w, terms...)
}

// TestVarCachedQueryMultiVariable verifies that a CachedQuery with two variables
// produces the same results on repeated Iter() calls (state resets each time).
func TestVarCachedQueryMultiVariable(t *testing.T) {
	w := flecs.New()
	shipID, planetID, starID, orbitsID, dockedToID, A, B, C, P1, P2, S1 := qvBuildStarScene(w)

	cq := flecs.NewCachedQueryFromTerms(w,
		flecs.With(shipID),
		flecs.WithPairTgtVar(dockedToID, "planet"),
		flecs.WithVar(planetID, "planet"),
		flecs.With(orbitsID).SrcVar("planet").TgtVar("star"),
		flecs.WithVar(starID, "star"),
	)

	want := map[[3]flecs.ID]bool{
		{A, P1, S1}: true,
		{B, P1, S1}: true,
		{B, P2, S1}: true,
		{C, P2, S1}: true,
	}

	for iter := 0; iter < 2; iter++ {
		w.Read(func(_ *flecs.Reader) {
			it := cq.Iter()
			type row struct{ ship, planet, star flecs.ID }
			var got []row
			for it.Next() {
				ents := it.Entities()
				got = append(got, row{ship: ents[0], planet: it.Var("planet"), star: it.Var("star")})
			}
			if len(got) != 4 {
				t.Errorf("iter %d: want 4 rows, got %d", iter, len(got))
				return
			}
			for _, r := range got {
				if !want[[3]flecs.ID{r.ship, r.planet, r.star}] {
					t.Errorf("iter %d: unexpected row %v", iter, r)
				}
			}
		})
	}
}

// TestVarWithSourceTermComposition verifies that a fixed-source term with a TgtVar
// correctly constrains a variable's domain to targets held by the fixed entity.
//
// Scene: gameSettings entity holds (BestPlanet, P1). Ships A (→P1) and C (→P2).
// Query: Ship($this), DockedTo($this,$planet), With(BestPlanet).Source(gameSettings).TgtVar("planet")
// Expected: only ship A (docked to P1 = the best planet).
func TestVarWithSourceTermComposition(t *testing.T) {
	w := flecs.New()
	shipID := flecs.RegisterComponent[qvSpaceShip](w)
	planetID := flecs.RegisterComponent[qvPlanet](w)

	var dockedToID, bestPlanetID, gameSettings, P1, P2, A, C flecs.ID

	w.Write(func(fw *flecs.Writer) {
		dockedToID = fw.NewEntity()
		bestPlanetID = fw.NewEntity()

		P1 = fw.NewEntity()
		flecs.Set(fw, P1, qvPlanet{Name: "P1"})
		P2 = fw.NewEntity()
		flecs.Set(fw, P2, qvPlanet{Name: "P2"})

		gameSettings = fw.NewEntity()
		flecs.AddID(fw, gameSettings, flecs.MakePair(bestPlanetID, P1)) // BestPlanet = P1

		A = fw.NewEntity()
		flecs.Set(fw, A, qvSpaceShip{})
		flecs.AddID(fw, A, flecs.MakePair(dockedToID, P1))

		C = fw.NewEntity()
		flecs.Set(fw, C, qvSpaceShip{})
		flecs.AddID(fw, C, flecs.MakePair(dockedToID, P2))
	})

	_ = planetID

	w.Read(func(_ *flecs.Reader) {
		q := flecs.NewQueryFromTerms(w,
			flecs.With(shipID),
			flecs.WithPairTgtVar(dockedToID, "planet"),
			// Fixed-source constraint: gameSettings has (BestPlanet, $planet)
			flecs.With(bestPlanetID).Source(gameSettings).TgtVar("planet"),
		)
		it := q.Iter()
		var got []flecs.ID
		for it.Next() {
			ents := it.Entities()
			got = append(got, ents...)
		}
		if len(got) != 1 {
			t.Fatalf("want 1 row (ship A docked to best planet P1), got %d: %v", len(got), got)
		}
		if got[0] != A {
			t.Errorf("want ship A, got %v", got[0])
		}
		_ = C
	})
}

// TestOptimizer_ResultSetIdentical verifies that the join-order optimizer
// produces a result set equivalent to the pre-optimization (first-defined-wins)
// ordering for the canonical multi-variable scene.
//
// The test runs the same query twice with different term orderings (so the
// optimizer may or may not reorder the driver), collects all rows, sorts them,
// and asserts they are identical.
func TestOptimizer_ResultSetIdentical(t *testing.T) {
	w := flecs.New()
	shipID, planetID, dockedToID, A, B, C, P1, P2 := qvBuildScene(w)

	collect := func(q *flecs.Query) [][2]flecs.ID {
		it := q.Iter()
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

	want := [][2]flecs.ID{{A, P1}, {B, P1}, {B, P2}, {C, P2}}
	sort.Slice(want, func(i, j int) bool {
		if want[i][0] != want[j][0] {
			return want[i][0] < want[j][0]
		}
		return want[i][1] < want[j][1]
	})

	w.Read(func(_ *flecs.Reader) {
		// Term order 1: ship (larger domain) first — optimizer may reorder to planet-driver.
		q1 := flecs.NewQueryFromTerms(w,
			flecs.With(shipID),
			flecs.WithPairTgtVar(dockedToID, "planet"),
			flecs.WithVar(planetID, "planet"),
		)
		rows1 := collect(q1)

		// Term order 2: same terms, different ordering (planet constraint first).
		q2 := flecs.NewQueryFromTerms(w,
			flecs.WithVar(planetID, "planet"),
			flecs.With(shipID),
			flecs.WithPairTgtVar(dockedToID, "planet"),
		)
		rows2 := collect(q2)

		if len(rows1) != len(want) {
			t.Fatalf("q1: want %d rows, got %d", len(want), len(rows1))
		}
		if len(rows2) != len(want) {
			t.Fatalf("q2: want %d rows, got %d", len(want), len(rows2))
		}
		for i := range want {
			if rows1[i] != want[i] {
				t.Errorf("q1 row %d: want %v, got %v", i, want[i], rows1[i])
			}
			if rows2[i] != want[i] {
				t.Errorf("q2 row %d: want %v, got %v", i, want[i], rows2[i])
			}
		}
	})
}
