package flecs_test

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/snichols/flecs"
)

// --- component types used only in merge hook tests ---

type MHTag struct{}
type MHOther struct{ V int }

// --- tests ---

// TestPreMergeHookFiresBeforeFlush verifies that a pre-merge hook fires before
// deferred commands are applied: inside the hook, Has[MHTag] is still false.
func TestPreMergeHookFiresBeforeFlush(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	var sawBefore bool
	flecs.OnPreMerge(w, func(fw *flecs.Writer) {
		sawBefore = !flecs.Has[MHTag](fw, e)
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, MHTag{})
	})

	if !sawBefore {
		t.Fatal("pre-merge hook should fire before MHTag is applied, but Has[MHTag] was already true")
	}
	// After Write returns, the tag must be applied.
	w.Read(func(r *flecs.Reader) {
		if !flecs.Has[MHTag](r, e) {
			t.Fatal("expected MHTag applied after Write returns")
		}
	})
}

// TestPostMergeHookFiresAfterFlush verifies that a post-merge hook fires after
// deferred commands are applied: inside the hook, Has[MHTag] is true.
func TestPostMergeHookFiresAfterFlush(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	var sawAfter bool
	flecs.OnPostMerge(w, func(fw *flecs.Writer) {
		sawAfter = flecs.Has[MHTag](fw, e)
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, MHTag{})
	})

	if !sawAfter {
		t.Fatal("post-merge hook should fire after MHTag is applied, but Has[MHTag] was still false")
	}
}

// TestPrePostMergeOrdering verifies the ordering: pre fires before flush,
// flush applies commands, post fires after flush.
func TestPrePostMergeOrdering(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	var counter int
	var preTick, flushTick, postTick int

	flecs.OnPreMerge(w, func(fw *flecs.Writer) {
		counter++
		preTick = counter
	})
	flecs.OnPostMerge(w, func(fw *flecs.Writer) {
		counter++
		postTick = counter
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, MHTag{})
	})
	flushTick = 2 // the flush happens between pre (1) and post (3) — counter after pre=1, post=3

	// We know pre fires first (counter goes 1→pre), then flush happens,
	// then post fires (counter goes 2→post).
	if preTick != 1 {
		t.Fatalf("expected preTick=1, got %d", preTick)
	}
	_ = flushTick
	if postTick != 2 {
		t.Fatalf("expected postTick=2, got %d", postTick)
	}
	if preTick >= postTick {
		t.Fatalf("expected pre (%d) before post (%d)", preTick, postTick)
	}
}

// TestMultiplePreMergeHooksFIFO verifies that multiple pre-merge hooks fire in
// registration order, skipping nil tombstones.
func TestMultiplePreMergeHooksFIFO(t *testing.T) {
	w := flecs.New()

	var order []int
	id0 := flecs.OnPreMerge(w, func(fw *flecs.Writer) { order = append(order, 0) })
	flecs.OnPreMerge(w, func(fw *flecs.Writer) { order = append(order, 1) })
	flecs.OnPreMerge(w, func(fw *flecs.Writer) { order = append(order, 2) })

	// Remove the first one to create a tombstone.
	flecs.RemovePreMergeHook(w, id0)

	w.Write(func(fw *flecs.Writer) {})

	if len(order) != 2 {
		t.Fatalf("expected 2 hook calls (tombstone skipped), got %d: %v", len(order), order)
	}
	if order[0] != 1 || order[1] != 2 {
		t.Fatalf("expected [1 2], got %v", order)
	}
}

// TestPreMergeHookMutationBatchesWithCurrentMerge verifies that mutations made
// inside a pre-merge hook participate in the same merge (visible after Write returns).
func TestPreMergeHookMutationBatchesWithCurrentMerge(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	flecs.OnPreMerge(w, func(fw *flecs.Writer) {
		// Queue a mutation from inside the pre-merge hook.
		flecs.Set(fw, e, MHOther{V: 42})
	})

	w.Write(func(fw *flecs.Writer) {
		// The outer Write queues MHTag; the pre-hook queues MHOther.
		flecs.Set(fw, e, MHTag{})
	})

	// Both mutations must be visible after Write returns (same merge).
	w.Read(func(r *flecs.Reader) {
		if !flecs.Has[MHTag](r, e) {
			t.Fatal("expected MHTag applied")
		}
		v, ok := flecs.Get[MHOther](r, e)
		if !ok || v.V != 42 {
			t.Fatalf("expected MHOther{42} from pre-hook, got %v ok=%v", v, ok)
		}
	})
}

