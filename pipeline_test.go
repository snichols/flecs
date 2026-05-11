package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// helper types local to pipeline tests
type PPos struct{ X float32 }
type PVel struct{ X float32 }

// TestPipelinePhaseAccessors verifies the three built-in phase accessors return
// distinct, alive IDs that are stable across repeated calls.
func TestPipelinePhaseAccessors(t *testing.T) {
	w := flecs.New()

	pre := w.PreUpdate()
	on := w.OnUpdate()
	post := w.PostUpdate()

	if pre == on || on == post || pre == post {
		t.Fatalf("phase IDs must be distinct: PreUpdate=%v OnUpdate=%v PostUpdate=%v", pre, on, post)
	}
	if !w.IsAlive(pre) {
		t.Fatal("PreUpdate phase entity must be alive")
	}
	if !w.IsAlive(on) {
		t.Fatal("OnUpdate phase entity must be alive")
	}
	if !w.IsAlive(post) {
		t.Fatal("PostUpdate phase entity must be alive")
	}
	// Stable: repeated calls return the same ID.
	if w.PreUpdate() != pre || w.OnUpdate() != on || w.PostUpdate() != post {
		t.Fatal("phase accessors must return consistent IDs across calls")
	}
}

// TestPipelinePhaseOrdering verifies that systems in PreUpdate, OnUpdate, and
// PostUpdate always execute in that fixed order regardless of registration order.
func TestPipelinePhaseOrdering(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[PPos](w.W(), e, PPos{})

	var order []string
	flecs.NewSystemInPhase(w, w.PreUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "pre")
		}
	})
	flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "on")
		}
	})
	flecs.NewSystemInPhase(w, w.PostUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "post")
		}
	})

	w.Progress(0)
	if len(order) != 3 || order[0] != "pre" || order[1] != "on" || order[2] != "post" {
		t.Fatalf("expected [pre on post], got %v", order)
	}
}

// TestPipelineRegistrationOrderIgnoredAcrossPhases registers an OnUpdate system
// before a PreUpdate system and verifies PreUpdate still runs first.
func TestPipelineRegistrationOrderIgnoredAcrossPhases(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[PPos](w.W(), e, PPos{})

	var order []string
	// Register OnUpdate first.
	flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "on")
		}
	})
	// Register PreUpdate second.
	flecs.NewSystemInPhase(w, w.PreUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "pre")
		}
	})

	w.Progress(0)
	if len(order) != 2 || order[0] != "pre" || order[1] != "on" {
		t.Fatalf("expected [pre on] regardless of registration order, got %v", order)
	}
}

// TestPipelineRegistrationOrderWithinPhase verifies that within a single phase,
// systems run in registration order.
func TestPipelineRegistrationOrderWithinPhase(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[PPos](w.W(), e, PPos{})

	var order []string
	flecs.NewSystemInPhase(w, w.PreUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "A")
		}
	})
	flecs.NewSystemInPhase(w, w.PreUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "B")
		}
	})

	w.Progress(0)
	if len(order) != 2 || order[0] != "A" || order[1] != "B" {
		t.Fatalf("expected [A B] (registration order within phase), got %v", order)
	}
}

// TestPipelineDefaultPhaseIsOnUpdate verifies that NewSystem (no phase argument)
// places the system in OnUpdate, running after PreUpdate and before PostUpdate.
func TestPipelineDefaultPhaseIsOnUpdate(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[PPos](w.W(), e, PPos{})

	var order []string
	flecs.NewSystemInPhase(w, w.PreUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "pre")
		}
	})
	// Default phase via NewSystem.
	flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "on")
		}
	})
	flecs.NewSystemInPhase(w, w.PostUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "post")
		}
	})

	w.Progress(0)
	if len(order) != 3 || order[0] != "pre" || order[1] != "on" || order[2] != "post" {
		t.Fatalf("expected [pre on post], got %v — NewSystem must default to OnUpdate", order)
	}
}

