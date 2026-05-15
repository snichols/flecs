package flecs_test

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snichols/flecs"
)

type mtCounter struct{ V int64 }

// TestMultiThreadedSystemProcessesEachEntityOnce verifies that a multi-threaded
// system increments every entity's counter exactly once regardless of worker count.
func TestMultiThreadedSystemProcessesEachEntityOnce(t *testing.T) {
	for _, wc := range []int{1, 2, 4, 8} {
		wc := wc
		t.Run(fmt.Sprintf("workers=%d", wc), func(t *testing.T) {
			t.Parallel()
			w := flecs.New()
			w.SetWorkerCount(wc)
			cntID := flecs.RegisterComponent[mtCounter](w)
			const n = 100_000
			w.Write(func(fw *flecs.Writer) {
				for range n {
					e := fw.NewEntity()
					flecs.Set(fw, e, mtCounter{V: 0})
				}
			})
			cq := flecs.NewCachedQuery(w, cntID)
			sys := flecs.NewSystem(w, cq, func(_ float32, it *flecs.QueryIter) {
				for it.Next() {
					counters := flecs.Field[mtCounter](it, cntID)
					for i := range counters {
						counters[i].V++
					}
				}
			})
			sys.SetMultiThreaded(true)
			w.Progress(0)

			var sum int64
			it := cq.Iter()
			for it.Next() {
				cols := flecs.Field[mtCounter](it, cntID)
				for _, c := range cols {
					sum += c.V
				}
			}
			if sum != n {
				t.Fatalf("expected sum=%d, got %d (each entity should be incremented exactly once)", n, sum)
			}
		})
	}
}

// TestMultiThreadedSystemCannotBatchWithSiblings verifies that a multi-threaded
// system and a parallel sibling run serially: the dispatcher waits for all MT
// workers to finish before dispatching the next system.
func TestMultiThreadedSystemCannotBatchWithSiblings(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(2)
	posID := flecs.RegisterComponent[parallelPos](w)

	w.Write(func(fw *flecs.Writer) {
		for range 100 {
			e := fw.NewEntity()
			flecs.Set(fw, e, parallelPos{})
		}
	})

	var mu sync.Mutex
	var mtRunnerCount int
	var sibSawMTRunning bool

	cqMT := flecs.NewCachedQuery(w, posID)
	sysMT := flecs.NewSystem(w, cqMT, func(_ float32, _ *flecs.QueryIter) {
		mu.Lock()
		mtRunnerCount++
		mu.Unlock()
		time.Sleep(30 * time.Millisecond) // hold window long enough for sibling to observe
		mu.Lock()
		mtRunnerCount--
		mu.Unlock()
	})
	sysMT.SetMultiThreaded(true)

	cqSib := flecs.NewCachedQuery(w, posID)
	sysSib := flecs.NewSystem(w, cqSib, func(_ float32, _ *flecs.QueryIter) {
		mu.Lock()
		if mtRunnerCount > 0 {
			sibSawMTRunning = true
		}
		mu.Unlock()
	})
	sysSib.SetParallel(true)
	sysSib.SetWriteSet([]flecs.ID{}) // read-only: would normally batch with any sibling

	w.Progress(0)

	if sibSawMTRunning {
		t.Fatal("parallel sibling ran while multi-threaded system workers were still active")
	}
}

// TestMultiThreadedSystemUnevenSplit verifies the C-flecs remainder-distribution
// formula: 1000 rows / 3 workers → worker 0 gets 334, workers 1-2 get 333.
func TestMultiThreadedSystemUnevenSplit(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(3)
	posID := flecs.RegisterComponent[parallelPos](w)

	const n = 1000
	w.Write(func(fw *flecs.Writer) {
		for range n {
			e := fw.NewEntity()
			flecs.Set(fw, e, parallelPos{})
		}
	})

	var mu sync.Mutex
	var counts []int

	cq := flecs.NewCachedQuery(w, posID)
	sys := flecs.NewSystem(w, cq, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			mu.Lock()
			counts = append(counts, it.Count())
			mu.Unlock()
		}
	})
	sys.SetMultiThreaded(true)

	w.Progress(0)

	if len(counts) != 3 {
		t.Fatalf("expected 3 worker entries (one per non-empty worker), got %d", len(counts))
	}
	total := 0
	for _, c := range counts {
		total += c
	}
	if total != n {
		t.Fatalf("expected total rows=%d, got %d", n, total)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(counts)))
	if counts[0] != 334 || counts[1] != 333 || counts[2] != 333 {
		t.Fatalf("expected counts [334, 333, 333] (sorted desc), got %v", counts)
	}
}

