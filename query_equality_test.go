package flecs_test

import (
	"sort"
	"testing"

	"github.com/snichols/flecs"
)

// Component types used exclusively by query equality tests.
type qePosition struct{ X, Y float32 }
type qeVelocity struct{ VX, VY float32 }

func qeCollect(q *flecs.Query) []flecs.ID {
	it := q.Iter()
	var out []flecs.ID
	for it.Next() {
		out = append(out, it.Entities()...)
	}
	return out
}

func qeCollectCQ(cq *flecs.CachedQuery) []flecs.ID {
	it := cq.Iter()
	var out []flecs.ID
	for it.Next() {
		out = append(out, it.Entities()...)
	}
	return out
}

func sortIDs(ids []flecs.ID) []flecs.ID {
	s := make([]flecs.ID, len(ids))
	copy(s, ids)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s
}

// Test 1: IsEntity(foo) + With(posID) where foo has Position → yields only foo.
func TestIsEntity_MatchesFoo(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qePosition](w)

	var foo, bar flecs.ID
	w.Write(func(fw *flecs.Writer) {
		foo = fw.NewEntity()
		flecs.Set(fw, foo, qePosition{1, 2})
		bar = fw.NewEntity()
		flecs.Set(fw, bar, qePosition{3, 4})
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.IsEntity(foo))
	got := qeCollect(q)
	if len(got) != 1 || got[0] != foo {
		t.Fatalf("expected [%v], got %v", foo, got)
	}
	_ = bar
}

// Test 2: IsEntity(foo) + With(posID) where foo has no Position → yields nothing.
func TestIsEntity_FooHasNoPosition(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qePosition](w)

	var foo, bar flecs.ID
	w.Write(func(fw *flecs.Writer) {
		foo = fw.NewEntity()
		bar = fw.NewEntity()
		flecs.Set(fw, bar, qePosition{1, 2})
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.IsEntity(foo))
	got := qeCollect(q)
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
	_ = bar
}

// Test 3: IsEntity(foo) + With(posID) where foo is dead → yields nothing (no panic).
func TestIsEntity_FooDead(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qePosition](w)

	var foo flecs.ID
	w.Write(func(fw *flecs.Writer) {
		foo = fw.NewEntity()
		flecs.Set(fw, foo, qePosition{1, 2})
	})
	fooSnapshot := foo
	w.Delete(foo)

	q := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.IsEntity(fooSnapshot))
	got := qeCollect(q)
	if len(got) != 0 {
		t.Fatalf("expected empty for dead entity, got %v", got)
	}
}

// Test 4: NotEntity(foo) + With(posID) → yields all Position-holders except foo.
func TestNotEntity_ExcludesFoo(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qePosition](w)

	var foo, bar, baz flecs.ID
	w.Write(func(fw *flecs.Writer) {
		foo = fw.NewEntity()
		flecs.Set(fw, foo, qePosition{1, 2})
		bar = fw.NewEntity()
		flecs.Set(fw, bar, qePosition{3, 4})
		baz = fw.NewEntity()
		flecs.Set(fw, baz, qePosition{5, 6})
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.NotEntity(foo))
	got := sortIDs(qeCollect(q))
	want := sortIDs([]flecs.ID{bar, baz})
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

// Test 5: NotEntity(foo) + With(posID) where foo doesn't have Position → yields all Position-holders.
func TestNotEntity_FooLacksPosition(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qePosition](w)

	var foo, bar, baz flecs.ID
	w.Write(func(fw *flecs.Writer) {
		foo = fw.NewEntity()
		bar = fw.NewEntity()
		flecs.Set(fw, bar, qePosition{1, 2})
		baz = fw.NewEntity()
		flecs.Set(fw, baz, qePosition{3, 4})
	})

	q := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.NotEntity(foo))
	got := sortIDs(qeCollect(q))
	want := sortIDs([]flecs.ID{bar, baz})
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

// Test 6: NameMatches("Player") against {Player1, Player2, Enemy, unnamed} → yields Player1, Player2.
func TestNameMatches_Basic(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qePosition](w)

	var player1, player2, enemy, unnamed flecs.ID
	w.Write(func(fw *flecs.Writer) {
		player1 = fw.NewEntity()
		flecs.Set(fw, player1, qePosition{})
		player2 = fw.NewEntity()
		flecs.Set(fw, player2, qePosition{})
		enemy = fw.NewEntity()
		flecs.Set(fw, enemy, qePosition{})
		unnamed = fw.NewEntity()
		flecs.Set(fw, unnamed, qePosition{})
	})
	w.SetName(player1, "Player1")
	w.SetName(player2, "Player2")
	w.SetName(enemy, "Enemy")

	q := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.NameMatches("Player"))
	got := sortIDs(qeCollect(q))
	want := sortIDs([]flecs.ID{player1, player2})
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
	_ = unnamed
}

