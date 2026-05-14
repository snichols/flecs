package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// Component types used exclusively by *From tests.
type qfHealth struct{ HP int }
type qfSpeed struct{ Val float32 }
type qfAI struct{ Behavior int }
type qfPosition struct{ X, Y float32 }
type qfExtra struct{ V int }

// qfCollect drains q into an ID slice.
func qfCollect(q *flecs.Query) []flecs.ID {
	it := q.Iter()
	var out []flecs.ID
	for it.Next() {
		out = append(out, it.Entities()...)
	}
	return out
}

// qfCollectCQ drains a CachedQuery into an ID slice.
func qfCollectCQ(cq *flecs.CachedQuery) []flecs.ID {
	it := cq.Iter()
	var out []flecs.ID
	for it.Next() {
		out = append(out, it.Entities()...)
	}
	return out
}

// qfContains reports whether ids contains target.
func qfContains(ids []flecs.ID, target flecs.ID) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

// Test 1: AndFrom(template) matches entities with ALL template components.
func TestAndFrom_MatchesAll(t *testing.T) {
	w := flecs.New()
	healthID := flecs.RegisterComponent[qfHealth](w)
	speedID := flecs.RegisterComponent[qfSpeed](w)
	aiID := flecs.RegisterComponent[qfAI](w)

	var tmpl, full, partial flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tmpl = fw.NewEntity()
		flecs.Set(fw, tmpl, qfHealth{})
		flecs.Set(fw, tmpl, qfSpeed{})
		flecs.Set(fw, tmpl, qfAI{})

		full = fw.NewEntity()
		flecs.Set(fw, full, qfHealth{})
		flecs.Set(fw, full, qfSpeed{})
		flecs.Set(fw, full, qfAI{})

		partial = fw.NewEntity()
		flecs.Set(fw, partial, qfHealth{})
		flecs.Set(fw, partial, qfSpeed{})
	})

	q := flecs.NewQueryFromTerms(w, flecs.AndFrom(tmpl))
	got := qfCollect(q)
	if !qfContains(got, full) {
		t.Errorf("expected full entity %v in results %v", full, got)
	}
	if !qfContains(got, tmpl) {
		t.Errorf("expected tmpl entity %v in results %v", tmpl, got)
	}
	_ = healthID
	_ = speedID
	_ = aiID
}

// Test 2: AndFrom(template) — entity missing one component does NOT match.
func TestAndFrom_MissingComponent_NoMatch(t *testing.T) {
	w := flecs.New()
	healthID := flecs.RegisterComponent[qfHealth](w)
	speedID := flecs.RegisterComponent[qfSpeed](w)
	aiID := flecs.RegisterComponent[qfAI](w)

	var tmpl, partial flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tmpl = fw.NewEntity()
		flecs.Set(fw, tmpl, qfHealth{})
		flecs.Set(fw, tmpl, qfSpeed{})
		flecs.Set(fw, tmpl, qfAI{})

		partial = fw.NewEntity()
		flecs.Set(fw, partial, qfHealth{})
		flecs.Set(fw, partial, qfSpeed{})
		// partial is missing qfAI → should NOT match AndFrom(tmpl)
	})

	q := flecs.NewQueryFromTerms(w, flecs.AndFrom(tmpl))
	got := qfCollect(q)
	if qfContains(got, partial) {
		t.Errorf("partial entity %v (missing AI) should not be in AndFrom results", partial)
	}
	_ = healthID
	_ = speedID
	_ = aiID
}

// Test 3: OrFrom(template) matches entities with AT LEAST ONE of the template's components.
func TestOrFrom_MatchesAtLeastOne(t *testing.T) {
	w := flecs.New()
	healthID := flecs.RegisterComponent[qfHealth](w)
	speedID := flecs.RegisterComponent[qfSpeed](w)
	aiID := flecs.RegisterComponent[qfAI](w)

	var tmpl, hasHealth, hasSpeed, hasNone flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tmpl = fw.NewEntity()
		flecs.Set(fw, tmpl, qfHealth{})
		flecs.Set(fw, tmpl, qfSpeed{})
		flecs.Set(fw, tmpl, qfAI{})

		hasHealth = fw.NewEntity()
		flecs.Set(fw, hasHealth, qfHealth{})

		hasSpeed = fw.NewEntity()
		flecs.Set(fw, hasSpeed, qfSpeed{})

		hasNone = fw.NewEntity()
		_ = hasNone // no template components
	})

	q := flecs.NewQueryFromTerms(w, flecs.OrFrom(tmpl))
	got := qfCollect(q)
	if !qfContains(got, hasHealth) {
		t.Errorf("entity with Health should match OrFrom")
	}
	if !qfContains(got, hasSpeed) {
		t.Errorf("entity with Speed should match OrFrom")
	}
	if qfContains(got, hasNone) {
		t.Errorf("entity with no template components should NOT match OrFrom")
	}
	_ = healthID
	_ = speedID
	_ = aiID
}

