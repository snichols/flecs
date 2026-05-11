package flecs

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// System is an opaque handle returned by NewSystem or NewSystemInPhase. It
// links a CachedQuery to a callback that runs once per World.Progress call
// within its assigned pipeline phase. Call Close to deregister.
//
// The callback receives the dt value passed to Progress and a fresh *QueryIter
// positioned before the first table. The callback is responsible for calling
// it.Next() in a loop; Field[T] and other iterator helpers work normally inside
// the callback.
//
// *System is NOT goroutine-safe; external synchronization is required.
type System struct {
	w       *World
	query   *CachedQuery
	fn      func(dt float32, it *QueryIter)
	phase   ID // which pipeline phase this system belongs to
	removed bool
}

// NewSystem registers a new system on w in the OnUpdate phase that runs q's
// callback fn on each World.Progress(dt) call. Systems within a phase run in
// registration order.
//
// Panics if any argument is nil, if q is already closed, or if q belongs to a
// different world than w.
//
// Closed (removed) entries in w.systems are compacted lazily before the new
// system is appended — the same amortized pattern as NewCachedQuery.
//
// The query must be *CachedQuery (not uncached *Query) because systems run every
// frame; the cached path's pre-filtered table tracking is essential for
// per-frame performance.
func NewSystem(w *World, q *CachedQuery, fn func(dt float32, it *QueryIter)) *System {
	if w == nil {
		panic("flecs: NewSystem: world must not be nil")
	}
	if q == nil {
		panic("flecs: NewSystem: query must not be nil")
	}
	if fn == nil {
		panic("flecs: NewSystem: fn must not be nil")
	}
	if q.IsClosed() {
		panic("flecs: NewSystem: query is already closed")
	}
	if q.w != w {
		panic("flecs: NewSystem: query belongs to a different world")
	}

	// Compact removed entries lazily before appending.
	live := w.systems[:0]
	for _, s := range w.systems {
		if !s.removed {
			live = append(live, s)
		}
	}
	w.systems = live

	sys := &System{w: w, query: q, fn: fn, phase: w.onUpdateID}
	w.systems = append(w.systems, sys)
	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "system added",
			slog.String("phase", phaseName(w, sys.phase)))
	}
	return sys
}

// NewSystemInPhase registers a new system in the given pipeline phase.
// phase must be one of w.PreUpdate(), w.OnUpdate(), w.PostUpdate(), or w.OnFixedUpdate().
//
// All other validations match NewSystem: panics on nil world/query/fn, closed
// query, or a query that belongs to a different world.
func NewSystemInPhase(w *World, phase ID, q *CachedQuery, fn func(dt float32, it *QueryIter)) *System {
	if w == nil {
		panic("flecs: NewSystemInPhase: world must not be nil")
	}
	if phase != w.preUpdateID && phase != w.onUpdateID && phase != w.postUpdateID && phase != w.onFixedUpdateID {
		panic(fmt.Sprintf("flecs: NewSystemInPhase: phase ID %d is not a recognized built-in phase; valid: PreUpdate, OnUpdate, PostUpdate, OnFixedUpdate", phase))
	}
	if q == nil {
		panic("flecs: NewSystemInPhase: query must not be nil")
	}
	if fn == nil {
		panic("flecs: NewSystemInPhase: fn must not be nil")
	}
	if q.IsClosed() {
		panic("flecs: NewSystemInPhase: query is already closed")
	}
	if q.w != w {
		panic("flecs: NewSystemInPhase: query belongs to a different world")
	}

	// Compact removed entries lazily before appending.
	live := w.systems[:0]
	for _, s := range w.systems {
		if !s.removed {
			live = append(live, s)
		}
	}
	w.systems = live

	sys := &System{w: w, query: q, fn: fn, phase: phase}
	w.systems = append(w.systems, sys)
	if w.logger != nil {
		w.logger.LogAttrs(context.Background(), slog.LevelDebug, "system added",
			slog.String("phase", phaseName(w, sys.phase)))
	}
	return sys
}

// Close marks this system as removed. Idempotent: safe to call multiple times.
// After Close returns, the system will be skipped in subsequent Progress calls.
//
// Deferred-removal semantics: if Close is called by another system during an
// active Progress dispatch, whether this system still runs in the current frame
// depends on whether Progress already captured the active set before Close was
// called. Progress snapshots the active systems at the start of dispatch; any
// system in that snapshot still runs even if Close is called mid-frame. On
// the next Progress call the system is definitively skipped.
func (s *System) Close() {
	if s.removed {
		return
	}
	s.removed = true
	if s.w.logger != nil {
		s.w.logger.LogAttrs(context.Background(), slog.LevelDebug, "system closed")
	}
}

