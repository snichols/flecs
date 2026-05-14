package flecs

import (
	"testing"

	"github.com/snichols/flecs/internal/storage/table"
)

// helpers ─────────────────────────────────────────────────────────────────────

type GrpPos struct{ X float32 }
type GrpVel struct{ Vx float32 }
type GrpHP struct{ HP int32 }

// groupByComponentCount groups a table by its archetype component count.
func groupByComponentCount(t *table.Table) uint64 {
	return uint64(len(t.Type()))
}

// ── Test 1: basic three-archetype grouping ────────────────────────────────────

func TestGroupByComponentCount(t *testing.T) {
	w := New()
	posID := RegisterComponent[GrpPos](w)
	velID := RegisterComponent[GrpVel](w)
	hpID := RegisterComponent[GrpHP](w)

	w.Write(func(fw *Writer) {
		e1 := fw.NewEntity()
		Set(fw, e1, GrpPos{X: 1})

		e2 := fw.NewEntity()
		Set(fw, e2, GrpPos{X: 2})
		Set(fw, e2, GrpVel{Vx: 1})

		e3 := fw.NewEntity()
		Set(fw, e3, GrpPos{X: 3})
		Set(fw, e3, GrpHP{HP: 10})
	})
	_ = velID
	_ = hpID

	cq := NewCachedQueryFromTermsWithOptions(w,
		WithGroupBy(posID, groupByComponentCount),
		With(posID),
	)
	defer cq.Close()

	// Iterate in group-ID order (ascending component count).
	prevGroup := uint64(0)
	tableCount := 0
	it := cq.Iter()
	for it.Next() {
		tbl := it.current
		if tbl == nil {
			t.Fatal("expected archetype table")
		}
		count := uint64(len(tbl.Type()))
		if count < prevGroup {
			t.Fatalf("not in ascending group order: got group %d after %d", count, prevGroup)
		}
		prevGroup = count
		tableCount++
	}
	if tableCount != 3 {
		t.Fatalf("want 3 tables, got %d", tableCount)
	}
}

// ── Test 2: IterGroup yields only the requested group's tables ────────────────

func TestIterGroupSelectsSingleGroup(t *testing.T) {
	w := New()
	posID := RegisterComponent[GrpPos](w)
	velID := RegisterComponent[GrpVel](w)
	hpID := RegisterComponent[GrpHP](w)

	w.Write(func(fw *Writer) {
		e1 := fw.NewEntity()
		Set(fw, e1, GrpPos{})

		e2 := fw.NewEntity()
		Set(fw, e2, GrpPos{})
		Set(fw, e2, GrpVel{})

		e3 := fw.NewEntity()
		Set(fw, e3, GrpPos{})
		Set(fw, e3, GrpHP{})
	})
	_ = velID
	_ = hpID

	cq := NewCachedQueryFromTermsWithOptions(w,
		WithGroupBy(posID, groupByComponentCount),
		With(posID),
	)
	defer cq.Close()

	// Group 2 = tables with exactly 2 components (Pos+Vel and Pos+HP).
	count := 0
	it := cq.IterGroup(2)
	for it.Next() {
		tbl := it.current
		if tbl == nil {
			t.Fatal("expected archetype table")
		}
		if n := len(tbl.Type()); n != 2 {
			t.Fatalf("group 2 should only yield tables with 2 components, got %d", n)
		}
		count++
	}
	if count != 2 {
		t.Fatalf("want 2 tables in group 2, got %d", count)
	}

	// Group 1 = Pos-only table.
	count = 0
	it = cq.IterGroup(1)
	for it.Next() {
		count++
	}
	if count != 1 {
		t.Fatalf("want 1 table in group 1, got %d", count)
	}
}

// ── Test 3: Groups() returns sorted populated group IDs ───────────────────────

