//go:build !flecs_exclusive_access

package flecs

// ExclusiveAccessBegin claims the world for the calling goroutine. In debug
// builds (-tags flecs_exclusive_access), any mutation from another goroutine
// panics with an exclusive-access violation error. In release builds this is a
// no-op.
//
// threadName is recorded for diagnostics; it appears in violation panic messages
// to help identify the owning goroutine by a human-readable label.
//
// Panics if the world is already claimed (including by the same goroutine), so
// that nested Begin calls are caught immediately. Pair each Begin with exactly
// one ExclusiveAccessEnd.
func (w *World) ExclusiveAccessBegin(threadName string) {}

// ExclusiveAccessEnd releases the world. If lockWorld is true, writes from all
// goroutines will panic in debug builds until the next ExclusiveAccessBegin
// call; reads still pass (world is "fully locked"). If lockWorld is false, the
// world returns to the unclaimed state where any goroutine may read or write.
//
// In release builds this is a no-op.
func (w *World) ExclusiveAccessEnd(lockWorld bool) {}

func (w *World) checkExclusiveAccessWrite() {}
func (w *World) checkExclusiveAccessRead()  {}