// IsClosed reports whether Close has been called.
func (s *System) IsClosed() bool {
	return s.removed
}

// Progress runs all registered (non-closed) systems in pipeline phase order:
// PreUpdate → OnFixedUpdate (accumulator loop) → OnUpdate → PostUpdate.
// Within a phase, systems run in registration order.
//
// dt must be >= 0; Progress panics on negative dt. A zero dt is allowed and
// documents as a "null frame" that still increments FrameCount.
//
// Each variable-rate phase (PreUpdate, OnUpdate, PostUpdate) is wrapped in its
// own w.Defer block. The OnFixedUpdate phase runs inside a per-iteration Defer
// block so that mutations from one fixed tick are visible to the next fixed
// tick within the same Progress call.
//
// Mutations queued by systems in an earlier phase are flushed before the next
// phase starts, so cross-phase visibility is guaranteed.
//
// Within a single phase iteration, mutations are queued and NOT yet visible to
// peer systems in the same iteration (same-phase safety contract).
//
// Deferred-removal semantics: each phase's active set is snapshotted at the
// start of that phase's Defer block. A system Closed in an earlier phase is
// excluded from later-phase snapshots.
//
// Compaction of closed systems is NOT done here; it happens lazily in NewSystem
// and NewSystemInPhase.
func (w *World) Progress(dt float32) {
	if dt < 0 {
		panic("flecs: Progress: dt must be >= 0")
	}
	w.frameCount++
	w.time += dt

	runPhase := func(p ID, phaseDT float32) {
		w.Defer(func() {
			active := make([]*System, 0, len(w.systems))
			for _, s := range w.systems {
				if !s.removed && s.phase == p {
					active = append(active, s)
				}
			}
			for _, s := range active {
				it := s.query.Iter()
				s.fn(phaseDT, it)
			}
		})
	}

	countPhase := func(p ID) int {
		n := 0
		for _, s := range w.systems {
			if !s.removed && s.phase == p {
				n++
			}
		}
		return n
	}

	runTimed := func(phaseIdx int, name string, p ID, phaseDT float32) {
		count := countPhase(p)
		w.lastFramePhases[phaseIdx].Name = name
		w.lastFramePhases[phaseIdx].SystemCount = count
		if count > 0 {
			start := time.Now()
			runPhase(p, phaseDT)
			w.lastFramePhases[phaseIdx].Duration = time.Since(start)
		} else {
			runPhase(p, phaseDT)
			w.lastFramePhases[phaseIdx].Duration = 0
		}
	}

	// PreUpdate with real dt.
	runTimed(0, "PreUpdate", w.preUpdateID, dt)

	// OnFixedUpdate: accumulator loop; sum durations across all iterations.
	{
		count := countPhase(w.onFixedUpdateID)
		w.lastFramePhases[1].Name = "OnFixedUpdate"
		w.lastFramePhases[1].SystemCount = count
		var total time.Duration
		if w.fixedTimestep > 0 {
			w.fixedAccumulator += dt
			for w.fixedAccumulator >= w.fixedTimestep {
				step := w.fixedTimestep
				if count > 0 {
					start := time.Now()
					runPhase(w.onFixedUpdateID, step)
					total += time.Since(start)
				} else {
					runPhase(w.onFixedUpdateID, step)
				}
				w.fixedAccumulator -= step
			}
		}
		w.lastFramePhases[1].Duration = total
	}

	// OnUpdate and PostUpdate with real dt.
	runTimed(2, "OnUpdate", w.onUpdateID, dt)
	runTimed(3, "PostUpdate", w.postUpdateID, dt)
}

// SystemCount returns the number of currently registered (non-closed) systems.
func (w *World) SystemCount() int {
	n := 0
	for _, s := range w.systems {
		if !s.removed {
			n++
		}
	}
	return n
}

// phaseName returns a human-readable name for the given pipeline phase ID.
func phaseName(w *World, phase ID) string {
	switch phase {
	case w.preUpdateID:
		return "PreUpdate"
	case w.onUpdateID:
		return "OnUpdate"
	case w.postUpdateID:
		return "PostUpdate"
	case w.onFixedUpdateID:
		return "OnFixedUpdate"
	default:
		return "unknown"
	}
}
