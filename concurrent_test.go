package flecs_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/snichols/flecs"
)

type readonlyPos struct{ X, Y float32 }

// TestReadonlyAllowsConcurrentReaders verifies that N goroutines can each
// iterate via Each1 while the main goroutine holds a Readonly window open,
// with no data race.
func TestReadonlyAllowsConcurrentReaders(t *testing.T) {
	w := flecs.New()
	for range 100 {
		e := w.NewEntity()
		flecs.Set(w, e, readonlyPos{X: 1, Y: 2})
	}

	const goroutines = 8
	var wg sync.WaitGroup
	var total atomic.Int64

	w.Readonly(func() {
		for range goroutines {
			wg.Add(1)
			go func() {
				defer wg.Done()
				flecs.Each1[readonlyPos](w, func(_ flecs.ID, p *readonlyPos) {
					total.Add(int64(p.X))
				})
			}()
		}
		wg.Wait()
	})

	if total.Load() != int64(goroutines*100) {
		t.Fatalf("expected %d, got %d", goroutines*100, total.Load())
	}
}

// TestReadonlyEnqueuesWrites verifies that Set/Delete inside a Readonly window
// are buffered and NOT applied until ReadonlyEnd flushes.
func TestReadonlyEnqueuesWrites(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w, e, readonlyPos{X: 1})

	w.ReadonlyBegin()

	// Writer goroutine enqueues a Set while the window is open.
	var writerDone sync.WaitGroup
	writerDone.Add(1)
	go func() {
		defer writerDone.Done()
		flecs.Set(w, e, readonlyPos{X: 99})
	}()
	writerDone.Wait()

	// Value must still be the original — write is deferred.
	if p, ok := flecs.Get[readonlyPos](w, e); !ok || p.X != 1 {
		t.Fatalf("expected X=1 before ReadonlyEnd, got %v (ok=%v)", p, ok)
	}

	w.ReadonlyEnd() // flush: deferred Set applies here

	// Value must now reflect the deferred write.
	if p, ok := flecs.Get[readonlyPos](w, e); !ok || p.X != 99 {
		t.Fatalf("expected X=99 after ReadonlyEnd, got %v (ok=%v)", p, ok)
	}
}

// TestReadonlyDeleteEnqueued verifies that Delete inside a Readonly window is
// buffered and applied on ReadonlyEnd.
func TestReadonlyDeleteEnqueued(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()

	w.ReadonlyBegin()

	var done sync.WaitGroup
	done.Add(1)
	go func() {
		defer done.Done()
		w.Delete(e)
	}()
	done.Wait()

	// Entity must still be alive — delete is deferred.
	if !w.IsAlive(e) {
		t.Fatal("entity should still be alive before ReadonlyEnd")
	}

	w.ReadonlyEnd() // flush: deferred Delete applies here

	if w.IsAlive(e) {
		t.Fatal("entity should be dead after ReadonlyEnd")
	}
}

// TestReadonlyNestedWithDefer verifies that Readonly nested inside Defer and
// Defer nested inside Readonly both flush correctly.
func TestReadonlyNestedWithDefer(t *testing.T) {
	// Case A: Readonly inside Defer.
	{
		w := flecs.New()
		e := w.NewEntity()
		flecs.Set(w, e, readonlyPos{X: 1})

		w.Defer(func() {
			w.Readonly(func() {
				flecs.Set(w, e, readonlyPos{X: 42})
			})
			// After inner ReadonlyEnd: deferDepth is back to 1 (outer Defer still open).
			// The Set was enqueued; it flushes when the outer DeferEnd fires.
		})

		if p, ok := flecs.Get[readonlyPos](w, e); !ok || p.X != 42 {
			t.Fatalf("case A: expected X=42, got %v (ok=%v)", p, ok)
		}
	}

	// Case B: Defer inside Readonly.
	{
		w := flecs.New()
		e := w.NewEntity()
		flecs.Set(w, e, readonlyPos{X: 1})

		w.Readonly(func() {
			w.Defer(func() {
				flecs.Set(w, e, readonlyPos{X: 77})
			})
			// After inner DeferEnd: deferDepth back to 1 (Readonly still open).
			// Write was applied during inner flush... wait, deferDepth is 1 after
			// ReadonlyBegin, so inner DeferBegin makes it 2, inner DeferEnd makes it 1
			// (no flush yet). Outer ReadonlyEnd sets readonly=false, calls DeferEnd
			// which decrements to 0 and flushes.
		})

		if p, ok := flecs.Get[readonlyPos](w, e); !ok || p.X != 77 {
			t.Fatalf("case B: expected X=77, got %v (ok=%v)", p, ok)
		}
	}
}

// TestDeferWrappedIterationStillPasses is a regression test confirming the
// original Each+Defer+Delete pattern works correctly and without deadlock.
func TestDeferWrappedIterationStillPasses(t *testing.T) {
	w := flecs.New()
	for range 10 {
		e := w.NewEntity()
		flecs.Set(w, e, readonlyPos{X: -1})
	}
	for range 5 {
		e := w.NewEntity()
		flecs.Set(w, e, readonlyPos{X: 1})
	}

	var deleted int
	w.Defer(func() {
		flecs.Each1[readonlyPos](w, func(e flecs.ID, p *readonlyPos) {
			if p.X < 0 {
				w.Delete(e)
				deleted++
			}
		})
	})

	if deleted != 10 {
		t.Fatalf("expected 10 deletes, got %d", deleted)
	}
	// 5 positive-X entities remain, plus all built-in entities.
	count := 0
	flecs.Each1[readonlyPos](w, func(_ flecs.ID, _ *readonlyPos) { count++ })
	if count != 5 {
		t.Fatalf("expected 5 surviving entities, got %d", count)
	}
}

// TestReadonlyIsDeferred verifies that IsDeferred returns true during a Readonly window.
func TestReadonlyIsDeferred(t *testing.T) {
	w := flecs.New()
	if w.IsDeferred() {
		t.Fatal("world should not be deferred initially")
	}
	w.ReadonlyBegin()
	if !w.IsDeferred() {
		t.Fatal("world should be deferred during Readonly window")
	}
	w.ReadonlyEnd()
	if w.IsDeferred() {
		t.Fatal("world should not be deferred after ReadonlyEnd")
	}
}
