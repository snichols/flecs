package docs_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/snichols/flecs"
)

// TestSystems_Basic verifies the introductory snippet from Systems.md:
// create a system, run one frame, and confirm Position is updated by dt.
func TestSystems_Basic(t *testing.T) {
	type Position struct{ X, Y float32 }
	type Velocity struct{ DX, DY float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 0, Y: 0})
		flecs.Set(fw, e, Velocity{DX: 10, DY: 5})
	})

	moveQ := flecs.NewCachedQuery(w, posID, velID)
	flecs.NewSystem(w, moveQ, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			positions := flecs.Field[Position](it, posID)
			velocities := flecs.Field[Velocity](it, velID)
			for i := range positions {
				positions[i].X += velocities[i].DX * dt
				positions[i].Y += velocities[i].DY * dt
			}
		}
	})

	const dt = 1.0 / 60.0
	w.Progress(dt)

	w.Read(func(r *flecs.Reader) {
		p, ok := flecs.Get[Position](r, e)
		if !ok {
			t.Fatal("Position not found after Progress")
		}
		wantX := float32(10 * dt)
		wantY := float32(5 * dt)
		if p.X != wantX {
			t.Errorf("Position.X = %v, want %v", p.X, wantX)
		}
		if p.Y != wantY {
			t.Errorf("Position.Y = %v, want %v", p.Y, wantY)
		}
	})
}

// TestSystems_PhaseOrder verifies that systems registered in different phases
// run in the canonical order: PreUpdate → OnUpdate → PostUpdate.
func TestSystems_PhaseOrder(t *testing.T) {
	type Tag struct{}

	w := flecs.New()
	tagID := flecs.RegisterComponent[Tag](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Tag{})
	})

	var order []string

	q := flecs.NewCachedQuery(w, tagID)
	flecs.NewSystemInPhase(w, w.PostUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "PostUpdate")
		}
	})
	flecs.NewSystemInPhase(w, w.PreUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "PreUpdate")
		}
	})
	flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "OnUpdate")
		}
	})

	w.Progress(0.016)

	want := []string{"PreUpdate", "OnUpdate", "PostUpdate"}
	if len(order) != len(want) {
		t.Fatalf("phase order: got %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("order[%d] = %q, want %q", i, order[i], want[i])
		}
	}
}

// TestSystems_DeltaTime verifies that the dt value passed to Progress reaches
// the system callback unchanged.
func TestSystems_DeltaTime(t *testing.T) {
	type Tag struct{}

	w := flecs.New()
	tagID := flecs.RegisterComponent[Tag](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Tag{})
	})
	_ = e

	var got float32
	q := flecs.NewCachedQuery(w, tagID)
	flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			got = dt
		}
	})

	const want = 1.0 / 30.0
	w.Progress(want)

	if got != want {
		t.Errorf("callback dt = %v, want %v", got, want)
	}
}

// TestSystems_FixedTimestep verifies the OnFixedUpdate accumulator behavior:
// Progress(0.3) with a step of 0.1 fires the fixed-update callback 3 times.
func TestSystems_FixedTimestep(t *testing.T) {
	type Tag struct{}

	w := flecs.New()
	tagID := flecs.RegisterComponent[Tag](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Tag{})
	})
	_ = e

	w.SetFixedTimestep(0.1)

	var runs int
	q := flecs.NewCachedQuery(w, tagID)
	flecs.NewSystemInPhase(w, w.OnFixedUpdate(), q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			runs++
		}
	})

	w.Progress(0.3) // 0.3/0.1 = 3 fixed ticks

	if runs != 3 {
		t.Errorf("fixed-update runs = %d, want 3", runs)
	}
}

// TestSystems_FixedTimestepDT verifies that the dt delivered to OnFixedUpdate
// callbacks equals the configured step size, not the variable frame dt.
func TestSystems_FixedTimestepDT(t *testing.T) {
	type Tag struct{}

	w := flecs.New()
	tagID := flecs.RegisterComponent[Tag](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Tag{})
	})
	_ = e

	const step = float32(1.0 / 60.0)
	w.SetFixedTimestep(step)

	var got float32
	q := flecs.NewCachedQuery(w, tagID)
	flecs.NewSystemInPhase(w, w.OnFixedUpdate(), q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			got = dt
		}
	})

	w.Progress(step) // exactly one fixed tick

	if got != step {
		t.Errorf("fixed-update dt = %v, want %v", got, step)
	}
}

