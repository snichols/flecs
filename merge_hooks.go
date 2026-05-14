package flecs

// OnPreMerge registers fn to be called immediately before each deferred-command
// merge. fn receives a *Writer so it can read world state and queue mutations;
// those mutations are appended to the queue about to be flushed and participate
// in the same coalescing pass.
//
// Registration returns an int ID (slice index). The hook fires on every
// subsequent merge until removed via RemovePreMergeHook.
//
// Hooks fire in FIFO registration order, skipping nil tombstones.
// OnPreMerge must be called outside a Write scope.
func OnPreMerge(w *World, fn func(fw *Writer)) int {
	w.checkExclusiveAccessWrite()
	w.preMergeHooks = append(w.preMergeHooks, fn)
	return len(w.preMergeHooks) - 1
}

// OnPostMerge registers fn to be called immediately after each deferred-command
// merge completes. fn receives a *Writer; mutations queued inside fn land in the
// fresh command queue and are flushed on the next merge.
//
// Registration returns an int ID (slice index). The hook fires on every
// subsequent merge until removed via RemovePostMergeHook.
//
// Hooks fire in FIFO registration order, skipping nil tombstones.
// OnPostMerge must be called outside a Write scope.
func OnPostMerge(w *World, fn func(fw *Writer)) int {
	w.checkExclusiveAccessWrite()
	w.postMergeHooks = append(w.postMergeHooks, fn)
	return len(w.postMergeHooks) - 1
}

// RemovePreMergeHook removes the pre-merge hook with the given id. Stale or
// already-removed ids are silent no-ops. Removal is idempotent: the slot is
// tombstoned (set to nil) so that subsequent IDs remain stable.
func RemovePreMergeHook(w *World, id int) {
	if id < 0 || id >= len(w.preMergeHooks) {
		return
	}
	w.preMergeHooks[id] = nil
}

// RemovePostMergeHook removes the post-merge hook with the given id. Stale or
// already-removed ids are silent no-ops.
func RemovePostMergeHook(w *World, id int) {
	if id < 0 || id >= len(w.postMergeHooks) {
		return
	}
	w.postMergeHooks[id] = nil
}

// firePreMergeHooks calls all registered pre-merge hooks in FIFO order.
// It captures a snapshot of the hook slice so that hooks registered during
// firing only take effect on the next merge.
// deferDepth is temporarily ensured ≥ 1 during execution so that any mutations
// the hook queues land in s0.queue (which is then captured and flushed as part
// of the current merge) rather than being applied immediately.
func (w *World) firePreMergeHooks() {
	if len(w.preMergeHooks) == 0 {
		return
	}
	snap := w.preMergeHooks
	s0 := w.stages[0]
	prev := s0.deferDepth
	if prev == 0 {
		s0.deferDepth = 1
	}
	fw := &w.writeCapability
	for _, fn := range snap {
		if fn != nil {
			fn(fw)
		}
	}
	s0.deferDepth = prev
}

// firePostMergeHooks calls all registered post-merge hooks in FIFO order.
// It captures a snapshot of the hook slice so that hooks registered during
// firing only take effect on the next merge.
// deferDepth is temporarily ensured ≥ 1 during execution so that any mutations
// the hook queues land in the fresh s0.queue (flushed on the next merge) rather
// than being applied immediately.
func (w *World) firePostMergeHooks() {
	if len(w.postMergeHooks) == 0 {
		return
	}
	snap := w.postMergeHooks
	s0 := w.stages[0]
	prev := s0.deferDepth
	if prev == 0 {
		s0.deferDepth = 1
	}
	fw := &w.writeCapability
	for _, fn := range snap {
		if fn != nil {
			fn(fw)
		}
	}
	s0.deferDepth = prev
}
