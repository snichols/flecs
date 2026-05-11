package flecs_test

import (
	"runtime"
	"sort"
	"testing"

	"github.com/snichols/flecs"
)

// Marker is a zero-size tag type used by query tests.
type Marker struct{}

// ── Single-term query ─────────────────────────────────────────────────────────

func TestQuerySingleTerm(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	e1 := w.NewEntity()
	e2 := w.NewEntity()
	e3 := w.NewEntity()
	flecs.Set(w.W(), e1, Position{1, 2})
	flecs.Set(w.W(), e2, Position{3, 4})
	flecs.Set(w.W(), e3, Position{5, 6})

	q := flecs.NewQuery(w, posID)
	var gotEntities []flecs.ID
	var gotPositions []Position

	q.Each(func(it *flecs.QueryIter) {
		gotEntities = append(gotEntities, it.Entities()...)
		gotPositions = append(gotPositions, flecs.Field[Position](it, posID)...)
	})

	if len(gotEntities) != 3 {
		t.Fatalf("single-term query: want 3 entities, got %d", len(gotEntities))
	}
	// Verify all three entity IDs appear.
	want := map[flecs.ID]bool{e1: true, e2: true, e3: true}
	for _, e := range gotEntities {
		delete(want, e)
	}
	if len(want) != 0 {
		t.Fatalf("single-term query: missing entities %v", want)
	}
	if len(gotPositions) != 3 {
		t.Fatalf("single-term query: want 3 positions, got %d", len(gotPositions))
	}
}

// ── Two-term AND query ────────────────────────────────────────────────────────

func TestQueryTwoTermAND(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	e1 := w.NewEntity()
	e2 := w.NewEntity()
	e3 := w.NewEntity()
	flecs.Set(w.W(), e1, Position{1, 0}) // Position only
	flecs.Set(w.W(), e2, Velocity{0, 1}) // Velocity only
	flecs.Set(w.W(), e3, Position{3, 0}) // Position + Velocity
	flecs.Set(w.W(), e3, Velocity{0, 3})

	q := flecs.NewQuery(w, posID, velID)
	var visited []flecs.ID
	q.Each(func(it *flecs.QueryIter) {
		visited = append(visited, it.Entities()...)
	})

	if len(visited) != 1 {
		t.Fatalf("two-term AND: want 1 entity (e3 only), got %d: %v", len(visited), visited)
	}
	if visited[0] != e3 {
		t.Fatalf("two-term AND: visited entity %d, want e3=%d", visited[0], e3)
	}
	// e1 and e2 must NOT appear.
	for _, e := range visited {
		if e == e1 || e == e2 {
			t.Fatalf("two-term AND: e1 or e2 incorrectly visited")
		}
	}
}

// ── Empty match ───────────────────────────────────────────────────────────────

func TestQueryEmptyMatch(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	// No entity has Position.

	q := flecs.NewQuery(w, posID)
	if q.Iter().Next() {
		t.Fatal("empty match: Iter().Next() should return false immediately")
	}

	called := 0
	q.Each(func(_ *flecs.QueryIter) { called++ })
	if called != 0 {
		t.Fatalf("empty match: Each invoked fn %d times, want 0", called)
	}
}

// ── Order independence ────────────────────────────────────────────────────────

func TestQueryOrderIndependence(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	e := w.NewEntity()
	flecs.Set(w.W(), e, Position{1, 2})
	flecs.Set(w.W(), e, Velocity{3, 4})

	collect := func(ids ...flecs.ID) []flecs.ID {
		q := flecs.NewQuery(w, ids...)
		var entities []flecs.ID
		q.Each(func(it *flecs.QueryIter) {
			entities = append(entities, it.Entities()...)
		})
		return entities
	}

	a := collect(posID, velID)
	b := collect(velID, posID)

	if len(a) != len(b) {
		t.Fatalf("order independence: different entity counts %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("order independence: entity at index %d differs: %d vs %d", i, a[i], b[i])
		}
	}
}

// ── Multiple matching tables ──────────────────────────────────────────────────