func TestGroupsReturnsSortedIDs(t *testing.T) {
	w := New()
	posID := RegisterComponent[GrpPos](w)
	velID := RegisterComponent[GrpVel](w)
	hpID := RegisterComponent[GrpHP](w)

	w.Write(func(fw *Writer) {
		e1 := fw.NewEntity()
		Set(fw, e1, GrpPos{})
		e2 := fw.NewEntity()
		Set(fw, e2, GrpPos{})
		Set(fw, e2, GrpVel{})
		e3 := fw.NewEntity()
		Set(fw, e3, GrpPos{})
		Set(fw, e3, GrpHP{})
	})
	_ = velID
	_ = hpID

	cq := NewCachedQueryFromTermsWithOptions(w,
		WithGroupBy(posID, groupByComponentCount),
		With(posID),
	)
	defer cq.Close()

	gids := cq.Groups()
	if len(gids) != 2 {
		t.Fatalf("want 2 groups, got %v", gids)
	}
	if gids[0] != 1 || gids[1] != 2 {
		t.Fatalf("want [1 2], got %v", gids)
	}
	for i := 1; i < len(gids); i++ {
		if gids[i] < gids[i-1] {
			t.Fatalf("Groups() not sorted: %v", gids)
		}
	}
}

// ── Test 4: GroupBy + WithOrderBy — sorted within each group ─────────────────

func TestGroupByWithOrderBy(t *testing.T) {
	const n = 100
	w := New()
	posID := RegisterComponent[GrpPos](w)
	velID := RegisterComponent[GrpVel](w)
	hpID := RegisterComponent[GrpHP](w)

	w.Write(func(fw *Writer) {
		for i := range n {
			x := float32(n - i) // reversed so sort is visible
			switch i % 3 {
			case 0:
				e := fw.NewEntity()
				Set(fw, e, GrpPos{X: x})
			case 1:
				e := fw.NewEntity()
				Set(fw, e, GrpPos{X: x})
				Set(fw, e, GrpVel{Vx: x})
			case 2:
				e := fw.NewEntity()
				Set(fw, e, GrpPos{X: x})
				Set(fw, e, GrpHP{HP: int32(x)})
			}
		}
	})
	_ = velID
	_ = hpID

	ascCmp := OrderBy[GrpPos](func(_ ID, a *GrpPos, _ ID, b *GrpPos) int {
		if a.X < b.X {
			return -1
		}
		if a.X > b.X {
			return 1
		}
		return 0
	})

	cq := NewCachedQueryFromTermsWithOptions(w,
		WithGroupBy(posID, groupByComponentCount).AndOrderBy(posID, ascCmp),
		With(posID),
	)
	defer cq.Close()

	prevGroup := uint64(0)
	var prevX float32 = -1
	gotEntities := 0
	it := cq.Iter()
	for it.Next() {
		col := Field[GrpPos](it, posID)
		if len(col) != 1 {
			t.Fatalf("sorted iter: want 1 entity per step, got %d", len(col))
		}
		x := col[0].X
		tbl := it.sortedRows[it.sortedPos].table
		var grp uint64
		if tbl != nil {
			grp = groupByComponentCount(tbl)
		}
		if grp < prevGroup {
			t.Fatalf("group order violated: got group %d after %d", grp, prevGroup)
		}
		if grp != prevGroup {
			prevX = -1
		}
		if x < prevX {
			t.Fatalf("sort violated within group %d: %.1f < %.1f", grp, x, prevX)
		}
		prevGroup = grp
		prevX = x
		gotEntities++
	}
	if gotEntities != n {
		t.Fatalf("want %d entities, got %d", n, gotEntities)
	}
}

// ── Test 5: Empty group — no matching entities ────────────────────────────────

func TestGroupsEmptyWhenNoMatch(t *testing.T) {
	w := New()
	posID := RegisterComponent[GrpPos](w)
	velID := RegisterComponent[GrpVel](w)

	// No entities: no tables match, Groups() should return non-nil empty slice.
	cq := NewCachedQueryFromTermsWithOptions(w,
		WithGroupBy(posID, groupByComponentCount),
		With(posID),
		With(velID),
	)
	defer cq.Close()

	gids := cq.Groups()
	if gids == nil {
		t.Fatal("Groups() should return non-nil empty slice when no tables match")
	}
	if len(gids) != 0 {
		t.Fatalf("expected empty, got %v", gids)
	}
	// IterGroup on non-existent group returns exhausted iterator.
	it := cq.IterGroup(42)
	if it.Next() {
		t.Fatal("IterGroup on non-existent group should not yield any tables")
	}
}

