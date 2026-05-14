package flecs

import (
	"testing"
	"time"
)

// newRateTestWorld creates a minimal world with one entity carrying a Position
// component (so the query matches) and returns the world plus a cached query.
func newRateTestWorld(t *testing.T) (*World, *CachedQuery) {
	t.Helper()
	type Position struct{ X float32 }
	w := New()
	posID := RegisterComponent[Position](w)
	w.Write(func(fw *Writer) {
		e := fw.NewEntity()
		Set(fw, e, Position{X: 1})
	})
	q := NewCachedQuery(w, posID)
	return w, q
}

// TestRateFilter_Rate2_ExactPattern verifies that with rate=2 the system fires on
// ticks 2, 4, 6, 8, 10 (5 runs out of 10 ticks).
func TestRateFilter_Rate2_ExactPattern(t *testing.T) {
	w, q := newRateTestWorld(t)
	runs := 0
	var fireTicks []int
	tick := 0
	sys := NewSystem(w, q, func(dt float32, it *QueryIter) {
		runs++
		fireTicks = append(fireTicks, tick)
	})
	sys.SetRate(2)
	if sys.GetRate() != 2 {
		t.Fatalf("GetRate() = %d, want 2", sys.GetRate())
	}

	for i := 1; i <= 10; i++ {
		tick = i
		w.Progress(1.0 / 60.0)
	}
	if runs != 5 {
		t.Errorf("expected 5 runs, got %d", runs)
	}
	want := []int{2, 4, 6, 8, 10}
	for i, got := range fireTicks {
		if got != want[i] {
			t.Errorf("fire[%d] = tick %d, want %d", i, got, want[i])
		}
	}
}

// TestRateFilter_Rate1_DisablesGating verifies rate=1 runs the system every tick.
func TestRateFilter_Rate1_DisablesGating(t *testing.T) {
	w, q := newRateTestWorld(t)
	runs := 0
	sys := NewSystem(w, q, func(dt float32, it *QueryIter) { runs++ })
	sys.SetRate(1)

	for i := 0; i < 10; i++ {
		w.Progress(1.0 / 60.0)
	}
	if runs != 10 {
		t.Errorf("expected 10 runs, got %d", runs)
	}
}

// TestRateFilter_Rate0_DisablesGating verifies rate=0 (sentinel) runs every tick.
func TestRateFilter_Rate0_DisablesGating(t *testing.T) {
	w, q := newRateTestWorld(t)
	runs := 0
	sys := NewSystem(w, q, func(dt float32, it *QueryIter) { runs++ })
	sys.SetRate(0)

	for i := 0; i < 8; i++ {
		w.Progress(1.0 / 60.0)
	}
	if runs != 8 {
		t.Errorf("expected 8 runs, got %d", runs)
	}
}

// TestRateFilter_Interval100ms_dt30ms locks in the exact firing pattern:
// dt=30ms each tick; fires on tick 4 (accum=120ms≥100ms, remainder=20ms),
// then tick 8 (accum=20+30+30+30=110ms≥100ms, remainder=10ms).
func TestRateFilter_Interval100ms_dt30ms(t *testing.T) {
	w, q := newRateTestWorld(t)
	runs := 0
	var fireTicks []int
	tick := 0
	sys := NewSystem(w, q, func(dt float32, it *QueryIter) {
		runs++
		fireTicks = append(fireTicks, tick)
	})
	sys.SetInterval(100 * time.Millisecond)
	if sys.GetInterval() != 100*time.Millisecond {
		t.Fatalf("GetInterval() = %v, want 100ms", sys.GetInterval())
	}

	const dt = float32(0.030) // 30ms
	for i := 1; i <= 10; i++ {
		tick = i
		w.Progress(dt)
	}
	if len(fireTicks) < 2 {
		t.Fatalf("expected ≥2 fires, got %d; ticks: %v", runs, fireTicks)
	}
	if fireTicks[0] != 4 {
		t.Errorf("first fire at tick %d, want 4", fireTicks[0])
	}
	// tick4: accum=120ms→fire, rem=20ms; tick5: 50ms; tick6: 80ms; tick7: 110ms→fire
	if fireTicks[1] != 7 {
		t.Errorf("second fire at tick %d, want 7", fireTicks[1])
	}
}