// TestPipelineCrossPhaseFlush is the key behavioral test: a value Set during
// PreUpdate (queued in that phase's Defer) is visible via Get during OnUpdate,
// because PreUpdate's Defer flushes before OnUpdate starts.
func TestPipelineCrossPhaseFlush(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)

	e := w.NewEntity()
	flecs.Set[PPos](w.W(), e, PPos{X: 1})

	// PreUpdate: queue a Set that changes X to 99.
	flecs.NewSystemInPhase(w, w.PreUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			flecs.Set[PPos](w.W(), e, PPos{X: 99})
		}
	})

	var seenInOnUpdate float32
	// OnUpdate: read X — expects 99 because PreUpdate's Defer has already flushed.
	flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, _ *flecs.QueryIter) {
		v, _ := flecs.Get[PPos](w.R(), e)
		seenInOnUpdate = v.X
	})

	w.Progress(0)
	if seenInOnUpdate != 99 {
		t.Fatalf("cross-phase flush: expected OnUpdate to see X=99, got %v", seenInOnUpdate)
	}
}

// TestPipelineCrossPhaseEntityCreation verifies that an entity created (and Set)
// in PreUpdate is visible to Has queries in OnUpdate after the inter-phase flush.
func TestPipelineCrossPhaseEntityCreation(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)

	var sharedEntity flecs.ID

	flecs.NewSystemInPhase(w, w.PreUpdate(), q, func(_ float32, _ *flecs.QueryIter) {
		// NewEntity is immediate; Set is queued and flushed at end of PreUpdate.
		sharedEntity = w.NewEntity()
		flecs.Set[PPos](w.W(), sharedEntity, PPos{X: 5})
	})

	var hasPos bool
	flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, _ *flecs.QueryIter) {
		hasPos = flecs.Has[PPos](w.R(), sharedEntity)
	})

	w.Progress(0)
	if !hasPos {
		t.Fatal("cross-phase entity creation: expected Has[PPos]==true in OnUpdate after PreUpdate flush")
	}
}

// TestPipelineWithinPhaseNoVisibility verifies that a Set queued by system A in
// PreUpdate is NOT yet visible to system B in the same phase: Get returns the old
// value because the Defer for that phase has not yet flushed.
func TestPipelineWithinPhaseNoVisibility(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)

	e := w.NewEntity()
	flecs.Set[PPos](w.W(), e, PPos{X: 1})

	// A: queue a mutation to X=99.
	flecs.NewSystemInPhase(w, w.PreUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			flecs.Set[PPos](w.W(), e, PPos{X: 99})
		}
	})

	var seenByB float32
	// B: read X — must see OLD value (1) because A's Set is still queued.
	flecs.NewSystemInPhase(w, w.PreUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			v, _ := flecs.Get[PPos](w.R(), e)
			seenByB = v.X
		}
	})

	w.Progress(0)
	if seenByB != 1 {
		t.Fatalf("within-phase: expected B to see old X=1 (mutation invisible until phase Defer flushes), got %v", seenByB)
	}
}

// TestPipelineInvalidPhasePanics verifies that NewSystemInPhase panics when
// passed a non-phase entity (e.g. w.ChildOf()).
func TestPipelineInvalidPhasePanics(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)
	fn := func(_ float32, _ *flecs.QueryIter) {}

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for invalid phase ID, got none")
		}
	}()
	flecs.NewSystemInPhase(w, w.ChildOf(), q, fn)
}

// TestPipelineExplicitOnUpdateEquivalentToDefault verifies that
// NewSystemInPhase(w, w.OnUpdate(), q, fn) is functionally equivalent to
// NewSystem(w, q, fn): both place the system in OnUpdate.
func TestPipelineExplicitOnUpdateEquivalentToDefault(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[PPos](w.W(), e, PPos{})

	var order []string
	flecs.NewSystemInPhase(w, w.PreUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "pre")
		}
	})
	flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "explicit-on")
		}
	})
	flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			order = append(order, "default-on")
		}
	})

	w.Progress(0)
	if len(order) != 3 || order[0] != "pre" || order[1] != "explicit-on" || order[2] != "default-on" {
		t.Fatalf("expected [pre explicit-on default-on], got %v", order)
	}
}