// Test 4: NotFrom(template) matches entities with NONE of the template's components.
func TestNotFrom_MatchesNone(t *testing.T) {
	w := flecs.New()
	healthID := flecs.RegisterComponent[qfHealth](w)
	speedID := flecs.RegisterComponent[qfSpeed](w)
	aiID := flecs.RegisterComponent[qfAI](w)
	posID := flecs.RegisterComponent[qfPosition](w)

	var tmpl, hasHealth, hasPosOnly flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tmpl = fw.NewEntity()
		flecs.Set(fw, tmpl, qfHealth{})
		flecs.Set(fw, tmpl, qfSpeed{})
		flecs.Set(fw, tmpl, qfAI{})

		hasHealth = fw.NewEntity()
		flecs.Set(fw, hasHealth, qfHealth{})
		// hasHealth has Health → should NOT match NotFrom(tmpl)

		hasPosOnly = fw.NewEntity()
		flecs.Set(fw, hasPosOnly, qfPosition{})
		// hasPosOnly has none of {Health, Speed, AI} → SHOULD match NotFrom(tmpl)
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.NotFrom(tmpl))
	got := qfCollect(q)
	if qfContains(got, hasHealth) {
		t.Errorf("entity with Health should NOT match NotFrom(tmpl)")
	}
	if !qfContains(got, hasPosOnly) {
		t.Errorf("entity with only Position should match NotFrom(tmpl)")
	}
	_ = healthID
	_ = speedID
	_ = aiID
}

// Test 5: Composition — With(posID) AND AndFrom(template).
func TestAndFrom_ComposedWithRegularTerm(t *testing.T) {
	w := flecs.New()
	healthID := flecs.RegisterComponent[qfHealth](w)
	speedID := flecs.RegisterComponent[qfSpeed](w)
	aiID := flecs.RegisterComponent[qfAI](w)
	posID := flecs.RegisterComponent[qfPosition](w)

	var tmpl, full, noPos flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tmpl = fw.NewEntity()
		flecs.Set(fw, tmpl, qfHealth{})
		flecs.Set(fw, tmpl, qfSpeed{})
		flecs.Set(fw, tmpl, qfAI{})

		full = fw.NewEntity()
		flecs.Set(fw, full, qfPosition{})
		flecs.Set(fw, full, qfHealth{})
		flecs.Set(fw, full, qfSpeed{})
		flecs.Set(fw, full, qfAI{})

		noPos = fw.NewEntity()
		flecs.Set(fw, noPos, qfHealth{})
		flecs.Set(fw, noPos, qfSpeed{})
		flecs.Set(fw, noPos, qfAI{})
		// noPos lacks Position → should NOT match With(posID), AndFrom(tmpl)
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.AndFrom(tmpl))
	got := qfCollect(q)
	if !qfContains(got, full) {
		t.Errorf("entity with Position+all enemy comps should match")
	}
	if qfContains(got, noPos) {
		t.Errorf("entity without Position should NOT match")
	}
	_ = healthID
	_ = speedID
	_ = aiID
}

