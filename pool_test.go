package flecs

// pool_test.go — concurrency, reset-correctness, and allocation-regression
// tests for the sync.Pool-backed seen-set used by traversal hot paths.
// This file is in package flecs (not flecs_test) so it can access idSeenPool.

import (
	"sync"
	"testing"
)

// TestSyncPool_ConcurrentGet verifies that N goroutines can simultaneously
// acquire and release maps from idSeenPool without data races or stale state.
// Run with -race to catch concurrent-access violations.
func TestSyncPool_ConcurrentGet(t *testing.T) {
	const goroutines = 50
	const iters = 500
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		g := g
		go func() {
			defer wg.Done()
			for i := range iters {
				m := idSeenPool.Get().(map[ID]struct{})
				if len(m) != 0 {
					t.Errorf("goroutine %d iter %d: pool returned dirty map (len=%d)", g, i, len(m))
				}
				// Simulate traversal: insert two IDs.
				m[ID(uint64(i))] = struct{}{}
				m[ID(uint64(i+1))] = struct{}{}
				// Mandatory reset before returning to pool.
				clear(m)
				idSeenPool.Put(m)
			}
		}()
	}
	wg.Wait()
}

// TestSyncPool_ResetClearsState verifies that clear(m) fully empties the map
// returned to the pool, so the next Get sees a zero-length map.
func TestSyncPool_ResetClearsState(t *testing.T) {
	// Drain and fill with known IDs.
	m := idSeenPool.Get().(map[ID]struct{})
	m[ID(1)] = struct{}{}
	m[ID(2)] = struct{}{}
	m[ID(3)] = struct{}{}
	if len(m) != 3 {
		t.Fatalf("expected 3 entries before reset, got %d", len(m))
	}
	// Reset.
	clear(m)
	if len(m) != 0 {
		t.Fatalf("expected 0 entries after clear(), got %d", len(m))
	}
	idSeenPool.Put(m)
	// Re-acquire — should be empty.
	m2 := idSeenPool.Get().(map[ID]struct{})
	if len(m2) != 0 {
		t.Errorf("re-acquired map has %d entries, want 0", len(m2))
	}
	clear(m2)
	idSeenPool.Put(m2)
}

// TestSyncPool_NoEscape documents the manual escape-analysis audit confirming
// that pooled maps are never returned to callers, sent over channels, or
// captured in closures that outlive the traversal call. No runtime assertion
// is possible; this test exists as a compile-time documentation checkpoint.
// Audit: walkUp acquires/releases within the function stack; hasViaIsAPooled,
// getViaIsAPooled, and getViaIsAByIDPooled acquire before delegation and
// release on return — all non-recursive top-level entry points.
func TestSyncPool_NoEscape(t *testing.T) {
	t.Log("escape audit: maps acquired from idSeenPool do not escape callsites — see pool.go and traversal.go")
}

// TestAllocations_Regression runs a representative steady-state workload
// (create 100 entities, query, mutate, query again, delete) and asserts
// total heap allocations per iteration remain below a documented threshold.
//
// Threshold: 500 allocs/op. Calibrated against the post-v0.104.0 baseline on
// the pool-optimized branch. Refresh by running:
//
//	go test -run TestAllocations_Regression -v
//
// and adjusting the constant if an intentional change alters the profile.
func TestAllocations_Regression(t *testing.T) {
	w := New()
	type Pos struct{ X, Y float64 }
	type Vel struct{ DX, DY float64 }
	posID := RegisterComponent[Pos](w)
	velID := RegisterComponent[Vel](w)

	entities := make([]ID, 100)
	cq := NewCachedQuery(w, posID, velID)

	workload := func() {
		w.Write(func(fw *Writer) {
			for i := range 100 {
				e := fw.NewEntity()
				Set(fw, e, Pos{float64(i), 0})
				Set(fw, e, Vel{1, 0})
				entities[i] = e
			}
		})
		cq.Each(func(it *QueryIter) { _ = it.Count() })
		w.Write(func(fw *Writer) {
			for _, e := range entities {
				Set(fw, e, Pos{2, 3})
			}
		})
		cq.Each(func(it *QueryIter) { _ = it.Count() })
		w.Write(func(fw *Writer) {
			for _, e := range entities {
				fw.Delete(e)
			}
		})
	}

	// Warm up: first run creates tables and grows entity pages.
	workload()

	const threshold = 500.0
	allocs := testing.AllocsPerRun(10, workload)
	if allocs > threshold {
		t.Errorf("regression: %.0f allocs/workload exceeds threshold %.0f — refresh threshold if intentional", allocs, threshold)
	}
}

// BenchmarkWalkUp_AllocCount measures allocations on the walkUp hot path
// with a 2-level ChildOf chain. Expect 0 allocs/op after sync.Pool pooling.
func BenchmarkWalkUp_AllocCount(b *testing.B) {
	b.ReportAllocs()
	w := New()
	var grandparent, parent, child ID
	w.Write(func(fw *Writer) {
		type Marker struct{}
		RegisterComponent[Marker](w)
		grandparent = fw.NewEntity()
		Set(fw, grandparent, Marker{})
		parent = fw.NewEntity()
		AddID(fw, parent, MakePair(w.ChildOf(), grandparent))
		child = fw.NewEntity()
		AddID(fw, child, MakePair(w.ChildOf(), parent))
	})
	markerID := RegisterComponent[struct{}](w)
	b.ResetTimer()
	for range b.N {
		w.Read(func(r *Reader) {
			_ = HasUp(r, child, markerID, w.ChildOf())
		})
	}
}

// BenchmarkHasViaIsA_AllocCount measures allocations through the hasViaIsAPooled
// path (called by HasID on IsA chains). Expect 0 allocs/op after pooling.
func BenchmarkHasViaIsA_AllocCount(b *testing.B) {
	b.ReportAllocs()
	w := New()
	type Marker struct{}
	markerID := RegisterComponent[Marker](w)
	var prefab, inst ID
	w.Write(func(fw *Writer) {
		prefab = fw.NewEntity()
		Set(fw, prefab, Marker{})
		inst = fw.NewEntity()
		AddID(fw, inst, MakePair(w.IsA(), prefab))
	})
	b.ResetTimer()
	for range b.N {
		w.Read(func(r *Reader) {
			_ = HasID(r, inst, markerID)
		})
	}
}

// BenchmarkGetViaIsA_AllocCount measures allocations through the getViaIsAPooled
// path (called by Get on IsA chains). Expect 0 allocs/op after pooling.
func BenchmarkGetViaIsA_AllocCount(b *testing.B) {
	b.ReportAllocs()
	w := New()
	type Pos struct{ X, Y float64 }
	var prefab, inst ID
	w.Write(func(fw *Writer) {
		prefab = fw.NewEntity()
		Set(fw, prefab, Pos{1, 2})
		inst = fw.NewEntity()
		AddID(fw, inst, MakePair(w.IsA(), prefab))
	})
	b.ResetTimer()
	for range b.N {
		w.Read(func(r *Reader) {
			_, _ = Get[Pos](r, inst)
		})
	}
}