// TestSystems_Close verifies that a system stops running after Close is called.
func TestSystems_Close(t *testing.T) {
	type Tag struct{}

	w := flecs.New()
	tagID := flecs.RegisterComponent[Tag](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Tag{})
	})
	_ = e

	var runs int
	q := flecs.NewCachedQuery(w, tagID)
	sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			runs++
		}
	})

	w.Progress(0.016)
	if runs != 1 {
		t.Fatalf("before Close: runs = %d, want 1", runs)
	}

	sys.Close()
	if !sys.IsClosed() {
		t.Fatal("IsClosed() returned false after Close()")
	}

	w.Progress(0.016)
	if runs != 1 {
		t.Errorf("after Close: runs = %d, still want 1", runs)
	}
}

// TestSystems_WriteSet verifies that SetWriteSet with an empty slice marks the
// system as read-only (no conflicts) and that SetWriteSet with explicit IDs
// replaces the derived write set. The test confirms the system still runs.
func TestSystems_WriteSet(t *testing.T) {
	type Position struct{ X, Y float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 1, Y: 2})
	})
	_ = e

	var ran bool
	q := flecs.NewCachedQuery(w, posID)
	sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			ran = true
		}
	})
	// Declare explicit write set — only posID.
	sys.SetWriteSet([]flecs.ID{posID})

	w.Progress(0.016)

	if !ran {
		t.Error("system with explicit write set did not run")
	}
}

// TestSystems_WriteSetEmpty verifies that a system with an empty write set
// (declared read-only) still runs its callback normally.
func TestSystems_WriteSetEmpty(t *testing.T) {
	type Position struct{ X float32 }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 5})
	})
	_ = e

	var ran bool
	q := flecs.NewCachedQuery(w, posID)
	sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			ran = true
		}
	})
	sys.SetWriteSet([]flecs.ID{}) // read-only

	w.Progress(0.016)

	if !ran {
		t.Error("read-only system (empty write set) did not run")
	}
}

// TestSystems_Parallel verifies that two parallel systems with disjoint write sets
// both run to completion when worker goroutines are configured.
func TestSystems_Parallel(t *testing.T) {
	type Position struct{ X float32 }
	type Health struct{ HP int }

	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	healthID := flecs.RegisterComponent[Health](w)
	w.SetWorkerCount(2)

	w.Write(func(fw *flecs.Writer) {
		for range 10 {
			e := fw.NewEntity()
			flecs.Set(fw, e, Position{X: 0})
			flecs.Set(fw, e, Health{HP: 100})
		}
	})

	var (
		posRan    int64
		healthRan int64
	)

	posQ := flecs.NewCachedQuery(w, posID)
	sysA := flecs.NewSystem(w, posQ, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			positions := flecs.Field[Position](it, posID)
			for i := range positions {
				positions[i].X += dt
			}
			atomic.AddInt64(&posRan, int64(len(positions)))
		}
	})
	sysA.SetParallel(true)

	hpQ := flecs.NewCachedQuery(w, healthID)
	sysB := flecs.NewSystem(w, hpQ, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			atomic.AddInt64(&healthRan, int64(len(flecs.Field[Health](it, healthID))))
		}
	})
	sysB.SetParallel(true)

	w.Progress(0.016)

	w.SetWorkerCount(0) // shut down workers before test ends

	if posRan == 0 {
		t.Error("parallel Position system did not run")
	}
	if healthRan == 0 {
		t.Error("parallel Health system did not run")
	}
}

// TestSystems_MultiThreaded verifies that a multi-threaded system processes
// all entities exactly once across all worker slices.
func TestSystems_MultiThreaded(t *testing.T) {
	type Counter struct{ N int32 }

	w := flecs.New()
	ctrID := flecs.RegisterComponent[Counter](w)
	w.SetWorkerCount(4)

	const entityCount = 100
	w.Write(func(fw *flecs.Writer) {
		for range entityCount {
			e := fw.NewEntity()
			flecs.Set(fw, e, Counter{N: 0})
		}
	})

	// Each entity's counter is incremented; no two workers touch the same row.
	q := flecs.NewCachedQuery(w, ctrID)
	sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			counters := flecs.Field[Counter](it, ctrID)
			for i := range counters {
				counters[i].N++
			}
		}
	})
	sys.SetMultiThreaded(true)

	w.Progress(0.016)
	w.SetWorkerCount(0)

	// Verify all counters incremented exactly once.
	var total int32
	w.Read(func(r *flecs.Reader) {
		flecs.Each1[Counter](r, func(_ flecs.ID, c *Counter) {
			total += c.N
		})
	})
	if total != entityCount {
		t.Errorf("total counter increments = %d, want %d", total, entityCount)
	}
}

