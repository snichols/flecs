package flecs_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/snichols/flecs"
)

// --- component types local to run_worker tests ---

type wkIdx struct{ I int }      // index-bearing component for partition tracking
type wkDFSparse struct{ V int } // DontFragment+Sparse component for test 10

// --- Test 1: 4 workers / 100 entities — total processed = 100, disjoint sets ---

func TestRunSystemWorker_100Entities4Workers(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[wkIdx](w)
	q := flecs.NewCachedQuery(w, posID)
	const n = 100
	w.Write(func(fw *flecs.Writer) {
		for i := range n {
			e := fw.NewEntity()
			flecs.Set(fw, e, wkIdx{I: i})
		}
	})
	var counts [n]atomic.Int32
	sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			for _, p := range flecs.Field[wkIdx](it, posID) {
				counts[p.I].Add(1)
			}
		}
	})
	for i := range 4 {
		flecs.RunSystemWorker(w, sys, i, 4, 0)
	}
	for i := range n {
		if c := counts[i].Load(); c != 1 {
			t.Errorf("entity index %d: processed %d times, want 1", i, c)
		}
	}
}

// --- Test 2: 4 workers / 7 entities (uneven) — each gets q or q+1 rows, total = 7 ---

func TestRunSystemWorker_7Entities4Workers_Uneven(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[wkIdx](w)
	q := flecs.NewCachedQuery(w, posID)
	const n = 7
	w.Write(func(fw *flecs.Writer) {
		for i := range n {
			e := fw.NewEntity()
			flecs.Set(fw, e, wkIdx{I: i})
		}
	})
	var counts [n]atomic.Int32
	sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			for _, p := range flecs.Field[wkIdx](it, posID) {
				counts[p.I].Add(1)
			}
		}
	})
	// Expected per-worker sizes: q=1, r=3 → workers 0..2 get 2 rows, worker 3 gets 1.
	for i := range 4 {
		flecs.RunSystemWorker(w, sys, i, 4, 0)
	}
	total := int32(0)
	for i := range n {
		c := counts[i].Load()
		total += c
		if c != 1 {
			t.Errorf("entity index %d: processed %d times, want 1", i, c)
		}
	}
	if total != n {
		t.Errorf("total entities processed %d, want %d", total, n)
	}
}

// --- Test 3: 1 worker / N entities — equivalent to RunSystem ---

func TestRunSystemWorker_SingleWorkerEquivalentToRunSystem(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[wkIdx](w)
	q := flecs.NewCachedQuery(w, posID)
	const n = 50
	w.Write(func(fw *flecs.Writer) {
		for i := range n {
			e := fw.NewEntity()
			flecs.Set(fw, e, wkIdx{I: i})
		}
	})
	var count atomic.Int32
	sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			count.Add(int32(it.Count()))
		}
	})
	flecs.RunSystemWorker(w, sys, 0, 1, 0)
	if got := count.Load(); got != n {
		t.Errorf("single-worker count %d, want %d", got, n)
	}
}

// --- Test 4: workerCount == 0 panics ---

func TestRunSystemWorker_ZeroWorkerCountPanics(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[wkIdx](w)
	q := flecs.NewCachedQuery(w, posID)
	sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {})
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for workerCount == 0, got none")
		}
	}()
	flecs.RunSystemWorker(w, sys, 0, 0, 0)
}

// --- Test 5: workerIndex out of range panics ---

func TestRunSystemWorker_WorkerIndexOutOfRangePanics(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[wkIdx](w)
	q := flecs.NewCachedQuery(w, posID)
	sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {})

	assertPanic := func(name string, fn func()) {
		t.Helper()
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("%s: expected panic, got none", name)
			}
		}()
		fn()
	}
	assertPanic("workerIndex >= workerCount", func() {
		flecs.RunSystemWorker(w, sys, 4, 4, 0)
	})
	assertPanic("workerIndex < 0", func() {
		flecs.RunSystemWorker(w, sys, -1, 4, 0)
	})
}

// --- Test 6: concurrent goroutines — race-detector clean, total matches, no entity twice ---

func TestRunSystemWorker_ConcurrentRaceDetectorClean(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[wkIdx](w)
	q := flecs.NewCachedQuery(w, posID)
	const n = 100
	w.Write(func(fw *flecs.Writer) {
		for i := range n {
			e := fw.NewEntity()
			flecs.Set(fw, e, wkIdx{I: i})
		}
	})
	var counts [n]atomic.Int32
	sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		// Read-only: increment per-entity counter atomically. No deferred mutations
		// so the flush is a no-op and concurrent flushes cannot race.
		for it.Next() {
			for _, p := range flecs.Field[wkIdx](it, posID) {
				counts[p.I].Add(1)
			}
		}
	})
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			flecs.RunSystemWorker(w, sys, idx, 4, 0)
		}(i)
	}
	wg.Wait()
	total := int32(0)
	for i := range n {
		c := counts[i].Load()
		total += c
		if c != 1 {
			t.Errorf("entity index %d: processed %d times, want 1", i, c)
		}
	}
	if total != n {
		t.Errorf("total %d, want %d", total, n)
	}
}

