package flecs_test

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"testing"
	"unsafe"

	"github.com/snichols/flecs"
)

// ── helpers ──────────────────────────────────────────────────────────────────

type SortVal struct{ V int32 }
type SortA struct{ X int32 }
type SortB struct{ Y float32 }
type SortC struct{ Z string }

func intAscCmp(eA flecs.ID, vA *SortVal, eB flecs.ID, vB *SortVal) int {
	if vA.V < vB.V {
		return -1
	}
	if vA.V > vB.V {
		return 1
	}
	return 0
}

// ── Test 1: Basic ascending sort ─────────────────────────────────────────────

func TestSortedQueryBasicAscending(t *testing.T) {
	const n = 100
	w := flecs.New()
	valID := flecs.RegisterComponent[SortVal](w)

	// Assign random values to n entities.
	vals := make([]int32, n)
	for i := range vals {
		vals[i] = rand.N[int32](10000)
	}

	w.Write(func(fw *flecs.Writer) {
		for i := range n {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortVal{V: vals[i]})
		}
	})

	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(valID, flecs.OrderBy[SortVal](intAscCmp)),
		flecs.With(valID),
	)
	defer cq.Close()

	var got []int32
	it := cq.Iter()
	for it.Next() {
		col := flecs.Field[SortVal](it, valID)
		if len(col) != 1 {
			t.Fatalf("sorted iter: want 1 entity per step, got %d", len(col))
		}
		got = append(got, col[0].V)
	}

	if len(got) != n {
		t.Fatalf("want %d entities, got %d", n, len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Fatalf("not ascending at index %d: %d < %d", i, got[i], got[i-1])
		}
	}
}

// ── Test 2: Descending sort ───────────────────────────────────────────────────

func TestSortedQueryDescending(t *testing.T) {
	w := flecs.New()
	valID := flecs.RegisterComponent[SortVal](w)

	w.Write(func(fw *flecs.Writer) {
		for _, v := range []int32{3, 1, 4, 1, 5, 9, 2, 6} {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortVal{V: v})
		}
	})

	descCmp := flecs.OrderBy[SortVal](func(eA flecs.ID, vA *SortVal, eB flecs.ID, vB *SortVal) int {
		return -intAscCmp(eA, vA, eB, vB)
	})

	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(valID, descCmp),
		flecs.With(valID),
	)
	defer cq.Close()

	var got []int32
	it := cq.Iter()
	for it.Next() {
		got = append(got, flecs.Field[SortVal](it, valID)[0].V)
	}

	for i := 1; i < len(got); i++ {
		if got[i] > got[i-1] {
			t.Fatalf("not descending at index %d: %d > %d", i, got[i], got[i-1])
		}
	}
}

// ── Test 3: Stable across re-iteration ───────────────────────────────────────

func TestSortedQueryStableReIteration(t *testing.T) {
	w := flecs.New()
	valID := flecs.RegisterComponent[SortVal](w)

	w.Write(func(fw *flecs.Writer) {
		for _, v := range []int32{5, 3, 8, 1, 9, 2, 7} {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortVal{V: v})
		}
	})

	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(valID, flecs.OrderBy[SortVal](intAscCmp)),
		flecs.With(valID),
	)
	defer cq.Close()

	collect := func() []int32 {
		var out []int32
		it := cq.Iter()
		for it.Next() {
			out = append(out, flecs.Field[SortVal](it, valID)[0].V)
		}
		return out
	}

	first := collect()
	second := collect()
	if len(first) != len(second) {
		t.Fatalf("lengths differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("order changed at index %d: %d vs %d", i, first[i], second[i])
		}
	}
}

// ── Test 4: Cache invalidation on value mutation ──────────────────────────────

