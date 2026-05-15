package flecs_test

import (
	"encoding/json"
	"testing"
	"time"

	flecs "github.com/snichols/flecs"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// progressMS calls w.Progress with dt = ms milliseconds.
func progressMS(w *flecs.World, ms float32) {
	w.Progress(ms / 1000.0)
}

// getTimer reads the Timer component from entity e inside a Read scope.
func getTimer(w *flecs.World, e flecs.ID) (flecs.Timer, bool) {
	var t flecs.Timer
	var ok bool
	w.Read(func(fr *flecs.Reader) {
		t, ok = flecs.Get[flecs.Timer](fr, e)
	})
	return t, ok
}

// getRateFilter reads the RateFilter component from entity e inside a Read scope.
func getRateFilter(w *flecs.World, e flecs.ID) (flecs.RateFilter, bool) {
	var rf flecs.RateFilter
	var ok bool
	w.Read(func(fr *flecs.Reader) {
		rf, ok = flecs.Get[flecs.RateFilter](fr, e)
	})
	return rf, ok
}

// ── Timer tests ──────────────────────────────────────────────────────────────

func TestTimer_SingleShotFires(t *testing.T) {
	w := flecs.New()
	defer w.Delete(flecs.ID(0))

	var timerE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		timerE = flecs.NewTimer(fw, 100*time.Millisecond)
	})

	// Progress(50ms) — should not fire.
	progressMS(w, 50)
	timer, _ := getTimer(w, timerE)
	if timer.Fired {
		t.Fatal("timer fired too early after 50ms")
	}
	if !timer.Active {
		t.Fatal("timer should still be active")
	}

	// Progress(60ms) — total 110ms >= 100ms: should fire exactly once.
	progressMS(w, 60)
	timer, _ = getTimer(w, timerE)
	if !timer.Fired {
		t.Fatal("timer did not fire after 110ms (Timeout=100ms)")
	}
	if timer.Active {
		t.Fatal("single-shot timer should be inactive after firing")
	}

	// Progress again — should NOT fire again.
	progressMS(w, 200)
	timer, _ = getTimer(w, timerE)
	if timer.Fired {
		t.Fatal("single-shot timer fired again after becoming inactive")
	}
}

func TestTimer_IntervalFires(t *testing.T) {
	w := flecs.New()

	var timerE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		timerE = flecs.NewInterval(fw, 100*time.Millisecond)
	})

	// Progress(250ms): fires twice (at 100ms and 200ms), remainder 50ms.
	progressMS(w, 250)
	timer, _ := getTimer(w, timerE)
	if !timer.Fired {
		t.Fatal("interval timer did not fire after 250ms (Timeout=100ms)")
	}
	if timer.Active != true {
		t.Fatal("interval timer should remain active")
	}
	// Remainder should be 250-100-100 = 50ms (subtract-with-loop).
	if timer.Elapsed < 40*time.Millisecond || timer.Elapsed > 60*time.Millisecond {
		t.Fatalf("expected Elapsed≈50ms after Progress(250ms), got %v", timer.Elapsed)
	}

	// Progress(60ms): Elapsed=50+60=110ms → fire, remainder=10ms.
	progressMS(w, 60)
	timer, _ = getTimer(w, timerE)
	if !timer.Fired {
		t.Fatal("interval timer did not fire on second Progress")
	}
	if timer.Elapsed < 5*time.Millisecond || timer.Elapsed > 20*time.Millisecond {
		t.Fatalf("expected Elapsed≈10ms, got %v", timer.Elapsed)
	}
}

