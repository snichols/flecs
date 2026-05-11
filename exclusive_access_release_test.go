//go:build !flecs_exclusive_access

package flecs_test

import (
	"sync"
	"testing"

	flecs "github.com/snichols/flecs"
)

type relPos struct{ X, Y float32 }

// TestExclusiveAccessReleaseBuildNoop proves that in the default (no-tag) build
// ExclusiveAccessBegin and ExclusiveAccessEnd are no-ops: cross-goroutine writes
// do NOT panic and the world remains usable.
func TestExclusiveAccessReleaseBuildNoop(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()

	// Begin / End should not panic and should not affect normal operation.
	w.ExclusiveAccessBegin("release-goroutine")

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// In a release build this must NOT panic even though another goroutine
		// "owns" the world in the exclusive-access sense.
		flecs.Set(w, e, relPos{7, 8})
	}()
	wg.Wait()

	w.ExclusiveAccessEnd(true) // lock mode is also a no-op in release

	// Write after "lock" must also succeed in release build.
	flecs.Set(w, e, relPos{1, 2})

	pos, ok := flecs.Get[relPos](w, e)
	if !ok || pos.X != 1 {
		t.Fatalf("expected {1,2} after release-build no-op, got %v ok=%v", pos, ok)
	}
}
