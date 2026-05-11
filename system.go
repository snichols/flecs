package flecs

import "fmt"

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
	return sys
}

// NewSystemInPhase registers a new system in the given pipeline phase.
// phase must be one of w.PreUpdate(), w.OnUpdate(), or w.PostUpdate().
//
// All other validations match NewSystem: panics on nil world/query/fn, closed
// query, or a query that belongs to a different world.
func NewSystemInPhase(w *World, phase ID, q *CachedQuery, fn func(dt float32, it *QueryIter)) *System {
	if w == nil {
		panic("flecs: NewSystemInPhase: world must not be nil")
	}
	if phase != w.preUpdateID && phase != w.onUpdateID && phase != w.postUpdateID {
		panic(fmt.Sprintf("flecs: NewSystemInPhase: phase ID %d is not a recognized built-in phase; valid: PreUpdate, OnUpdate, PostUpdate", phase))
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
	s.removed = true
}

// IsClosed reports whether Close has been called.
func (s *System) IsClosed() bool {
	return s.removed
}

// Progress runs all registered (non-closed) systems in pipeline phase order:
// PreUpdate, then OnUpdate, then PostUpdate. Within a phase, systems run in
// registration order.
//
// Each phase is wrapped in its own w.Defer block. Mutations queued by systems
// in an earlier phase are flushed before the next phase starts, so cross-phase
// visibility is guaranteed: a value Set in PreUpdate is visible to Get calls in
// OnUpdate.
//
// Within a single phase, mutations are queued and NOT yet visible to peer
// systems in the same phase (same-phase safety contract, unchanged from 7.1).
//
// Deferred-removal semantics: each phase's active set is snapshotted at the
// start of that phase's Defer block. A system Closed in an earlier phase is
// excluded from the snapshot of later phases (its removed flag is set before
// those snapshots are taken). A system Closed by a peer within the same phase
// is still in that phase's snapshot and runs this frame.
//
// Panic behavior: if a system's fn panics, the panic propagates through
// Progress. Go's defer semantics guarantee that the current phase's DeferEnd
// still runs, flushing mutations queued before the panic.
//
// Compaction of closed systems is NOT done here; it happens lazily in NewSystem
// and NewSystemInPhase.
func (w *World) Progress(dt float32) {
	phases := [3]ID{w.preUpdateID, w.onUpdateID, w.postUpdateID}
	for _, phase := range phases {
		p := phase // capture for closure
		w.Defer(func() {
			// Snapshot active systems for this phase at the start of the phase's
			// Defer block. Systems Closed in a prior phase are already removed=true
			// and are excluded here.
			active := make([]*System, 0, len(w.systems))
			for _, s := range w.systems {
				if !s.removed && s.phase == p {
					active = append(active, s)
				}
			}
			for _, s := range active {
				it := s.query.Iter()
				s.fn(dt, it)
			}
		})
	}
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
