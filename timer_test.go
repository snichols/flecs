package flecs_test

import (
	"math"
	"testing"

	"github.com/snichols/flecs"
)

// ── Time and FrameCount ───────────────────────────────────────────────────────

func TestTimerTimeAccumulates(t *testing.T) {
	w := flecs.New()
	w.Progress(0.016)
	w.Progress(0.016)
	w.Progress(0.016)
	got := w.Time()
	want := float32(0.048)
	if math.Abs(float64(got-want)) > 1e-5 {
		t.Fatalf("Time(): want ~%.4f, got %.4f", want, got)
	}
}

func TestTimerFrameCountIncrements(t *testing.T) {
	w := flecs.New()
	if w.FrameCount() != 0 {
		t.Fatalf("initial FrameCount: want 0, got %d", w.FrameCount())
	}
	for i := 0; i < 5; i++ {
		w.Progress(0.016)
	}
	if w.FrameCount() != 5 {
		t.Fatalf("FrameCount after 5 Progress calls: want 5, got %d", w.FrameCount())
	}
}

// ── SetFixedTimestep / FixedTimestep ─────────────────────────────────────────

func TestTimerSetFixedTimestepRoundTrip(t *testing.T) {
	w := flecs.New()
	step := float32(1.0 / 60.0)
	w.SetFixedTimestep(step)
	if got := w.FixedTimestep(); math.Abs(float64(got-step)) > 1e-7 {
		t.Fatalf("FixedTimestep round-trip: want %v, got %v", step, got)
	}
}

func TestTimerSetFixedTimestepZeroDisables(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[PPos](w.W(), e, PPos{X: 0})

	runs := 0
	flecs.NewSystemInPhase(w, w.OnFixedUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			runs++
		}
	})

	// step==0: disabled — even a large dt must produce zero runs
	w.SetFixedTimestep(0)
	w.Progress(10.0)
	if runs != 0 {
		t.Fatalf("disabled fixed step: expected 0 runs, got %d", runs)
	}
}

func TestTimerSetFixedTimestepNegativePanics(t *testing.T) {
	w := flecs.New()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for negative step, got none")
		}
	}()
	w.SetFixedTimestep(-1)
}

func TestTimerProgressNegativeDTPanics(t *testing.T) {
	w := flecs.New()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for negative dt, got none")
		}
	}()
	w.Progress(-0.001)
}

// ── OnFixedUpdate dispatch counts ────────────────────────────────────────────

func TestTimerFixedUpdateExactlyOncePerStep(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[PPos](w.W(), e, PPos{X: 0})

	runs := 0
	flecs.NewSystemInPhase(w, w.OnFixedUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			runs++
		}
	})

	w.SetFixedTimestep(0.1)
	w.Progress(0.1) // acc 0→0.1 → 1 run; carry ≈0
	if runs != 1 {
		t.Fatalf("after Progress(0.1) with step=0.1: want 1 run, got %d", runs)
	}
	w.Progress(0.05) // acc ≈0+0.05 < 0.1 → 0 runs
	if runs != 1 {
		t.Fatalf("after Progress(0.05): want still 1 run, got %d", runs)
	}
	w.Progress(0.05) // acc ≈0.05+0.05 ≥ 0.1 → 1 more run
	if runs != 2 {
		t.Fatalf("after second Progress(0.05): want 2 runs total, got %d", runs)
	}
}

func TestTimerFixedUpdateMultiplePerProgress(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[PPos](w.W(), e, PPos{X: 0})

	runs := 0
	flecs.NewSystemInPhase(w, w.OnFixedUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			runs++
		}
	})

	w.SetFixedTimestep(0.1)
	w.Progress(0.3) // 0.3/0.1 = 3 fixed iterations
	if runs != 3 {
		t.Fatalf("Progress(0.3) with step=0.1: want 3 runs, got %d", runs)
	}
}

// ── dt values seen by systems ─────────────────────────────────────────────────