func TestQueryMultipleMatchingTables(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)
	markerID := flecs.RegisterComponent[Marker](w)

	// Archetype [P, V]
	e1 := w.NewEntity()
	flecs.Set(w.W(), e1, Position{1, 0})
	flecs.Set(w.W(), e1, Velocity{10, 0})

	// Archetype [P, V, Marker]
	e2 := w.NewEntity()
	flecs.Set(w.W(), e2, Position{2, 0})
	flecs.Set(w.W(), e2, Velocity{20, 0})
	flecs.Set(w.W(), e2, Marker{})

	q := flecs.NewQuery(w, posID, velID)
	var visited []flecs.ID
	tablesVisited := 0

	q.Each(func(it *flecs.QueryIter) {
		tablesVisited++
		visited = append(visited, it.Entities()...)
		// Verify Field works on both tables.
		_ = flecs.Field[Position](it, posID)
		_ = flecs.Field[Velocity](it, velID)
	})

	if tablesVisited != 2 {
		t.Fatalf("multiple tables: want 2 tables visited, got %d", tablesVisited)
	}
	if len(visited) != 2 {
		t.Fatalf("multiple tables: want 2 entities total, got %d", len(visited))
	}
	ids := map[flecs.ID]bool{e1: true, e2: true}
	for _, e := range visited {
		delete(ids, e)
	}
	if len(ids) != 0 {
		t.Fatalf("multiple tables: missing entities %v", ids)
	}
	_ = markerID
}

// ── Field[T] correctness — live view ─────────────────────────────────────────

func TestFieldCorrectnessLiveView(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	e1 := w.NewEntity()
	e2 := w.NewEntity()
	flecs.Set(w.W(), e1, Position{1, 2})
	flecs.Set(w.W(), e2, Position{3, 4})

	q := flecs.NewQuery(w, posID)
	it := q.Iter()
	if !it.Next() {
		t.Fatal("live view: Next() returned false")
	}

	// Locate e1's slot.
	entities := it.Entities()
	positions := flecs.Field[Position](it, posID)
	var e1Row int = -1
	for i, e := range entities {
		if e == e1 {
			e1Row = i
			break
		}
	}
	if e1Row < 0 {
		t.Fatal("live view: e1 not found in table")
	}

	// Mutate through the slice.
	positions[e1Row].X = 99

	// Re-query and verify the mutation is visible.
	got, ok := flecs.Get[Position](w.R(), e1)
	if !ok {
		t.Fatal("live view: Get returned false after mutation")
	}
	if got.X != 99 {
		t.Fatalf("live view: X after mutation: got %v, want 99", got.X)
	}
}

// ── Field[T] type-mismatch panic ──────────────────────────────────────────────

func TestFieldTypeMismatchPanic(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	e := w.NewEntity()
	flecs.Set(w.W(), e, Position{1, 2})

	q := flecs.NewQuery(w, posID)
	it := q.Iter()
	if !it.Next() {
		t.Fatal("type-mismatch panic: Next() returned false")
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Field[Velocity] on Position column should panic")
		}
	}()
	// Wrong Go type for posID: posID holds Position, not Velocity.
	_ = flecs.Field[Velocity](it, posID)
}

// ── Field[T] missing-id panic ─────────────────────────────────────────────────

func TestFieldMissingIDPanic(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	e := w.NewEntity()
	flecs.Set(w.W(), e, Position{1, 2})
	// No Velocity on e; velID is not in the [Position] table.

	q := flecs.NewQuery(w, posID)
	it := q.Iter()
	if !it.Next() {
		t.Fatal("missing-id panic: Next() returned false")
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Field with id not in table should panic")
		}
	}()
	_ = flecs.Field[Velocity](it, velID)
}

// ── Tag-component query ───────────────────────────────────────────────────────

func TestQueryTagComponent(t *testing.T) {
	w := flecs.New()
	markerID := flecs.RegisterComponent[Marker](w)

	e1 := w.NewEntity()
	e2 := w.NewEntity()
	flecs.Set(w.W(), e1, Marker{})
	flecs.Set(w.W(), e2, Marker{})

	q := flecs.NewQuery(w, markerID)
	var count int
	var sliceLen int

	q.Each(func(it *flecs.QueryIter) {
		count += it.Count()
		s := flecs.Field[Marker](it, markerID)
		sliceLen = len(s)
	})

	if count != 2 {
		t.Fatalf("tag query: want 2 entities, got %d", count)
	}
	if sliceLen != 2 {
		t.Fatalf("tag query: Field[Marker] slice len want 2, got %d", sliceLen)
	}
}

// ── GC-safe iteration ─────────────────────────────────────────────────────────

