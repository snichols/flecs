package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// helper: world with one entity carrying a Tag component and a cached query for it.
func newLifecycleWorld(t *testing.T) (*flecs.World, flecs.ID) {
	t.Helper()
	type Tag struct{}
	w := flecs.New()
	tagID := flecs.RegisterComponent[Tag](w)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Tag{})
	})
	return w, tagID
}

// TestSystemLifecycle_DisableSkipsPipeline verifies that a disabled system is
// not invoked by Progress while an enabled peer still runs.
func TestSystemLifecycle_DisableSkipsPipeline(t *testing.T) {
	w, tagID := newLifecycleWorld(t)
	q := flecs.NewCachedQuery(w, tagID)

	var runA, runB int
	flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			runA++
		}
	})
	sysB := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			runB++
		}
	})

	sysB.SetEnabled(false)
	w.Progress(0.016)

	if runA != 1 {
		t.Errorf("enabled system A: runs = %d, want 1", runA)
	}
	if runB != 0 {
		t.Errorf("disabled system B: runs = %d, want 0", runB)
	}
}

// TestSystemLifecycle_ReEnableRuns verifies that re-enabling a disabled system
// causes it to run on the next Progress call.
func TestSystemLifecycle_ReEnableRuns(t *testing.T) {
	w, tagID := newLifecycleWorld(t)
	q := flecs.NewCachedQuery(w, tagID)

	var runs int
	sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			runs++
		}
	})

	sys.SetEnabled(false)
	w.Progress(0.016)
	if runs != 0 {
		t.Fatalf("disabled: runs = %d, want 0", runs)
	}

	sys.SetEnabled(true)
	w.Progress(0.016)
	if runs != 1 {
		t.Errorf("re-enabled: runs = %d, want 1", runs)
	}
}

// TestSystemLifecycle_IsEnabledState verifies IsEnabled across toggle cycles.
func TestSystemLifecycle_IsEnabledState(t *testing.T) {
	w, tagID := newLifecycleWorld(t)
	q := flecs.NewCachedQuery(w, tagID)
	sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
		}
	})

	if !sys.IsEnabled() {
		t.Error("newly created system: IsEnabled() = false, want true")
	}

	sys.SetEnabled(false)
	if sys.IsEnabled() {
		t.Error("after SetEnabled(false): IsEnabled() = true, want false")
	}

	sys.SetEnabled(true)
	if !sys.IsEnabled() {
		t.Error("after SetEnabled(true): IsEnabled() = false, want true")
	}
}

// TestSystemLifecycle_DisableMidProgress verifies that flipping a system's
// enabled flag from within another system's callback takes effect starting
// from the next Progress call, not the current frame. (Snapshot semantics
// match Close — the active set is captured before dispatch begins.)
func TestSystemLifecycle_DisableMidProgress(t *testing.T) {
	w, tagID := newLifecycleWorld(t)
	q := flecs.NewCachedQuery(w, tagID)

	var runB int
	var sysB *flecs.System

	// sysA disables sysB while Progress is running.
	flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			sysB.SetEnabled(false)
		}
	})
	sysB = flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			runB++
		}
	})

	// Frame 1: sysB is in the snapshot and may still run (snapshot semantics).
	w.Progress(0.016)
	runAfterFrame1 := runB

	// Frame 2: sysB is definitively disabled — it must not run.
	w.Progress(0.016)

	if runB != runAfterFrame1 {
		t.Errorf("frame 2: sysB ran after being disabled mid-frame; runs went from %d to %d", runAfterFrame1, runB)
	}
}

// TestSystemLifecycle_RunSystem verifies that RunSystem invokes the system
// callback once with the supplied dt and that side effects are visible after
// the call returns.
func TestSystemLifecycle_RunSystem(t *testing.T) {
	type Counter struct{ N int }
	w := flecs.New()
	ctrID := flecs.RegisterComponent[Counter](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Counter{N: 0})
	})

	var gotDT float32
	q := flecs.NewCachedQuery(w, ctrID)
	sys := flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		gotDT = dt
		for it.Next() {
			counters := flecs.Field[Counter](it, ctrID)
			for i := range counters {
				counters[i].N++
			}
		}
	})

	const wantDT = float32(0.25)
	flecs.RunSystem(sys, wantDT)

	if gotDT != wantDT {
		t.Errorf("RunSystem dt = %v, want %v", gotDT, wantDT)
	}

	w.Read(func(r *flecs.Reader) {
		c, ok := flecs.Get[Counter](r, e)
		if !ok {
			t.Fatal("Counter not found after RunSystem")
		}
		if c.N != 1 {
			t.Errorf("Counter.N = %d, want 1", c.N)
		}
	})
}

// TestSystemLifecycle_RunSystemDisabledStillRuns verifies that RunSystem
// executes a disabled system (explicit invocation overrides the pipeline flag).
func TestSystemLifecycle_RunSystemDisabledStillRuns(t *testing.T) {
	w, tagID := newLifecycleWorld(t)
	q := flecs.NewCachedQuery(w, tagID)

	var runs int
	sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			runs++
		}
	})
	sys.SetEnabled(false)

	flecs.RunSystem(sys, 0.016)

	if runs != 1 {
		t.Errorf("RunSystem on disabled system: runs = %d, want 1", runs)
	}
}

