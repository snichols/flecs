package flecs_test

import (
	"sort"
	"testing"

	"github.com/snichols/flecs"
)

// ── Basic construction ────────────────────────────────────────────────────────

func TestCachedQueryBasicConstruction(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	// Empty world: no tables with both P and V.
	cq := flecs.NewCachedQuery(w, posID, velID)
	defer cq.Close()
	if cq.Count() != 0 {
		t.Fatalf("empty world: want Count()=0, got %d", cq.Count())
	}

	// Add an entity with both; a new [P,V] table is created.
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1})
		flecs.Set(fw, e, Velocity{DX: 1})
	})

	if cq.Count() != 1 {
		t.Fatalf("after Set[P]+Set[V]: want Count()=1, got %d", cq.Count())
	}
	if cq.EntityCount() != 1 {
		t.Fatalf("after Set[P]+Set[V]: want EntityCount()=1, got %d", cq.EntityCount())
	}
}

// ── Single-term query ─────────────────────────────────────────────────────────

func TestCachedQuerySingleTerm(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < 3; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, Position{X: float32(i)})
		}
	})

	cq := flecs.NewCachedQuery(w, posID)
	defer cq.Close()

	if cq.Count() != 1 {
		t.Fatalf("want 1 table, got %d", cq.Count())
	}
	if cq.EntityCount() != 3 {
		t.Fatalf("want 3 entities, got %d", cq.EntityCount())
	}
}

// ── Iter walks cached tables ──────────────────────────────────────────────────

func TestCachedQueryIterWalksCachedTables(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	// Create 5 entities with [P,V].
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < 5; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, Position{X: float32(i)})
			flecs.Set(fw, e, Velocity{DX: 1})
		}
	})

	cq := flecs.NewCachedQuery(w, posID, velID)
	defer cq.Close()

	countEntities := func() int {
		n := 0
		cq.Each(func(it *flecs.QueryIter) { n += it.Count() })
		return n
	}

	if got := countEntities(); got != 5 {
		t.Fatalf("initial: want 5 entities, got %d", got)
	}

	// Add 5 more in a new [P,V,Marker] table.
	type LocalMarker struct{}
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < 5; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, Position{X: float32(i + 5)})
			flecs.Set(fw, e, Velocity{DX: 2})
			flecs.Set(fw, e, LocalMarker{})
		}
	})

	// CachedQuery was notified when [P,V,Marker] was created.
	if got := countEntities(); got != 10 {
		t.Fatalf("after new table: want 10 entities, got %d", got)
	}
}

// ── Initial population includes pre-existing matches ─────────────────────────

func TestCachedQueryInitialPopulation(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	// Create entities BEFORE constructing the cached query.
	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < 4; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, Position{X: float32(i)})
			flecs.Set(fw, e, Velocity{DX: float32(i)})
		}
	})

	cq := flecs.NewCachedQuery(w, posID, velID)
	defer cq.Close()

	if cq.EntityCount() != 4 {
		t.Fatalf("want 4 pre-existing entities, got %d", cq.EntityCount())
	}
}

// ── No new table = no cache growth ───────────────────────────────────────────

func TestCachedQueryNoGrowthWithoutNewTable(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1})
	})

	cq := flecs.NewCachedQuery(w, posID)
	defer cq.Close()

	initial := cq.Count()

	// Repeated Iter calls must not grow cq.tables.
	for i := 0; i < 5; i++ {
		it := cq.Iter()
		for it.Next() {
		}
	}
	if cq.Count() != initial {
		t.Fatalf("repeated Iter grew table count: want %d, got %d", initial, cq.Count())
	}

	// Setting a component that the entity already has also must not create a
	// new table (it's an in-place update).
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, Position{X: 99})
	})
	if cq.Count() != initial {
		t.Fatalf("in-place Set grew table count: want %d, got %d", initial, cq.Count())
	}
}

// ── Migration adds matching table ─────────────────────────────────────────────

func TestCachedQueryMigrationAddsMatchingTable(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	// Entity starts in [P]-only table.
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1})
	})

	cq := flecs.NewCachedQuery(w, posID, velID)
	defer cq.Close()

	if cq.Count() != 0 {
		t.Fatalf("before migration: want Count()=0, got %d", cq.Count())
	}

	// Migrate e to [P,V].
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, Velocity{DX: 1})
	})

	if cq.Count() != 1 {
		t.Fatalf("after migration: want Count()=1, got %d", cq.Count())
	}
	if cq.EntityCount() != 1 {
		t.Fatalf("after migration: want EntityCount()=1, got %d", cq.EntityCount())
	}
}

// ── Migration does NOT add non-matching table ─────────────────────────────────