// --- Test 7: deferred mutations flushed per worker call ---

func TestRunSystemWorker_DeferredMutationsFlushedPerCall(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[wkIdx](w)
	q := flecs.NewCachedQuery(w, posID)
	const n = 20
	entities := make([]flecs.ID, n)
	w.Write(func(fw *flecs.Writer) {
		for i := range n {
			e := fw.NewEntity()
			flecs.Set(fw, e, wkIdx{I: i})
			entities[i] = e
		}
	})
	// Each worker sets wkIdx.I to -1 for its entities via deferred Set.
	// Mutations are flushed before RunSystemWorker returns, so each subsequent
	// worker reads the world state as updated by all prior workers.
	sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		fw := it.Writer()
		for it.Next() {
			for _, e := range it.Entities() {
				flecs.Set(fw, e, wkIdx{I: -1})
			}
		}
	})
	for i := range 4 {
		flecs.RunSystemWorker(w, sys, i, 4, 0)
	}
	w.Read(func(r *flecs.Reader) {
		for _, e := range entities {
			v, ok := flecs.Get[wkIdx](r, e)
			if !ok || v.I != -1 {
				val := -99
				if ok {
					val = v.I
				}
				t.Errorf("entity %v: wkIdx.I = %d, want -1", e, val)
			}
		}
	})
}

// --- Test 8: empty system (no matched entities) — all workers run, no panic ---

func TestRunSystemWorker_EmptySystem(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[wkIdx](w)
	q := flecs.NewCachedQuery(w, posID)
	// No entities created — query matches nothing.
	var callCount int
	sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		callCount++
		for it.Next() {
			t.Error("unexpected entity match in empty system")
		}
	})
	for i := range 4 {
		flecs.RunSystemWorker(w, sys, i, 4, 0)
	}
	if callCount != 4 {
		t.Errorf("callback called %d times, want 4", callCount)
	}
}

// --- Test 9: disabled system + RunSystemWorker still runs (parity with RunSystem) ---

func TestRunSystemWorker_DisabledSystemStillRuns(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[wkIdx](w)
	q := flecs.NewCachedQuery(w, posID)
	const n = 10
	w.Write(func(fw *flecs.Writer) {
		for i := range n {
			e := fw.NewEntity()
			flecs.Set(fw, e, wkIdx{I: i})
		}
	})
	var count atomic.Int32
	sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			count.Add(int32(it.Count()))
		}
	})
	sys.SetEnabled(false)
	// Progress skips disabled systems; RunSystemWorker bypasses the enabled flag.
	w.Progress(0)
	if count.Load() != 0 {
		t.Error("Progress ran disabled system, want 0 invocations")
	}
	flecs.RunSystemWorker(w, sys, 0, 1, 0)
	if got := count.Load(); got != n {
		t.Errorf("RunSystemWorker on disabled system: count %d, want %d", got, n)
	}
}

// --- Test 10: sparse-term query partition semantics ---
//
// For pure-sparse (DontFragment) queries, the per-table worker clipping path is
// bypassed: nextSparseOnly iterates the sparse driver without consulting
// workerTotal. Consequently all workers see all matching entities — no
// entity-range partitioning occurs. This is the documented semantics:
// RunSystemWorker provides no partition guarantee for sparse-only queries.
// Callers who need sparse partitioning must coordinate assignment externally.

func TestRunSystemWorker_SparseTermPartitionSemantics(t *testing.T) {
	w := flecs.New()
	dfID := flecs.RegisterComponent[wkDFSparse](w)
	flecs.SetSparse(w, dfID)
	flecs.SetDontFragment(w, dfID)
	q := flecs.NewCachedQuery(w, dfID)
	const n = 8
	w.Write(func(fw *flecs.Writer) {
		for i := range n {
			e := fw.NewEntity()
			flecs.Set(fw, e, wkDFSparse{V: i})
		}
	})
	var total atomic.Int32
	sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			total.Add(1)
		}
	})
	const workers = 4
	for i := range workers {
		flecs.RunSystemWorker(w, sys, i, workers, 0)
	}
	// All workers iterate all n entities → total = workers * n.
	// This documents that pure-sparse queries are not partitioned.
	want := int32(workers * n)
	if got := total.Load(); got != want {
		t.Errorf("sparse total %d, want %d (all %d workers × %d entities, no partitioning)",
			got, want, workers, n)
	}
}
