package flecs

import (
	"time"
	"unsafe"

	"github.com/snichols/flecs/internal/storage/table"
)

// Timer is the entity-based timer component (Timer addon, Phase 16.36).
//
// Elapsed advances by the dt passed to World.Progress; it never advances by time.Now().
// Fired is transient: set when the timer fires this tick, cleared at the start of the
// next tick's timer pass. To read Fired inside a system, use IsTimerFired.
//
// Distinct from the OnFixedUpdate frame-timing accumulator in timer.go, which tracks
// frame count and fixed-step state. That file is not modified by this addon.
type Timer struct {
	Timeout    time.Duration
	Elapsed    time.Duration
	Active     bool
	SingleShot bool
	Fired      bool // transient: true during the tick this timer fires, false otherwise
}

// RateFilter fires every Rate parent ticks (Timer addon, Phase 16.36).
//
// Src == 0 ⇒ fires every World.Progress call (world frame).
// Src != 0 ⇒ fires every Rate times the referenced Timer or RateFilter entity fires.
//
// Fired is transient: set when the filter fires this tick, cleared at start of next tick.
// TimeElapsed accumulates dt since the last fire (reset to 0 on each fire).
type RateFilter struct {
	Rate        int32         // fires every Nth parent tick; N<=1 ⇒ every parent tick
	TickCount   int32         // counter toward Rate; reset to 0 on each fire
	Src         ID            // 0 ⇒ world frame; non-zero ⇒ Timer or RateFilter entity
	TimeElapsed time.Duration // wall-clock accumulated since last fire; reset on fire
	Fired       bool          // transient: true during the tick this filter fires
}

// ensureTimerID lazy-registers the Timer component on w and caches the ID.
func ensureTimerID(w *World) ID {
	if w.timerComponentID == 0 {
		w.timerComponentID = RegisterComponent[Timer](w)
	}
	return w.timerComponentID
}

// ensureRateFilterID lazy-registers the RateFilter component on w and caches the ID.
func ensureRateFilterID(w *World) ID {
	if w.rateFilterComponentID == 0 {
		w.rateFilterComponentID = RegisterComponent[RateFilter](w)
	}
	return w.rateFilterComponentID
}

// NewTimer creates a new entity configured as a single-shot timer.
// The timer begins active immediately. It fires once when Elapsed >= timeout and
// then becomes inactive (Active=false).
func NewTimer(fw *Writer, timeout time.Duration) ID {
	w := fw.scopeWorld()
	ensureTimerID(w)
	e := fw.NewEntity()
	Set[Timer](fw, e, Timer{Timeout: timeout, SingleShot: true, Active: true})
	return e
}

// NewInterval creates a new entity configured as a repeating interval timer.
// The timer begins active immediately. It fires every time Elapsed accumulates
// to at least interval; the accumulator carries over (subtract-with-loop semantics).
func NewInterval(fw *Writer, interval time.Duration) ID {
	w := fw.scopeWorld()
	ensureTimerID(w)
	e := fw.NewEntity()
	Set[Timer](fw, e, Timer{Timeout: interval, SingleShot: false, Active: true})
	return e
}

// SetTimeout configures entity e as a single-shot timer with the given timeout.
// Resets Elapsed and Fired; sets SingleShot=true and Active=true.
// If e does not yet have a Timer component, one is added.
func SetTimeout(fw *Writer, e ID, timeout time.Duration) {
	w := fw.scopeWorld()
	ensureTimerID(w)
	t := getRefOnWorld[Timer](w, e)
	if t == nil {
		Set[Timer](fw, e, Timer{Timeout: timeout, SingleShot: true, Active: true})
		return
	}
	t.Timeout = timeout
	t.SingleShot = true
	t.Elapsed = 0
	t.Fired = false
	t.Active = true
}

// SetInterval configures entity e as a repeating interval timer.
// Resets Elapsed and Fired; sets SingleShot=false and Active=true.
// If e does not yet have a Timer component, one is added.
func SetInterval(fw *Writer, e ID, interval time.Duration) {
	w := fw.scopeWorld()
	ensureTimerID(w)
	t := getRefOnWorld[Timer](w, e)
	if t == nil {
		Set[Timer](fw, e, Timer{Timeout: interval, SingleShot: false, Active: true})
		return
	}
	t.Timeout = interval
	t.SingleShot = false
	t.Elapsed = 0
	t.Fired = false
	t.Active = true
}

// GetTimeout returns the Timeout of the Timer component on entity e.
// Returns 0 if e has no Timer component or the Timer addon is not yet initialized.
func GetTimeout(s scope, e ID) time.Duration {
	w := s.scopeWorld()
	if w.timerComponentID == 0 {
		return 0
	}
	t := getRefOnWorld[Timer](w, e)
	if t == nil {
		return 0
	}
	return t.Timeout
}

// GetInterval returns the Timeout of the Timer component on entity e.
// Semantically equivalent to GetTimeout; provided for clarity when the timer
// was created with NewInterval.
func GetInterval(s scope, e ID) time.Duration {
	return GetTimeout(s, e)
}

// StartTimer activates the timer on entity e, clearing Elapsed and Fired.
// No-op if e has no Timer component.
func StartTimer(fw *Writer, e ID) {
	w := fw.scopeWorld()
	t := getRefOnWorld[Timer](w, e)
	if t == nil {
		return
	}
	t.Active = true
	t.Elapsed = 0
	t.Fired = false
}

// StopTimer deactivates the timer on entity e (Active=false). Elapsed is preserved.
// No-op if e has no Timer component.
func StopTimer(fw *Writer, e ID) {
	w := fw.scopeWorld()
	t := getRefOnWorld[Timer](w, e)
	if t == nil {
		return
	}
	t.Active = false
}

