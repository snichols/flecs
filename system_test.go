package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// --- helper types used only in system tests ---

type SPos struct{ X, Y float32 }
type SVel struct{ X, Y float32 }

// TestSystemBasicRunsOnProgress verifies that a system's callback is invoked
// once per Progress call.
func TestSystemBasicRunsOnProgress(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[SPos](w)
	q := flecs.NewCachedQuery(w, posID)

	count := 0
	flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			count++
		}
	})

	e := w.NewEntity()
	flecs.Set[SPos](w, e, SPos{})

	w.Progress(0)
	if count != 1 {
		t.Fatalf("expected count=1 after first Progress, got %d", count)
	}
	w.Progress(0)
	if count != 2 {
		t.Fatalf("expected count=2 after second Progress, got %d", count)
	}
}

// TestSystemSeesMatchingEntities verifies the system sees the correct entities.
func TestSystemSeesMatchingEntities(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[SPos](w)
	q := flecs.NewCachedQuery(w, posID)

	for _, x := range []float32{1, 2, 3} {
		e := w.NewEntity()
		flecs.Set[SPos](w, e, SPos{X: x})
	}

	var sum float32
	flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			positions := flecs.Field[SPos](it, posID)
			for i := range positions {
				sum += positions[i].X
			}
		}
	})

	w.Progress(0)
	if sum != 6 {
		t.Fatalf("expected sum=6, got %v", sum)
	}
}

// TestSystemDtPassThrough verifies the dt value is forwarded to the callback.
func TestSystemDtPassThrough(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[SPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[SPos](w, e, SPos{})

	var captured float32
	flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			captured = dt
		}
	})

	w.Progress(0.016)
	if captured != 0.016 {
		t.Fatalf("expected captured=0.016, got %v", captured)
	}
	w.Progress(0.033)
	if captured != 0.033 {
		t.Fatalf("expected captured=0.033, got %v", captured)
	}
}

// TestSystemVelocityIntegration verifies the classic position+velocity update.
func TestSystemVelocityIntegration(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[SPos](w)
	velID := flecs.RegisterComponent[SVel](w)
	q := flecs.NewCachedQuery(w, posID, velID)

	e := w.NewEntity()
	flecs.Set[SPos](w, e, SPos{X: 0, Y: 0})
	flecs.Set[SVel](w, e, SVel{X: 1, Y: 2})

	flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			positions := flecs.Field[SPos](it, posID)
			velocities := flecs.Field[SVel](it, velID)
			for i := range positions {
				positions[i].X += velocities[i].X * dt
				positions[i].Y += velocities[i].Y * dt
			}
		}
	})

	const dt = 0.016
	const frames = 10
	for range frames {
		w.Progress(dt)
	}

	pos, _ := flecs.Get[SPos](w, e)
	approxEq := func(a, b float32) bool {
		d := a - b
		if d < 0 {
			d = -d
		}
		return d < 1e-4
	}
	if !approxEq(pos.X, 1*dt*frames) || !approxEq(pos.Y, 2*dt*frames) {
		t.Fatalf("expected approx {%v %v}, got %v", 1*dt*frames, 2*dt*frames, pos)
	}
}

// TestSystemMultipleRunInOrder verifies systems execute in registration order.
func TestSystemMultipleRunInOrder(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[SPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[SPos](w, e, SPos{})

	var order []string
	for _, name := range []string{"A", "B", "C"} {
		name := name
		flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
			for it.Next() {
				order = append(order, name)
			}
		})
	}

	w.Progress(0)
	if len(order) != 3 || order[0] != "A" || order[1] != "B" || order[2] != "C" {
		t.Fatalf("expected [A B C], got %v", order)
	}
}

// TestSystemCloseStopsExecution verifies a closed system does not run.
func TestSystemCloseStopsExecution(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[SPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[SPos](w, e, SPos{})

	called := false
	s := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			called = true
		}
	})

	s.Close()
	w.Progress(0)
	if called {
		t.Fatal("closed system should not run")
	}
}

// TestSystemCloseIdempotent verifies Close can be called multiple times safely.
func TestSystemCloseIdempotent(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[SPos](w)
	q := flecs.NewCachedQuery(w, posID)
	s := flecs.NewSystem(w, q, func(_ float32, _ *flecs.QueryIter) {})

	s.Close()
	s.Close() // must not panic
	if !s.IsClosed() {
		t.Fatal("expected IsClosed() == true")
	}
}