func TestTimer_StartStopReset(t *testing.T) {
	w := flecs.New()

	var timerE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		timerE = flecs.NewInterval(fw, 100*time.Millisecond)
	})

	// Stop the timer.
	w.Write(func(fw *flecs.Writer) {
		flecs.StopTimer(fw, timerE)
	})
	progressMS(w, 200)
	timer, _ := getTimer(w, timerE)
	if timer.Fired {
		t.Fatal("stopped timer should not fire")
	}
	if timer.Active {
		t.Fatal("timer should still be inactive")
	}

	// Start re-arms with Elapsed=0, Active=true.
	w.Write(func(fw *flecs.Writer) {
		flecs.StartTimer(fw, timerE)
	})
	timer, _ = getTimer(w, timerE)
	if !timer.Active {
		t.Fatal("StartTimer should set Active=true")
	}
	if timer.Elapsed != 0 {
		t.Fatalf("StartTimer should clear Elapsed; got %v", timer.Elapsed)
	}

	// Accumulate 60ms, then Reset (clears Elapsed without changing Active).
	progressMS(w, 60)
	timer, _ = getTimer(w, timerE)
	if timer.Elapsed == 0 {
		t.Fatal("timer should have accumulated elapsed after Start")
	}
	w.Write(func(fw *flecs.Writer) {
		flecs.ResetTimer(fw, timerE)
	})
	timer, _ = getTimer(w, timerE)
	if timer.Elapsed != 0 {
		t.Fatalf("ResetTimer should clear Elapsed; got %v", timer.Elapsed)
	}
	if !timer.Active {
		t.Fatal("ResetTimer should not change Active")
	}
}

func TestTimer_NotActive_DoesNotAccumulate(t *testing.T) {
	w := flecs.New()

	var timerE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		timerE = flecs.NewInterval(fw, 100*time.Millisecond)
	})
	// Stop in a separate Write block so the Timer component has been flushed.
	w.Write(func(fw *flecs.Writer) {
		flecs.StopTimer(fw, timerE)
	})

	for i := 0; i < 5; i++ {
		progressMS(w, 50)
	}
	timer, _ := getTimer(w, timerE)
	if timer.Elapsed != 0 {
		t.Fatalf("inactive timer accumulated Elapsed=%v; expected 0", timer.Elapsed)
	}
	if timer.Fired {
		t.Fatal("inactive timer should never fire")
	}
}

func TestTimer_FiredFlag_OneTickWindow(t *testing.T) {
	w := flecs.New()

	var timerE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		timerE = flecs.NewTimer(fw, 100*time.Millisecond)
	})

	// Fire the timer.
	progressMS(w, 200)
	timer, _ := getTimer(w, timerE)
	if !timer.Fired {
		t.Fatal("timer should be Fired=true immediately after the firing tick")
	}

	// Next Progress where it doesn't fire (timer is now SingleShot and inactive).
	progressMS(w, 200)
	timer, _ = getTimer(w, timerE)
	if timer.Fired {
		t.Fatal("Fired should be false after next Progress where timer does not fire")
	}
}

// ── RateFilter tests ─────────────────────────────────────────────────────────

func TestRateFilter_WorldFrame(t *testing.T) {
	w := flecs.New()

	var rfE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rfE = flecs.NewRateFilter(fw, 3, 0)
	})

	fired := 0
	for tick := 1; tick <= 9; tick++ {
		progressMS(w, 16)
		rf, _ := getRateFilter(w, rfE)
		if rf.Fired {
			fired++
			if tick != 3 && tick != 6 && tick != 9 {
				t.Fatalf("RateFilter fired on tick %d; expected only ticks 3,6,9", tick)
			}
		} else {
			if tick == 3 || tick == 6 || tick == 9 {
				t.Fatalf("RateFilter did not fire on tick %d", tick)
			}
		}
	}
	if fired != 3 {
		t.Fatalf("expected 3 fires over 9 ticks, got %d", fired)
	}
}

func TestRateFilter_ParentTimer(t *testing.T) {
	w := flecs.New()

	var timerE, rfE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		timerE = flecs.NewInterval(fw, 100*time.Millisecond)
		rfE = flecs.NewRateFilter(fw, 2, timerE)
	})

	// 4 × 100ms Progress calls: timer fires every call, RF fires every 2nd timer fire.
	fires := []bool{}
	for i := 0; i < 4; i++ {
		progressMS(w, 100)
		rf, _ := getRateFilter(w, rfE)
		fires = append(fires, rf.Fired)
	}
	// Expect: false, true, false, true
	want := []bool{false, true, false, true}
	for i, got := range fires {
		if got != want[i] {
			t.Fatalf("tick %d: Fired=%v, want %v", i+1, got, want[i])
		}
	}
}