func TestQueryGCSafe(t *testing.T) {
	w := flecs.New()
	wsID := flecs.RegisterComponent[WithStr](w)

	want := "persistent heap string that must survive GC"
	e := w.NewEntity()
	flecs.Set(w.W(), e, WithStr{S: want})

	runtime.GC()
	runtime.GC()

	q := flecs.NewQuery(w, wsID)
	it := q.Iter()
	if !it.Next() {
		t.Fatal("GC safe: Next() returned false")
	}
	s := flecs.Field[WithStr](it, wsID)
	if len(s) == 0 {
		t.Fatal("GC safe: Field returned empty slice")
	}
	if s[0].S != want {
		t.Fatalf("GC safe: string corrupted; got len=%d, want len=%d", len(s[0].S), len(want))
	}
}

// ── QueryIter.Table() panic before Next ───────────────────────────────────────

func TestQueryIterTablePanicsBeforeNext(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	e := w.NewEntity()
	flecs.Set(w.W(), e, Position{})

	it := flecs.NewQuery(w, posID).Iter()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Table() before Next should panic")
		}
	}()
	_ = it.Table()
}

// ── QueryIter.Query() back-reference ─────────────────────────────────────────

func TestQueryIterQueryBackref(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	e := w.NewEntity()
	flecs.Set(w.W(), e, Position{})

	q := flecs.NewQuery(w, posID)
	it := q.Iter()
	if it.Next() {
		if it.Query() != q {
			t.Fatal("QueryIter.Query() should return the originating Query")
		}
	}
}

// ── Empty-terms-list panic ────────────────────────────────────────────────────

func TestQueryEmptyTermsPanic(t *testing.T) {
	w := flecs.New()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewQuery with no IDs should panic")
		}
	}()
	_ = flecs.NewQuery(w) // no ids
}

// ── Nil world panic ───────────────────────────────────────────────────────────

func TestQueryNilWorldPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewQuery with nil world should panic")
		}
	}()
	_ = flecs.NewQuery(nil, 1)
}

// ── Terms accessor ────────────────────────────────────────────────────────────

func TestQueryTerms(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	// Input order reversed; Terms() returns sorted copy.
	q := flecs.NewQuery(w, velID, posID)
	terms := q.Terms()
	if len(terms) != 2 {
		t.Fatalf("Terms: want 2, got %d", len(terms))
	}
	// Verify sorted ascending.
	if terms[0] > terms[1] {
		t.Fatalf("Terms: not sorted: %d > %d", terms[0], terms[1])
	}
}

// ── Smallest-set seeding correctness ─────────────────────────────────────────

func TestQuerySmallestSetSeeding(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	// Create 100 entities with Position (all in [Position] table).
	for i := 0; i < 100; i++ {
		e := w.NewEntity()
		flecs.Set(w.W(), e, Position{float32(i), 0})
	}
	// Add Velocity to 2 of them — they migrate to [Position, Velocity].
	e1 := w.NewEntity()
	flecs.Set(w.W(), e1, Position{101, 0})
	flecs.Set(w.W(), e1, Velocity{1, 0})
	e2 := w.NewEntity()
	flecs.Set(w.W(), e2, Position{102, 0})
	flecs.Set(w.W(), e2, Velocity{2, 0})

	// tablesFor(posID) = [[Position], [Position,Velocity]] → 2 tables
	// tablesFor(velID) = [[Position,Velocity]] → 1 table
	// Iter() should seed from velID (smaller set).
	q := flecs.NewQuery(w, posID, velID)
	it := q.Iter()

	candidateCount := flecs.QueryIterCandidateCount(it)
	if candidateCount != 1 {
		t.Fatalf("smallest-set seeding: want 1 candidate table (seeded from velID), got %d", candidateCount)
	}

	// Drain the iterator and verify only e1 and e2 are returned.
	var visited []flecs.ID
	for it.Next() {
		visited = append(visited, it.Entities()...)
	}
	if len(visited) != 2 {
		t.Fatalf("smallest-set seeding: want 2 entities (e1, e2), got %d", len(visited))
	}
	sort.Slice(visited, func(i, j int) bool { return visited[i] < visited[j] })
	want := []flecs.ID{e1, e2}
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	for i := range want {
		if visited[i] != want[i] {
			t.Fatalf("smallest-set seeding: entity mismatch at index %d: got %d, want %d", i, visited[i], want[i])
		}
	}
}
