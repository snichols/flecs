package flecs

// stage is an internal per-goroutine command-queue context.
//
// The main stage (id == 0) is used by the calling goroutine inside world.Write
// and world.deferScope. Worker stages (id == 1..N) are owned by the N goroutines
// in the persistent worker pool; each accumulates deferred mutations on the hot
// path without synchronization — single-goroutine ownership is the invariant.
//
// Cross-stage merge policy: stages are merged into the world in ascending id
// order. Within a stage the existing per-entity FIFO coalescer applies (100
// AddID calls on one entity → one archetype migration). Across stages there is no
// coalescing — two stages mutating the same entity produce two separate archetype
// migrations in id order. Merges happen at the end of the multi-threaded dispatch
// block (after wg.Wait) and when the outermost Write scope exits.
type stage struct {
	id         int
	queue      *cmdQueue
	world      *World
	deferDepth int // > 0 while inside a Write or deferScope; mutations are queued not immediate
}