func TestRateFilter_ChainedRateFilters(t *testing.T) {
	w := flecs.New()

	// RF1: Rate=2, Src=0 (world frame) → fires ticks 2,4,6,...
	// RF2: Rate=3, Src=RF1           → fires every 3rd RF1 fire = ticks 6,12,...
	var rf1, rf2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rf1 = flecs.NewRateFilter(fw, 2, 0)
		rf2 = flecs.NewRateFilter(fw, 3, rf1)
	})

	fired2 := 0
	for tick := 1; tick <= 12; tick++ {
		progressMS(w, 16)
		rf, _ := getRateFilter(w, rf2)
		if rf.Fired {
			fired2++
			if tick != 6 && tick != 12 {
				t.Fatalf("RF2 fired on tick %d; expected only ticks 6 and 12", tick)
			}
		} else {
			if tick == 6 || tick == 12 {
				t.Fatalf("RF2 did not fire on tick %d", tick)
			}
		}
	}
	if fired2 != 2 {
		t.Fatalf("expected 2 fires from RF2 over 12 ticks, got %d", fired2)
	}
}

// ── System.SetTickSource tests ────────────────────────────────────────────────

func TestSystem_SetTickSource_Timer(t *testing.T) {
	w := flecs.New()

	type Marker struct{ N int }
	_ = flecs.RegisterComponent[Marker](w)

	var timerE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		timerE = flecs.NewTimer(fw, 50*time.Millisecond)
		e := fw.NewEntity()
		flecs.Set(fw, e, Marker{})
	})

	runs := 0
	q := flecs.NewCachedQuery(w, flecs.RegisterComponent[Marker](w))
	sys := flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			runs++
		}
	})
	sys.SetTickSource(timerE)

	// Progress(60ms): timer fires → system runs once.
	progressMS(w, 60)
	if runs != 1 {
		t.Fatalf("expected system to run once after timer fires; got %d", runs)
	}

	// Subsequent Progress calls: timer is SingleShot and inactive → system never runs again.
	for i := 0; i < 5; i++ {
		progressMS(w, 60)
	}
	if runs != 1 {
		t.Fatalf("single-shot system should run exactly once; got %d runs", runs)
	}
}

func TestSystem_SetTickSource_Interval(t *testing.T) {
	w := flecs.New()

	type Pos struct{ X float32 }
	_ = flecs.RegisterComponent[Pos](w)

	var timerE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		timerE = flecs.NewInterval(fw, 100*time.Millisecond)
		e := fw.NewEntity()
		flecs.Set(fw, e, Pos{})
	})

	runs := 0
	q := flecs.NewCachedQuery(w, flecs.RegisterComponent[Pos](w))
	sys := flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			runs++
		}
	})
	sys.SetTickSource(timerE)

	// 4 × Progress(50ms): timer fires at ticks 2 and 4.
	for i := 0; i < 4; i++ {
		progressMS(w, 50)
	}
	if runs != 2 {
		t.Fatalf("expected system to run twice over 4×50ms; got %d", runs)
	}
}

func TestSystem_SetTickSource_RateFilter(t *testing.T) {
	w := flecs.New()

	type Vel struct{ DX float32 }
	_ = flecs.RegisterComponent[Vel](w)

	var rfE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rfE = flecs.NewRateFilter(fw, 3, 0) // fires every 3rd Progress call
		e := fw.NewEntity()
		flecs.Set(fw, e, Vel{})
	})

	runs := 0
	q := flecs.NewCachedQuery(w, flecs.RegisterComponent[Vel](w))
	sys := flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			runs++
		}
	})
	sys.SetTickSource(rfE)

	for tick := 1; tick <= 9; tick++ {
		progressMS(w, 16)
	}
	if runs != 3 {
		t.Fatalf("expected system to run 3 times over 9 ticks (rate=3); got %d", runs)
	}
}