// TestPipelineEmptyPhases verifies that Progress does not panic when PreUpdate
// and PostUpdate have no systems registered.
func TestPipelineEmptyPhases(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[PPos](w.W(), e, PPos{})

	ran := false
	flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			ran = true
		}
	})

	w.Progress(0) // PreUpdate and PostUpdate are empty — must not panic
	if !ran {
		t.Fatal("expected OnUpdate system to run when Pre/PostUpdate are empty")
	}
}

// TestPipelineSystemCountAcrossPhases verifies SystemCount sums across all phases.
func TestPipelineSystemCountAcrossPhases(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)
	fn := func(_ float32, _ *flecs.QueryIter) {}

	flecs.NewSystemInPhase(w, w.PreUpdate(), q, fn)
	flecs.NewSystemInPhase(w, w.PreUpdate(), q, fn)
	flecs.NewSystemInPhase(w, w.OnUpdate(), q, fn)
	flecs.NewSystemInPhase(w, w.OnUpdate(), q, fn)
	flecs.NewSystemInPhase(w, w.OnUpdate(), q, fn)
	s := flecs.NewSystemInPhase(w, w.PostUpdate(), q, fn)

	if got := w.SystemCount(); got != 6 {
		t.Fatalf("expected SystemCount=6 (2+3+1), got %d", got)
	}

	s.Close()
	if got := w.SystemCount(); got != 5 {
		t.Fatalf("expected SystemCount=5 after closing PostUpdate system, got %d", got)
	}
}

// TestPipelineCloseDuringDispatchCrossPhase verifies that a system A in PreUpdate
// can close system B in OnUpdate, and B is excluded from the same frame because
// OnUpdate's snapshot is taken AFTER PreUpdate's Defer flushes.
func TestPipelineCloseDuringDispatchCrossPhase(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)
	e := w.NewEntity()
	flecs.Set[PPos](w.W(), e, PPos{})

	bRan := false
	var sysB *flecs.System

	flecs.NewSystemInPhase(w, w.PreUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			sysB.Close()
		}
	})
	sysB = flecs.NewSystemInPhase(w, w.OnUpdate(), q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			bRan = true
		}
	})

	// A (PreUpdate) closes B (OnUpdate) before OnUpdate's per-phase snapshot is taken.
	// B must NOT run this frame.
	w.Progress(0)
	if bRan {
		t.Fatal("B (OnUpdate) must not run when closed by A (PreUpdate) in the same frame: cross-phase snapshot excludes it")
	}
}

// TestPipelineOuterDeferInteraction verifies that wrapping Progress in a user
// Write scope composes correctly: phase Write scopes nest (DeferDepth >= 2), systems
// run, and mutations are flushed when the outermost Write scope exits.
func TestPipelineOuterDeferInteraction(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[PPos](w)
	q := flecs.NewCachedQuery(w, posID)

	e := w.NewEntity()
	flecs.Set[PPos](w.W(), e, PPos{X: 1})

	count := 0
	flecs.NewSystem(w, q, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			count++
			// Queue a mutation; flushes when outermost Write scope exits.
			flecs.Set[PPos](w.W(), e, PPos{X: 99})
		}
	})

	// Outer Write → Progress → per-phase Write scopes all nest correctly.
	w.Write(func(_ *flecs.Writer) {
		w.Progress(0.016)
	})

	if count != 1 {
		t.Fatalf("expected system to run once inside outer Write, got count=%d", count)
	}
	v, _ := flecs.Get[PPos](w.R(), e)
	if v.X != 99 {
		t.Fatalf("expected mutation flushed by outer Write exit (X=99), got %v", v.X)
	}
}