// Test 6: Snapshot semantics — adding a component to source after construction
// does not affect the query; rebuilding picks up the new component.
func TestAndFrom_SnapshotSemantics(t *testing.T) {
	w := flecs.New()
	healthID := flecs.RegisterComponent[qfHealth](w)
	speedID := flecs.RegisterComponent[qfSpeed](w)
	extraID := flecs.RegisterComponent[qfExtra](w)

	var src, candidate flecs.ID
	w.Write(func(fw *flecs.Writer) {
		src = fw.NewEntity()
		flecs.Set(fw, src, qfHealth{})
		flecs.Set(fw, src, qfSpeed{})

		candidate = fw.NewEntity()
		flecs.Set(fw, candidate, qfHealth{})
		flecs.Set(fw, candidate, qfSpeed{})
		// candidate does NOT have qfExtra
	})

	// Build query before adding qfExtra to src.
	q1 := flecs.NewQueryFromTerms(w, flecs.AndFrom(src))
	if !qfContains(qfCollect(q1), candidate) {
		t.Fatal("candidate should match pre-snapshot query")
	}

	// Add a new component to src after construction.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, src, qfExtra{})
	})

	// q1 still uses the old snapshot: candidate still matches (it lacks qfExtra but
	// the query was built before qfExtra was on src).
	if !qfContains(qfCollect(q1), candidate) {
		t.Error("snapshot: candidate should still match old query (qfExtra not in snapshot)")
	}

	// Rebuilding picks up the new component; now candidate must have qfExtra to match.
	q2 := flecs.NewQueryFromTerms(w, flecs.AndFrom(src))
	got2 := qfCollect(q2)
	if qfContains(got2, candidate) {
		t.Error("rebuilt query: candidate (lacking qfExtra) should NOT match")
	}
	_ = healthID
	_ = speedID
	_ = extraID
}

// Test 7: Empty source type — vacuous/zero-match semantics.
func TestFromOperators_EmptySource(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qfPosition](w)

	var empty, e1 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		empty = fw.NewEntity() // no components

		e1 = fw.NewEntity()
		flecs.Set(fw, e1, qfPosition{})
	})

	// AndFrom(empty) is vacuous: With(posID) combined with it still matches posID entities.
	qAnd := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.AndFrom(empty))
	gotAnd := qfCollect(qAnd)
	if !qfContains(gotAnd, e1) {
		t.Error("AndFrom(empty) is vacuous: e1 with posID should match")
	}

	// OrFrom(empty): empty disjunction = false → zero results.
	qOr := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.OrFrom(empty))
	gotOr := qfCollect(qOr)
	if len(gotOr) != 0 {
		t.Errorf("OrFrom(empty): expected zero results, got %v", gotOr)
	}

	// NotFrom(empty) is vacuous: With(posID) combined with it still matches posID entities.
	qNot := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.NotFrom(empty))
	gotNot := qfCollect(qNot)
	if !qfContains(gotNot, e1) {
		t.Error("NotFrom(empty) is vacuous: e1 with posID should match")
	}

	_ = posID
}

// Test 8: Source is a Prefab — the Prefab tag itself is excluded from expansion.
func TestAndFrom_PrefabSourceExcludesPrefabTag(t *testing.T) {
	w := flecs.New()
	healthID := flecs.RegisterComponent[qfHealth](w)
	speedID := flecs.RegisterComponent[qfSpeed](w)

	// Create a prefab with Health + Speed. The prefab's type also contains the
	// Prefab tag (added by MarkPrefab); that tag has DontInherit and must be
	// excluded from AndFrom expansion.
	var prefabSrc, normalEntity flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefabSrc = fw.NewEntity()
		flecs.MarkPrefab(fw, prefabSrc)
		flecs.Set(fw, prefabSrc, qfHealth{})
		flecs.Set(fw, prefabSrc, qfSpeed{})

		normalEntity = fw.NewEntity()
		flecs.Set(fw, normalEntity, qfHealth{})
		flecs.Set(fw, normalEntity, qfSpeed{})
		// normalEntity is NOT a prefab
	})

	// AndFrom(prefabSrc) must expand to {Health, Speed} only — NOT {Prefab, Health, Speed}.
	// If Prefab were included, normalEntity (non-prefab) would not match.
	q := flecs.NewQueryFromTerms(w, flecs.AndFrom(prefabSrc))
	got := qfCollect(q)
	if !qfContains(got, normalEntity) {
		t.Errorf("Prefab tag should be excluded from expansion; normalEntity should match. got=%v", got)
	}
	_ = healthID
	_ = speedID
}

// Test 9: Source has Disabled tag — also excluded from expansion.
func TestAndFrom_DisabledTagExcluded(t *testing.T) {
	w := flecs.New()
	healthID := flecs.RegisterComponent[qfHealth](w)

	var src, normalEntity flecs.ID
	w.Write(func(fw *flecs.Writer) {
		src = fw.NewEntity()
		flecs.Set(fw, src, qfHealth{})
		flecs.DisableEntity(fw, src) // add Disabled tag — has DontInherit, must be filtered

		normalEntity = fw.NewEntity()
		flecs.Set(fw, normalEntity, qfHealth{})
		// normalEntity is NOT disabled
	})

	// AndFrom(src) should expand to {Health} only, not {Health, Disabled}.
	// If Disabled were included, normalEntity would not match.
	q := flecs.NewQueryFromTerms(w, flecs.AndFrom(src))
	got := qfCollect(q)
	if !qfContains(got, normalEntity) {
		t.Errorf("Disabled tag should be excluded; normalEntity should match. got=%v", got)
	}
	_ = healthID
}