func TestSortedQueryInvalidationOnMutation(t *testing.T) {
	w := flecs.New()
	valID := flecs.RegisterComponent[SortVal](w)

	var targets []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		for _, v := range []int32{10, 20, 30, 40, 50} {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortVal{V: v})
			targets = append(targets, e)
		}
	})

	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(valID, flecs.OrderBy[SortVal](intAscCmp)),
		flecs.With(valID),
	)
	defer cq.Close()

	// Initial order: 10 20 30 40 50
	var before []int32
	it := cq.Iter()
	for it.Next() {
		before = append(before, flecs.Field[SortVal](it, valID)[0].V)
	}
	if len(before) != 5 || before[0] != 10 || before[4] != 50 {
		t.Fatalf("unexpected initial order: %v", before)
	}

	// Mutate: set targets[0] (value=10) to 99, so it should move to end.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, targets[0], SortVal{V: 99})
	})

	var after []int32
	it2 := cq.Iter()
	for it2.Next() {
		after = append(after, flecs.Field[SortVal](it2, valID)[0].V)
	}

	if len(after) != 5 {
		t.Fatalf("want 5 entities after mutation, got %d", len(after))
	}
	if after[4] != 99 {
		t.Fatalf("mutated entity should sort last: got %v", after)
	}
	for i := 1; i < len(after); i++ {
		if after[i] < after[i-1] {
			t.Fatalf("not ascending after mutation at index %d: %v", i, after)
		}
	}
}

// ── Test 5: New entity added after construction ───────────────────────────────

func TestSortedQueryNewEntityCorrectPosition(t *testing.T) {
	w := flecs.New()
	valID := flecs.RegisterComponent[SortVal](w)

	w.Write(func(fw *flecs.Writer) {
		for _, v := range []int32{10, 30, 50} {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortVal{V: v})
		}
	})

	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(valID, flecs.OrderBy[SortVal](intAscCmp)),
		flecs.With(valID),
	)
	defer cq.Close()

	// Add a new entity with value 20 (should land between 10 and 30).
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, SortVal{V: 20})
	})

	var got []int32
	it := cq.Iter()
	for it.Next() {
		got = append(got, flecs.Field[SortVal](it, valID)[0].V)
	}

	if len(got) != 4 {
		t.Fatalf("want 4 entities, got %d: %v", len(got), got)
	}
	want := []int32{10, 20, 30, 50}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("position %d: want %d got %d (full: %v)", i, w, got[i], got)
		}
	}
}

// ── Test 6: String comparator via OrderBy[string] ────────────────────────────

type SortName struct{ Name string }

func TestSortedQueryStringComparator(t *testing.T) {
	w := flecs.New()
	nameID := flecs.RegisterComponent[SortName](w)

	names := []string{"Charlie", "Alice", "Eve", "Bob", "Dave"}
	w.Write(func(fw *flecs.Writer) {
		for _, n := range names {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortName{Name: n})
		}
	})

	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(nameID, flecs.OrderBy[SortName](func(eA flecs.ID, vA *SortName, eB flecs.ID, vB *SortName) int {
			if vA.Name < vB.Name {
				return -1
			}
			if vA.Name > vB.Name {
				return 1
			}
			return 0
		})),
		flecs.With(nameID),
	)
	defer cq.Close()

	var got []string
	it := cq.Iter()
	for it.Next() {
		got = append(got, flecs.Field[SortName](it, nameID)[0].Name)
	}

	want := make([]string, len(names))
	copy(want, names)
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("length mismatch: %d vs %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("index %d: want %q got %q", i, want[i], got[i])
		}
	}
}

// ── Test 7: Multi-term sorted query — all fields accessible ──────────────────

type SA struct{ X int32 }
type SB struct{ Y float32 }
type SC struct{ Z int32 }

func TestSortedQueryMultiTermAllFieldsAccessible(t *testing.T) {
	w := flecs.New()
	aID := flecs.RegisterComponent[SA](w)
	bID := flecs.RegisterComponent[SB](w)
	cID := flecs.RegisterComponent[SC](w)

	// Create 5 entities with all three components.
	w.Write(func(fw *flecs.Writer) {
		for i := int32(0); i < 5; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, SA{X: 5 - i}) // reverse order
			flecs.Set(fw, e, SB{Y: float32(i)})
			flecs.Set(fw, e, SC{Z: i * 10})
		}
	})

	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(aID, flecs.OrderBy[SA](func(eA flecs.ID, vA *SA, eB flecs.ID, vB *SA) int {
			if vA.X < vB.X {
				return -1
			}
			if vA.X > vB.X {
				return 1
			}
			return 0
		})),
		flecs.With(aID),
		flecs.With(bID),
		flecs.With(cID),
	)
	defer cq.Close()

	var gotX []int32
	var gotB []float32
	it := cq.Iter()
	for it.Next() {
		ents := it.Entities()
		if len(ents) != 1 {
			t.Fatalf("want 1 entity per step, got %d", len(ents))
		}
		colA := flecs.Field[SA](it, aID)
		colB := flecs.Field[SB](it, bID)
		if len(colA) != 1 || len(colB) != 1 {
			t.Fatalf("field slice lengths: A=%d B=%d", len(colA), len(colB))
		}
		gotX = append(gotX, colA[0].X)
		gotB = append(gotB, colB[0].Y)
	}

	if len(gotX) != 5 {
		t.Fatalf("want 5 results, got %d", len(gotX))
	}
	// Sorted ascending by X: 1 2 3 4 5
	for i, v := range gotX {
		if v != int32(i+1) {
			t.Fatalf("index %d: want X=%d got %d", i, i+1, v)
		}
	}
	// B values should be accessible (not zero-sliced)
	for _, v := range gotB {
		_ = v // just ensure no panic
	}
}

