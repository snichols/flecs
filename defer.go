package flecs

// IsDeferred reports whether the world is currently in a deferred mode.
// Returns true iff a deferred scope is active (deferDepth > 0).
func (w *World) IsDeferred() bool {
	w.deferMu.Lock()
	d := w.deferDepth > 0
	w.deferMu.Unlock()
	return d
}