// ResetTimer clears Elapsed and Fired on entity e without changing Active.
// No-op if e has no Timer component.
func ResetTimer(fw *Writer, e ID) {
	w := fw.scopeWorld()
	t := getRefOnWorld[Timer](w, e)
	if t == nil {
		return
	}
	t.Elapsed = 0
	t.Fired = false
}

// IsTimerFired reports whether the Timer or RateFilter component on entity e fired
// in the last World.Progress call. Returns false if e is not alive, has neither
// component, or the addon has not been initialized.
func IsTimerFired(s scope, e ID) bool {
	w := s.scopeWorld()
	if w.timerComponentID != 0 {
		if t := getRefOnWorld[Timer](w, e); t != nil {
			return t.Fired
		}
	}
	if w.rateFilterComponentID != 0 {
		if rf := getRefOnWorld[RateFilter](w, e); rf != nil {
			return rf.Fired
		}
	}
	return false
}

// NewRateFilter creates a new entity configured as a rate filter.
// rate is the N in "fire every N parent ticks"; N<=1 means every parent tick.
// source=0 uses the world frame (fires every Progress call).
// source!=0 uses the Fired flag of that Timer or RateFilter entity.
func NewRateFilter(fw *Writer, rate int32, source ID) ID {
	w := fw.scopeWorld()
	ensureRateFilterID(w)
	e := fw.NewEntity()
	Set[RateFilter](fw, e, RateFilter{Rate: rate, Src: source})
	return e
}

// SetRate sets the Rate field on a RateFilter entity e.
//
// This is a package-level free function that configures a RateFilter entity's Rate field.
// It is distinct from (*System).SetRate(n), which configures a per-system tick-count gate:
//   - flecs.SetRate(fw, e, n) — sets the Rate field on a RateFilter entity (this function)
//   - (*System).SetRate(n)    — sets a per-system n-tick gate (no entity, no *Writer)
//
// No-op if e has no RateFilter component.
func SetRate(fw *Writer, e ID, rate int32) {
	w := fw.scopeWorld()
	rf := getRefOnWorld[RateFilter](w, e)
	if rf == nil {
		return
	}
	rf.Rate = rate
}

// tickAllTimers runs once per Progress call BEFORE phase dispatch.
//
// For each Timer entity:
//  1. Clear the previous tick's Fired flag.
//  2. Skip inactive timers (Active=false).
//  3. Accumulate dt into Elapsed.
//  4. While Elapsed >= Timeout: fire (Fired=true), subtract Timeout.
//     SingleShot: deactivate (Active=false, Elapsed=0) after the first fire; no loop.
//     Interval: loop, carrying the remainder into the next tick.
//
// This matches upstream timer.c subtract-and-carry semantics
// (flecs/src/addons/timer.c, ecs_set_interval accumulator).
// The loop (vs single-subtract-with-cap) matches the expected remainder semantics
// verified by TestTimer_IntervalFires (Elapsed≈50ms after Progress(250ms) with Timeout=100ms).
func tickAllTimers(w *World, dtDur time.Duration) {
	cid := w.timerComponentID
	if cid == 0 {
		return
	}
	w.compIndex.Each(cid, func(t *table.Table) bool {
		n := t.Count()
		for row := 0; row < n; row++ {
			ptr := t.Get(row, cid)
			if ptr == nil {
				continue
			}
			timer := (*Timer)(ptr)
			timer.Fired = false
			if !timer.Active {
				continue
			}
			timer.Elapsed += dtDur
			if timer.SingleShot {
				if timer.Elapsed >= timer.Timeout {
					timer.Fired = true
					timer.Active = false
					timer.Elapsed = 0
				}
			} else {
				for timer.Elapsed >= timer.Timeout {
					timer.Fired = true
					timer.Elapsed -= timer.Timeout
				}
			}
		}
		return true
	})
}

// tickAllRateFilters runs once per Progress call AFTER tickAllTimers.
//
// For each RateFilter entity:
//  1. Clear the previous tick's Fired flag.
//  2. Accumulate dt into TimeElapsed.
//  3. Determine whether the parent fired this tick:
//     - Src==0: parentFired=true (fires every Progress call).
//     - Src!=0: read Timer.Fired or RateFilter.Fired on the source entity.
//     Must run after tickAllTimers so the parent Timer's Fired flag is current.
//  4. If parentFired: increment TickCount; if TickCount >= Rate, fire and reset.
func tickAllRateFilters(w *World, dtDur time.Duration) {
	cid := w.rateFilterComponentID
	if cid == 0 {
		return
	}
	w.compIndex.Each(cid, func(t *table.Table) bool {
		n := t.Count()
		for row := 0; row < n; row++ {
			ptr := t.Get(row, cid)
			if ptr == nil {
				continue
			}
			rf := (*RateFilter)(unsafe.Pointer(ptr))
			rf.Fired = false
			rf.TimeElapsed += dtDur

			var parentFired bool
			if rf.Src == 0 {
				parentFired = true
			} else {
				if w.timerComponentID != 0 {
					if parentTimer := getRefOnWorld[Timer](w, rf.Src); parentTimer != nil {
						parentFired = parentTimer.Fired
					}
				}
				if !parentFired && w.rateFilterComponentID != 0 {
					if parentRF := getRefOnWorld[RateFilter](w, rf.Src); parentRF != nil {
						parentFired = parentRF.Fired
					}
				}
			}

			if parentFired {
				rf.TickCount++
				effectiveRate := rf.Rate
				if effectiveRate <= 1 {
					effectiveRate = 1
				}
				if rf.TickCount >= effectiveRate {
					rf.Fired = true
					rf.TickCount = 0
					rf.TimeElapsed = 0
				}
			}
		}
		return true
	})
}