func TestCachedQueryMigrationNonMatchingTable(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	cq := flecs.NewCachedQuery(w, posID, velID)
	defer cq.Close()

	// Create [P]-only entity; table does not match [P,V] query.
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1})
		_ = e
	})

	if cq.Count() != 0 {
		t.Fatalf("want Count()=0 after non-matching migration, got %d", cq.Count())
	}
}

// ── Close stops matching ──────────────────────────────────────────────────────

func TestCachedQueryCloseStopsMatching(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	cq := flecs.NewCachedQuery(w, posID)
	cq.Close()

	// Post-close migration should not grow the cache.
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1})
	})

	if cq.Count() != 0 {
		t.Fatalf("post-Close: want Count()=0, got %d", cq.Count())
	}
	if cq.EntityCount() != 0 {
		t.Fatalf("post-Close: want EntityCount()=0, got %d", cq.EntityCount())
	}
}

// ── Close is idempotent ───────────────────────────────────────────────────────

func TestCachedQueryCloseIdempotent(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	cq := flecs.NewCachedQuery(w, posID)

	cq.Close()
	cq.Close() // must not panic

	if !cq.IsClosed() {
		t.Fatal("IsClosed() should return true after Close")
	}
}

// ── Iter after Close ──────────────────────────────────────────────────────────

func TestCachedQueryIterAfterClose(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1})
	})

	cq := flecs.NewCachedQuery(w, posID)
	cq.Close()

	it := cq.Iter()
	if it.Next() {
		t.Fatal("Next() after Close should return false")
	}
}

// ── Terms after Close ─────────────────────────────────────────────────────────

func TestCachedQueryTermsAfterClose(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	cq := flecs.NewCachedQuery(w, posID)
	cq.Close()
	if cq.Terms() != nil {
		t.Fatal("Terms() after Close should return nil")
	}
}

// ── Multiple cached queries ───────────────────────────────────────────────────

func TestCachedQueryMultiple(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	// [P]-only entity.
	w.Write(func(fw *flecs.Writer) {
		eP := fw.NewEntity()
		flecs.Set(fw, eP, Position{X: 1})
		// [V]-only entity.
		eV := fw.NewEntity()
		flecs.Set(fw, eV, Velocity{DX: 1})
		// [P,V] entity.
		ePV := fw.NewEntity()
		flecs.Set(fw, ePV, Position{X: 2})
		flecs.Set(fw, ePV, Velocity{DX: 2})
	})

	cqP := flecs.NewCachedQuery(w, posID)
	cqV := flecs.NewCachedQuery(w, velID)
	defer cqP.Close()
	defer cqV.Close()

	if cqP.EntityCount() != 2 {
		t.Errorf("P query: want 2 entities, got %d", cqP.EntityCount())
	}
	if cqV.EntityCount() != 2 {
		t.Errorf("V query: want 2 entities, got %d", cqV.EntityCount())
	}

	// New [P,V,Marker] entity — both queries must grow.
	type LocalM struct{}
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 3})
		flecs.Set(fw, e, Velocity{DX: 3})
		flecs.Set(fw, e, LocalM{})
	})

	if cqP.EntityCount() != 3 {
		t.Errorf("P query after new table: want 3 entities, got %d", cqP.EntityCount())
	}
	if cqV.EntityCount() != 3 {
		t.Errorf("V query after new table: want 3 entities, got %d", cqV.EntityCount())
	}
}

// ── Same entity set as uncached ───────────────────────────────────────────────

// TestCachedQuerySameAsUncached verifies that a CachedQuery and an uncached
// Query visit the same set of entities. Iteration order may differ: the
// uncached query seeds from compIndex (registration order), while the cached
// query's initial population walks w.tables (a map with non-deterministic
// iteration order). We compare entity SETS, not order.
func TestCachedQuerySameAsUncached(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	w.Write(func(fw *flecs.Writer) {
		for i := 0; i < 5; i++ {
			e := fw.NewEntity()
			flecs.Set(fw, e, Position{X: float32(i)})
		}
	})

	q := flecs.NewQuery(w, posID)
	var uncachedEntities []flecs.ID
	q.Each(func(it *flecs.QueryIter) {
		uncachedEntities = append(uncachedEntities, it.Entities()...)
	})

	cq := flecs.NewCachedQuery(w, posID)
	defer cq.Close()
	var cachedEntities []flecs.ID
	cq.Each(func(it *flecs.QueryIter) {
		cachedEntities = append(cachedEntities, it.Entities()...)
	})

	if len(uncachedEntities) != len(cachedEntities) {
		t.Fatalf("entity count mismatch: uncached=%d cached=%d",
			len(uncachedEntities), len(cachedEntities))
	}

	toSortedSlice := func(ids []flecs.ID) []flecs.ID {
		cp := append([]flecs.ID(nil), ids...)
		sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
		return cp
	}
	u := toSortedSlice(uncachedEntities)
	c := toSortedSlice(cachedEntities)
	for i := range u {
		if u[i] != c[i] {
			t.Errorf("entity mismatch at sorted index %d: uncached=%v cached=%v", i, u[i], c[i])
		}
	}
}