func TestTimerFixedUpdateDTIsAlwaysStep(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[PPos](w.W(), e, PPos{X: 0})

	step := float32(1.0 / 60.0)
	w.SetFixedTimestep(step)

	var capturedDTs []float32
	flecs.NewSystemInPhase(w, w.OnFixedUpdate(), q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			capturedDTs = append(capturedDTs, dt)
		}
	})

	// Run 3 fixed iterations in one Progress.
	w.Progress(step * 3)
	if len(capturedDTs) != 3 {
		t.Fatalf("expected 3 captured dts, got %d", len(capturedDTs))
	}
	for i, dt := range capturedDTs {
		if math.Abs(float64(dt-step)) > 1e-7 {
			t.Fatalf("capturedDTs[%d]: want %v (step), got %v", i, step, dt)
		}
	}
}

func TestTimerVariableUpdateSeesRealDT(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[PPos](w.W(), e, PPos{X: 0})

	var capturedDT float32
	flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			capturedDT = dt
		}
	})

	frameDT := float32(0.033)
	w.Progress(frameDT)
	if math.Abs(float64(capturedDT-frameDT)) > 1e-7 {
		t.Fatalf("OnUpdate captured dt: want %v, got %v", frameDT, capturedDT)
	}
}

// ── Phase order with OnFixedUpdate ───────────────────────────────────────────

func TestTimerPhaseOrderWithFixed(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[PPos](w.W(), e, PPos{X: 0})

	var order []string
	flecs.NewSystemInPhase(w, w.PreUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "pre")
		}
	})
	flecs.NewSystemInPhase(w, w.OnFixedUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "fixed")
		}
	})
	flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "on")
		}
	})
	flecs.NewSystemInPhase(w, w.PostUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "post")
		}
	})

	step := float32(0.1)
	w.SetFixedTimestep(step)
	w.Progress(step) // exactly 1 fixed tick

	want := []string{"pre", "fixed", "on", "post"}
	if len(order) != len(want) {
		t.Fatalf("phase order: want %v, got %v", want, order)
	}
	for i, v := range want {
		if order[i] != v {
			t.Fatalf("phase order[%d]: want %q, got %q (full: %v)", i, v, order[i], order)
		}
	}
}

// ── Per-iteration Defer (between-tick mutation visibility) ───────────────────

func TestTimerFixedUpdatePerIterationDefer(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[PPos](w.W(), e, PPos{X: 0})

	// Each fixed tick: read current X, write X+1. With per-iteration Defer the
	// write from tick N is visible (flushed) at tick N+1's read time.
	var seenXValues []float32
	flecs.NewSystemInPhase(w, w.OnFixedUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			v, _ := flecs.Get[PPos](w.R(), e)
			seenXValues = append(seenXValues, v.X)
			flecs.Set[PPos](w.W(), e, PPos{X: v.X + 1})
		}
	})

	step := float32(0.1)
	w.SetFixedTimestep(step)
	w.Progress(step * 3) // 3 ticks

	// Tick 0 sees X=0 (initial); tick 1 sees X=1 (flushed from tick 0); tick 2 sees X=2.
	if len(seenXValues) != 3 {
		t.Fatalf("expected 3 ticks, got %d", len(seenXValues))
	}
	for i, want := range []float32{0, 1, 2} {
		if seenXValues[i] != want {
			t.Fatalf("tick %d: want X=%.0f, got X=%.0f", i, want, seenXValues[i])
		}
	}
}

// ── Fractional accumulator carry ─────────────────────────────────────────────

func TestTimerAccumulatorFractionalCarry(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[PPos](w.W(), e, PPos{X: 0})

	runs := 0
	flecs.NewSystemInPhase(w, w.OnFixedUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			runs++
		}
	})

	// 60 frames at ~50fps (dt=0.025s), step=1/60s ≈ 0.01667s
	// Expected fixed ticks: 60 * 0.025 / (1/60) = 60 * 0.025 * 60 = 90
	step := float32(1.0 / 60.0)
	w.SetFixedTimestep(step)
	for i := 0; i < 60; i++ {
		w.Progress(0.025)
	}
	// Allow ±1 for float32 rounding drift.
	if runs < 89 || runs > 91 {
		t.Fatalf("60 frames @ 50fps with step=1/60: expected ~90 fixed ticks, got %d", runs)
	}
}

