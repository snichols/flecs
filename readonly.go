package flecs

// readonlyBegin opens a readonly window. While open, any goroutine may safely
// read world state because all structural mutations are redirected through the
// deferred command queue.
//
// Internal: increments deferDepth then sets the readonly flag, so that any
// mutator called while the flag is true also enqueues rather than applies.
func (w *World) readonlyBegin() {
	w.deferMu.Lock()
	w.deferDepth++
	w.deferMu.Unlock()
	w.readonly.Store(true)
}

// readonlyEnd closes a readonly window opened by readonlyBegin and flushes all
// mutations that were deferred during the window.
func (w *World) readonlyEnd() {
	w.readonly.Store(false)
	w.deferMu.Lock()
	if w.deferDepth <= 0 {
		w.deferMu.Unlock()
		panic("flecs: readonlyEnd called without matching readonlyBegin")
	}
	w.deferDepth--
	if w.deferDepth > 0 {
		w.deferMu.Unlock()
		return
	}
	q := w.deferred
	w.deferred = acquireCmdQueue()
	w.deferMu.Unlock()
	q.flush(w)
	releaseCmdQueue(q)
}