// TestSystems_Stats verifies that World.Stats() reflects the correct frame count
// and per-phase system counts after a Progress call.
func TestSystems_Stats(t *testing.T) {
	type Tag struct{}

	w := flecs.New()
	tagID := flecs.RegisterComponent[Tag](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Tag{})
	})
	_ = e

	q := flecs.NewCachedQuery(w, tagID)
	flecs.NewSystemInPhase(w, w.PreUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
		}
	})
	flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
		}
	})

	w.Progress(0.016)

	w.Read(func(r *flecs.Reader) {
		s := w.Stats()
		if s.FrameCount != 1 {
			t.Errorf("FrameCount = %d, want 1", s.FrameCount)
		}
		if s.SystemCount != 2 {
			t.Errorf("SystemCount = %d, want 2", s.SystemCount)
		}
		if len(s.LastFramePhases) != 4 {
			t.Fatalf("LastFramePhases len = %d, want 4", len(s.LastFramePhases))
		}
		// Phase 0 = PreUpdate (1 system), Phase 2 = OnUpdate (1 system).
		if s.LastFramePhases[0].Name != "PreUpdate" {
			t.Errorf("phase[0].Name = %q, want PreUpdate", s.LastFramePhases[0].Name)
		}
		if s.LastFramePhases[0].SystemCount != 1 {
			t.Errorf("phase[0].SystemCount = %d, want 1", s.LastFramePhases[0].SystemCount)
		}
		if s.LastFramePhases[2].Name != "OnUpdate" {
			t.Errorf("phase[2].Name = %q, want OnUpdate", s.LastFramePhases[2].Name)
		}
		if s.LastFramePhases[2].SystemCount != 1 {
			t.Errorf("phase[2].SystemCount = %d, want 1", s.LastFramePhases[2].SystemCount)
		}
	})
}

// TestSystems_SystemCountInPhase verifies that SystemCountInPhase returns the
// correct per-phase active system count.
func TestSystems_SystemCountInPhase(t *testing.T) {
	type Tag struct{}

	w := flecs.New()
	tagID := flecs.RegisterComponent[Tag](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Tag{})
	})
	_ = e

	q := flecs.NewCachedQuery(w, tagID)
	flecs.NewSystemInPhase(w, w.PreUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
		}
	})
	flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
		}
	})
	flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
		}
	})

	w.Read(func(r *flecs.Reader) {
		if n := w.SystemCountInPhase(w.PreUpdate()); n != 1 {
			t.Errorf("PreUpdate count = %d, want 1", n)
		}
		if n := w.SystemCountInPhase(w.OnUpdate()); n != 2 {
			t.Errorf("OnUpdate count = %d, want 2", n)
		}
		if n := w.SystemCountInPhase(w.PostUpdate()); n != 0 {
			t.Errorf("PostUpdate count = %d, want 0", n)
		}
	})
}

// TestSystems_SetEnabled verifies the code example in the "Disabling a System"
// section of Systems.md: SetEnabled(false) pauses, SetEnabled(true) resumes.
func TestSystems_SetEnabled(t *testing.T) {
	type Tag struct{}

	w := flecs.New()
	tagID := flecs.RegisterComponent[Tag](w)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Tag{})
	})

	var runs int
	q := flecs.NewCachedQuery(w, tagID)
	sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			runs++
		}
	})

	sys.SetEnabled(false)
	if sys.IsEnabled() {
		t.Error("IsEnabled() = true after SetEnabled(false)")
	}

	w.Progress(0.016)
	if runs != 0 {
		t.Errorf("disabled system runs = %d, want 0", runs)
	}

	sys.SetEnabled(true)
	w.Progress(0.016)
	if runs != 1 {
		t.Errorf("re-enabled system runs = %d, want 1", runs)
	}
}