// ── Test 8: Empty result ──────────────────────────────────────────────────────

func TestSortedQueryEmpty(t *testing.T) {
	w := flecs.New()
	valID := flecs.RegisterComponent[SortVal](w)
	// No entities with SortVal.

	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(valID, flecs.OrderBy[SortVal](intAscCmp)),
		flecs.With(valID),
	)
	defer cq.Close()

	count := 0
	it := cq.Iter()
	for it.Next() {
		count++
	}
	if count != 0 {
		t.Fatalf("want 0 iterations, got %d", count)
	}
}

// ── Test 9: Construction panic — component not in term set ───────────────────

func TestSortedQueryConstructionPanicBadComponent(t *testing.T) {
	w := flecs.New()
	valID := flecs.RegisterComponent[SortVal](w)
	otherID := flecs.RegisterComponent[SortA](w)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when sort-by component is not in term set")
		}
	}()

	// otherID is NOT in the terms — must panic.
	_ = flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(otherID, flecs.OrderBy[SortA](func(eA flecs.ID, vA *SortA, eB flecs.ID, vB *SortA) int {
			return 0
		})),
		flecs.With(valID),
	)
}

// ── Test 9b: Construction panic — pair sort component ────────────────────────

func TestSortedQueryConstructionPanicPairComponent(t *testing.T) {
	w := flecs.New()
	var rel, tgt flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
	})
	pairID := flecs.MakePair(rel, tgt)
	valID := flecs.RegisterComponent[SortVal](w)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when sort-by component is a pair")
		}
	}()

	_ = flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(pairID, flecs.OrderByFunc(func(eA flecs.ID, vA unsafe.Pointer, eB flecs.ID, vB unsafe.Pointer) int {
			return 0
		})),
		flecs.With(valID),
	)
}

// ── Test 10: Mixed sparse + archetype + sort-by ───────────────────────────────

type SortArchComp struct{ X int32 }
type SortDFComp struct{ Tag bool }

func TestSortedQueryMixedSparseArchetype(t *testing.T) {
	w := flecs.New()
	archID := flecs.RegisterComponent[SortArchComp](w)
	dfID := flecs.RegisterComponent[SortDFComp](w)

	// Make dfID a DontFragment component (pure sparse-set).
	flecs.SetDontFragment(w, dfID)

	// Create 8 entities, 5 of which have both components.
	var withBoth []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		for i := int32(0); i < 5; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortArchComp{X: 5 - i}) // 5,4,3,2,1
			flecs.Set(fw, e, SortDFComp{Tag: true})
			withBoth = append(withBoth, e)
		}
		// 3 entities with only archID — should NOT match the query.
		for i := int32(0); i < 3; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortArchComp{X: int32(i + 10)})
		}
	})
	_ = withBoth

	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(archID, flecs.OrderBy[SortArchComp](func(eA flecs.ID, vA *SortArchComp, eB flecs.ID, vB *SortArchComp) int {
			if vA.X < vB.X {
				return -1
			}
			if vA.X > vB.X {
				return 1
			}
			return 0
		})),
		flecs.With(archID),
		flecs.With(dfID),
	)
	defer cq.Close()

	var got []int32
	it := cq.Iter()
	for it.Next() {
		col := flecs.Field[SortArchComp](it, archID)
		got = append(got, col[0].X)
	}

	if len(got) != 5 {
		t.Fatalf("want 5 results (mixed match), got %d: %v", len(got), got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Fatalf("not ascending at index %d: %v", i, got)
		}
	}
}

