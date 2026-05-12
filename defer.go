package flecs

// IsDeferred reports whether the world is currently in a deferred mode.
// Returns true iff a deferred scope is active (stages[0].deferDepth > 0).
func (w *World) IsDeferred() bool {
	return w.stages[0].deferDepth > 0
}