// Test 7: NameMatches("PLAYER") → yields Player1, Player2 (case-insensitive).
func TestNameMatches_CaseInsensitive(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qePosition](w)

	var player1, player2, enemy flecs.ID
	w.Write(func(fw *flecs.Writer) {
		player1 = fw.NewEntity()
		flecs.Set(fw, player1, qePosition{})
		player2 = fw.NewEntity()
		flecs.Set(fw, player2, qePosition{})
		enemy = fw.NewEntity()
		flecs.Set(fw, enemy, qePosition{})
	})
	w.SetName(player1, "Player1")
	w.SetName(player2, "Player2")
	w.SetName(enemy, "Enemy")

	q := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.NameMatches("PLAYER"))
	got := sortIDs(qeCollect(q))
	want := sortIDs([]flecs.ID{player1, player2})
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

// Test 8: NameMatches("nothing") → yields nothing.
func TestNameMatches_NoMatch(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qePosition](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, qePosition{})
	})
	w.SetName(e, "Foo")

	q := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.NameMatches("nothing"))
	got := qeCollect(q)
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

// Test 9: NameMatches("") → yields every entity with a non-empty name.
func TestNameMatches_EmptyPattern(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qePosition](w)

	var named1, named2, unnamed flecs.ID
	w.Write(func(fw *flecs.Writer) {
		named1 = fw.NewEntity()
		flecs.Set(fw, named1, qePosition{})
		named2 = fw.NewEntity()
		flecs.Set(fw, named2, qePosition{})
		unnamed = fw.NewEntity()
		flecs.Set(fw, unnamed, qePosition{})
	})
	w.SetName(named1, "Alpha")
	w.SetName(named2, "Beta")

	q := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.NameMatches(""))
	got := sortIDs(qeCollect(q))
	want := sortIDs([]flecs.ID{named1, named2})
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
	_ = unnamed
}

// Test 10: Composition — With(pos) + With(vel) + NameMatches("Player") + NotEntity(dead).
func TestComposition_ThreeWay(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qePosition](w)
	velID := flecs.RegisterComponent[qeVelocity](w)

	var p1, p2, dead, enemy flecs.ID
	w.Write(func(fw *flecs.Writer) {
		p1 = fw.NewEntity()
		flecs.Set(fw, p1, qePosition{})
		flecs.Set(fw, p1, qeVelocity{})

		p2 = fw.NewEntity()
		flecs.Set(fw, p2, qePosition{})
		flecs.Set(fw, p2, qeVelocity{})

		dead = fw.NewEntity()
		flecs.Set(fw, dead, qePosition{})
		flecs.Set(fw, dead, qeVelocity{})

		enemy = fw.NewEntity()
		flecs.Set(fw, enemy, qePosition{})
	})
	w.SetName(p1, "Player1")
	w.SetName(p2, "Player2")
	w.SetName(dead, "PlayerDead")
	w.SetName(enemy, "Enemy")

	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.With(velID),
		flecs.NameMatches("Player"),
		flecs.NotEntity(dead),
	)
	got := sortIDs(qeCollect(q))
	want := sortIDs([]flecs.ID{p1, p2})
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

// Test 11: CachedQuery with IsEntity — re-execute after target gains the component; cache reflects.
func TestCachedQuery_IsEntity_Invalidation(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qePosition](w)

	var foo, bar flecs.ID
	w.Write(func(fw *flecs.Writer) {
		bar = fw.NewEntity()
		flecs.Set(fw, bar, qePosition{1, 2})
		foo = fw.NewEntity()
		// foo has no Position yet
	})

	cq := flecs.NewCachedQueryFromTerms(w, flecs.With(posID), flecs.IsEntity(foo))
	defer cq.Close()

	// Before: foo has no Position → no results
	got := qeCollectCQ(cq)
	if len(got) != 0 {
		t.Fatalf("before: expected empty, got %v", got)
	}

	// Add Position to foo → foo migrates to a new table containing posID
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, foo, qePosition{5, 6})
	})

	// After: cache should now include the table containing foo
	got = qeCollectCQ(cq)
	if len(got) != 1 || got[0] != foo {
		t.Fatalf("after: expected [%v], got %v", foo, got)
	}
	_ = bar
}

// Test 12: CachedQuery with NameMatches — rename entity to match; cache reflects on next call.
func TestCachedQuery_NameMatches_Invalidation(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qePosition](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, qePosition{})
	})
	w.SetName(e, "Enemy")

	cq := flecs.NewCachedQueryFromTerms(w, flecs.With(posID), flecs.NameMatches("Player"))
	defer cq.Close()

	// Initial: "Enemy" does not match "Player"
	got := qeCollectCQ(cq)
	if len(got) != 0 {
		t.Fatalf("before rename: expected empty, got %v", got)
	}

	// Drain initial Changed() true (first call always returns true)
	_ = cq.Changed()

	// Rename to "Player1" → triggers OnSet[Name] observer → sets tablesAdded
	w.SetName(e, "Player1")

	if !cq.Changed() {
		t.Fatal("expected Changed() == true after rename")
	}

	// Now query reflects the new name
	got = qeCollectCQ(cq)
	if len(got) != 1 || got[0] != e {
		t.Fatalf("after rename: expected [%v], got %v", e, got)
	}
}

