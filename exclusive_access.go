package flecs

import "fmt"

func (w *World) ExclusiveAccessBegin(threadName string) {
	id := currentGoid()
	current := w.exclusiveAccess.Load()
	if current != 0 {
		panic(fmt.Errorf("flecs: ExclusiveAccessBegin called while world is already claimed (owner: %q)", w.exclusiveThread))
	}
	w.exclusiveThread = threadName
	w.exclusiveAccess.Store(id)
}

func (w *World) ExclusiveAccessEnd(lockWorld bool) {
	w.exclusiveThread = ""
	if lockWorld {
		w.exclusiveAccess.Store(^uint64(0))
	} else {
		w.exclusiveAccess.Store(0)
	}
}

func (w *World) checkExclusiveAccessWrite() {
	owner := w.exclusiveAccess.Load()
	if owner == 0 {
		return
	}
	if owner == ^uint64(0) {
		panic(fmt.Errorf("flecs: exclusive_access violation: world is locked for writes"))
	}
	if owner != currentGoid() {
		panic(fmt.Errorf("flecs: exclusive_access violation: world is owned by goroutine %q; caller is a different goroutine", w.exclusiveThread))
	}
}

func (w *World) checkExclusiveAccessRead() {
	owner := w.exclusiveAccess.Load()
	if owner == 0 || owner == ^uint64(0) {
		return
	}
	if owner != currentGoid() {
		panic(fmt.Errorf("flecs: exclusive_access violation: world is owned by goroutine %q; concurrent read from a different goroutine is not allowed", w.exclusiveThread))
	}
}