// TestSystemCloseDuringDispatch verifies the deferred-removal semantics:
// a system closed by a peer during the same Progress frame still runs
// (because Progress snapshots the active set at the start of dispatch).
func TestSystemCloseDuringDispatch(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[SPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[SPos](w, e, SPos{})

	bRan := false
	var sysB *flecs.System

	flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			// A closes B during the current frame.
			sysB.Close()
		}
	})
	sysB = flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			bRan = true
		}
	})

	// Both A and B were active when Progress started; B still runs this frame.
	w.Progress(0)
	if !bRan {
		t.Fatal("expected B to run in the same frame it was closed (deferred-removal contract)")
	}

	// On subsequent Progress, B is skipped.
	bRan = false
	w.Progress(0)
	if bRan {
		t.Fatal("expected B not to run after being closed")
	}
}

// TestSystemCompactionAfterClose verifies lazy compaction in NewSystem.
func TestSystemCompactionAfterClose(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[SPos](w)
	q := flecs.NewCachedQuery(w, posID)

	s1 := flecs.NewSystem(w, q, func(_ float32, _ *flecs.QueryIter) {})
	s2 := flecs.NewSystem(w, q, func(_ float32, _ *flecs.QueryIter) {})
	_ = flecs.NewSystem(w, q, func(_ float32, _ *flecs.QueryIter) {})

	s1.Close()
	s2.Close()

	// Adding a new system triggers compaction; slice should have 2 live entries.
	flecs.NewSystem(w, q, func(_ float32, _ *flecs.QueryIter) {})

	if got := w.SystemCount(); got != 2 {
		t.Fatalf("expected SystemCount=2, got %d", got)
	}
	if got := flecs.SystemSliceLen(w); got != 2 {
		t.Fatalf("expected SystemSliceLen=2 after compaction, got %d", got)
	}
}

// TestSystemEmptyMatch verifies that a system runs (and completes without error)
// even when the query matches no tables.
func TestSystemEmptyMatch(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[SPos](w)
	q := flecs.NewCachedQuery(w, posID)
	// No entities created → no matching tables.

	ran := false
	flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		ran = true
		_ = it.Next() // false immediately
	})

	w.Progress(0) // must not panic
	if !ran {
		t.Fatal("expected fn to be called even with empty match")
	}
}

// TestSystemDeferBehavior verifies that Delete inside a system is queued and
// applied after Progress returns, not during the current iteration.
func TestSystemDeferBehavior(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[SPos](w)
	q := flecs.NewCachedQuery(w, posID)

	e := w.NewEntity()
	flecs.Set[SPos](w, e, SPos{})

	seenAliveInsideFn := false
	flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			// Delete is queued (Progress wraps in Defer).
			w.Delete(e)
			// During the same iteration, e is still alive.
			seenAliveInsideFn = w.IsAlive(e)
		}
	})

	w.Progress(0)

	if !seenAliveInsideFn {
		t.Fatal("entity should still be alive during the system's iteration (delete is deferred)")
	}
	if w.IsAlive(e) {
		t.Fatal("entity should be dead after Progress returns (deferred delete was flushed)")
	}
}

// TestSystemMutationDuringIteration verifies the outer Defer safety contract:
// Set inside a system is queued and the old value is visible until Progress ends.
func TestSystemMutationDuringIteration(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[SPos](w)
	q := flecs.NewCachedQuery(w, posID)

	e := w.NewEntity()
	flecs.Set[SPos](w, e, SPos{X: 1})

	var seenInside float32
	flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			// Queue a mutation. The outer Defer ensures it's not yet applied.
			flecs.Set[SPos](w, e, SPos{X: 99})
			v, _ := flecs.Get[SPos](w, e)
			seenInside = v.X
		}
	})

	w.Progress(0)

	if seenInside != 1 {
		t.Fatalf("expected old value 1 inside fn (mutation deferred), got %v", seenInside)
	}
	v, _ := flecs.Get[SPos](w, e)
	if v.X != 99 {
		t.Fatalf("expected new value 99 after Progress, got %v", v.X)
	}
}

// TestSystemMultipleProgressCalls verifies sustained integration over 100 frames.
func TestSystemMultipleProgressCalls(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[SPos](w)
	velID := flecs.RegisterComponent[SVel](w)
	q := flecs.NewCachedQuery(w, posID, velID)

	e := w.NewEntity()
	flecs.Set[SPos](w, e, SPos{})
	flecs.Set[SVel](w, e, SVel{X: 1, Y: 0})

	flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			positions := flecs.Field[SPos](it, posID)
			velocities := flecs.Field[SVel](it, velID)
			for i := range positions {
				positions[i].X += velocities[i].X * dt
			}
		}
	})

	const dt float32 = 0.016
	const frames = 100
	for range frames {
		w.Progress(dt)
	}

	pos, _ := flecs.Get[SPos](w, e)
	want := float32(1) * dt * frames
	diff := pos.X - want
	if diff < 0 {
		diff = -diff
	}
	if diff > 1e-3 {
		t.Fatalf("expected X≈%v after %d frames, got %v", want, frames, pos.X)
	}
}