// TestMultiThreadedSystemEmptyWorkers verifies that workers with no rows skip
// silently: 2 rows / 4 workers → workers 0-1 each get 1 row, workers 2-3 skip.
func TestMultiThreadedSystemEmptyWorkers(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(4)
	posID := flecs.RegisterComponent[parallelPos](w)

	w.Write(func(fw *flecs.Writer) {
		for range 2 {
			e := fw.NewEntity()
			flecs.Set(fw, e, parallelPos{})
		}
	})

	var mu sync.Mutex
	var counts []int

	cq := flecs.NewCachedQuery(w, posID)
	sys := flecs.NewSystem(w, cq, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			mu.Lock()
			counts = append(counts, it.Count())
			mu.Unlock()
		}
	})
	sys.SetMultiThreaded(true)

	w.Progress(0)

	// Workers 0 and 1 each see 1 row; workers 2 and 3 skip (it.Next() returns false).
	if len(counts) != 2 {
		t.Fatalf("expected 2 non-empty workers, got %d (counts=%v)", len(counts), counts)
	}
	for i, c := range counts {
		if c != 1 {
			t.Fatalf("worker entry %d: expected count=1, got %d", i, c)
		}
	}
}

// TestMultiThreadedSystemWithDeferredMutations verifies that workers calling
// it.Writer().Delete from inside the iter loop route mutations to their own
// per-stage queue (no contention) and all deletes are applied correctly after
// the stage-merge at wg.Wait.
func TestMultiThreadedSystemWithDeferredMutations(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(4)
	posID := flecs.RegisterComponent[parallelPos](w)

	const n = 200
	entities := make([]flecs.ID, n)
	w.Write(func(fw *flecs.Writer) {
		for i := range n {
			e := fw.NewEntity()
			flecs.Set(fw, e, parallelPos{X: float32(i)})
			entities[i] = e
		}
	})

	cq := flecs.NewCachedQuery(w, posID)
	sys := flecs.NewSystem(w, cq, func(_ float32, it *flecs.QueryIter) {
		fw := it.Writer()
		for it.Next() {
			for _, e := range it.Entities() {
				fw.Delete(e) // routes to this worker's per-stage queue
			}
		}
	})
	sys.SetMultiThreaded(true)

	w.Progress(0)

	// All entities must be dead after per-stage queues are merged.
	for _, e := range entities {
		if w.IsAlive(e) {
			t.Fatalf("entity %v should be dead after deferred Delete from multi-threaded worker", e)
		}
	}
}

// --- Phase 16.50: per-stage parallel-batch tests ---

// parallelTag types for per-stage tests (distinct from types used above).
type psTagA struct{}
type psTagB struct{}
type psTagC struct{}
type psTagD struct{}

