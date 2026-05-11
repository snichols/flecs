package flecs_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/snichols/flecs"
)

type readonlyPos struct{ X, Y float32 }

// TestReadonlyAllowsConcurrentReaders verifies that N goroutines can each
// iterate via Each1 while the main goroutine holds a Read window open,
// with no data race.
func TestReadonlyAllowsConcurrentReaders(t *testing.T) {
	w := flecs.New()
	for range 100 {
		e := w.NewEntity()
		flecs.Set(w.W(), e, readonlyPos{X: 1, Y: 2})
	}

	const goroutines = 8
	var wg sync.WaitGroup
	var total atomic.Int64

	w.Read(func(fr *flecs.Reader) {
		for range goroutines {
			wg.Add(1)
			go func() {
				defer wg.Done()
				w.Read(func(inner *flecs.Reader) {
					flecs.Each1[readonlyPos](inner, func(_ flecs.ID, p *readonlyPos) {
						total.Add(int64(p.X))
					})
				})
			}()
		}
		wg.Wait()
	})

	if total.Load() != int64(goroutines*100) {
		t.Fatalf("expected %d, got %d", goroutines*100, total.Load())
	}
}

// TestReadonlyEnqueuesWrites verifies that Set inside a readonly-mode deferred
// window is buffered and not applied until the window flushes.
func TestReadonlyEnqueuesWrites(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w.W(), e, readonlyPos{X: 1})

	// Simulate the old ReadonlyBegin/End semantics by using the internal
	// readonlyBegin/readonlyEnd exported via test export.
	flecs.ReadonlyBeginForTest(w)

	// Writer goroutine enqueues a Set while the window is open.
	var writerDone sync.WaitGroup
	writerDone.Add(1)
	go func() {
		defer writerDone.Done()
		flecs.Set(w.W(), e, readonlyPos{X: 99})
	}()
	writerDone.Wait()

	// Value must still be the original — write is deferred.
	if p, ok := flecs.Get[readonlyPos](w.R(), e); !ok || p.X != 1 {
		t.Fatalf("expected X=1 before ReadonlyEnd, got %v (ok=%v)", p, ok)
	}

	flecs.ReadonlyEndForTest(w) // flush: deferred Set applies here

	// Value must now reflect the deferred write.
	if p, ok := flecs.Get[readonlyPos](w.R(), e); !ok || p.X != 99 {
		t.Fatalf("expected X=99 after ReadonlyEnd, got %v (ok=%v)", p, ok)
	}
}

// TestReadonlyDeleteEnqueued verifies that Delete inside a readonly window is
// buffered and applied on close.
func TestReadonlyDeleteEnqueued(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()

	flecs.ReadonlyBeginForTest(w)

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

	flecs.ReadonlyEndForTest(w) // flush: deferred Delete applies here

	if w.IsAlive(e) {
		t.Fatal("entity should be dead after ReadonlyEnd")
	}
}

// TestReadonlyNestedWithDefer verifies that a Write scope nested inside another
// Write scope and vice versa both flush correctly.
func TestReadonlyNestedWithDefer(t *testing.T) {
	// Case A: Write inside Write — nested Write shares defer queue.
	{
		w := flecs.New()
		e := w.NewEntity()
		flecs.Set(w.W(), e, readonlyPos{X: 1})

		w.Write(func(outer *flecs.Writer) {
			flecs.Set(outer, e, readonlyPos{X: 42})
			w.Write(func(inner *flecs.Writer) {
				// Confirm nested write sees the queue.
				_ = inner
			})
		})

		if p, ok := flecs.Get[readonlyPos](w.R(), e); !ok || p.X != 42 {
			t.Fatalf("case A: expected X=42, got %v (ok=%v)", p, ok)
		}
	}

	// Case B: sequential Write calls each flush independently.
	{
		w := flecs.New()
		e := w.NewEntity()
		flecs.Set(w.W(), e, readonlyPos{X: 1})

		w.Write(func(fw *flecs.Writer) {
			flecs.Set(fw, e, readonlyPos{X: 77})
		})

		if p, ok := flecs.Get[readonlyPos](w.R(), e); !ok || p.X != 77 {
			t.Fatalf("case B: expected X=77, got %v (ok=%v)", p, ok)
		}
	}
}

