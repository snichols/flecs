package flecs

import "fmt"

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
func (w *World) ExclusiveAccessBegin(threadName string) {
	if !flecsExclusiveAccess {
		return
	}
	id := goid()
	current := w.exclusiveAccess.Load()
	if current != 0 {
		panic(fmt.Errorf("flecs: ExclusiveAccessBegin called while world is already claimed (owner: %q)", w.exclusiveThread))
	}
	w.exclusiveThread = threadName
	w.exclusiveAccess.Store(id)
}

// ExclusiveAccessEnd releases the world. If lockWorld is true, writes from all
// goroutines will panic in debug builds until the next ExclusiveAccessBegin
// call; reads still pass (world is "fully locked"). If lockWorld is false, the
// world returns to the unclaimed state where any goroutine may read or write.
//
// In release builds this is a no-op.
func (w *World) ExclusiveAccessEnd(lockWorld bool) {
	if !flecsExclusiveAccess {
		return
	}
	w.exclusiveThread = ""
	if lockWorld {
		w.exclusiveAccess.Store(^uint64(0))
	} else {
		w.exclusiveAccess.Store(0)
	}
}

// checkExclusiveAccessWrite panics in debug builds if another goroutine claimed
// the world via ExclusiveAccessBegin, or if the world is fully locked for writes.
// In release builds the body is a single constant-false branch and the compiler
// eliminates the entire call.
func (w *World) checkExclusiveAccessWrite() {
	if !flecsExclusiveAccess {
		return
	}
	owner := w.exclusiveAccess.Load()
	if owner == 0 {
		return
	}
	if owner == ^uint64(0) {
		panic(fmt.Errorf("flecs: exclusive_access violation: world is locked for writes"))
	}
	if owner != goid() {
		panic(fmt.Errorf("flecs: exclusive_access violation: world is owned by goroutine %q; caller is a different goroutine", w.exclusiveThread))
	}
}

// checkExclusiveAccessRead panics in debug builds if the world is exclusively
// owned by a different goroutine. Reads are always permitted when the world is
// unclaimed (owner == 0) or fully locked (owner == ^uint64(0)).
func (w *World) checkExclusiveAccessRead() {
	if !flecsExclusiveAccess {
		return
	}
	owner := w.exclusiveAccess.Load()
	if owner == 0 || owner == ^uint64(0) {
		return
	}
	if owner != goid() {
		panic(fmt.Errorf("flecs: exclusive_access violation: world is owned by goroutine %q; concurrent read from a different goroutine is not allowed", w.exclusiveThread))
	}
}