// TestParallelSystems_DeferredMutationsCorrect verifies that N parallel systems
// each enqueue distinct structural mutations (Delete) and all are applied after
// Progress with per-stage routing.
func TestParallelSystems_DeferredMutationsCorrect(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(4)

	aID := flecs.RegisterComponent[psTagA](w)
	bID := flecs.RegisterComponent[psTagB](w)
	cID := flecs.RegisterComponent[psTagC](w)
	dID := flecs.RegisterComponent[psTagD](w)

	const n = 50
	var asEnt, bsEnt, csEnt, dsEnt []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		for range n {
			e := fw.NewEntity()
			flecs.AddID(fw, e, aID)
			asEnt = append(asEnt, e)
		}
		for range n {
			e := fw.NewEntity()
			flecs.AddID(fw, e, bID)
			bsEnt = append(bsEnt, e)
		}
		for range n {
			e := fw.NewEntity()
			flecs.AddID(fw, e, cID)
			csEnt = append(csEnt, e)
		}
		for range n {
			e := fw.NewEntity()
			flecs.AddID(fw, e, dID)
			dsEnt = append(dsEnt, e)
		}
	})

	makeDeleter := func(ids []flecs.ID, cq *flecs.CachedQuery) func(float32, *flecs.QueryIter) {
		return func(_ float32, it *flecs.QueryIter) {
			fw := it.Writer()
			for _, e := range ids {
				fw.Delete(e)
			}
		}
	}

	cqA := flecs.NewCachedQuery(w, aID)
	cqB := flecs.NewCachedQuery(w, bID)
	cqC := flecs.NewCachedQuery(w, cID)
	cqD := flecs.NewCachedQuery(w, dID)

	sA := flecs.NewSystem(w, cqA, makeDeleter(asEnt, cqA))
	sA.SetParallel(true)
	sA.SetWriteSet([]flecs.ID{aID})

	sB := flecs.NewSystem(w, cqB, makeDeleter(bsEnt, cqB))
	sB.SetParallel(true)
	sB.SetWriteSet([]flecs.ID{bID})

	sC := flecs.NewSystem(w, cqC, makeDeleter(csEnt, cqC))
	sC.SetParallel(true)
	sC.SetWriteSet([]flecs.ID{cID})

	sD := flecs.NewSystem(w, cqD, makeDeleter(dsEnt, cqD))
	sD.SetParallel(true)
	sD.SetWriteSet([]flecs.ID{dID})

	w.Progress(0)

	for i, e := range asEnt {
		if w.IsAlive(e) {
			t.Fatalf("asEnt[%d] still alive after parallel Delete", i)
		}
	}
	for i, e := range bsEnt {
		if w.IsAlive(e) {
			t.Fatalf("bsEnt[%d] still alive after parallel Delete", i)
		}
	}
	for i, e := range csEnt {
		if w.IsAlive(e) {
			t.Fatalf("csEnt[%d] still alive after parallel Delete", i)
		}
	}
	for i, e := range dsEnt {
		if w.IsAlive(e) {
			t.Fatalf("dsEnt[%d] still alive after parallel Delete", i)
		}
	}
}

// TestParallelSystems_NoDataRace exercises the per-stage routing under the race
// detector. Run with go test -race -count=10 to catch concurrent access.
// The test creates 4 parallel systems that all perform deferred mutations;
// with the old shared-stage path this triggered concurrent map/slice writes.
func TestParallelSystems_NoDataRace(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(4)

	aID := flecs.RegisterComponent[psTagA](w)
	bID := flecs.RegisterComponent[psTagB](w)
	cID := flecs.RegisterComponent[psTagC](w)
	dID := flecs.RegisterComponent[psTagD](w)

	const n = 100
	w.Write(func(fw *flecs.Writer) {
		for range n {
			e := fw.NewEntity()
			flecs.AddID(fw, e, aID)
		}
		for range n {
			e := fw.NewEntity()
			flecs.AddID(fw, e, bID)
		}
		for range n {
			e := fw.NewEntity()
			flecs.AddID(fw, e, cID)
		}
		for range n {
			e := fw.NewEntity()
			flecs.AddID(fw, e, dID)
		}
	})

	mutate := func(otherID flecs.ID) func(float32, *flecs.QueryIter) {
		return func(_ float32, it *flecs.QueryIter) {
			fw := it.Writer()
			for it.Next() {
				for _, e := range it.Entities() {
					fw.AddID(e, otherID)
				}
			}
		}
	}

	cqA := flecs.NewCachedQuery(w, aID)
	cqB := flecs.NewCachedQuery(w, bID)
	cqC := flecs.NewCachedQuery(w, cID)
	cqD := flecs.NewCachedQuery(w, dID)

	// Each system adds a tag from its own ID namespace; write sets are disjoint.
	sA := flecs.NewSystem(w, cqA, mutate(bID))
	sA.SetParallel(true)
	sA.SetWriteSet([]flecs.ID{aID})

	sB := flecs.NewSystem(w, cqB, mutate(cID))
	sB.SetParallel(true)
	sB.SetWriteSet([]flecs.ID{bID})

	sC := flecs.NewSystem(w, cqC, mutate(dID))
	sC.SetParallel(true)
	sC.SetWriteSet([]flecs.ID{cID})

	sD := flecs.NewSystem(w, cqD, mutate(aID))
	sD.SetParallel(true)
	sD.SetWriteSet([]flecs.ID{dID})

	// Run multiple ticks; the race detector catches concurrent stage-0 access.
	for range 5 {
		w.Progress(0)
	}
}