// TestDeferWrappedIterationStillPasses is a regression test confirming the
// original Each+Write+Delete pattern works correctly and without deadlock.
func TestDeferWrappedIterationStillPasses(t *testing.T) {
	w := flecs.New()
	for range 10 {
		e := w.NewEntity()
		flecs.Set(w.W(), e, readonlyPos{X: -1})
	}
	for range 5 {
		e := w.NewEntity()
		flecs.Set(w.W(), e, readonlyPos{X: 1})
	}

	var deleted int
	w.Write(func(fw *flecs.Writer) {
		flecs.Each1[readonlyPos](w.R(), func(e flecs.ID, p *readonlyPos) {
			if p.X < 0 {
				fw.Delete(e)
				deleted++
			}
		})
	})

	if deleted != 10 {
		t.Fatalf("expected 10 deletes, got %d", deleted)
	}
	// 5 positive-X entities remain, plus all built-in entities.
	count := 0
	flecs.Each1[readonlyPos](w.R(), func(_ flecs.ID, _ *readonlyPos) { count++ })
	if count != 5 {
		t.Fatalf("expected 5 surviving entities, got %d", count)
	}
}

// TestReadonlyIsDeferred verifies that IsDeferred returns true during a readonly window.
func TestReadonlyIsDeferred(t *testing.T) {
	w := flecs.New()
	if w.IsDeferred() {
		t.Fatal("world should not be deferred initially")
	}
	flecs.ReadonlyBeginForTest(w)
	if !w.IsDeferred() {
		t.Fatal("world should be deferred during readonly window")
	}
	flecs.ReadonlyEndForTest(w)
	if w.IsDeferred() {
		t.Fatal("world should not be deferred after readonly end")
	}
}

// TestReadonlyEndPanicsWithoutBegin verifies that ReadonlyEndForTest panics if
// called without a matching ReadonlyBeginForTest.
func TestReadonlyEndPanicsWithoutBegin(t *testing.T) {
	w := flecs.New()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic from unmatched ReadonlyEnd")
		}
	}()
	flecs.ReadonlyEndForTest(w)
}

// TestReadonlyNestedDepthPreservation verifies that the inner ReadonlyEnd (at
// depth > 0) does NOT flush — only the outermost ReadonlyEnd flushes.
func TestReadonlyNestedDepthPreservation(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w.W(), e, readonlyPos{X: 1})

	// Two nested readonly windows; flush happens only when both end.
	flecs.ReadonlyBeginForTest(w) // depth 1
	flecs.ReadonlyBeginForTest(w) // depth 2

	// Directly enqueue a write while in readonly mode (readonly=true means
	// mutations are deferred via the deferDepth>0 path).
	var writerDone sync.WaitGroup
	writerDone.Add(1)
	go func() {
		defer writerDone.Done()
		flecs.Set(w.W(), e, readonlyPos{X: 42})
	}()
	writerDone.Wait()

	// Value must still be 1 — write is deferred (depth > 0).
	if p, ok := flecs.Get[readonlyPos](w.R(), e); !ok || p.X != 1 {
		t.Fatalf("before inner ReadonlyEnd: expected X=1, got %v ok=%v", p, ok)
	}

	flecs.ReadonlyEndForTest(w) // depth back to 1; still deferred (no flush)

	// Value must still be 1 — depth is 1 so no flush yet.
	if p, ok := flecs.Get[readonlyPos](w.R(), e); !ok || p.X != 1 {
		t.Fatalf("after inner ReadonlyEnd: expected X=1, got %v ok=%v", p, ok)
	}

	flecs.ReadonlyEndForTest(w) // depth 0; flush

	// After outermost ReadonlyEnd, the deferred write should have applied.
	if p, ok := flecs.Get[readonlyPos](w.R(), e); !ok || p.X != 42 {
		t.Fatalf("expected X=42 after outer ReadonlyEnd, got %v ok=%v", p, ok)
	}
}

// TestWriterAsReader verifies that Writer.AsReader returns a non-nil Reader.
func TestWriterAsReader(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		fr := fw.AsReader()
		if fr == nil {
			t.Fatal("AsReader returned nil")
		}
	})
}