// ── Test 11 (bonus): Sort by a sparse component ───────────────────────────────

type SortSparseVal struct{ V int32 }

func TestSortedQueryBySparseComponent(t *testing.T) {
	w := flecs.New()
	sparseID := flecs.RegisterComponent[SortSparseVal](w)
	flecs.SetSparse(w, sparseID)

	// Create 6 entities in non-sorted order.
	w.Write(func(fw *flecs.Writer) {
		for _, v := range []int32{6, 2, 5, 1, 4, 3} {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortSparseVal{V: v})
		}
	})

	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(sparseID, flecs.OrderBy[SortSparseVal](func(eA flecs.ID, vA *SortSparseVal, eB flecs.ID, vB *SortSparseVal) int {
			if vA.V < vB.V {
				return -1
			}
			if vA.V > vB.V {
				return 1
			}
			return 0
		})),
		flecs.With(sparseID),
	)
	defer cq.Close()

	var got []int32
	it := cq.Iter()
	for it.Next() {
		ents := it.Entities()
		if len(ents) != 1 {
			t.Fatalf("want 1 entity per step, got %d", len(ents))
		}
		col := flecs.Field[SortSparseVal](it, sparseID)
		got = append(got, col[0].V)
	}

	if len(got) != 6 {
		t.Fatalf("want 6 results, got %d", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Fatalf("not ascending at index %d: %v", i, got)
		}
	}
}

// ── Test: Entities() returns length-1 slice in sorted mode ───────────────────

func TestSortedQueryEntitiesLengthOne(t *testing.T) {
	w := flecs.New()
	valID := flecs.RegisterComponent[SortVal](w)

	var created []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		for _, v := range []int32{3, 1, 2} {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortVal{V: v})
			created = append(created, e)
		}
	})

	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(valID, flecs.OrderBy[SortVal](intAscCmp)),
		flecs.With(valID),
	)
	defer cq.Close()

	it := cq.Iter()
	steps := 0
	for it.Next() {
		ents := it.Entities()
		if len(ents) != 1 {
			t.Fatalf("step %d: Entities() length = %d, want 1", steps, len(ents))
		}
		col := flecs.Field[SortVal](it, valID)
		if len(col) != 1 {
			t.Fatalf("step %d: Field length = %d, want 1", steps, len(col))
		}
		steps++
	}
	if steps != 3 {
		t.Fatalf("want 3 steps, got %d", steps)
	}
}

// ── Test: Sorted Each() iteration ─────────────────────────────────────────────

func TestSortedQueryEach(t *testing.T) {
	w := flecs.New()
	valID := flecs.RegisterComponent[SortVal](w)

	w.Write(func(fw *flecs.Writer) {
		for _, v := range []int32{5, 1, 3} {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortVal{V: v})
		}
	})

	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(valID, flecs.OrderBy[SortVal](intAscCmp)),
		flecs.With(valID),
	)
	defer cq.Close()

	var got []int32
	cq.Each(func(it *flecs.QueryIter) {
		got = append(got, flecs.Field[SortVal](it, valID)[0].V)
	})

	if len(got) != 3 || got[0] != 1 || got[1] != 3 || got[2] != 5 {
		t.Fatalf("unexpected sorted Each order: %v", got)
	}
}

// ── Test: Closed sorted query returns empty iterator ─────────────────────────

func TestSortedQueryClosedReturnsEmpty(t *testing.T) {
	w := flecs.New()
	valID := flecs.RegisterComponent[SortVal](w)

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, SortVal{V: 1})
	})

	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(valID, flecs.OrderBy[SortVal](intAscCmp)),
		flecs.With(valID),
	)
	cq.Close()

	it := cq.Iter()
	if it.Next() {
		t.Fatal("closed query: Next() must return false")
	}
}

// ── Test: WithOrderBy zero-option behaves like NewCachedQueryFromTerms ────────