func TestSystem_TickSource_ReplacesInternalRate(t *testing.T) {
	// Precedence: existing interval/rate gates evaluate FIRST.
	// If both SetTickSource AND SetRate/SetInterval are set, the system runs only
	// when BOTH pass (AND semantics, same as interval×rate composition).
	// This test documents the chosen behavior: AND composition (no panic).
	w := flecs.New()

	type Tag struct{}
	_ = flecs.RegisterComponent[Tag](w)

	var timerE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		timerE = flecs.NewInterval(fw, 100*time.Millisecond)
		e := fw.NewEntity()
		flecs.Set(fw, e, Tag{})
	})

	runs := 0
	q := flecs.NewCachedQuery(w, flecs.RegisterComponent[Tag](w))
	sys := flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			runs++
		}
	})
	// Both a per-system rate gate (every 2nd tick) and a tick-source gate (every 100ms).
	// System runs only when BOTH pass.
	sys.SetRate(2)
	sys.SetTickSource(timerE)

	// 6 × Progress(100ms): timer fires every tick; rate gate fires on ticks 2,4,6.
	for i := 0; i < 6; i++ {
		progressMS(w, 100)
	}
	// Expected: system runs on ticks 2, 4, 6 (rate gate ticks that coincide with timer fires).
	if runs != 3 {
		t.Fatalf("AND composition: expected 3 runs over 6 ticks; got %d", runs)
	}
}

func TestSystem_TickSource_DeletedEntity(t *testing.T) {
	w := flecs.New()

	type Comp struct{ V int }
	_ = flecs.RegisterComponent[Comp](w)

	var timerE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		timerE = flecs.NewInterval(fw, 50*time.Millisecond)
		e := fw.NewEntity()
		flecs.Set(fw, e, Comp{})
	})

	runs := 0
	q := flecs.NewCachedQuery(w, flecs.RegisterComponent[Comp](w))
	sys := flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {
		for it.Next() {
			runs++
		}
	})
	sys.SetTickSource(timerE)

	// Delete the timer entity.
	w.Write(func(fw *flecs.Writer) {
		fw.Delete(timerE)
	})

	// Subsequent Progress calls: no crash, system never fires.
	for i := 0; i < 5; i++ {
		progressMS(w, 100)
	}
	if runs != 0 {
		t.Fatalf("system bound to deleted timer should never run; got %d runs", runs)
	}
}

// ── Deferred ops ─────────────────────────────────────────────────────────────

func TestTimer_DeferredOps(t *testing.T) {
	w := flecs.New()

	var timerE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		timerE = flecs.NewInterval(fw, 100*time.Millisecond)
	})

	// Stop inside a Write block.
	w.Write(func(fw *flecs.Writer) {
		flecs.StopTimer(fw, timerE)
	})
	progressMS(w, 200)
	timer, _ := getTimer(w, timerE)
	if timer.Fired || timer.Active {
		t.Fatal("StopTimer inside Write should deactivate timer; it fired after stop")
	}

	// Start inside a Write block: timer re-arms.
	w.Write(func(fw *flecs.Writer) {
		flecs.StartTimer(fw, timerE)
	})
	timer, _ = getTimer(w, timerE)
	if !timer.Active || timer.Elapsed != 0 {
		t.Fatalf("StartTimer inside Write: Active=%v Elapsed=%v; expected Active=true Elapsed=0",
			timer.Active, timer.Elapsed)
	}

	// Accumulate 60ms, then Reset inside Write.
	progressMS(w, 60)
	w.Write(func(fw *flecs.Writer) {
		flecs.ResetTimer(fw, timerE)
	})
	timer, _ = getTimer(w, timerE)
	if timer.Elapsed != 0 {
		t.Fatalf("ResetTimer inside Write should clear Elapsed; got %v", timer.Elapsed)
	}
	if !timer.Active {
		t.Fatal("ResetTimer should not change Active")
	}

	// After Reset, timer should fire after another 100ms.
	progressMS(w, 100)
	timer, _ = getTimer(w, timerE)
	if !timer.Fired {
		t.Fatal("timer should fire 100ms after Reset+Start")
	}
}

// ── Accessor tests ────────────────────────────────────────────────────────────