// TestSystemLifecycle_RunSystemMutationsFlushed verifies that structural
// mutations made inside RunSystem are applied (flushed) before RunSystem returns.
func TestSystemLifecycle_RunSystemMutationsFlushed(t *testing.T) {
	type Tag struct{}
	type Marker struct{}
	w := flecs.New()
	tagID := flecs.RegisterComponent[Tag](w)
	_ = flecs.RegisterComponent[Marker](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Tag{})
	})

	q := flecs.NewCachedQuery(w, tagID)
	sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			flecs.Set(it.Writer(), e, Marker{})
		}
	})

	flecs.RunSystem(sys, 0.016)

	// Marker must be visible immediately after RunSystem (not pending flush).
	w.Read(func(r *flecs.Reader) {
		if !flecs.Has[Marker](r, e) {
			t.Error("Marker component not visible after RunSystem — deferred mutations not flushed")
		}
	})
}

// TestSystemLifecycle_Phases verifies that Phases() returns the four built-in
// phase IDs in the documented execution order.
func TestSystemLifecycle_Phases(t *testing.T) {
	w := flecs.New()
	w.Read(func(r *flecs.Reader) {
		phases := r.Phases()
		if len(phases) != 4 {
			t.Fatalf("Phases() len = %d, want 4", len(phases))
		}
		want := []flecs.ID{w.PreUpdate(), w.OnFixedUpdate(), w.OnUpdate(), w.PostUpdate()}
		for i, id := range want {
			if phases[i] != id {
				t.Errorf("Phases()[%d] = %d, want %d", i, phases[i], id)
			}
		}
	})
}

// TestSystemLifecycle_SystemsInPhaseOrder verifies that SystemsInPhase returns
// systems in registration order.
func TestSystemLifecycle_SystemsInPhaseOrder(t *testing.T) {
	w, tagID := newLifecycleWorld(t)
	q := flecs.NewCachedQuery(w, tagID)

	var order []int
	var systems [3]*flecs.System
	for i := range 3 {
		i := i
		systems[i] = flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
			for it.Next() {
				order = append(order, i)
			}
		})
	}

	w.Read(func(r *flecs.Reader) {
		got := r.SystemsInPhase(w.OnUpdate())
		if len(got) != 3 {
			t.Fatalf("SystemsInPhase(OnUpdate) len = %d, want 3", len(got))
		}
		for i, s := range got {
			if s != systems[i] {
				t.Errorf("systems[%d]: got %p, want %p", i, s, systems[i])
			}
		}
	})
}

// TestSystemLifecycle_SystemsInPhaseEmpty verifies that SystemsInPhase returns
// a non-nil empty slice when no systems are registered for the phase.
func TestSystemLifecycle_SystemsInPhaseEmpty(t *testing.T) {
	w := flecs.New()
	w.Read(func(r *flecs.Reader) {
		got := r.SystemsInPhase(w.PostUpdate())
		if got == nil {
			t.Error("SystemsInPhase for empty phase returned nil, want empty slice")
		}
		if len(got) != 0 {
			t.Errorf("SystemsInPhase for empty phase len = %d, want 0", len(got))
		}
	})
}

// TestSystemLifecycle_SystemsInPhaseIncludesDisabled verifies that
// SystemsInPhase lists disabled systems alongside enabled ones.
func TestSystemLifecycle_SystemsInPhaseIncludesDisabled(t *testing.T) {
	w, tagID := newLifecycleWorld(t)
	q := flecs.NewCachedQuery(w, tagID)

	const total = 4
	const disabled = 2
	for i := range total {
		sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
			for it.Next() {
			}
		})
		if i < disabled {
			sys.SetEnabled(false)
		}
	}

	w.Read(func(r *flecs.Reader) {
		got := r.SystemsInPhase(w.OnUpdate())
		if len(got) != total {
			t.Errorf("SystemsInPhase len = %d, want %d (includes disabled)", len(got), total)
		}
	})
}

// TestSystemLifecycle_EachSystemHalt verifies that EachSystem stops calling fn
// when fn returns false.
func TestSystemLifecycle_EachSystemHalt(t *testing.T) {
	w, tagID := newLifecycleWorld(t)
	q := flecs.NewCachedQuery(w, tagID)

	for range 5 {
		flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
			for it.Next() {
			}
		})
	}

	var seen int
	w.Read(func(r *flecs.Reader) {
		r.EachSystem(w.OnUpdate(), func(_ *flecs.System) bool {
			seen++
			return seen < 2 // halt after 2
		})
	})

	if seen != 2 {
		t.Errorf("EachSystem halted after %d calls, want 2", seen)
	}
}

// TestSystemLifecycle_SystemsInPhasePanicsOnBadPhase verifies that
// SystemsInPhase panics when given a non-built-in phase ID.
func TestSystemLifecycle_SystemsInPhasePanicsOnBadPhase(t *testing.T) {
	w := flecs.New()
	defer func() {
		if r := recover(); r == nil {
			t.Error("SystemsInPhase with invalid phase ID did not panic")
		}
	}()
	w.Read(func(r *flecs.Reader) {
		r.SystemsInPhase(flecs.ID(9999))
	})
}