// TestParallelSystems_CoalescingPreserved verifies that within a parallel
// system's stage, per-entity FIFO coalescing still folds repeated AddID
// commands into a single archetype migration.
func TestParallelSystems_CoalescingPreserved(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(2)

	aID := flecs.RegisterComponent[psTagA](w)
	bID := flecs.RegisterComponent[psTagB](w)

	const n = 10
	entities := make([]flecs.ID, n)
	w.Write(func(fw *flecs.Writer) {
		for i := range n {
			e := fw.NewEntity()
			flecs.AddID(fw, e, aID)
			entities[i] = e
		}
	})

	cqA := flecs.NewCachedQuery(w, aID)
	sys := flecs.NewSystem(w, cqA, func(_ float32, it *flecs.QueryIter) {
		fw := it.Writer()
		for _, e := range entities {
			// Queue 20 AddID(bID) commands for the same entity — coalescer
			// must fold them into a single archetype migration.
			for range 20 {
				fw.AddID(e, bID)
			}
		}
	})
	sys.SetParallel(true)
	sys.SetWriteSet([]flecs.ID{aID})

	w.Progress(0)

	// After coalescing, every entity should have both aID and bID.
	for i, e := range entities {
		if !w.IsAlive(e) {
			t.Fatalf("entities[%d] not alive after Progress", i)
		}
		w.Read(func(r *flecs.Reader) {
			if !flecs.HasID(r, e, bID) {
				t.Errorf("entities[%d] missing bID after deferred AddID coalescing", i)
			}
		})
	}
}

// TestParallelSystems_BatchLargerThanWorkerCount verifies that when a parallel
// batch has more systems than workers, stage slots wrap around (modulo) and all
// mutations are still applied correctly.
func TestParallelSystems_BatchLargerThanWorkerCount(t *testing.T) {
	// 2 workers, 4 parallel systems — stages 1 and 2 each serve 2 systems serially.
	w := flecs.New()
	w.SetWorkerCount(2)

	aID := flecs.RegisterComponent[psTagA](w)
	bID := flecs.RegisterComponent[psTagB](w)
	cID := flecs.RegisterComponent[psTagC](w)
	dID := flecs.RegisterComponent[psTagD](w)

	const n = 20
	var as, bs, cs, ds []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		for range n {
			e := fw.NewEntity()
			flecs.AddID(fw, e, aID)
			as = append(as, e)
		}
		for range n {
			e := fw.NewEntity()
			flecs.AddID(fw, e, bID)
			bs = append(bs, e)
		}
		for range n {
			e := fw.NewEntity()
			flecs.AddID(fw, e, cID)
			cs = append(cs, e)
		}
		for range n {
			e := fw.NewEntity()
			flecs.AddID(fw, e, dID)
			ds = append(ds, e)
		}
	})

	del := func(ents []flecs.ID) func(float32, *flecs.QueryIter) {
		return func(_ float32, it *flecs.QueryIter) {
			fw := it.Writer()
			for _, e := range ents {
				fw.Delete(e)
			}
		}
	}

	cqA := flecs.NewCachedQuery(w, aID)
	cqB := flecs.NewCachedQuery(w, bID)
	cqC := flecs.NewCachedQuery(w, cID)
	cqD := flecs.NewCachedQuery(w, dID)

	sA := flecs.NewSystem(w, cqA, del(as))
	sA.SetParallel(true)
	sA.SetWriteSet([]flecs.ID{aID})

	sB := flecs.NewSystem(w, cqB, del(bs))
	sB.SetParallel(true)
	sB.SetWriteSet([]flecs.ID{bID})

	sC := flecs.NewSystem(w, cqC, del(cs))
	sC.SetParallel(true)
	sC.SetWriteSet([]flecs.ID{cID})

	sD := flecs.NewSystem(w, cqD, del(ds))
	sD.SetParallel(true)
	sD.SetWriteSet([]flecs.ID{dID})

	w.Progress(0)

	allEnts := append(append(append(as, bs...), cs...), ds...)
	for i, e := range allEnts {
		if w.IsAlive(e) {
			t.Fatalf("entity[%d] still alive after parallel Delete (batch > worker count)", i)
		}
	}
}