// Test 13: IsEntity(0) → panic at construction.
func TestIsEntity_ZeroPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for IsEntity(0)")
		}
	}()
	flecs.IsEntity(0)
}

// Test 14: NotEntity(0) → panic at construction.
func TestNotEntity_ZeroPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for NotEntity(0)")
		}
	}()
	flecs.NotEntity(0)
}

// Test 15: IsEntity(e).Up(rel) → panic at validator time.
func TestIsEntity_TraversalPanic(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qePosition](w)

	var foo, rel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		foo = fw.NewEntity()
		flecs.Set(fw, foo, qePosition{})
		rel = fw.NewEntity()
	})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for IsEntity with traversal")
		}
	}()
	flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.IsEntity(foo).Up(rel))
}

// Test 16: IsEntity(e).Source(other) → panic at validator time.
func TestIsEntity_SourcePanic(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qePosition](w)

	var foo, other flecs.ID
	w.Write(func(fw *flecs.Writer) {
		foo = fw.NewEntity()
		flecs.Set(fw, foo, qePosition{})
		other = fw.NewEntity()
	})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for IsEntity with fixed source")
		}
	}()
	flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.IsEntity(foo).Source(other))
}

// Test 17: WithoutScope(b => b.NotEntity(foo)) — NOT(e != foo) passes only for foo.
func TestWithoutScope_NotEntity(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qePosition](w)

	var foo, bar, baz flecs.ID
	w.Write(func(fw *flecs.Writer) {
		foo = fw.NewEntity()
		flecs.Set(fw, foo, qePosition{})
		bar = fw.NewEntity()
		flecs.Set(fw, bar, qePosition{})
		baz = fw.NewEntity()
		flecs.Set(fw, baz, qePosition{})
	})

	// WithoutScope(b => b.NotEntity(foo)):
	// Sub-expression "e != foo" is true for bar and baz.
	// Scope negates: passes when sub is false → passes only for foo.
	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
			b.NotEntity(foo)
		}),
	)
	got := qeCollect(q)
	if len(got) != 1 || got[0] != foo {
		t.Fatalf("expected only [%v], got %v", foo, got)
	}
	_, _ = bar, baz
}

// Test 18: TermKind.String() returns correct names for new equality kinds.
func TestTermKindString_EqualityKinds(t *testing.T) {
	cases := []struct {
		kind flecs.TermKind
		want string
	}{
		{flecs.TermEq, "Eq"},
		{flecs.TermNotEq, "NotEq"},
		{flecs.TermNameMatch, "NameMatch"},
	}
	for _, tc := range cases {
		if got := tc.kind.String(); got != tc.want {
			t.Errorf("TermKind(%d).String() = %q, want %q", int(tc.kind), got, tc.want)
		}
	}
}

// Test 19: ScopeBuilder.IsEntity — WithoutScope(b => b.With(pos).IsEntity(foo)) passes only foo.
func TestScopeBuilder_IsEntity(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qePosition](w)

	var foo, bar flecs.ID
	w.Write(func(fw *flecs.Writer) {
		foo = fw.NewEntity()
		flecs.Set(fw, foo, qePosition{})
		bar = fw.NewEntity()
		flecs.Set(fw, bar, qePosition{})
	})

	// WithoutScope(b => b.With(pos).IsEntity(foo)):
	// Sub-expression: has Position AND is foo → true for foo only.
	// Scope negates: passes when sub is false → passes for bar only.
	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
			b.With(posID).IsEntity(foo)
		}),
	)
	got := qeCollect(q)
	if len(got) != 1 || got[0] != bar {
		t.Fatalf("expected only bar [%v], got %v", bar, got)
	}
	_ = foo
}

// Test 20: ScopeBuilder.NameMatches — WithoutScope(b => b.NameMatches("Enemy")) excludes enemies.
func TestScopeBuilder_NameMatches(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[qePosition](w)

	var hero, enemy flecs.ID
	w.Write(func(fw *flecs.Writer) {
		hero = fw.NewEntity()
		flecs.Set(fw, hero, qePosition{})
		enemy = fw.NewEntity()
		flecs.Set(fw, enemy, qePosition{})
	})
	w.SetName(hero, "Hero")
	w.SetName(enemy, "Enemy")

	// WithoutScope(b => b.NameMatches("Enemy")):
	// Sub-expression: name contains "Enemy" → true for enemy.
	// Scope negates: passes when sub is false → passes for hero.
	q := flecs.NewQueryFromTerms(w,
		flecs.With(posID),
		flecs.WithoutScope(func(b *flecs.ScopeBuilder) {
			b.NameMatches("Enemy")
		}),
	)
	got := qeCollect(q)
	if len(got) != 1 || got[0] != hero {
		t.Fatalf("expected only hero [%v], got %v", hero, got)
	}
	_ = enemy
}