func TestTimer_GetTimeoutInterval_Accessors(t *testing.T) {
	w := flecs.New()

	var singleE, intervalE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		singleE = flecs.NewTimer(fw, 200*time.Millisecond)
		intervalE = flecs.NewInterval(fw, 50*time.Millisecond)
	})

	// GetTimeout inside Read.
	w.Read(func(fr *flecs.Reader) {
		if got := flecs.GetTimeout(fr, singleE); got != 200*time.Millisecond {
			t.Errorf("GetTimeout(singleE)=%v; want 200ms", got)
		}
		if got := flecs.GetInterval(fr, intervalE); got != 50*time.Millisecond {
			t.Errorf("GetInterval(intervalE)=%v; want 50ms", got)
		}
	})

	// GetTimeout / GetInterval inside Write (scope interface).
	w.Write(func(fw *flecs.Writer) {
		if got := flecs.GetTimeout(fw, singleE); got != 200*time.Millisecond {
			t.Errorf("GetTimeout(singleE) in Write=%v; want 200ms", got)
		}
		if got := flecs.GetInterval(fw, intervalE); got != 50*time.Millisecond {
			t.Errorf("GetInterval(intervalE) in Write=%v; want 50ms", got)
		}
	})
}

// ── JSON round-trip ───────────────────────────────────────────────────────────

func TestTimer_JSON_RoundTrip(t *testing.T) {
	w1 := flecs.New()

	// Pre-register component types in w1 before creating user entities so
	// the component entity IDs (63, 64) are established before user entities
	// (65+). w2 replicates the same registration order so IDs match.
	flecs.RegisterComponent[flecs.Timer](w1)
	flecs.RegisterComponent[flecs.RateFilter](w1)

	var timerE, rfE flecs.ID
	w1.Write(func(fw *flecs.Writer) {
		timerE = flecs.NewInterval(fw, 100*time.Millisecond)
		rfE = flecs.NewRateFilter(fw, 3, timerE)
	})

	// Advance the world to build up some state.
	progressMS(w1, 60)
	progressMS(w1, 60) // timer fires once, remainder 20ms

	data, err := w1.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	w2 := flecs.New()
	flecs.RegisterComponent[flecs.Timer](w2)
	flecs.RegisterComponent[flecs.RateFilter](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	// Verify Timer fields survived the round-trip.
	w2.Read(func(fr *flecs.Reader) {
		timer, ok := flecs.Get[flecs.Timer](fr, timerE)
		if !ok {
			t.Fatal("Timer entity not found after JSON round-trip")
		}
		if timer.Timeout != 100*time.Millisecond {
			t.Errorf("Timer.Timeout=%v; want 100ms", timer.Timeout)
		}
		if !timer.Active {
			t.Error("Timer.Active should be true")
		}
		if timer.SingleShot {
			t.Error("Timer.SingleShot should be false for interval timer")
		}

		rf, ok := flecs.Get[flecs.RateFilter](fr, rfE)
		if !ok {
			t.Fatal("RateFilter entity not found after JSON round-trip")
		}
		if rf.Rate != 3 {
			t.Errorf("RateFilter.Rate=%d; want 3", rf.Rate)
		}
		if rf.Src != timerE {
			t.Errorf("RateFilter.Src=%v; want %v", rf.Src, timerE)
		}
	})
}

// ── Snapshot round-trip ───────────────────────────────────────────────────────

func TestTimer_Snapshot_RoundTrip(t *testing.T) {
	w := flecs.New()

	var timerE, rfE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		timerE = flecs.NewInterval(fw, 200*time.Millisecond)
		rfE = flecs.NewRateFilter(fw, 2, 0)
	})

	// Build up partial state.
	progressMS(w, 150) // timer Elapsed=150ms, RF TickCount=1

	snap := flecs.TakeSnapshot(w)

	// Advance further to dirty the state.
	progressMS(w, 150) // timer fires, RF TickCount=2 → fires
	progressMS(w, 100)

	// Restore and verify the saved state.
	flecs.RestoreSnapshot(w, snap)

	w.Read(func(fr *flecs.Reader) {
		timer, ok := flecs.Get[flecs.Timer](fr, timerE)
		if !ok {
			t.Fatal("Timer not found after RestoreSnapshot")
		}
		// After RestoreSnapshot, timer.Elapsed should be ~150ms.
		if timer.Elapsed < 140*time.Millisecond || timer.Elapsed > 160*time.Millisecond {
			t.Errorf("Timer.Elapsed after restore=%v; want ~150ms", timer.Elapsed)
		}

		rf, ok := flecs.Get[flecs.RateFilter](fr, rfE)
		if !ok {
			t.Fatal("RateFilter not found after RestoreSnapshot")
		}
		if rf.TickCount != 1 {
			t.Errorf("RateFilter.TickCount after restore=%d; want 1", rf.TickCount)
		}
	})
}

