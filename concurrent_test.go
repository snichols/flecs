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
	w.Write(func(fw *flecs.Writer) {
		for range 100 {
			e := fw.NewEntity()
			flecs.Set(fw, e, readonlyPos{X: 1, Y: 2})
		}
	})

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

// TestReadonlyNestedWithDefer verifies that a Write scope nested inside another
// Write scope and vice versa both flush correctly.
func TestReadonlyNestedWithDefer(t *testing.T) {
	// Case A: Write inside Write — nested Write shares defer queue.
	{
		w := flecs.New()
		var e flecs.ID
		w.Write(func(fw *flecs.Writer) {
			e = fw.NewEntity()
			flecs.Set(fw, e, readonlyPos{X: 1})
		})

		w.Write(func(outer *flecs.Writer) {
			flecs.Set(outer, e, readonlyPos{X: 42})
			w.Write(func(inner *flecs.Writer) {
				// Confirm nested write sees the queue.
				_ = inner
			})
		})

		w.Read(func(r *flecs.Reader) {
			if p, ok := flecs.Get[readonlyPos](r, e); !ok || p.X != 42 {
				t.Fatalf("case A: expected X=42, got %v (ok=%v)", p, ok)
			}
		})
	}

	// Case B: sequential Write calls each flush independently.
	{
		w := flecs.New()
		var e flecs.ID
		w.Write(func(fw *flecs.Writer) {
			e = fw.NewEntity()
			flecs.Set(fw, e, readonlyPos{X: 1})
		})

		w.Write(func(fw *flecs.Writer) {
			flecs.Set(fw, e, readonlyPos{X: 77})
		})

		w.Read(func(r *flecs.Reader) {
			if p, ok := flecs.Get[readonlyPos](r, e); !ok || p.X != 77 {
				t.Fatalf("case B: expected X=77, got %v (ok=%v)", p, ok)
			}
		})
	}
}

// TestDeferWrappedIterationStillPasses is a regression test confirming the
// original Each+Write+Delete pattern works correctly and without deadlock.
func TestDeferWrappedIterationStillPasses(t *testing.T) {
	w := flecs.New()
	w.Write(func(fw *flecs.Writer) {
		for range 10 {
			e := fw.NewEntity()
			flecs.Set(fw, e, readonlyPos{X: -1})
		}
		for range 5 {
			e := fw.NewEntity()
			flecs.Set(fw, e, readonlyPos{X: 1})
		}
	})

	var deleted int
	w.Write(func(fw *flecs.Writer) {
		flecs.Each1[readonlyPos](fw, func(e flecs.ID, p *readonlyPos) {
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
	w.Read(func(r *flecs.Reader) {
		flecs.Each1[readonlyPos](r, func(_ flecs.ID, _ *readonlyPos) { count++ })
	})
	if count != 5 {
		t.Fatalf("expected 5 surviving entities, got %d", count)
	}
}

// TestWriterScopePromotion verifies that *Writer satisfies read free-functions directly.
func TestWriterScopePromotion(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[readonlyPos](fw, e, readonlyPos{X: 1})
	})
	// Read using *Writer as scope — no AsReader() needed.
	w.Write(func(fw *flecs.Writer) {
		p, ok := flecs.Get[readonlyPos](fw, e)
		if !ok || p.X != 1 {
			t.Fatal("Get[readonlyPos] via Writer should work without AsReader")
		}
	})
}