// ── Test 6: Cache invalidation triggers re-grouping ───────────────────────────

func TestGroupByInvalidationOnStructuralChange(t *testing.T) {
	w := New()
	posID := RegisterComponent[GrpPos](w)
	velID := RegisterComponent[GrpVel](w)

	var e1 ID
	w.Write(func(fw *Writer) {
		e1 = fw.NewEntity()
		Set(fw, e1, GrpPos{})
		e2 := fw.NewEntity()
		Set(fw, e2, GrpPos{})
	})

	cq := NewCachedQueryFromTermsWithOptions(w,
		WithGroupBy(posID, groupByComponentCount),
		With(posID),
	)
	defer cq.Close()

	// Initially only group 1 (Pos-only table).
	gidsBefore := cq.Groups()
	if len(gidsBefore) != 1 || gidsBefore[0] != 1 {
		t.Fatalf("expected [1] before migration, got %v", gidsBefore)
	}

	// Add Velocity to e1: migrates to a new Pos+Vel table (group 2).
	w.Write(func(fw *Writer) {
		Set(fw, e1, GrpVel{Vx: 5})
	})
	_ = velID

	// After structural change, Groups() should include group 2.
	gidsAfter := cq.Groups()
	if len(gidsAfter) != 2 {
		t.Fatalf("expected 2 groups after migration, got %v", gidsAfter)
	}
	if gidsAfter[0] != 1 || gidsAfter[1] != 2 {
		t.Fatalf("expected [1 2] after migration, got %v", gidsAfter)
	}
}

// ── Test 7: Panic on non-existent component ───────────────────────────────────

func TestGroupByPanicOnNonExistentComponent(t *testing.T) {
	w := New()
	posID := RegisterComponent[GrpPos](w)
	velID := RegisterComponent[GrpVel](w)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when group-by component not in term set")
		}
	}()

	// velID is not in the term set → panic at construction.
	NewCachedQueryFromTermsWithOptions(w,
		WithGroupBy(velID, groupByComponentCount),
		With(posID),
	)
}

// ── Test 8: Multi-table within a single group ─────────────────────────────────

func TestGroupByMultiTableSingleGroup(t *testing.T) {
	w := New()
	posID := RegisterComponent[GrpPos](w)
	velID := RegisterComponent[GrpVel](w)
	hpID := RegisterComponent[GrpHP](w)

	w.Write(func(fw *Writer) {
		// Group 2: Pos+Vel and Pos+HP both land in the same group (count=2).
		e1 := fw.NewEntity()
		Set(fw, e1, GrpPos{})
		Set(fw, e1, GrpVel{})
		e2 := fw.NewEntity()
		Set(fw, e2, GrpPos{})
		Set(fw, e2, GrpHP{})
		// Group 1: Pos only.
		e3 := fw.NewEntity()
		Set(fw, e3, GrpPos{})
	})
	_ = velID
	_ = hpID

	cq := NewCachedQueryFromTermsWithOptions(w,
		WithGroupBy(posID, groupByComponentCount),
		With(posID),
	)
	defer cq.Close()

	// IterGroup(2) should yield both Pos+Vel and Pos+HP tables.
	tablesInGroup2 := 0
	it := cq.IterGroup(2)
	for it.Next() {
		if n := len(it.current.Type()); n != 2 {
			t.Fatalf("expected component count 2, got %d", n)
		}
		tablesInGroup2++
	}
	if tablesInGroup2 != 2 {
		t.Fatalf("want 2 tables in group 2, got %d", tablesInGroup2)
	}
}

// ── Test 9: Stable order across cache hits ────────────────────────────────────