// TestParallelSystems_OnPreMergeFiresOnce verifies that a pre-merge hook fires
// exactly once per parallel batch merge boundary (not once per worker stage).
//
// With 4 workers and 2 parallel systems in OnUpdate, the expected fire count is:
//   - 1 from mergeWorkerStages (batch merge in OnUpdate)
//   - 3 from deferScope end-of-phase (PreUpdate + OnUpdate + PostUpdate)
//   - Total: 4
//
// A wrong implementation that fired once per worker stage (N=4) would produce 7.
// Testing for exactly 4 verifies "once per batch boundary."
func TestParallelSystems_OnPreMergeFiresOnce(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(4)

	aID := flecs.RegisterComponent[psTagA](w)
	bID := flecs.RegisterComponent[psTagB](w)

	w.Write(func(fw *flecs.Writer) {
		for range 10 {
			e := fw.NewEntity()
			flecs.AddID(fw, e, aID)
		}
		for range 10 {
			e := fw.NewEntity()
			flecs.AddID(fw, e, bID)
		}
	})

	var preFires atomic.Int32
	flecs.OnPreMerge(w, func(_ *flecs.Writer) {
		preFires.Add(1)
	})

	cqA := flecs.NewCachedQuery(w, aID)
	sA := flecs.NewSystem(w, cqA, func(_ float32, _ *flecs.QueryIter) {})
	sA.SetParallel(true)
	sA.SetWriteSet([]flecs.ID{aID})

	cqB := flecs.NewCachedQuery(w, bID)
	sB := flecs.NewSystem(w, cqB, func(_ float32, _ *flecs.QueryIter) {})
	sB.SetParallel(true)
	sB.SetWriteSet([]flecs.ID{bID})

	w.Progress(0)

	// 3 phases run (PreUpdate, OnUpdate, PostUpdate; OnFixedUpdate skipped),
	// each ending with a deferScope flush = 3 fires. Plus 1 from mergeWorkerStages
	// for the 2-system batch = 4 total. A wrong per-stage impl (4 stages) = 7.
	const want = 4
	if got := preFires.Load(); got != want {
		t.Fatalf("OnPreMerge fired %d times, want %d (once per batch boundary + once per phase-end)", got, want)
	}
}

// TestParallelSystems_OnPostMergeFiresOnce verifies that a post-merge hook fires
// exactly once per parallel batch merge boundary (not once per worker stage).
// See TestParallelSystems_OnPreMergeFiresOnce for the expected-count rationale.
func TestParallelSystems_OnPostMergeFiresOnce(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(4)

	aID := flecs.RegisterComponent[psTagA](w)
	bID := flecs.RegisterComponent[psTagB](w)

	w.Write(func(fw *flecs.Writer) {
		for range 10 {
			e := fw.NewEntity()
			flecs.AddID(fw, e, aID)
		}
		for range 10 {
			e := fw.NewEntity()
			flecs.AddID(fw, e, bID)
		}
	})

	var postFires atomic.Int32
	flecs.OnPostMerge(w, func(_ *flecs.Writer) {
		postFires.Add(1)
	})

	cqA := flecs.NewCachedQuery(w, aID)
	sA := flecs.NewSystem(w, cqA, func(_ float32, _ *flecs.QueryIter) {})
	sA.SetParallel(true)
	sA.SetWriteSet([]flecs.ID{aID})

	cqB := flecs.NewCachedQuery(w, bID)
	sB := flecs.NewSystem(w, cqB, func(_ float32, _ *flecs.QueryIter) {})
	sB.SetParallel(true)
	sB.SetWriteSet([]flecs.ID{bID})

	w.Progress(0)

	const want = 4 // 3 phase-end deferScope flushes + 1 batch mergeWorkerStages
	if got := postFires.Load(); got != want {
		t.Fatalf("OnPostMerge fired %d times, want %d (once per batch boundary + once per phase-end)", got, want)
	}
}

