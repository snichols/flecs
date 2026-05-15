package flecs

// stage is an internal per-goroutine command-queue context.
//
// The main stage (id == 0) is used by the calling goroutine inside world.Write
// and world.deferScope. Worker stages (id == 1..N) are owned by the N goroutines
// in the persistent worker pool; each accumulates deferred mutations on the hot
// path without synchronization — single-goroutine ownership is the invariant.
//
// Parallel-batch systems: the batch is dispatched in sequential waves of at most
// workerCount. Within each wave, system wavePos exclusively owns stage (wavePos+1),
// so concurrent goroutines write to disjoint stages. Waves execute sequentially
// via wg.Wait(), preventing cross-wave concurrent access to any stage.
//
// Cross-stage merge policy: stages are merged into the world in ascending id
// order. Within a stage the existing per-entity FIFO coalescer applies (100
// AddID calls on one entity → one archetype migration). Across stages there is no
// coalescing — two stages mutating the same entity produce two separate archetype
// migrations in id order. Merges happen after wg.Wait for both multi-threaded and
// parallel-batch dispatch blocks, and when the outermost Write scope exits.
type stage struct {
	id         int
	queue      *cmdQueue
	world      *World
	deferDepth int  // > 0 while inside a Write or deferScope; mutations are queued not immediate
	inMerge    bool // true while pre/post merge hooks are firing; guards against re-entrant Write
}