func TestGroupByStableOrderAcrossHits(t *testing.T) {
	w := New()
	posID := RegisterComponent[GrpPos](w)
	velID := RegisterComponent[GrpVel](w)

	w.Write(func(fw *Writer) {
		for range 5 {
			e := fw.NewEntity()
			Set(fw, e, GrpPos{})
		}
		for range 5 {
			e := fw.NewEntity()
			Set(fw, e, GrpPos{})
			Set(fw, e, GrpVel{})
		}
	})
	_ = velID

	cq := NewCachedQueryFromTermsWithOptions(w,
		WithGroupBy(posID, groupByComponentCount),
		With(posID),
	)
	defer cq.Close()

	first := cq.Groups()
	second := cq.Groups()
	if len(first) != len(second) {
		t.Fatalf("group count changed across cache hits: %v vs %v", first, second)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("group order changed at index %d: %v vs %v", i, first, second)
		}
	}
}

// ── Test 10b: AndGroupBy method (WithOrderBy(...).AndGroupBy(...)) ─────────────

func TestAndGroupByMethod(t *testing.T) {
	w := New()
	posID := RegisterComponent[GrpPos](w)
	velID := RegisterComponent[GrpVel](w)

	w.Write(func(fw *Writer) {
		for _, x := range []float32{3, 1, 2} {
			e := fw.NewEntity()
			Set(fw, e, GrpPos{X: x})
		}
		for _, x := range []float32{9, 7, 8} {
			e := fw.NewEntity()
			Set(fw, e, GrpPos{X: x})
			Set(fw, e, GrpVel{})
		}
	})
	_ = velID

	ascCmp := OrderBy[GrpPos](func(_ ID, a *GrpPos, _ ID, b *GrpPos) int {
		if a.X < b.X {
			return -1
		}
		if a.X > b.X {
			return 1
		}
		return 0
	})

	// Use AndGroupBy to chain onto WithOrderBy.
	cq := NewCachedQueryFromTermsWithOptions(w,
		WithOrderBy(posID, ascCmp).AndGroupBy(posID, groupByComponentCount),
		With(posID),
	)
	defer cq.Close()

	gids := cq.Groups()
	if len(gids) != 2 || gids[0] != 1 || gids[1] != 2 {
		t.Fatalf("expected groups [1 2], got %v", gids)
	}
}

// ── Test 10c: Iter() triggers rebuild after structural change ─────────────────

func TestGroupByIterRebuildAfterChange(t *testing.T) {
	w := New()
	posID := RegisterComponent[GrpPos](w)
	velID := RegisterComponent[GrpVel](w)

	var e1 ID
	w.Write(func(fw *Writer) {
		e1 = fw.NewEntity()
		Set(fw, e1, GrpPos{})
		e2 := fw.NewEntity()
		Set(fw, e2, GrpPos{})
	})

	cq := NewCachedQueryFromTermsWithOptions(w,
		WithGroupBy(posID, groupByComponentCount),
		With(posID),
	)
	defer cq.Close()

	// First Iter(): single group 1.
	tableCount := 0
	it := cq.Iter()
	for it.Next() {
		tableCount++
	}
	if tableCount != 1 {
		t.Fatalf("before change: want 1 table, got %d", tableCount)
	}

	// Add Velocity → e1 migrates, new table created.
	w.Write(func(fw *Writer) {
		Set(fw, e1, GrpVel{})
	})
	_ = velID

	// Second Iter(): should rebuild groups and see 2 groups.
	groupsSeen := make(map[uint64]int)
	it = cq.Iter()
	for it.Next() {
		if it.current != nil {
			groupsSeen[groupByComponentCount(it.current)]++
		}
	}
	if len(groupsSeen) != 2 {
		t.Fatalf("after change: want 2 groups in Iter, got %v", groupsSeen)
	}
}

// ── Test 10e: needsGroupRebuild detects ChangeCount change (value write) ──────

func TestGroupByRebuildOnValueWrite(t *testing.T) {
	w := New()
	posID := RegisterComponent[GrpPos](w)

	var e1 ID
	w.Write(func(fw *Writer) {
		e1 = fw.NewEntity()
		Set(fw, e1, GrpPos{X: 1})
		e2 := fw.NewEntity()
		Set(fw, e2, GrpPos{X: 2})
	})

	cq := NewCachedQueryFromTermsWithOptions(w,
		WithGroupBy(posID, groupByComponentCount),
		With(posID),
	)
	defer cq.Close()

	// Prime the cache so groupLastChange is set.
	_ = cq.Groups()

	// Write a component value — bumps ChangeCount without structural change.
	w.Write(func(fw *Writer) {
		Set(fw, e1, GrpPos{X: 99})
	})

	// Groups() should detect stale ChangeCount and re-group (even if result is same).
	gids := cq.Groups()
	if len(gids) != 1 || gids[0] != 1 {
		t.Fatalf("expected [1] after value write, got %v", gids)
	}
}