// ── IsTimerFired with scope ───────────────────────────────────────────────────

func TestIsTimerFired_ViaScope(t *testing.T) {
	w := flecs.New()

	var timerE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		timerE = flecs.NewTimer(fw, 50*time.Millisecond)
	})

	progressMS(w, 100)

	// IsTimerFired via Reader scope.
	w.Read(func(fr *flecs.Reader) {
		if !flecs.IsTimerFired(fr, timerE) {
			t.Error("IsTimerFired should be true via Reader after firing tick")
		}
	})
	// IsTimerFired via Writer scope.
	w.Write(func(fw *flecs.Writer) {
		if !flecs.IsTimerFired(fw, timerE) {
			t.Error("IsTimerFired should be true via Writer after firing tick")
		}
	})

	// After the next Progress, Fired should be cleared.
	progressMS(w, 100)
	w.Read(func(fr *flecs.Reader) {
		if flecs.IsTimerFired(fr, timerE) {
			t.Error("IsTimerFired should be false after a non-firing tick")
		}
	})
}

// ── SetTimeout / SetInterval free functions ───────────────────────────────────

func TestSetTimeout_FreeFunction(t *testing.T) {
	w := flecs.New()

	// SetTimeout on an entity that already has a Timer component.
	var timerE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		timerE = flecs.NewInterval(fw, 200*time.Millisecond) // interval timer
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.SetTimeout(fw, timerE, 50*time.Millisecond) // convert to single-shot
	})

	timer, ok := getTimer(w, timerE)
	if !ok {
		t.Fatal("Timer not found after SetTimeout")
	}
	if timer.Timeout != 50*time.Millisecond {
		t.Errorf("SetTimeout: Timeout=%v; want 50ms", timer.Timeout)
	}
	if !timer.SingleShot {
		t.Error("SetTimeout should set SingleShot=true")
	}
	if !timer.Active {
		t.Error("SetTimeout should set Active=true")
	}
	if timer.Elapsed != 0 {
		t.Errorf("SetTimeout should clear Elapsed; got %v", timer.Elapsed)
	}

	// SetTimeout on entity without Timer adds the component.
	var bare flecs.ID
	w.Write(func(fw *flecs.Writer) {
		bare = fw.NewEntity()
		flecs.SetTimeout(fw, bare, 75*time.Millisecond)
	})
	timer, ok = getTimer(w, bare)
	if !ok {
		t.Fatal("Timer not found after SetTimeout on bare entity")
	}
	if timer.Timeout != 75*time.Millisecond {
		t.Errorf("SetTimeout on bare: Timeout=%v; want 75ms", timer.Timeout)
	}
}

func TestSetInterval_FreeFunction(t *testing.T) {
	w := flecs.New()

	// SetInterval on an entity that already has a Timer component.
	var timerE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		timerE = flecs.NewTimer(fw, 200*time.Millisecond) // single-shot timer
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.SetInterval(fw, timerE, 30*time.Millisecond) // convert to interval
	})

	timer, ok := getTimer(w, timerE)
	if !ok {
		t.Fatal("Timer not found after SetInterval")
	}
	if timer.Timeout != 30*time.Millisecond {
		t.Errorf("SetInterval: Timeout=%v; want 30ms", timer.Timeout)
	}
	if timer.SingleShot {
		t.Error("SetInterval should set SingleShot=false")
	}
	if !timer.Active {
		t.Error("SetInterval should set Active=true")
	}

	// SetInterval on entity without Timer adds the component.
	var bare flecs.ID
	w.Write(func(fw *flecs.Writer) {
		bare = fw.NewEntity()
		flecs.SetInterval(fw, bare, 60*time.Millisecond)
	})
	timer, ok = getTimer(w, bare)
	if !ok {
		t.Fatal("Timer not found after SetInterval on bare entity")
	}
	if timer.SingleShot {
		t.Error("SetInterval on bare: should have SingleShot=false")
	}
}