// ── CachedQuery with Defer ────────────────────────────────────────────────────

func TestCachedQueryWithDefer(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1})
	})

	cq := flecs.NewCachedQuery(w, posID, velID)
	defer cq.Close()

	if cq.Count() != 0 {
		t.Fatalf("before defer: want 0, got %d", cq.Count())
	}

	// Queue Set[V] inside a deferred block.
	flecs.DeferBeginForTest(w)
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, Velocity{DX: 1})
	})
	if cq.Count() != 0 {
		t.Fatalf("during defer: want 0, got %d", cq.Count())
	}
	flecs.DeferEndForTest(w) // flushes; e migrates to [P,V] → notifyTableCreated fires

	if cq.Count() != 1 {
		t.Fatalf("after DeferEnd: want 1, got %d", cq.Count())
	}
	if cq.EntityCount() != 1 {
		t.Fatalf("after DeferEnd: want 1 entity, got %d", cq.EntityCount())
	}
}

// ── Tag component in cache ────────────────────────────────────────────────────

func TestCachedQueryTag(t *testing.T) {
	w := flecs.New()
	type Tag struct{}
	tagID := flecs.RegisterComponent[Tag](w)
	posID := flecs.RegisterComponent[Position](w)

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Tag{})
		flecs.Set(fw, e, Position{X: 7})
	})

	cq := flecs.NewCachedQuery(w, tagID, posID)
	defer cq.Close()

	if cq.Count() != 1 {
		t.Fatalf("tag query: want Count()=1, got %d", cq.Count())
	}
	if cq.EntityCount() != 1 {
		t.Fatalf("tag query: want EntityCount()=1, got %d", cq.EntityCount())
	}
}

// ── Pair component in cache ───────────────────────────────────────────────────

func TestCachedQueryPairComponent(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)
	pairID := flecs.MakePair(posID, velID)

	cq := flecs.NewCachedQuery(w, pairID)
	defer cq.Close()

	if cq.Count() != 0 {
		t.Fatalf("before AddID: want 0, got %d", cq.Count())
	}

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.AddID(fw, e, pairID)
	})

	if cq.Count() != 1 {
		t.Fatalf("after AddID pair: want 1, got %d", cq.Count())
	}
}

// ── Field[T] over cached iter ─────────────────────────────────────────────────

func TestCachedQueryFieldT(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1, Y: 2})
		flecs.Set(fw, e, Velocity{DX: 3, DY: 4})
	})

	cq := flecs.NewCachedQuery(w, posID, velID)
	defer cq.Close()

	var gotPos []Position
	var gotVel []Velocity
	cq.Each(func(it *flecs.QueryIter) {
		gotPos = append(gotPos, flecs.Field[Position](it, posID)...)
		gotVel = append(gotVel, flecs.Field[Velocity](it, velID)...)
	})

	if len(gotPos) != 1 || gotPos[0] != (Position{X: 1, Y: 2}) {
		t.Errorf("position via Field: want [{1 2}], got %v", gotPos)
	}
	if len(gotVel) != 1 || gotVel[0] != (Velocity{DX: 3, DY: 4}) {
		t.Errorf("velocity via Field: want [{3 4}], got %v", gotVel)
	}
}

// ── Pruning of removed queries ────────────────────────────────────────────────

func TestCachedQueryPruning(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	// Create 3 cached queries.
	cq1 := flecs.NewCachedQuery(w, posID)
	cq2 := flecs.NewCachedQuery(w, posID)
	cq3 := flecs.NewCachedQuery(w, posID)

	if n := flecs.CachedQuerySliceLen(w); n != 3 {
		t.Fatalf("after 3 creates: want slice len 3, got %d", n)
	}

	// Close 2 of them.
	cq1.Close()
	cq2.Close()

	// Creating a 4th triggers amortized compaction: cq1+cq2 pruned, cq3+cq4 kept.
	cq4 := flecs.NewCachedQuery(w, posID)

	if n := flecs.CachedQuerySliceLen(w); n != 2 {
		t.Fatalf("after compaction: want slice len 2, got %d", n)
	}

	_ = cq3
	cq4.Close()
}

// ── Panic on nil world ────────────────────────────────────────────────────────

func TestCachedQueryPanicNilWorld(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil world")
		}
	}()
	flecs.NewCachedQuery(nil, 1)
}

// ── Panic on zero terms ───────────────────────────────────────────────────────

func TestCachedQueryPanicZeroTerms(t *testing.T) {
	w := flecs.New()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for zero terms")
		}
	}()
	flecs.NewCachedQuery(w)
}
