package flecs_test

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/snichols/flecs"
)

// component types used only in parallel tests
type parallelPos struct{ X float32 }
type parallelVel struct{ DX float32 }
type parallelAcc struct{ DDX float32 }

func TestSetParallelDefault(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[parallelPos](w)
	e := w.NewEntity()
	flecs.Set(w.W(), e, parallelPos{})
	cq := flecs.NewCachedQuery(w, posID)
	sys := flecs.NewSystem(w, cq, func(_ float32, _ *flecs.QueryIter) {})
	if sys.Parallel() {
		t.Fatal("expected Parallel() to be false by default")
	}
}

func TestSetWorkerCountDefault(t *testing.T) {
	w := flecs.New()
	if w.WorkerCount() != 0 {
		t.Fatalf("expected default WorkerCount() == 0, got %d", w.WorkerCount())
	}
}

func TestSetWorkerCountNegativePanics(t *testing.T) {
	w := flecs.New()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for negative SetWorkerCount")
		}
	}()
	w.SetWorkerCount(-1)
}

func TestParallelSystemsRunConcurrently(t *testing.T) {
	// Two parallel systems with disjoint write sets run at the same time.
	// We detect true concurrency via a shared counter: if both systems ever
	// have current == 2 simultaneously, they overlapped.
	w := flecs.New()
	w.SetWorkerCount(2)

	posID := flecs.RegisterComponent[parallelPos](w)
	velID := flecs.RegisterComponent[parallelVel](w)

	for range 100 {
		e := w.NewEntity()
		flecs.Set(w.W(), e, parallelPos{})
		flecs.Set(w.W(), e, parallelVel{})
	}

	var mu sync.Mutex
	var concurrent, maxConcurrent int

	sleepDur := 30 * time.Millisecond

	incDec := func() {
		mu.Lock()
		concurrent++
		if concurrent > maxConcurrent {
			maxConcurrent = concurrent
		}
		mu.Unlock()

		time.Sleep(sleepDur)

		mu.Lock()
		concurrent--
		mu.Unlock()
	}

	cqPos := flecs.NewCachedQuery(w, posID)
	sysA := flecs.NewSystem(w, cqPos, func(_ float32, _ *flecs.QueryIter) {
		incDec()
	})
	sysA.SetParallel(true)
	sysA.SetWriteSet([]flecs.ID{posID})

	cqVel := flecs.NewCachedQuery(w, velID)
	sysB := flecs.NewSystem(w, cqVel, func(_ float32, _ *flecs.QueryIter) {
		incDec()
	})
	sysB.SetParallel(true)
	sysB.SetWriteSet([]flecs.ID{velID})

	w.Progress(0)

	if maxConcurrent < 2 {
		t.Fatalf("expected both systems to run concurrently (maxConcurrent=%d)", maxConcurrent)
	}
}

func TestConflictingParallelSystemsSerialize(t *testing.T) {
	// Two parallel systems sharing a write set must run serially.
	w := flecs.New()
	w.SetWorkerCount(2)

	posID := flecs.RegisterComponent[parallelPos](w)

	for range 100 {
		e := w.NewEntity()
		flecs.Set(w.W(), e, parallelPos{})
	}

	var mu sync.Mutex
	var concurrent, maxConcurrent int

	incDec := func() {
		mu.Lock()
		concurrent++
		if concurrent > maxConcurrent {
			maxConcurrent = concurrent
		}
		mu.Unlock()

		time.Sleep(20 * time.Millisecond)

		mu.Lock()
		concurrent--
		mu.Unlock()
	}

	cqA := flecs.NewCachedQuery(w, posID)
	sysA := flecs.NewSystem(w, cqA, func(_ float32, _ *flecs.QueryIter) {
		incDec()
	})
	sysA.SetParallel(true)
	sysA.SetWriteSet([]flecs.ID{posID}) // both write posID → conflict

	cqB := flecs.NewCachedQuery(w, posID)
	sysB := flecs.NewSystem(w, cqB, func(_ float32, _ *flecs.QueryIter) {
		incDec()
	})
	sysB.SetParallel(true)
	sysB.SetWriteSet([]flecs.ID{posID}) // conflict with sysA

	w.Progress(0)

	if maxConcurrent > 1 {
		t.Fatalf("conflicting systems should not run concurrently (maxConcurrent=%d)", maxConcurrent)
	}
}