func TestIsTimerFired_RateFilter(t *testing.T) {
	w := flecs.New()

	var rfE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rfE = flecs.NewRateFilter(fw, 1, 0) // fires every tick (world frame, rate=1)
	})

	progressMS(w, 16)

	// IsTimerFired on a RateFilter entity (not a Timer entity).
	w.Read(func(fr *flecs.Reader) {
		if !flecs.IsTimerFired(fr, rfE) {
			t.Error("IsTimerFired on RateFilter entity should be true after firing tick")
		}
	})

	progressMS(w, 16)
	w.Read(func(fr *flecs.Reader) {
		// Rate=1 fires every tick, so Fired should be true again.
		if !flecs.IsTimerFired(fr, rfE) {
			t.Error("RateFilter(rate=1) should fire every tick")
		}
	})
}

// ── SetRate free function ─────────────────────────────────────────────────────

func TestSetRate_FreeFunction(t *testing.T) {
	w := flecs.New()

	var rfE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rfE = flecs.NewRateFilter(fw, 5, 0)
	})
	// SetRate in a separate Write block after the RateFilter component is flushed.
	w.Write(func(fw *flecs.Writer) {
		flecs.SetRate(fw, rfE, 2) // change Rate to 2
	})

	// With Rate=2 and world-frame source, fires on ticks 2,4,...
	fires := 0
	for tick := 1; tick <= 4; tick++ {
		progressMS(w, 16)
		rf, _ := getRateFilter(w, rfE)
		if rf.Fired {
			fires++
		}
	}
	if fires != 2 {
		t.Fatalf("SetRate(2): expected 2 fires over 4 ticks, got %d", fires)
	}
}

// ── TickSource accessor ───────────────────────────────────────────────────────

func TestSystem_TickSource_Accessor(t *testing.T) {
	w := flecs.New()

	type C struct{}
	_ = flecs.RegisterComponent[C](w)

	var timerE flecs.ID
	w.Write(func(fw *flecs.Writer) {
		timerE = flecs.NewTimer(fw, 100*time.Millisecond)
		e := fw.NewEntity()
		flecs.Set(fw, e, C{})
	})

	q := flecs.NewCachedQuery(w, flecs.RegisterComponent[C](w))
	sys := flecs.NewSystem(w, q, func(dt float32, it *flecs.QueryIter) {})

	if sys.TickSource() != 0 {
		t.Fatal("TickSource should be 0 before SetTickSource")
	}
	sys.SetTickSource(timerE)
	if sys.TickSource() != timerE {
		t.Fatalf("TickSource()=%v; want %v", sys.TickSource(), timerE)
	}
	// Fluent chaining.
	ret := sys.SetTickSource(0)
	if ret != sys {
		t.Fatal("SetTickSource should return *System for chaining")
	}
	if sys.TickSource() != 0 {
		t.Fatal("SetTickSource(0) should clear the binding")
	}
}

// ── JSON marshaling of RateFilter.Src ────────────────────────────────────────

func TestTimer_JSON_RoundTrip_Src(t *testing.T) {
	w1 := flecs.New()

	flecs.RegisterComponent[flecs.Timer](w1)
	flecs.RegisterComponent[flecs.RateFilter](w1)

	var timerE, rfE flecs.ID
	w1.Write(func(fw *flecs.Writer) {
		timerE = flecs.NewInterval(fw, 50*time.Millisecond)
		rfE = flecs.NewRateFilter(fw, 4, timerE)
	})

	data, err := w1.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	// Verify the JSON contains meaningful data.
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	w2 := flecs.New()
	flecs.RegisterComponent[flecs.Timer](w2)
	flecs.RegisterComponent[flecs.RateFilter](w2)
	if err := w2.UnmarshalJSON(data); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	w2.Read(func(fr *flecs.Reader) {
		rf, ok := flecs.Get[flecs.RateFilter](fr, rfE)
		if !ok {
			t.Fatal("RateFilter not found after JSON round-trip")
		}
		if rf.Rate != 4 {
			t.Errorf("RateFilter.Rate=%d; want 4", rf.Rate)
		}
		if rf.Src != timerE {
			t.Errorf("RateFilter.Src=%v; want %v", rf.Src, timerE)
		}
	})
}
