package flecs_test

import (
	"fmt"
	"sort"
	"sync"
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
// w.Delete from inside the iter loop are safe (mutex-protected defer queue)
// and all deletes are applied correctly after the phase.
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
		for it.Next() {
			for _, e := range it.Entities() {
				w.Delete(e) // deferred via mutex-protected queue
			}
		}
	})
	sys.SetMultiThreaded(true)

	w.Progress(0)

	// All entities must be dead after the Defer block flushes.
	for _, e := range entities {
		if w.IsAlive(e) {
			t.Fatalf("entity %v should be dead after deferred Delete from multi-threaded worker", e)
		}
	}
}