// TestParallelSystems_PhaseBoundaries verifies that parallel systems in different
// phases are dispatched correctly and that merge happens at each phase boundary.
func TestParallelSystems_PhaseBoundaries(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(2)

	aID := flecs.RegisterComponent[psTagA](w)
	bID := flecs.RegisterComponent[psTagB](w)

	var aEnts, bEnts []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		for range 10 {
			e := fw.NewEntity()
			flecs.AddID(fw, e, aID)
			aEnts = append(aEnts, e)
		}
		for range 10 {
			e := fw.NewEntity()
			flecs.AddID(fw, e, bID)
			bEnts = append(bEnts, e)
		}
	})

	// System in PreUpdate phase.
	prePhase := w.PreUpdate()
	cqA := flecs.NewCachedQuery(w, aID)
	sA := flecs.NewSystemInPhase(w, prePhase, cqA, func(_ float32, it *flecs.QueryIter) {
		fw := it.Writer()
		for _, e := range aEnts {
			fw.Delete(e)
		}
	})
	sA.SetParallel(true)
	sA.SetWriteSet([]flecs.ID{aID})

	// System in OnUpdate phase (different phase from sA).
	onUpdatePhase := w.OnUpdate()
	cqB := flecs.NewCachedQuery(w, bID)
	sB := flecs.NewSystemInPhase(w, onUpdatePhase, cqB, func(_ float32, it *flecs.QueryIter) {
		fw := it.Writer()
		for _, e := range bEnts {
			fw.Delete(e)
		}
	})
	sB.SetParallel(true)
	sB.SetWriteSet([]flecs.ID{bID})

	w.Progress(0)

	for i, e := range aEnts {
		if w.IsAlive(e) {
			t.Fatalf("aEnts[%d] still alive after PreUpdate parallel Delete", i)
		}
	}
	for i, e := range bEnts {
		if w.IsAlive(e) {
			t.Fatalf("bEnts[%d] still alive after OnUpdate parallel Delete", i)
		}
	}
}

// TestParallelSystems_WithSerialSystem verifies that a serial system following a
// parallel batch observes the merged state from the batch's deferred mutations.
// The batch must have 2+ parallel systems so mergeWorkerStages fires before the
// serial system runs (single-system "batches" run without a mid-phase merge).
func TestParallelSystems_WithSerialSystem(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(2)

	aID := flecs.RegisterComponent[psTagA](w)
	bID := flecs.RegisterComponent[psTagB](w)
	cID := flecs.RegisterComponent[psTagC](w)

	const n = 20
	aEnts := make([]flecs.ID, n)
	cEnts := make([]flecs.ID, n)
	w.Write(func(fw *flecs.Writer) {
		for i := range n {
			e := fw.NewEntity()
			flecs.AddID(fw, e, aID)
			aEnts[i] = e
		}
		for i := range n {
			e := fw.NewEntity()
			flecs.AddID(fw, e, cID)
			cEnts[i] = e
		}
	})

	// Parallel system A: adds bID to all aID entities via deferred writer.
	cqA := flecs.NewCachedQuery(w, aID)
	sA := flecs.NewSystem(w, cqA, func(_ float32, it *flecs.QueryIter) {
		fw := it.Writer()
		for _, e := range aEnts {
			fw.AddID(e, bID)
		}
	})
	sA.SetParallel(true)
	sA.SetWriteSet([]flecs.ID{aID})

	// Parallel system C: no-op, just needed to form a 2-system batch so
	// mergeWorkerStages fires before the serial system below.
	cqC := flecs.NewCachedQuery(w, cID)
	sC := flecs.NewSystem(w, cqC, func(_ float32, _ *flecs.QueryIter) {})
	sC.SetParallel(true)
	sC.SetWriteSet([]flecs.ID{cID})

	// Serial system: runs after the batch is merged; must see bID entities.
	var serialSawCount int
	cqB := flecs.NewCachedQuery(w, bID)
	sSerial := flecs.NewSystem(w, cqB, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			serialSawCount += it.Count()
		}
	})
	_ = sSerial

	w.Progress(0)

	if serialSawCount != n {
		t.Fatalf("serial system saw %d entities with bID, want %d (parallel batch merge must precede serial execution)", serialSawCount, n)
	}
}