// TestSystems_RunSystem verifies the code example in the "Single-system Run"
// section of Systems.md: RunSystem invokes once with the given dt.
func TestSystems_RunSystem(t *testing.T) {
	type Counter struct{ N int }

	w := flecs.New()
	ctrID := flecs.RegisterComponent[Counter](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Counter{N: 0})
	})

	q := flecs.NewCachedQuery(w, ctrID)
	sys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			counters := flecs.Field[Counter](it, ctrID)
			for i := range counters {
				counters[i].N++
			}
		}
	})

	flecs.RunSystem(sys, 1.0/60.0)

	w.Read(func(r *flecs.Reader) {
		c, ok := flecs.Get[Counter](r, e)
		if !ok {
			t.Fatal("Counter not found")
		}
		if c.N != 1 {
			t.Errorf("Counter.N = %d after RunSystem, want 1", c.N)
		}
	})
}

// TestSystems_PipelineIntrospection verifies the code examples in the
// "Pipeline Introspection" section of Systems.md.
func TestSystems_PipelineIntrospection(t *testing.T) {
	type Tag struct{}

	w := flecs.New()
	tagID := flecs.RegisterComponent[Tag](w)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Tag{})
	})

	q := flecs.NewCachedQuery(w, tagID)
	sysA := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
		}
	})
	sysB := flecs.NewSystemInPhase(w, w.PreUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
		}
	})
	sysB.SetEnabled(false) // introspection must still list it

	w.Read(func(r *flecs.Reader) {
		// Phases() returns four IDs in execution order.
		phases := r.Phases()
		if len(phases) != 4 {
			t.Fatalf("Phases() len = %d, want 4", len(phases))
		}
		if phases[0] != w.PreUpdate() {
			t.Errorf("phases[0] = %v, want PreUpdate", phases[0])
		}
		if phases[2] != w.OnUpdate() {
			t.Errorf("phases[2] = %v, want OnUpdate", phases[2])
		}

		// SystemsInPhase lists all non-closed systems including disabled.
		onUpdateSystems := r.SystemsInPhase(w.OnUpdate())
		if len(onUpdateSystems) != 1 || onUpdateSystems[0] != sysA {
			t.Errorf("SystemsInPhase(OnUpdate) = %v, want [sysA]", onUpdateSystems)
		}
		preUpdateSystems := r.SystemsInPhase(w.PreUpdate())
		if len(preUpdateSystems) != 1 || preUpdateSystems[0] != sysB {
			t.Errorf("SystemsInPhase(PreUpdate) = %v, want [sysB]", preUpdateSystems)
		}

		// EachSystem halts on false return.
		var seen int
		r.EachSystem(w.OnUpdate(), func(_ *flecs.System) bool {
			seen++
			return false
		})
		if seen != 1 {
			t.Errorf("EachSystem halted after %d calls, want 1", seen)
		}
	})
}

// TestSystems_RateFilters verifies the code examples in the "Rate Filters"
// section of Systems.md: SetInterval and SetRate reduce system run frequency.
func TestSystems_RateFilters(t *testing.T) {
	type Tag struct{}

	w := flecs.New()
	tagID := flecs.RegisterComponent[Tag](w)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set(fw, e, Tag{})
	})

	q := flecs.NewCachedQuery(w, tagID)

	// SetRate(4): system fires every 4th tick.
	rateRuns := 0
	rateSys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			rateRuns++
		}
	})
	rateSys.SetRate(4)
	if rateSys.GetRate() != 4 {
		t.Errorf("GetRate() = %d, want 4", rateSys.GetRate())
	}

	for i := 0; i < 8; i++ {
		w.Progress(1.0 / 60.0)
	}
	if rateRuns != 2 {
		t.Errorf("rate=4 system ran %d times in 8 ticks, want 2", rateRuns)
	}

	// SetInterval(200ms) with dt=100ms: fires every 2nd tick.
	rateSys.SetEnabled(false) // park the rate system

	intervalRuns := 0
	intSys := flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			intervalRuns++
		}
	})
	intSys.SetInterval(200 * time.Millisecond)
	if intSys.GetInterval() != 200*time.Millisecond {
		t.Errorf("GetInterval() = %v, want 200ms", intSys.GetInterval())
	}

	for i := 0; i < 6; i++ {
		w.Progress(0.100) // 100ms each tick
	}
	// fires at accumulated 200ms, 400ms, 600ms → 3 fires
	if intervalRuns != 3 {
		t.Errorf("interval=200ms system ran %d times in 6×100ms ticks, want 3", intervalRuns)
	}
}