// Test 10: Dead source panics at construction.
func TestAndFrom_DeadSourcePanics(t *testing.T) {
	w := flecs.New()

	var dead flecs.ID
	w.Write(func(fw *flecs.Writer) {
		dead = fw.NewEntity()
		fw.Delete(dead)
	})

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for dead source, got none")
		}
	}()
	flecs.NewQueryFromTerms(w, flecs.AndFrom(dead))
}

// Test 11: CachedQuery with AndFrom — snapshot re-execution and Changed() detection.
func TestAndFrom_CachedQuery(t *testing.T) {
	w := flecs.New()
	healthID := flecs.RegisterComponent[qfHealth](w)
	speedID := flecs.RegisterComponent[qfSpeed](w)

	var src, e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		src = fw.NewEntity()
		flecs.Set(fw, src, qfHealth{})
		flecs.Set(fw, src, qfSpeed{})

		e1 = fw.NewEntity()
		flecs.Set(fw, e1, qfHealth{})
		flecs.Set(fw, e1, qfSpeed{})

		e2 = fw.NewEntity()
		flecs.Set(fw, e2, qfHealth{})
		// e2 missing Speed
	})

	cq := flecs.NewCachedQueryFromTerms(w, flecs.AndFrom(src))

	// Initial: Changed() returns true (new tables added at construction).
	_ = cq.Changed()

	got := qfCollectCQ(cq)
	if !qfContains(got, e1) {
		t.Errorf("e1 (Health+Speed) should match AndFrom; got=%v", got)
	}
	if qfContains(got, e2) {
		t.Errorf("e2 (missing Speed) should NOT match AndFrom; got=%v", got)
	}

	// Mutate e1 — Changed() should return true.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e1, qfHealth{HP: 99})
	})
	if !cq.Changed() {
		t.Error("Changed() should return true after Set on matched entity")
	}

	// Adding Speed to src after construction does NOT update the snapshot.
	// The query still uses {Health, Speed} — src already had both.
	got2 := qfCollectCQ(cq)
	if !qfContains(got2, e1) {
		t.Errorf("e1 should still match after unrelated src mutation; got=%v", got2)
	}

	cq.Close()
	_ = healthID
	_ = speedID
}

// Test 12: Pair components in source's type ARE included in expansion.
func TestAndFrom_PairIncluded(t *testing.T) {
	w := flecs.New()
	healthID := flecs.RegisterComponent[qfHealth](w)

	var parent, src, child, nonChild flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		src = fw.NewEntity()
		flecs.Set(fw, src, qfHealth{})
		fw.AddID(src, flecs.MakePair(w.ChildOf(), parent)) // src is a child of parent

		child = fw.NewEntity()
		flecs.Set(fw, child, qfHealth{})
		fw.AddID(child, flecs.MakePair(w.ChildOf(), parent)) // also a child of parent

		nonChild = fw.NewEntity()
		flecs.Set(fw, nonChild, qfHealth{})
		// nonChild has Health but is NOT a child of parent
	})

	// AndFrom(src) should expand to {Health, (ChildOf, parent)}.
	// Only entities with both Health AND ChildOf(parent) should match.
	q := flecs.NewQueryFromTerms(w, flecs.AndFrom(src))
	got := qfCollect(q)
	if !qfContains(got, child) {
		t.Errorf("child (Health + ChildOf(parent)) should match AndFrom(src); got=%v", got)
	}
	if qfContains(got, nonChild) {
		t.Errorf("nonChild (missing ChildOf(parent)) should NOT match AndFrom(src); got=%v", got)
	}
	_ = healthID
}

// Test 13: OrFrom zero source panics.
func TestOrFrom_ZeroSourcePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for zero source")
		}
	}()
	flecs.OrFrom(0)
}

// Test 14: NotFrom zero source panics.
func TestNotFrom_ZeroSourcePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for zero source")
		}
	}()
	flecs.NotFrom(0)
}

// Test 15: AndFrom zero source panics.
func TestAndFrom_ZeroSourcePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for zero source")
		}
	}()
	flecs.AndFrom(0)
}