func TestSortedQueryZeroOptions(t *testing.T) {
	w := flecs.New()
	valID := flecs.RegisterComponent[SortVal](w)

	w.Write(func(fw *flecs.Writer) {
		for _, v := range []int32{1, 2, 3} {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortVal{V: v})
		}
	})

	// Zero-value CachedQueryOptions = no sort.
	cq := flecs.NewCachedQueryFromTermsWithOptions(w, flecs.CachedQueryOptions{}, flecs.With(valID))
	defer cq.Close()

	count := 0
	it := cq.Iter()
	for it.Next() {
		count += it.Count()
	}
	if count != 3 {
		t.Fatalf("want 3 entities, got %d", count)
	}
}

// ── Test: Pure-DontFragment sorted query ─────────────────────────────────────

type SortDF struct{ V int32 }

func TestSortedQueryPureDontFragment(t *testing.T) {
	w := flecs.New()
	dfID := flecs.RegisterComponent[SortDF](w)
	flecs.SetDontFragment(w, dfID)

	w.Write(func(fw *flecs.Writer) {
		for _, v := range []int32{5, 3, 1, 4, 2} {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortDF{V: v})
		}
	})

	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(dfID, flecs.OrderBy[SortDF](func(eA flecs.ID, vA *SortDF, eB flecs.ID, vB *SortDF) int {
			if vA.V < vB.V {
				return -1
			}
			if vA.V > vB.V {
				return 1
			}
			return 0
		})),
		flecs.With(dfID),
	)
	defer cq.Close()

	var got []int32
	it := cq.Iter()
	for it.Next() {
		ents := it.Entities()
		if len(ents) != 1 {
			t.Fatalf("want 1 entity per step, got %d", len(ents))
		}
		col := flecs.Field[SortDF](it, dfID)
		got = append(got, col[0].V)
	}

	if len(got) != 5 {
		t.Fatalf("want 5 results, got %d", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Fatalf("not ascending at index %d: %v", i, got)
		}
	}
}

// ── Test: Pure-DontFragment sorted — new entity triggers re-sort ──────────────

func TestSortedQueryDontFragmentInvalidation(t *testing.T) {
	w := flecs.New()
	dfID := flecs.RegisterComponent[SortDF](w)
	flecs.SetDontFragment(w, dfID)

	w.Write(func(fw *flecs.Writer) {
		for _, v := range []int32{10, 30, 50} {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortDF{V: v})
		}
	})

	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(dfID, flecs.OrderBy[SortDF](func(eA flecs.ID, vA *SortDF, eB flecs.ID, vB *SortDF) int {
			if vA.V < vB.V {
				return -1
			}
			if vA.V > vB.V {
				return 1
			}
			return 0
		})),
		flecs.With(dfID),
	)
	defer cq.Close()

	// Initial iteration: 3 entities.
	count0 := 0
	it0 := cq.Iter()
	for it0.Next() {
		count0++
	}
	if count0 != 3 {
		t.Fatalf("want 3 initially, got %d", count0)
	}

	// Add a new entity — DontFragment: this bumps the sparse-set version.
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, SortDF{V: 20})
	})

	var got []int32
	it := cq.Iter()
	for it.Next() {
		got = append(got, flecs.Field[SortDF](it, dfID)[0].V)
	}

	if len(got) != 4 {
		t.Fatalf("want 4 after add, got %d: %v", len(got), got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Fatalf("not ascending at index %d: %v", i, got)
		}
	}
}

// ── Test: Mixed sorted query with TermNot DontFragment ───────────────────────

type ArchSort struct{ X int32 }
type ExcludeDFTag struct{ Marker bool }