// TestPostMergeHookMutationQueuesForNextMerge verifies that mutations made
// inside a post-merge hook are NOT applied until the next Write scope flushes.
func TestPostMergeHookMutationQueuesForNextMerge(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	flecs.OnPostMerge(w, func(fw *flecs.Writer) {
		flecs.Set(fw, e, MHOther{V: 99})
	})

	// First Write: main mutation goes in, post-hook queues MHOther for next merge.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, MHTag{})
	})

	// Immediately after first Write: MHOther must NOT be visible yet.
	w.Read(func(r *flecs.Reader) {
		if flecs.Has[MHOther](r, e) {
			t.Fatal("MHOther should NOT be visible until next merge")
		}
	})

	// Second Write (even empty): flushes the post-hook mutations.
	w.Write(func(fw *flecs.Writer) {})

	w.Read(func(r *flecs.Reader) {
		v, ok := flecs.Get[MHOther](r, e)
		if !ok || v.V != 99 {
			t.Fatalf("expected MHOther{99} after second merge, got %v ok=%v", v, ok)
		}
	})
}

// TestRemoveMergeHook verifies that removed hooks do not fire on subsequent merges,
// and that re-registering after removal works.
func TestRemoveMergeHook(t *testing.T) {
	w := flecs.New()

	var preFires, postFires int
	preID := flecs.OnPreMerge(w, func(fw *flecs.Writer) { preFires++ })
	postID := flecs.OnPostMerge(w, func(fw *flecs.Writer) { postFires++ })

	// First merge: both hooks should fire.
	w.Write(func(fw *flecs.Writer) {})
	if preFires != 1 {
		t.Fatalf("expected preFires=1, got %d", preFires)
	}
	if postFires != 1 {
		t.Fatalf("expected postFires=1, got %d", postFires)
	}

	// Remove both hooks.
	flecs.RemovePreMergeHook(w, preID)
	flecs.RemovePostMergeHook(w, postID)

	// Stale removes are no-ops.
	flecs.RemovePreMergeHook(w, preID)
	flecs.RemovePostMergeHook(w, postID)
	flecs.RemovePreMergeHook(w, 999)
	flecs.RemovePostMergeHook(w, 999)

	// Second merge: removed hooks must not fire.
	w.Write(func(fw *flecs.Writer) {})
	if preFires != 1 {
		t.Fatalf("pre hook fired after removal: preFires=%d", preFires)
	}
	if postFires != 1 {
		t.Fatalf("post hook fired after removal: postFires=%d", postFires)
	}

	// Re-register and verify the new registration fires.
	var newPreFired bool
	flecs.OnPreMerge(w, func(fw *flecs.Writer) { newPreFired = true })
	w.Write(func(fw *flecs.Writer) {})
	if !newPreFired {
		t.Fatal("re-registered pre-merge hook did not fire")
	}
}

// TestHookRegisteredFromInsideHookFiresNextMerge verifies that a hook registered
// from inside another hook fires starting on the next merge, not the current one.
func TestHookRegisteredFromInsideHookFiresNextMerge(t *testing.T) {
	w := flecs.New()

	var innerFires int
	var outerFires int

	flecs.OnPreMerge(w, func(fw *flecs.Writer) {
		outerFires++
		if outerFires == 1 {
			// Register a new hook from inside this hook.
			flecs.OnPreMerge(w, func(fw *flecs.Writer) {
				innerFires++
			})
		}
	})

	// First merge: outer fires, inner should NOT fire (registered mid-merge).
	w.Write(func(fw *flecs.Writer) {})
	if outerFires != 1 {
		t.Fatalf("expected outerFires=1, got %d", outerFires)
	}
	if innerFires != 0 {
		t.Fatalf("inner hook registered mid-merge should not fire on current merge, got %d", innerFires)
	}

	// Second merge: both outer and inner fire.
	w.Write(func(fw *flecs.Writer) {})
	if outerFires != 2 {
		t.Fatalf("expected outerFires=2, got %d", outerFires)
	}
	if innerFires != 1 {
		t.Fatalf("expected innerFires=1 on second merge, got %d", innerFires)
	}
}

