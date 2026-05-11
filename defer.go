package flecs

// IsDeferred reports whether the world is currently in a deferred mode.
// Returns true iff DeferBegin has been called more times than DeferEnd.
func (w *World) IsDeferred() bool {
	w.deferMu.Lock()
	d := w.deferDepth > 0
	w.deferMu.Unlock()
	return d
}

// DeferBegin increments the defer depth counter. Within a deferred block,
// Set/Remove/Delete/AddID/RemoveID/SetPair/SetName/RemoveName operations are
// queued and applied on DeferEnd. Reads (Get/Has/Owns/IsAlive) see CURRENT
// state, not the deferred future state.
//
// DeferBegin/DeferEnd calls must be balanced. Call DeferEnd to close the scope.
// Nested DeferBegin increments the depth counter; the queue flushes only when
// the outermost DeferEnd brings depth back to zero.
func (w *World) DeferBegin() {
	w.deferMu.Lock()
	w.deferDepth++
	w.deferMu.Unlock()
}

// DeferEnd decrements the defer depth counter. When depth reaches zero, all
// queued operations are applied in the order they were enqueued and the queue
// is cleared. If depth is still non-zero after decrementing (nested scopes),
// no flush occurs.
//
// Panics if called without a matching DeferBegin.
//
// Flush semantics:
//   - Each queued operation is invoked with deferDepth == 0, so it applies
//     immediately (hooks and observers fire in flush order).
//   - The mutex is NOT held during flush; individual queue appends from
//     concurrent goroutines that run during the flush are protected by the
//     mutex on their own.
//   - If a closure panics during flush, the flush aborts and remaining queued
//     operations are lost. The panic propagates to the caller.
//   - Re-deferring during flush: if a closure explicitly calls DeferBegin, a
//     fresh nested defer scope starts. That scope's DeferEnd must be called
//     before control returns to the outer flush loop.
func (w *World) DeferEnd() {
	w.deferMu.Lock()
	if w.deferDepth <= 0 {
		w.deferMu.Unlock()
		panic("flecs: DeferEnd called without matching DeferBegin")
	}
	w.deferDepth--
	if w.deferDepth > 0 {
		w.deferMu.Unlock()
		return
	}
	queue := w.deferred
	w.deferred = nil
	w.deferMu.Unlock()
	for _, fn := range queue {
		fn(w)
	}
}

// Defer wraps fn in DeferBegin/DeferEnd. Even if fn panics, DeferEnd is
// guaranteed to run via a Go defer statement.
//
// Recommended over manual DeferBegin/DeferEnd for the matched-pair guarantee.
//
// Example:
//
//	w.Defer(func() {
//	    flecs.Each1[Position](w, func(e flecs.ID, p *Position) {
//	        if p.X < 0 {
//	            flecs.Delete(w, e)  // queued
//	        }
//	    })
//	})  // all deletes apply here, after iteration finishes
func (w *World) Defer(fn func()) {
	w.DeferBegin()
	defer w.DeferEnd()
	fn()
}