// TestRateFilter_IntervalVariableDT_SubtractWithCap checks the subtract-with-cap
// accumulator: 50ms, 200ms (single tick exceeds interval), 30ms.
// With interval=100ms:
//   - tick1 dt=50ms: accum=50ms, no fire
//   - tick2 dt=200ms: accum=250ms ≥ 100ms → fire, remainder=150ms > 100ms → cap to 0
//   - tick3 dt=30ms: accum=30ms, no fire
func TestRateFilter_IntervalVariableDT_SubtractWithCap(t *testing.T) {
	w, q := newRateTestWorld(t)
	runs := 0
	var fireTicks []int
	tick := 0
	sys := NewSystem(w, q, func(dt float32, it *QueryIter) {
		runs++
		fireTicks = append(fireTicks, tick)
	})
	sys.SetInterval(100 * time.Millisecond)

	dts := []float32{0.050, 0.200, 0.030}
	for i, dt := range dts {
		tick = i + 1
		w.Progress(dt)
	}
	if runs != 1 {
		t.Errorf("expected 1 fire, got %d at ticks %v", runs, fireTicks)
	}
	if runs >= 1 && fireTicks[0] != 2 {
		t.Errorf("fire at tick %d, want 2", fireTicks[0])
	}
	// verify accum after cap: tick3 dt=30ms → accum via same float32→Duration path
	var dt30 float32 = 0.030
	want := time.Duration(float64(dt30) * float64(time.Second))
	if sys.intervalAccum != want {
		t.Errorf("intervalAccum = %v, want %v", sys.intervalAccum, want)
	}
}

// TestRateFilter_SetInterval0_Disables checks that SetInterval(0) disables
// interval gating after it was active.
func TestRateFilter_SetInterval0_Disables(t *testing.T) {
	w, q := newRateTestWorld(t)
	runs := 0
	sys := NewSystem(w, q, func(dt float32, it *QueryIter) { runs++ })
	sys.SetInterval(1 * time.Second)

	// With 1s interval and dt=100ms, no fire in 5 ticks.
	for i := 0; i < 5; i++ {
		w.Progress(0.100)
	}
	if runs != 0 {
		t.Errorf("expected 0 runs before disable, got %d", runs)
	}

	// Disable interval gating.
	sys.SetInterval(0)
	for i := 0; i < 5; i++ {
		w.Progress(0.100)
	}
	if runs != 5 {
		t.Errorf("expected 5 runs after SetInterval(0), got %d", runs)
	}
}

// TestRateFilter_SetRate0_And1_Disable verifies both sentinels disable rate gating.
func TestRateFilter_SetRate0_And1_Disable(t *testing.T) {
	for _, rate := range []int32{0, 1} {
		rate := rate
		t.Run("rate="+string(rune('0'+rate)), func(t *testing.T) {
			w, q := newRateTestWorld(t)
			runs := 0
			sys := NewSystem(w, q, func(dt float32, it *QueryIter) { runs++ })
			sys.SetRate(rate)
			for i := 0; i < 10; i++ {
				w.Progress(1.0 / 60.0)
			}
			if runs != 10 {
				t.Errorf("rate=%d: expected 10 runs, got %d", rate, runs)
			}
		})
	}
}

// TestRateFilter_CombinedIntervalAndRate verifies AND composition: system fires
// only on ticks where BOTH the interval gate and the rate gate pass.
// rate=3, interval=50ms, dt=30ms:
//
//	rateCounter fires at ticks 3, 6, 9, ...
//	intervalAccum: 30,60,90,120 → first interval fire when accum≥50 (tick2 accum=60ms).
//
// Let's trace more carefully:
//
//	tick1: rate=1%3≠0 → rate skip; interval accum=30ms
//	tick2: rate=2%3≠0 → rate skip; interval accum=60ms≥50ms → interval would fire but rate skipped
//	tick3: rate=3%3=0 → rate passes; interval check: accum was running...
//
// Wait, let me reconsider the implementation. In the implementation the interval
// check runs BEFORE the rate check, and both advances happen even if one skips.
// Let me re-read the implementation:
//   - interval gate runs first: accumulates and may skip
//   - rate gate runs second: increments counter and may skip
//
// So if interval skips, rate counter does NOT increment.
// And if interval passes, rate counter increments and may skip.
// This is correct AND composition: both must pass in the same tick.
//
// With interval=50ms, rate=2, dt=30ms:
//
//	tick1: accum=30ms < 50ms → interval skip (rate counter not incremented)
//	tick2: accum=60ms ≥ 50ms → interval passes, accum=10ms; rate counter=1, 1%2≠0 → rate skip
//	tick3: accum=10+30=40ms < 50ms → interval skip
//	tick4: accum=40+30=70ms ≥ 50ms → interval passes, accum=20ms; rate counter=2, 2%2=0 → FIRE
//	tick5: accum=20+30=50ms ≥ 50ms → interval passes, accum=0ms; rate counter=3, 3%2≠0 → rate skip
//	tick6: accum=0+30=30ms < 50ms → interval skip
//	tick7: accum=30+30=60ms ≥ 50ms → interval passes, accum=10ms; rate counter=4, 4%2=0 → FIRE
//
// So fires at ticks 4 and 7.
func TestRateFilter_CombinedIntervalAndRate(t *testing.T) {
	w, q := newRateTestWorld(t)
	runs := 0
	var fireTicks []int
	tick := 0
	sys := NewSystem(w, q, func(dt float32, it *QueryIter) {
		runs++
		fireTicks = append(fireTicks, tick)
	})
	sys.SetInterval(50 * time.Millisecond)
	sys.SetRate(2)

	const dt = float32(0.030)
	for i := 1; i <= 10; i++ {
		tick = i
		w.Progress(dt)
	}
	if runs < 1 {
		t.Fatalf("expected ≥1 fire with combined gates, got 0")
	}
	// First fire must be at tick 4.
	if fireTicks[0] != 4 {
		t.Errorf("first fire at tick %d, want 4", fireTicks[0])
	}
	// Second fire must be at tick 7.
	if len(fireTicks) >= 2 && fireTicks[1] != 7 {
		t.Errorf("second fire at tick %d, want 7", fireTicks[1])
	}
}