// ── Disabled fixed timestep: system never runs ────────────────────────────────

func TestTimerOnFixedUpdateNeverRunsWhenDisabled(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[PPos](w.W(), e, PPos{X: 0})

	ran := false
	flecs.NewSystemInPhase(w, w.OnFixedUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			ran = true
		}
	})

	// Default fixedTimestep is 0 (disabled).
	for i := 0; i < 10; i++ {
		w.Progress(0.016)
	}
	if ran {
		t.Fatal("OnFixedUpdate system ran without SetFixedTimestep > 0")
	}
}

// ── Mid-game SetFixedTimestep change ─────────────────────────────────────────

func TestTimerMidGameTimestepChange(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[PPos](w.W(), e, PPos{X: 0})

	runs := 0
	flecs.NewSystemInPhase(w, w.OnFixedUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			runs++
		}
	})

	// Phase 1: 10 frames at 60Hz with step=1/60 → 10 fixed ticks.
	w.SetFixedTimestep(1.0 / 60.0)
	for i := 0; i < 10; i++ {
		w.Progress(1.0 / 60.0)
	}
	runsAfterPhase1 := runs

	// Phase 2: switch to 1/30 step; 10 frames at 60Hz → ~5 ticks per frame → 5 total.
	w.SetFixedTimestep(1.0 / 30.0)
	for i := 0; i < 10; i++ {
		w.Progress(1.0 / 60.0)
	}
	totalRuns := runs

	// Phase 1: ~10 ticks (allow ±1). Phase 2: at 60Hz with step=1/30, each
	// frame has dt=1/60 < step=1/30 so only every other frame fires → ~5 ticks.
	if runsAfterPhase1 < 9 || runsAfterPhase1 > 11 {
		t.Fatalf("phase 1: want ~10 fixed ticks, got %d", runsAfterPhase1)
	}
	phase2Runs := totalRuns - runsAfterPhase1
	if phase2Runs < 4 || phase2Runs > 6 {
		t.Fatalf("phase 2: want ~5 fixed ticks, got %d", phase2Runs)
	}
}

// ── NewSystemInPhase with OnFixedUpdate works ─────────────────────────────────

func TestTimerNewSystemInPhaseOnFixedUpdate(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)
	fn := func(_ float32, _ *flecs.QueryIter) {}

	// Must not panic.
	sys := flecs.NewSystemInPhase(w, w.OnFixedUpdate(), q, fn)
	if sys == nil {
		t.Fatal("NewSystemInPhase returned nil for OnFixedUpdate")
	}
}

// ── NewSystemInPhase still rejects invalid phases ─────────────────────────────

func TestTimerNewSystemInPhaseInvalidPhaseStillPanics(t *testing.T) {
	cases := []struct {
		name  string
		phase func(w *flecs.World) flecs.ID
	}{
		{"ChildOf", func(w *flecs.World) flecs.ID { return w.ChildOf() }},
		{"IsA", func(w *flecs.World) flecs.ID { return w.IsA() }},
		{"NewEntity", func(w *flecs.World) flecs.ID { return w.NewEntity() }},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			w := flecs.New()
			posID := flecs.RegisterComponent[PPos](w)
			q := flecs.NewCachedQuery(w, posID)
			fn := func(_ float32, _ *flecs.QueryIter) {}
			phase := tc.phase(w)

			defer func() {
				if recover() == nil {
					t.Fatalf("expected panic for phase=%v (%s), got none", phase, tc.name)
				}
			}()
			flecs.NewSystemInPhase(w, phase, q, fn)
		})
	}
}