func TestSortedQueryNotDontFragment(t *testing.T) {
	w := flecs.New()
	archID := flecs.RegisterComponent[ArchSort](w)
	excludeID := flecs.RegisterComponent[ExcludeDFTag](w)
	flecs.SetDontFragment(w, excludeID)

	// Create 6 entities; 2 of them have the DontFragment tag (should be excluded).
	w.Write(func(fw *flecs.Writer) {
		for i, v := range []int32{5, 4, 3, 2, 1} {
			e := fw.NewEntity()
			flecs.Set(fw, e, ArchSort{X: v})
			if i < 2 { // first two get the DontFragment "exclude" tag
				flecs.Set(fw, e, ExcludeDFTag{Marker: true})
			}
		}
	})

	// Query: has ArchSort, NOT ExcludeDFTag.
	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(archID, flecs.OrderBy[ArchSort](func(eA flecs.ID, vA *ArchSort, eB flecs.ID, vB *ArchSort) int {
			if vA.X < vB.X {
				return -1
			}
			if vA.X > vB.X {
				return 1
			}
			return 0
		})),
		flecs.With(archID),
		flecs.Without(excludeID),
	)
	defer cq.Close()

	var got []int32
	it := cq.Iter()
	for it.Next() {
		got = append(got, flecs.Field[ArchSort](it, archID)[0].X)
	}

	if len(got) != 3 {
		t.Fatalf("want 3 results (2 excluded), got %d: %v", len(got), got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Fatalf("not ascending at index %d: %v", i, got)
		}
	}
}

// ── Test: Sorted query with optional term (nil valPtr) ───────────────────────

type SortOptComp struct{ X int32 }
type SortOptMaybe struct{ V int32 }

func TestSortedQueryOptionalTerm(t *testing.T) {
	w := flecs.New()
	baseID := flecs.RegisterComponent[SortOptComp](w)
	optID := flecs.RegisterComponent[SortOptMaybe](w)

	w.Write(func(fw *flecs.Writer) {
		for i, v := range []int32{3, 1, 2} {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortOptComp{X: v})
			if i == 0 {
				flecs.Set(fw, e, SortOptMaybe{V: 100})
			}
		}
	})

	// Sort by optional term — entities without it receive nil valPtr.
	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(baseID, flecs.OrderBy[SortOptComp](func(eA flecs.ID, vA *SortOptComp, eB flecs.ID, vB *SortOptComp) int {
			if vA.X < vB.X {
				return -1
			}
			if vA.X > vB.X {
				return 1
			}
			return 0
		})),
		flecs.With(baseID),
		flecs.Maybe(optID),
	)
	defer cq.Close()

	count := 0
	it := cq.Iter()
	for it.Next() {
		ents := it.Entities()
		if len(ents) != 1 {
			t.Fatalf("want 1 entity per step, got %d", len(ents))
		}
		_ = flecs.Field[SortOptComp](it, baseID)
		_, _ = flecs.FieldMaybe[SortOptMaybe](it, optID)
		count++
	}
	if count != 3 {
		t.Fatalf("want 3, got %d", count)
	}
}

// ── Test: Mixed sorted query with Union pair ───────────────────────────────────

type SortUnionArch struct{ X int32 }

func TestSortedQueryMixedWithUnion(t *testing.T) {
	w := flecs.New()
	archID := flecs.RegisterComponent[SortUnionArch](w)

	var rel, tgtA, tgtB flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgtA = fw.NewEntity()
		tgtB = fw.NewEntity()
	})
	flecs.SetUnion(w, rel)

	// 5 entities total: 3 with (rel,tgtA), 2 with (rel,tgtB).
	// Query: With(archID), With((rel,tgtA)) → should match only 3.
	w.Write(func(fw *flecs.Writer) {
		for i, v := range []int32{5, 4, 3} {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortUnionArch{X: v})
			fw.AddID(e, flecs.MakePair(rel, tgtA))
			_ = i
		}
		for _, v := range []int32{2, 1} {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortUnionArch{X: v})
			fw.AddID(e, flecs.MakePair(rel, tgtB))
		}
	})

	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(archID, flecs.OrderBy[SortUnionArch](func(eA flecs.ID, vA *SortUnionArch, eB flecs.ID, vB *SortUnionArch) int {
			if vA.X < vB.X {
				return -1
			}
			if vA.X > vB.X {
				return 1
			}
			return 0
		})),
		flecs.With(archID),
		flecs.With(flecs.MakePair(rel, tgtA)),
	)
	defer cq.Close()

	var got []int32
	it := cq.Iter()
	for it.Next() {
		got = append(got, flecs.Field[SortUnionArch](it, archID)[0].X)
	}

	if len(got) != 3 {
		t.Fatalf("want 3 results (tgtA only), got %d: %v", len(got), got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Fatalf("not ascending at index %d: %v", i, got)
		}
	}
}

// ── Test: Nil world panics ────────────────────────────────────────────────────