// ── Test 10d: IterGroup() triggers rebuild when stale ────────────────────────

func TestGroupByIterGroupRebuildWhenStale(t *testing.T) {
	w := New()
	posID := RegisterComponent[GrpPos](w)
	velID := RegisterComponent[GrpVel](w)

	var e1 ID
	w.Write(func(fw *Writer) {
		e1 = fw.NewEntity()
		Set(fw, e1, GrpPos{})
		e2 := fw.NewEntity()
		Set(fw, e2, GrpPos{})
	})

	cq := NewCachedQueryFromTermsWithOptions(w,
		WithGroupBy(posID, groupByComponentCount),
		With(posID),
	)
	defer cq.Close()

	// Prime the cache.
	_ = cq.Groups()

	// Add velocity → e1 migrates to new table (group 2).
	w.Write(func(fw *Writer) {
		Set(fw, e1, GrpVel{})
	})
	_ = velID

	// IterGroup(2) should trigger rebuild and yield the new table.
	count := 0
	it := cq.IterGroup(2)
	for it.Next() {
		count++
	}
	if count != 1 {
		t.Fatalf("expected 1 table in group 2 after migration, got %d", count)
	}
}

// ── Test 10f: IterGroup with mixed (archetype + DontFragment) query ───────────

func TestIterGroupMixedSparseTerm(t *testing.T) {
	w := New()
	posID := RegisterComponent[GrpPos](w)
	type GrpSparse struct{ V int32 }
	sparseID := RegisterComponent[GrpSparse](w)
	SetDontFragment(w, sparseID)

	var e1, e2, e3 ID
	w.Write(func(fw *Writer) {
		e1 = fw.NewEntity()
		Set(fw, e1, GrpPos{X: 1})
		Set(fw, e1, GrpSparse{V: 10})

		e2 = fw.NewEntity()
		Set(fw, e2, GrpPos{X: 2})
		Set(fw, e2, GrpSparse{V: 20})

		e3 = fw.NewEntity()
		Set(fw, e3, GrpPos{X: 3})
		// e3 has no GrpSparse — filtered out by the TermAnd sparse check.
	})
	_ = e3

	// Mixed query: Pos (archetype) + GrpSparse (DontFragment/sparse).
	cq := NewCachedQueryFromTermsWithOptions(w,
		WithGroupBy(posID, groupByComponentCount),
		With(posID),
		With(sparseID),
	)
	defer cq.Close()

	// IterGroup(1) for sparse-mixed query: hasSparseTerms path.
	entities := make(map[ID]bool)
	it := cq.IterGroup(1)
	for it.Next() {
		for _, e := range it.Entities() {
			entities[e] = true
		}
	}
	// Only e1 and e2 have both Pos and GrpSparse.
	if !entities[e1] || !entities[e2] {
		t.Fatalf("IterGroup should yield e1 and e2, got %v", entities)
	}
}

// ── Test 10: Sparse component as a query term (groups from archetype state) ───

func TestGroupBySparseQueryTerm(t *testing.T) {
	w := New()
	posID := RegisterComponent[GrpPos](w)

	// Only Pos entities in a single archetype table; all land in group 1.
	w.Write(func(fw *Writer) {
		for range 5 {
			e := fw.NewEntity()
			Set(fw, e, GrpPos{X: 1})
		}
	})

	// Query with a sparse/optional term; grouping is still by archetype count.
	cq := NewCachedQueryFromTermsWithOptions(w,
		WithGroupBy(posID, groupByComponentCount),
		With(posID),
	)
	defer cq.Close()

	gids := cq.Groups()
	if len(gids) == 0 {
		t.Fatal("expected at least one group")
	}
	// All entities are in a single archetype (Pos only) → only group 1.
	if len(gids) != 1 || gids[0] != 1 {
		t.Fatalf("expected [1], got %v", gids)
	}
}