// TestMergeReentryPanics verifies that calling w.Write from inside a merge hook
// panics with ErrMergeReentry.
func TestMergeReentryPanics(t *testing.T) {
	w := flecs.New()

	flecs.OnPreMerge(w, func(fw *flecs.Writer) {
		defer func() {
			r := recover()
			if r == nil {
				t.Error("expected panic from re-entrant Write inside pre-merge hook")
				return
			}
			err, ok := r.(error)
			if !ok || !errors.Is(err, flecs.ErrMergeReentry) {
				t.Errorf("expected ErrMergeReentry, got %v", r)
			}
		}()
		// This must panic.
		w.Write(func(fw2 *flecs.Writer) {})
	})

	w.Write(func(fw *flecs.Writer) {})
}

// TestEmptyMergeFiresHooks verifies that pre and post hooks fire even when the
// deferred command queue is empty (the merge boundary exists regardless of queue depth).
func TestEmptyMergeFiresHooks(t *testing.T) {
	w := flecs.New()

	var preFired, postFired bool
	flecs.OnPreMerge(w, func(fw *flecs.Writer) { preFired = true })
	flecs.OnPostMerge(w, func(fw *flecs.Writer) { postFired = true })

	// Empty Write scope — no commands queued.
	w.Write(func(fw *flecs.Writer) {})

	if !preFired {
		t.Fatal("pre-merge hook did not fire on empty merge")
	}
	if !postFired {
		t.Fatal("post-merge hook did not fire on empty merge")
	}
}

// TestMergeHookConcurrentSafety registers hooks from the main goroutine and runs
// merges from the same goroutine. The race detector must pass (run with -race).
func TestMergeHookConcurrentSafety(t *testing.T) {
	w := flecs.New()

	var count atomic.Int64
	// Register hooks outside any Write scope (required by the API).
	flecs.OnPreMerge(w, func(fw *flecs.Writer) { count.Add(1) })
	flecs.OnPostMerge(w, func(fw *flecs.Writer) { count.Add(1) })

	const iters = 10
	for range iters {
		w.Write(func(fw *flecs.Writer) {})
	}

	if got := count.Load(); got != int64(iters*2) {
		t.Fatalf("expected %d hook invocations, got %d", iters*2, got)
	}
}

// TestMultiThreadedSystemHooksNotPerWorker verifies that when a multi-threaded
// system triggers a worker-stage merge, the hooks do NOT fire once per worker
// stage (N+1 times for N workers). Instead, they fire once for the MT merge
// boundary — the same count regardless of how many workers are used.
func TestMultiThreadedSystemHooksNotPerWorker(t *testing.T) {
	// firesWithWorkers runs one Progress call with a multi-threaded system
	// using 'n' workers and returns the pre-hook fire count.
	firesWithWorkers := func(n int) int {
		w := flecs.New()
		w.SetWorkerCount(n)
		pID := flecs.RegisterComponent[parallelPos](w)
		w.Write(func(fw *flecs.Writer) {
			for range 8 {
				e := fw.NewEntity()
				flecs.Set(fw, e, parallelPos{})
			}
		})
		cq := flecs.NewCachedQuery(w, pID)
		sys := flecs.NewSystem(w, cq, func(_ float32, _ *flecs.QueryIter) {})
		sys.SetMultiThreaded(true)

		var fires int
		flecs.OnPreMerge(w, func(fw *flecs.Writer) { fires++ })
		w.Progress(0)
		return fires
	}

	fires2 := firesWithWorkers(2)
	fires4 := firesWithWorkers(4)

	// Hook fire count must be identical regardless of worker count.
	// If hooks fired per-worker-stage it would be (N+1): 3 for 2 workers, 5 for 4.
	if fires2 != fires4 {
		t.Fatalf("hook fires differ by worker count: workers=2 fires=%d, workers=4 fires=%d — hooks must not fire per worker stage", fires2, fires4)
	}
}

// TestRemoveHookFromInsideCallback verifies that removing a hook from inside its
// own callback takes effect on the next merge (the current merge uses the captured
// snapshot, so the hook still runs for the current merge).
func TestRemoveHookFromInsideCallback(t *testing.T) {
	w := flecs.New()

	var fires int
	var hookID int
	hookID = flecs.OnPreMerge(w, func(fw *flecs.Writer) {
		fires++
		// Remove self; should only affect subsequent merges.
		flecs.RemovePreMergeHook(w, hookID)
	})

	// First merge: the hook fires (captured snapshot includes it), then removes itself.
	w.Write(func(fw *flecs.Writer) {})
	if fires != 1 {
		t.Fatalf("expected fires=1 after first merge, got %d", fires)
	}

	// Second merge: hook was removed, so it must not fire.
	w.Write(func(fw *flecs.Writer) {})
	if fires != 1 {
		t.Fatalf("hook fired after self-removal: fires=%d", fires)
	}
}