// TestSystemNoNextCall verifies that a system that never calls it.Next() is safe.
func TestSystemNoNextCall(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[SPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[SPos](w, e, SPos{X: 7})

	ran := false
	flecs.NewSystem(w, q, func(_ float32, _ *flecs.QueryIter) {
		ran = true
		// Intentionally does not call it.Next().
	})

	w.Progress(0) // must not panic

	if !ran {
		t.Fatal("expected fn to be called")
	}
	// Entity position must be unchanged.
	pos, _ := flecs.Get[SPos](w, e)
	if pos.X != 7 {
		t.Fatalf("expected X=7 (unchanged), got %v", pos.X)
	}
}

// TestSystemPanicPropagates verifies that a panicking system propagates the
// panic through Progress, and that the outer Defer's flush still runs.
func TestSystemPanicPropagates(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[SPos](w)
	q := flecs.NewCachedQuery(w, posID)

	// System A sets a value on an entity — queued via the outer Defer.
	eA := w.NewEntity()
	flecs.Set[SPos](w, eA, SPos{})
	flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			flecs.Set[SPos](w, eA, SPos{X: 42})
		}
	})

	// System B panics.
	flecs.NewSystem(w, q, func(_ float32, _ *flecs.QueryIter) {
		panic("intentional panic")
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic from Progress")
		}
		// The outer Defer's DeferEnd must have run: A's queued Set should be applied.
		pos, ok := flecs.Get[SPos](w, eA)
		if !ok || pos.X != 42 {
			t.Fatalf("expected A's mutation to be flushed after panic, got %v ok=%v", pos, ok)
		}
	}()

	w.Progress(0)
}

// TestSystemConstructorPanics verifies nil/invalid arguments are rejected.
func TestSystemConstructorPanics(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[SPos](w)
	q := flecs.NewCachedQuery(w, posID)
	fn := func(_ float32, _ *flecs.QueryIter) {}

	mustPanic := func(name string, f func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: expected panic, got none", name)
			}
		}()
		f()
	}

	mustPanic("nil world", func() { flecs.NewSystem(nil, q, fn) })
	mustPanic("nil query", func() { flecs.NewSystem(w, nil, fn) })
	mustPanic("nil fn", func() { flecs.NewSystem(w, q, nil) })

	closed := flecs.NewCachedQuery(w, posID)
	closed.Close()
	mustPanic("closed query", func() { flecs.NewSystem(w, closed, fn) })

	w2 := flecs.New()
	posID2 := flecs.RegisterComponent[SPos](w2)
	q2 := flecs.NewCachedQuery(w2, posID2)
	mustPanic("wrong world", func() { flecs.NewSystem(w, q2, fn) })
}

// TestSystemSystemCount verifies SystemCount reflects live systems only.
func TestSystemSystemCount(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[SPos](w)
	q := flecs.NewCachedQuery(w, posID)
	fn := func(_ float32, _ *flecs.QueryIter) {}

	if got := w.SystemCount(); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}

	s1 := flecs.NewSystem(w, q, fn)
	flecs.NewSystem(w, q, fn)
	if got := w.SystemCount(); got != 2 {
		t.Fatalf("expected 2, got %d", got)
	}

	s1.Close()
	if got := w.SystemCount(); got != 1 {
		t.Fatalf("expected 1 after Close, got %d", got)
	}
}

// TestSystemMultipleSystemsSameQuery verifies that two systems on the same query
// each get independent iterators.
func TestSystemMultipleSystemsSameQuery(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[SPos](w)
	q := flecs.NewCachedQuery(w, posID)

	e := w.NewEntity()
	flecs.Set[SPos](w, e, SPos{X: 1})

	var sumA, sumB float32
	flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			for _, p := range flecs.Field[SPos](it, posID) {
				sumA += p.X
			}
		}
	})
	flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			for _, p := range flecs.Field[SPos](it, posID) {
				sumB += p.X
			}
		}
	})

	w.Progress(0)
	if sumA != 1 || sumB != 1 {
		t.Fatalf("expected sumA=1, sumB=1, got %v, %v", sumA, sumB)
	}
}

// TestSystemIsClosed verifies IsClosed reflects state correctly.
func TestSystemIsClosed(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[SPos](w)
	q := flecs.NewCachedQuery(w, posID)
	s := flecs.NewSystem(w, q, func(_ float32, _ *flecs.QueryIter) {})

	if s.IsClosed() {
		t.Fatal("expected IsClosed=false before Close")
	}
	s.Close()
	if !s.IsClosed() {
		t.Fatal("expected IsClosed=true after Close")
	}
}