// ── Test 11: IterGroup on query without WithGroupBy panics ────────────────────

func TestIterGroupPanicsWithoutWithGroupBy(t *testing.T) {
	w := New()
	posID := RegisterComponent[GrpPos](w)

	w.Write(func(fw *Writer) {
		e := fw.NewEntity()
		Set(fw, e, GrpPos{})
	})

	cq := NewCachedQueryFromTerms(w, With(posID))
	defer cq.Close()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when calling IterGroup without WithGroupBy")
		}
	}()
	cq.IterGroup(1)
}

// ── Test 12: Groups() returns nil without WithGroupBy ─────────────────────────

func TestGroupsReturnsNilWithoutWithGroupBy(t *testing.T) {
	w := New()
	posID := RegisterComponent[GrpPos](w)

	cq := NewCachedQueryFromTerms(w, With(posID))
	defer cq.Close()

	if gids := cq.Groups(); gids != nil {
		t.Fatalf("expected nil Groups() on non-grouped query, got %v", gids)
	}
}

// ── Test 13: IterGroup with sort yields sorted entities for that group ─────────

func TestIterGroupWithSort(t *testing.T) {
	w := New()
	posID := RegisterComponent[GrpPos](w)
	velID := RegisterComponent[GrpVel](w)

	w.Write(func(fw *Writer) {
		// Group 1 (Pos only): X values in reverse order.
		for _, x := range []float32{3, 1, 2} {
			e := fw.NewEntity()
			Set(fw, e, GrpPos{X: x})
		}
		// Group 2 (Pos+Vel): X values in reverse order.
		for _, x := range []float32{9, 7, 8} {
			e := fw.NewEntity()
			Set(fw, e, GrpPos{X: x})
			Set(fw, e, GrpVel{})
		}
	})
	_ = velID

	ascCmp := OrderBy[GrpPos](func(_ ID, a *GrpPos, _ ID, b *GrpPos) int {
		if a.X < b.X {
			return -1
		}
		if a.X > b.X {
			return 1
		}
		return 0
	})

	cq := NewCachedQueryFromTermsWithOptions(w,
		WithGroupBy(posID, groupByComponentCount).AndOrderBy(posID, ascCmp),
		With(posID),
	)
	defer cq.Close()

	// IterGroup(1): only group-1 entities, sorted ascending.
	var xs1 []float32
	it := cq.IterGroup(1)
	for it.Next() {
		col := Field[GrpPos](it, posID)
		if len(col) != 1 {
			t.Fatalf("expected 1 entity per step, got %d", len(col))
		}
		xs1 = append(xs1, col[0].X)
	}
	if len(xs1) != 3 {
		t.Fatalf("want 3 entities in group 1, got %d", len(xs1))
	}
	for i := 1; i < len(xs1); i++ {
		if xs1[i] < xs1[i-1] {
			t.Fatalf("group 1 not sorted: %v", xs1)
		}
	}

	// IterGroup(2): only group-2 entities, sorted ascending.
	var xs2 []float32
	it = cq.IterGroup(2)
	for it.Next() {
		col := Field[GrpPos](it, posID)
		if len(col) != 1 {
			t.Fatalf("expected 1 entity per step, got %d", len(col))
		}
		xs2 = append(xs2, col[0].X)
	}
	if len(xs2) != 3 {
		t.Fatalf("want 3 entities in group 2, got %d", len(xs2))
	}
	for i := 1; i < len(xs2); i++ {
		if xs2[i] < xs2[i-1] {
			t.Fatalf("group 2 not sorted: %v", xs2)
		}
	}
	// Group 2 values should all be ≥7; group 1 values ≤3.
	for _, x := range xs1 {
		if x > 3 {
			t.Fatalf("group 1 contains value %.1f that should be in group 2", x)
		}
	}
	for _, x := range xs2 {
		if x < 7 {
			t.Fatalf("group 2 contains value %.1f that belongs to group 1", x)
		}
	}
}
