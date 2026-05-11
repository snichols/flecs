package flecs

// ReadonlyBegin opens a readonly window. While open, any goroutine may safely
// read world state (via Each1/Each2/Each3/Each4, Iter, Get, Has, etc.) because
// all structural mutations from any goroutine are redirected through the deferred
// command queue rather than applied immediately.
//
// ReadonlyBegin must be paired with a matching ReadonlyEnd. Use the [World.Readonly]
// convenience wrapper to ensure the pair is always balanced.
//
// Implementation: DeferBegin increments deferDepth (under deferMu) so that any
// mutator called before readonly.Store sees deferDepth > 0 and enqueues.  The
// readonly flag is set after DeferBegin returns, covering the window for
// concurrent goroutines that hold a reference to the world.
func (w *World) ReadonlyBegin() {
	w.DeferBegin()
	w.readonly.Store(true)
}

// ReadonlyEnd closes a readonly window opened by ReadonlyBegin and flushes all
// mutations that were deferred during the window. The readonly flag is cleared
// before DeferEnd so that the flush itself runs in immediate (non-deferred) mode.
func (w *World) ReadonlyEnd() {
	w.readonly.Store(false)
	w.DeferEnd()
}

// Readonly opens a readonly window, calls fn, then closes the window and flushes
// all mutations that were enqueued during fn.  ReadonlyEnd is guaranteed to run
// even if fn panics.
//
// Example — concurrent readers while the world is held read-only:
//
//	w.Readonly(func() {
//	    var wg sync.WaitGroup
//	    for range 8 {
//	        wg.Add(1)
//	        go func() {
//	            defer wg.Done()
//	            flecs.Each1[Position](w, func(e flecs.ID, p *Position) { ... })
//	        }()
//	    }
//	    wg.Wait()
//	}) // deferred mutations (if any) are applied here
func (w *World) Readonly(fn func()) {
	w.ReadonlyBegin()
	defer w.ReadonlyEnd()
	fn()
}