func TestMixedBatchSerialThenParallel(t *testing.T) {
	// [serial, parallel-A, parallel-B-disjoint]: serial first, then A and B concurrent.
	w := flecs.New()
	w.SetWorkerCount(2)

	posID := flecs.RegisterComponent[parallelPos](w)
	velID := flecs.RegisterComponent[parallelVel](w)

	for range 50 {
		e := w.NewEntity()
		flecs.Set(w.W(), e, parallelPos{})
		flecs.Set(w.W(), e, parallelVel{})
	}

	var order []string
	var orderMu sync.Mutex
	record := func(name string) {
		orderMu.Lock()
		order = append(order, name)
		orderMu.Unlock()
	}

	var parallelMu sync.Mutex
	var parallelConcurrent, parallelMax int

	// Serial system (registered first so it runs first).
	cqSerial := flecs.NewCachedQuery(w, posID)
	flecs.NewSystem(w, cqSerial, func(_ float32, _ *flecs.QueryIter) {
		record("serial")
	})

	sleepDur := 30 * time.Millisecond

	cqA := flecs.NewCachedQuery(w, posID)
	sysA := flecs.NewSystem(w, cqA, func(_ float32, _ *flecs.QueryIter) {
		parallelMu.Lock()
		parallelConcurrent++
		if parallelConcurrent > parallelMax {
			parallelMax = parallelConcurrent
		}
		parallelMu.Unlock()

		record("A")
		time.Sleep(sleepDur)

		parallelMu.Lock()
		parallelConcurrent--
		parallelMu.Unlock()
	})
	sysA.SetParallel(true)
	sysA.SetWriteSet([]flecs.ID{posID})

	cqB := flecs.NewCachedQuery(w, velID)
	sysB := flecs.NewSystem(w, cqB, func(_ float32, _ *flecs.QueryIter) {
		parallelMu.Lock()
		parallelConcurrent++
		if parallelConcurrent > parallelMax {
			parallelMax = parallelConcurrent
		}
		parallelMu.Unlock()

		record("B")
		time.Sleep(sleepDur)

		parallelMu.Lock()
		parallelConcurrent--
		parallelMu.Unlock()
	})
	sysB.SetParallel(true)
	sysB.SetWriteSet([]flecs.ID{velID})

	w.Progress(0)

	// Serial must come first.
	if len(order) == 0 || order[0] != "serial" {
		t.Fatalf("expected serial system to run first, got order=%v", order)
	}
	// A and B must have overlapped.
	if parallelMax < 2 {
		t.Fatalf("expected A and B to run concurrently (parallelMax=%d)", parallelMax)
	}
}

func TestWriteSetOverrideEnablesParallel(t *testing.T) {
	// Same query, but explicit disjoint write sets → run in parallel.
	w := flecs.New()
	w.SetWorkerCount(2)

	posID := flecs.RegisterComponent[parallelPos](w)
	velID := flecs.RegisterComponent[parallelVel](w)

	for range 50 {
		e := w.NewEntity()
		flecs.Set(w.W(), e, parallelPos{})
		flecs.Set(w.W(), e, parallelVel{})
	}

	cq := flecs.NewCachedQuery(w, posID, velID)

	var mu sync.Mutex
	var concurrent, maxConcurrent int

	sleepDur := 30 * time.Millisecond

	incDec := func() {
		mu.Lock()
		concurrent++
		if concurrent > maxConcurrent {
			maxConcurrent = concurrent
		}
		mu.Unlock()
		time.Sleep(sleepDur)
		mu.Lock()
		concurrent--
		mu.Unlock()
	}

	// Both systems use the same query but declare disjoint write sets.
	cqA := flecs.NewCachedQuery(w, posID, velID)
	sysA := flecs.NewSystem(w, cqA, func(_ float32, _ *flecs.QueryIter) {
		incDec()
	})
	sysA.SetParallel(true)
	sysA.SetWriteSet([]flecs.ID{posID}) // only writes pos

	cqB := flecs.NewCachedQuery(w, posID, velID)
	sysB := flecs.NewSystem(w, cqB, func(_ float32, _ *flecs.QueryIter) {
		incDec()
	})
	sysB.SetParallel(true)
	sysB.SetWriteSet([]flecs.ID{velID}) // only writes vel → no conflict

	_ = cq // silence unused warning

	w.Progress(0)

	if maxConcurrent < 2 {
		t.Fatalf("expected parallel execution with disjoint explicit write sets (maxConcurrent=%d)", maxConcurrent)
	}
}

func TestReadOnlyWriteSetEnablesParallel(t *testing.T) {
	// WriteSet([]) on both → read-only systems, always run in parallel.
	w := flecs.New()
	w.SetWorkerCount(2)

	posID := flecs.RegisterComponent[parallelPos](w)

	for range 50 {
		e := w.NewEntity()
		flecs.Set(w.W(), e, parallelPos{})
	}

	var mu sync.Mutex
	var concurrent, maxConcurrent int

	sleepDur := 30 * time.Millisecond

	incDec := func() {
		mu.Lock()
		concurrent++
		if concurrent > maxConcurrent {
			maxConcurrent = concurrent
		}
		mu.Unlock()
		time.Sleep(sleepDur)
		mu.Lock()
		concurrent--
		mu.Unlock()
	}

	for range 2 {
		cq := flecs.NewCachedQuery(w, posID)
		sys := flecs.NewSystem(w, cq, func(_ float32, _ *flecs.QueryIter) {
			incDec()
		})
		sys.SetParallel(true)
		sys.SetWriteSet([]flecs.ID{}) // read-only
	}

	w.Progress(0)

	if maxConcurrent < 2 {
		t.Fatalf("expected read-only systems to run concurrently (maxConcurrent=%d)", maxConcurrent)
	}
}