func TestNewCachedQueryFromTermsWithOptionsNilWorld(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil world")
		}
	}()
	_ = flecs.NewCachedQueryFromTermsWithOptions(nil,
		flecs.WithOrderBy(0, flecs.OrderByFunc(func(eA flecs.ID, vA unsafe.Pointer, eB flecs.ID, vB unsafe.Pointer) int { return 0 })),
		flecs.With(flecs.ID(1)),
	)
}

// ── Test: tablesAdded path — new table created after sorted query built ────────

func TestSortedQueryTablesAddedPath(t *testing.T) {
	w := flecs.New()
	valID := flecs.RegisterComponent[SortVal](w)
	_ = flecs.RegisterComponent[Velocity](w)

	// Create entities with BOTH val+vel first.
	w.Write(func(fw *flecs.Writer) {
		for _, v := range []int32{5, 3} {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortVal{V: v})
			flecs.Set(fw, e, Velocity{DX: 1})
		}
	})

	// Build sorted query on existing [SortVal+Velocity] table.
	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(valID, flecs.OrderBy[SortVal](intAscCmp)),
		flecs.With(valID),
	)
	defer cq.Close()

	// Now add a SortVal-only entity — creates new [SortVal] table → tablesAdded=true.
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, SortVal{V: 1})
	})

	var got []int32
	it := cq.Iter()
	for it.Next() {
		got = append(got, flecs.Field[SortVal](it, valID)[0].V)
	}

	if len(got) != 3 {
		t.Fatalf("want 3 entities (new table added), got %d: %v", len(got), got)
	}
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Fatalf("not ascending at index %d: %v", i, got)
		}
	}
}

// ── Test: Union TermNot in sorted query ───────────────────────────────────────

type SortUnionNot struct{ X int32 }

func TestSortedQueryUnionNotTerm(t *testing.T) {
	w := flecs.New()
	archID := flecs.RegisterComponent[SortUnionNot](w)

	var rel, tgtA, tgtB flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgtA = fw.NewEntity()
		tgtB = fw.NewEntity()
	})
	flecs.SetUnion(w, rel)

	// Union TermNot semantics: Without((rel,tgtB)) excludes any entity that has
	// ANY (rel,*) pair (union store presence check, not target-specific).
	// Entities with (rel,tgtA) or (rel,tgtB) are both excluded; only entities
	// with no union pair at all pass the filter.
	w.Write(func(fw *flecs.Writer) {
		for _, v := range []int32{4, 2} {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortUnionNot{X: v})
			fw.AddID(e, flecs.MakePair(rel, tgtA)) // excluded: has (rel,*)
		}
		for _, v := range []int32{3, 1} {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortUnionNot{X: v})
			fw.AddID(e, flecs.MakePair(rel, tgtB)) // excluded: has (rel,*)
		}
		for _, v := range []int32{6, 5} {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortUnionNot{X: v}) // included: no (rel,*) pair
		}
	})

	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(archID, flecs.OrderBy[SortUnionNot](func(eA flecs.ID, vA *SortUnionNot, eB flecs.ID, vB *SortUnionNot) int {
			if vA.X < vB.X {
				return -1
			}
			if vA.X > vB.X {
				return 1
			}
			return 0
		})),
		flecs.With(archID),
		flecs.Without(flecs.MakePair(rel, tgtB)),
	)
	defer cq.Close()

	var got []int32
	it := cq.Iter()
	for it.Next() {
		got = append(got, flecs.Field[SortUnionNot](it, archID)[0].X)
	}

	// Only the 2 no-union-pair entities pass Without((rel,tgtB)).
	if len(got) != 2 {
		t.Fatalf("want 2 results (no-union-pair entities), got %d: %v", len(got), got)
	}
	if got[0] != 5 || got[1] != 6 {
		t.Fatalf("unexpected values: %v", got)
	}
}

// ── Test: sparseAndOnly with TermNot DontFragment (covers continue branches) ──

type SortDFRequired struct{ V int32 }
type SortDFExclude struct{ Flag bool }