// TestRateFilter_DisabledSystem_CountersDoNotAdvance verifies that while a system
// is disabled, neither rateCounter nor intervalAccum advance.
func TestRateFilter_DisabledSystem_CountersDoNotAdvance(t *testing.T) {
	w, q := newRateTestWorld(t)
	runs := 0
	sys := NewSystem(w, q, func(dt float32, it *QueryIter) { runs++ })
	sys.SetRate(2)
	sys.SetInterval(100 * time.Millisecond)

	// Disable immediately.
	sys.SetEnabled(false)
	for i := 0; i < 10; i++ {
		w.Progress(0.050)
	}
	if runs != 0 {
		t.Errorf("disabled system ran %d times, want 0", runs)
	}
	if sys.rateCounter != 0 {
		t.Errorf("rateCounter = %d, want 0 (should not advance while disabled)", sys.rateCounter)
	}
	if sys.intervalAccum != 0 {
		t.Errorf("intervalAccum = %v, want 0 (should not advance while disabled)", sys.intervalAccum)
	}
}

// TestRateFilter_ReenableAfterDisable verifies counters resume from pre-disable
// state and no catch-up storm occurs.
func TestRateFilter_ReenableAfterDisable(t *testing.T) {
	w, q := newRateTestWorld(t)
	runs := 0
	sys := NewSystem(w, q, func(dt float32, it *QueryIter) { runs++ })
	sys.SetRate(4)

	// Run 2 ticks — counter is now 2 (no fire yet at rate=4).
	w.Progress(1.0 / 60.0)
	w.Progress(1.0 / 60.0)
	if runs != 0 {
		t.Errorf("expected 0 runs after 2 ticks at rate=4, got %d", runs)
	}
	if sys.rateCounter != 2 {
		t.Errorf("rateCounter = %d, want 2", sys.rateCounter)
	}

	// Disable.
	sys.SetEnabled(false)
	for i := 0; i < 5; i++ {
		w.Progress(1.0 / 60.0)
	}
	if sys.rateCounter != 2 {
		t.Errorf("rateCounter = %d after disable, want 2 (no advance)", sys.rateCounter)
	}

	// Re-enable; counter resumes at 2. Next fire at tick 4 (rateCounter=4, 4%4=0).
	sys.SetEnabled(true)
	w.Progress(1.0 / 60.0) // rateCounter=3, no fire
	if runs != 0 {
		t.Errorf("expected 0 runs after 3rd active tick, got %d", runs)
	}
	w.Progress(1.0 / 60.0) // rateCounter=4, fire
	if runs != 1 {
		t.Errorf("expected 1 run after 4th active tick, got %d", runs)
	}
}

// TestRateFilter_SetBetweenTicks_RaceClean verifies that calling SetRate and
// SetInterval between Progress calls (the documented usage) is race-detector clean.
// *System is not goroutine-safe; callers must only modify it from the same goroutine
// that drives Progress, between ticks.
func TestRateFilter_SetBetweenTicks_RaceClean(t *testing.T) {
	w, q := newRateTestWorld(t)
	runs := 0
	sys := NewSystem(w, q, func(dt float32, it *QueryIter) { runs++ })

	// Interleave rate/interval changes with Progress calls on the same goroutine.
	sys.SetRate(3)
	w.Progress(1.0 / 60.0)
	w.Progress(1.0 / 60.0)
	sys.SetInterval(50 * time.Millisecond)
	w.Progress(1.0 / 60.0)
	sys.SetRate(0)
	w.Progress(1.0 / 60.0)
	sys.SetInterval(0)
	w.Progress(1.0 / 60.0)
	// After disabling both gates the last tick should always run.
	if runs < 1 {
		t.Errorf("expected ≥1 run after disabling all gates, got %d", runs)
	}
}

// TestRateFilter_NegativePanic_SetInterval checks that SetInterval(-1) panics.
func TestRateFilter_NegativePanic_SetInterval(t *testing.T) {
	w, q := newRateTestWorld(t)
	sys := NewSystem(w, q, func(dt float32, it *QueryIter) {})
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic from SetInterval(-1), got none")
		}
	}()
	sys.SetInterval(-1)
}

// TestRateFilter_NegativePanic_SetRate checks that SetRate(-1) panics.
func TestRateFilter_NegativePanic_SetRate(t *testing.T) {
	w, q := newRateTestWorld(t)
	sys := NewSystem(w, q, func(dt float32, it *QueryIter) {})
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic from SetRate(-1), got none")
		}
	}()
	sys.SetRate(-1)
}