func TestDeferDuringParallel(t *testing.T) {
	// Parallel system calls flecs.Set; mutations queue via deferred mechanism
	// and apply after the phase. Test passes -race by design (mutex-protected).
	w := flecs.New()
	w.SetWorkerCount(2)

	posID := flecs.RegisterComponent[parallelPos](w)
	velID := flecs.RegisterComponent[parallelVel](w)

	const n = 50
	entities := make([]flecs.ID, n)
	for i := range n {
		e := w.NewEntity()
		flecs.Set(w.W(), e, parallelPos{X: float32(i)})
		entities[i] = e
	}

	cq := flecs.NewCachedQuery(w, posID)
	sys := flecs.NewSystem(w, cq, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			for _, e := range it.Entities() {
				// Set is deferred (we're inside a Defer block from Progress).
				flecs.Set(w.W(), e, parallelVel{DX: 1})
			}
		}
	})
	sys.SetParallel(true)
	sys.SetWriteSet([]flecs.ID{velID}) // writes vel, not pos

	w.Progress(0)

	// After Progress, deferred Sets should have been flushed.
	for _, e := range entities {
		v, ok := flecs.Get[parallelVel](w.R(), e)
		if !ok {
			t.Fatalf("entity %v: Vel not set after parallel deferred Set", e)
		}
		if v.DX != 1 {
			t.Fatalf("entity %v: expected DX=1, got %v", e, v.DX)
		}
	}
}

func TestSetWorkerCountMidFrame(t *testing.T) {
	// SetWorkerCount during Progress is a no-op per documented behavior.
	w := flecs.New()
	posID := flecs.RegisterComponent[parallelPos](w)
	e := w.NewEntity()
	flecs.Set(w.W(), e, parallelPos{})
	cq := flecs.NewCachedQuery(w, posID)

	ran := false
	flecs.NewSystem(w, cq, func(_ float32, _ *flecs.QueryIter) {
		w.SetWorkerCount(4) // no-op: we're inside Progress
		ran = true
	})

	w.Progress(0)

	if !ran {
		t.Fatal("system did not run")
	}
	// Worker count should still be 0 (the no-op left it unchanged).
	if w.WorkerCount() != 0 {
		t.Fatalf("expected WorkerCount() == 0 after mid-frame no-op, got %d", w.WorkerCount())
	}
}

func TestWorkerPoolShutdown(t *testing.T) {
	// SetWorkerCount(4) then SetWorkerCount(0): goroutines exit.
	before := runtime.NumGoroutine()

	w := flecs.New()
	w.SetWorkerCount(4)

	// Give goroutines time to start.
	time.Sleep(20 * time.Millisecond)
	after4 := runtime.NumGoroutine()
	if after4 < before+4 {
		t.Logf("goroutine count after SetWorkerCount(4): %d (expected >= %d); scheduler may be slow", after4, before+4)
	}

	w.SetWorkerCount(0)
	// Allow goroutines to drain and exit.
	time.Sleep(50 * time.Millisecond)

	afterShutdown := runtime.NumGoroutine()
	// Allow a small slack for goroutines in other tests.
	if afterShutdown > before+2 {
		t.Fatalf("expected goroutines to exit after SetWorkerCount(0): before=%d, after=%d", before, afterShutdown)
	}
}

func TestParallelRace(t *testing.T) {
	// Runs a parallel scenario multiple times; -race detector catches data races.
	// This test is designed to be run with -race -count=10.
	w := flecs.New()
	w.SetWorkerCount(4)

	posID := flecs.RegisterComponent[parallelPos](w)
	velID := flecs.RegisterComponent[parallelVel](w)
	accID := flecs.RegisterComponent[parallelAcc](w)

	for range 200 {
		e := w.NewEntity()
		flecs.Set(w.W(), e, parallelPos{})
		flecs.Set(w.W(), e, parallelVel{})
		flecs.Set(w.W(), e, parallelAcc{})
	}

	cqA := flecs.NewCachedQuery(w, posID)
	sysA := flecs.NewSystem(w, cqA, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			cols := flecs.Field[parallelPos](it, posID)
			for i := range cols {
				cols[i].X += 1
			}
		}
	})
	sysA.SetParallel(true)
	sysA.SetWriteSet([]flecs.ID{posID})

	cqB := flecs.NewCachedQuery(w, velID)
	sysB := flecs.NewSystem(w, cqB, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			cols := flecs.Field[parallelVel](it, velID)
			for i := range cols {
				cols[i].DX += 0.5
			}
		}
	})
	sysB.SetParallel(true)
	sysB.SetWriteSet([]flecs.ID{velID})

	cqC := flecs.NewCachedQuery(w, accID)
	sysC := flecs.NewSystem(w, cqC, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			cols := flecs.Field[parallelAcc](it, accID)
			for i := range cols {
				cols[i].DDX += 0.1
			}
		}
	})
	sysC.SetParallel(true)
	sysC.SetWriteSet([]flecs.ID{accID})

	for range 5 {
		w.Progress(1.0 / 60.0)
	}
}