func TestSortedQuerySparseOnlyWithNotDontFragment(t *testing.T) {
	w := flecs.New()
	reqID := flecs.RegisterComponent[SortDFRequired](w)
	excID := flecs.RegisterComponent[SortDFExclude](w)
	flecs.SetDontFragment(w, reqID)
	flecs.SetDontFragment(w, excID)

	// 4 entities: 2 with reqID only, 2 with reqID + excID.
	// Query: With(reqID), Without(excID) — sparseAndOnly=true.
	// This exercises the TermNot DontFragment continue branches in rebuildSorted
	// and needsSortRebuild.
	w.Write(func(fw *flecs.Writer) {
		for _, v := range []int32{3, 1} {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortDFRequired{V: v})
		}
		for _, v := range []int32{4, 2} {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortDFRequired{V: v})
			flecs.Set(fw, e, SortDFExclude{Flag: true})
		}
	})

	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(reqID, flecs.OrderBy[SortDFRequired](func(eA flecs.ID, vA *SortDFRequired, eB flecs.ID, vB *SortDFRequired) int {
			if vA.V < vB.V {
				return -1
			}
			if vA.V > vB.V {
				return 1
			}
			return 0
		})),
		flecs.With(reqID),
		flecs.Without(excID),
	)
	defer cq.Close()

	var got []int32
	it := cq.Iter()
	for it.Next() {
		got = append(got, flecs.Field[SortDFRequired](it, reqID)[0].V)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 results, got %d: %v", len(got), got)
	}
	if got[0] != 1 || got[1] != 3 {
		t.Fatalf("want [1,3], got %v", got)
	}

	// Second Iter() triggers needsSortRebuild, exercising the TermNot continue at line 125.
	it2 := cq.Iter()
	count := 0
	for it2.Next() {
		count++
	}
	if count != 2 {
		t.Fatalf("second iter: want 2, got %d", count)
	}
}

// ── Test: sort by optional component — absent from some tables ────────────────

type SortByOptBase struct{ X int32 }
type SortByOptKey struct{ K int32 }

func TestSortedQuerySortByOptionalAbsent(t *testing.T) {
	w := flecs.New()
	baseID := flecs.RegisterComponent[SortByOptBase](w)
	keyID := flecs.RegisterComponent[SortByOptKey](w)

	// 3 entities with baseID, 2 of which also have keyID.
	// Sort BY keyID (optional); entities without keyID receive nil valPtr.
	w.Write(func(fw *flecs.Writer) {
		for _, v := range []int32{5, 3} {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortByOptBase{X: v})
			flecs.Set(fw, e, SortByOptKey{K: v})
		}
		e := fw.NewEntity()
		flecs.Set(fw, e, SortByOptBase{X: 4}) // no keyID
	})

	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(keyID, flecs.OrderBy[SortByOptKey](func(eA flecs.ID, vA *SortByOptKey, eB flecs.ID, vB *SortByOptKey) int {
			if vA == nil && vB == nil {
				return 0
			}
			if vA == nil {
				return -1 // nil sorts first
			}
			if vB == nil {
				return 1
			}
			if vA.K < vB.K {
				return -1
			}
			if vA.K > vB.K {
				return 1
			}
			return 0
		})),
		flecs.With(baseID),
		flecs.Maybe(keyID),
	)
	defer cq.Close()

	count := 0
	it := cq.Iter()
	for it.Next() {
		_ = flecs.Field[SortByOptBase](it, baseID)
		count++
	}
	if count != 3 {
		t.Fatalf("want 3 results, got %d", count)
	}
}

// ── Benchmark ─────────────────────────────────────────────────────────────────

func BenchmarkSortedQuery100(b *testing.B) {
	const n = 100
	w := flecs.New()
	valID := flecs.RegisterComponent[SortVal](w)
	w.Write(func(fw *flecs.Writer) {
		for i := range n {
			e := fw.NewEntity()
			flecs.Set(fw, e, SortVal{V: int32(n - i)})
		}
	})
	cq := flecs.NewCachedQueryFromTermsWithOptions(w,
		flecs.WithOrderBy(valID, flecs.OrderBy[SortVal](intAscCmp)),
		flecs.With(valID),
	)
	defer cq.Close()

	b.ResetTimer()
	for range b.N {
		it := cq.Iter()
		for it.Next() {
			_ = flecs.Field[SortVal](it, valID)
		}
	}
}

// ensure fmt is used (for Sprintf in diagnostics)
var _ = fmt.Sprintf