// TestParallelSystems_UserWriteFromOutsideSystem verifies that world.Write(fn)
// from the main goroutine works correctly between Progress calls when parallel
// systems are registered.
func TestParallelSystems_UserWriteFromOutsideSystem(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(2)

	aID := flecs.RegisterComponent[psTagA](w)
	bID := flecs.RegisterComponent[psTagB](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.AddID(fw, e, aID)
	})

	cqA := flecs.NewCachedQuery(w, aID)
	sA := flecs.NewSystem(w, cqA, func(_ float32, _ *flecs.QueryIter) {})
	sA.SetParallel(true)
	sA.SetWriteSet([]flecs.ID{aID})

	w.Progress(0)

	// Write from main goroutine between Progress calls — must succeed.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, bID)
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.HasID(r, e, bID) {
			t.Fatal("entity missing bID after World.Write between Progress calls")
		}
	})
}

// TestParallelSystems_SnapshotRestore verifies that TakeSnapshot / RestoreSnapshot
// between Progress calls produces a clean state with per-stage parallel dispatch.
func TestParallelSystems_SnapshotRestore(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(2)

	aID := flecs.RegisterComponent[psTagA](w)
	bID := flecs.RegisterComponent[psTagB](w)

	const n = 10
	aEnts := make([]flecs.ID, n)
	w.Write(func(fw *flecs.Writer) {
		for i := range n {
			e := fw.NewEntity()
			flecs.AddID(fw, e, aID)
			aEnts[i] = e
		}
	})

	snap := flecs.TakeSnapshot(w)

	// Parallel system: adds bID to all aID entities.
	cqA := flecs.NewCachedQuery(w, aID)
	sA := flecs.NewSystem(w, cqA, func(_ float32, it *flecs.QueryIter) {
		fw := it.Writer()
		for _, e := range aEnts {
			fw.AddID(e, bID)
		}
	})
	sA.SetParallel(true)
	sA.SetWriteSet([]flecs.ID{aID})

	w.Progress(0)

	// Restore snapshot; entities should be back to original state (no bID).
	flecs.RestoreSnapshot(w, snap)

	for i, e := range aEnts {
		w.Read(func(r *flecs.Reader) {
			if !w.IsAlive(e) {
				t.Errorf("aEnts[%d] not alive after RestoreSnapshot", i)
				return
			}
			if flecs.HasID(r, e, bID) {
				t.Errorf("aEnts[%d] has bID after RestoreSnapshot (should be absent)", i)
			}
		})
	}
}

// TestParallelSystems_DuringReclamation exercises the per-stage routing while
// table reclamation is active. The combination must not produce use-after-free
// or missed EventOnTableDelete events.
func TestParallelSystems_DuringReclamation(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(2)
	w.SetTableReclamationThreshold(1) // reclaim after 1 empty tick

	aID := flecs.RegisterComponent[psTagA](w)
	bID := flecs.RegisterComponent[psTagB](w)

	const n = 20
	aEnts := make([]flecs.ID, n)
	w.Write(func(fw *flecs.Writer) {
		for i := range n {
			e := fw.NewEntity()
			flecs.AddID(fw, e, aID)
			aEnts[i] = e
		}
	})

	// Parallel system: deletes all aID entities via deferred queue.
	cqA := flecs.NewCachedQuery(w, aID)
	sA := flecs.NewSystem(w, cqA, func(_ float32, it *flecs.QueryIter) {
		fw := it.Writer()
		for _, e := range aEnts {
			fw.Delete(e)
		}
	})
	sA.SetParallel(true)
	sA.SetWriteSet([]flecs.ID{aID})

	// Second parallel system runs in same batch — exercises multi-stage reclamation path.
	cqB := flecs.NewCachedQuery(w, bID)
	sB := flecs.NewSystem(w, cqB, func(_ float32, _ *flecs.QueryIter) {})
	sB.SetParallel(true)
	sB.SetWriteSet([]flecs.ID{bID})

	// First tick deletes aID entities; reclamation fires on second tick.
	w.Progress(0)
	w.Progress(0) // triggers table reclamation sweep

	for i, e := range aEnts {
		if w.IsAlive(e) {
			t.Fatalf("aEnts[%d] still alive after Delete + reclamation tick", i)
		}
	}
}
