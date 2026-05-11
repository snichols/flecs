//go:build !race

package flecs_test

import (
	"testing"

	flecs "github.com/snichols/flecs"
)

// TestExclusiveAccessZeroOverheadCommonPath verifies that with no ExclusiveAccessBegin
// ever called, Set allocates 0 bytes per call (the common-path cost is just one atomic.Load).
// This test is excluded from race-detector builds because the race detector's shadow memory
// instrumentation causes closure captures to escape to the heap, yielding spurious allocations.
func TestExclusiveAccessZeroOverheadCommonPath(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, xaPos{0, 0}) // warm up archetype
	})

	allocs := testing.AllocsPerRun(1000, func() {
		w.Write(func(fw *flecs.Writer) {
			flecs.Set(fw, e, xaPos{1, 2})
		})
	})
	if allocs > 0 {
		t.Fatalf("expected 0 allocations per Set in common path, got %.1f", allocs)
	}
}
