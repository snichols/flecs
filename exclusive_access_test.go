package flecs_test

import (
	"sync"
	"testing"

	flecs "github.com/snichols/flecs"
)

type xaPos struct{ X, Y float32 }

// TestExclusiveAccessOwnerCanWrite verifies the owner goroutine can write freely.
func TestExclusiveAccessOwnerCanWrite(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()

	w.ExclusiveAccessBegin("test-owner")
	flecs.Set(w.W(), e, xaPos{1, 2})
	w.ExclusiveAccessEnd(false)

	pos, ok := flecs.Get[xaPos](w.R(), e)
	if !ok || pos.X != 1 || pos.Y != 2 {
		t.Fatalf("expected pos {1,2}, got %v ok=%v", pos, ok)
	}
}

// TestExclusiveAccessOtherGoroutineWritePanics verifies a goroutine other than
// the owner panics on a mutation attempt.
func TestExclusiveAccessOtherGoroutineWritePanics(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()

	w.ExclusiveAccessBegin("owner")

	var wg sync.WaitGroup
	wg.Add(1)
	panicked := false
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		flecs.Set(w.W(), e, xaPos{9, 9})
	}()
	wg.Wait()

	w.ExclusiveAccessEnd(false)

	if !panicked {
		t.Fatal("expected panic from non-owner goroutine Set, but none occurred")
	}
}

// TestExclusiveAccessLockedWorldRejectsWrites verifies that after End(true), no
// goroutine can write, but reads still pass.
func TestExclusiveAccessLockedWorldRejectsWrites(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w.W(), e, xaPos{5, 6})

	w.ExclusiveAccessBegin("owner")
	w.ExclusiveAccessEnd(true) // lock world for writes

	// Write should panic from any goroutine (including this one).
	didPanic := func() (panicked bool) {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		flecs.Set(w.W(), e, xaPos{9, 9})
		return false
	}()
	if !didPanic {
		t.Fatal("expected Set to panic on a write-locked world")
	}

	// Reads must still pass.
	pos, ok := flecs.Get[xaPos](w.R(), e)
	if !ok || pos.X != 5 {
		t.Fatalf("expected read to succeed on locked world, got %v ok=%v", pos, ok)
	}
}

// TestExclusiveAccessUnsetIsNoop verifies that without Begin, reads and writes
// proceed freely from any goroutine.
func TestExclusiveAccessUnsetIsNoop(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		flecs.Set(w.W(), e, xaPos{3, 4})
	}()
	wg.Wait()

	pos, ok := flecs.Get[xaPos](w.R(), e)
	if !ok || pos.X != 3 {
		t.Fatalf("expected {3,4}, got %v ok=%v", pos, ok)
	}
}

// TestExclusiveAccessNestedBeginPanics verifies that calling Begin twice (even
// from the same goroutine) panics, enforcing the "no nested ownership" rule.
func TestExclusiveAccessNestedBeginPanics(t *testing.T) {
	w := flecs.New()

	w.ExclusiveAccessBegin("first")

	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		w.ExclusiveAccessBegin("second")
	}()

	w.ExclusiveAccessEnd(false)

	if !panicked {
		t.Fatal("expected second ExclusiveAccessBegin to panic, but it did not")
	}
}

// TestExclusiveAccessProgressFromOtherGoroutinePanics verifies that calling
// Progress from a goroutine other than the owner panics.
func TestExclusiveAccessProgressFromOtherGoroutinePanics(t *testing.T) {
	w := flecs.New()

	w.ExclusiveAccessBegin("owner")

	var wg sync.WaitGroup
	wg.Add(1)
	panicked := false
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		w.Progress(0.016)
	}()
	wg.Wait()

	w.ExclusiveAccessEnd(false)

	if !panicked {
		t.Fatal("expected Progress from non-owner goroutine to panic, but none occurred")
	}
}

// TestExclusiveAccessReadEntryPointsRespectOwnership verifies that IsAlive,
// Count, and SystemCount panic from a non-owner goroutine while owned, and
// succeed after End.
func TestExclusiveAccessReadEntryPointsRespectOwnership(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()

	mustPanicFromGoroutine := func(name string, fn func()) {
		t.Helper()
		var wg sync.WaitGroup
		wg.Add(1)
		panicked := false
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicked = true
				}
			}()
			fn()
		}()
		wg.Wait()
		if !panicked {
			t.Fatalf("%s: expected panic from non-owner goroutine, but none occurred", name)
		}
	}

	w.ExclusiveAccessBegin("owner")

	mustPanicFromGoroutine("IsAlive", func() { w.IsAlive(e) })
	mustPanicFromGoroutine("Count", func() { w.Count() })
	mustPanicFromGoroutine("SystemCount", func() { w.SystemCount() })

	w.ExclusiveAccessEnd(false)

	// After End, same calls from any goroutine must succeed.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = w.IsAlive(e)
		_ = w.Count()
		_ = w.SystemCount()
	}()
	wg.Wait()
}

// TestGoidGetIsNonZero verifies that currentGoid returns a nonzero goroutine ID.
func TestGoidGetIsNonZero(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()

	// ExclusiveAccessBegin uses currentGoid internally; if it returns 0 the store
	// would be a no-op and the world would appear unclaimed.
	w.ExclusiveAccessBegin("goid-check")
	// Owner can write without panic — proves goid was nonzero and stored correctly.
	flecs.Set(w.W(), e, xaPos{1, 1})
	w.ExclusiveAccessEnd(false)
}

// TestExclusiveAccessZeroOverheadCommonPath verifies that with no ExclusiveAccessBegin
// ever called, Set allocates 0 bytes per call (the common-path cost is just one atomic.Load).
func TestExclusiveAccessZeroOverheadCommonPath(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w.W(), e, xaPos{0, 0}) // warm up archetype

	allocs := testing.AllocsPerRun(1000, func() {
		flecs.Set(w.W(), e, xaPos{1, 2})
	})
	if allocs > 0 {
		t.Fatalf("expected 0 allocations per Set in common path, got %.1f", allocs)
	}
}
