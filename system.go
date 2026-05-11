package flecs

// System is an opaque handle returned by NewSystem. It links a CachedQuery to a
// callback that runs once per World.Progress call. Call Close to deregister.
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
	removed bool
}

// NewSystem registers a new system on w that runs q's callback fn on each
// World.Progress(dt) call. Systems run in registration order.
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

	sys := &System{w: w, query: q, fn: fn}
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

// Progress runs all registered (non-closed) systems in registration order.
// Each system receives dt and a fresh *QueryIter for its query. The user's
// callback is responsible for calling it.Next() in a loop; if fn does not call
// Next, no entities are touched — that is the caller's choice.
//
// The entire dispatch is wrapped in a single w.Defer block so that any
// mutations (Set, Delete, etc.) performed by system callbacks are queued and
// applied at end-of-frame, after all systems have run. This is the safety
// contract that prevents iterator corruption from mid-iteration entity migrations.
//
// Deferred-removal semantics: the active set is snapshotted at the start of
// Progress. A system that calls Close on a peer during dispatch does not remove
// the peer from the current frame; the peer still runs (same-frame contract).
// On subsequent Progress calls the peer is properly skipped.
//
// Panic behavior: if a system's fn panics, the panic propagates through
// Progress. Go's defer semantics guarantee that the outer DeferEnd still runs,
// so mutations queued by any prior system in the same frame are flushed before
// the panic surfaces.
//
// Compaction of closed systems is NOT done here; it happens lazily in NewSystem.
func (w *World) Progress(dt float32) {
	w.Defer(func() {
		// Snapshot active systems before dispatch. This ensures that if a system
		// closes a peer during the current frame, the peer still runs this frame
		// (deferred-removal contract). Closed systems are not included in the
		// snapshot, so they are never called.
		active := make([]*System, 0, len(w.systems))
		for _, s := range w.systems {
			if !s.removed {
				active = append(active, s)
			}
		}
		for _, s := range active {
			it := s.query.Iter()
			s.fn(dt, it)
		}
	})
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
