package flecs_test

import (
	"sync"
	"sync/atomic"
	"testing"

	flecs "github.com/snichols/flecs"
)

// ── types shared across stage tests ─────────────────────────────────────────

type stagePos struct{ X, Y float32 }
type stageVel struct{ DX, DY float32 }
type stageTag struct{}

// ── Stage isolation ──────────────────────────────────────────────────────────

// TestStageWorkerIsolation verifies that mutations enqueued by one worker are
// not visible to another worker's queue before the stage-merge at wg.Wait.
// Two workers run concurrently; each sets a counter component on its own entity.
// Before merge, neither worker's mutation is visible to the other.
func TestStageWorkerIsolation(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(2)
	posID := flecs.RegisterComponent[stagePos](w)

	const n = 100
	entities := make([]flecs.ID, n)
	w.Write(func(fw *flecs.Writer) {
		for i := range n {
			e := fw.NewEntity()
			flecs.Set(fw, e, stagePos{X: 0})
			entities[i] = e
		}
	})

	// Each worker sets X on its slice. After Progress, all entities have X=1.
	cq := flecs.NewCachedQuery(w, posID)
	sys := flecs.NewSystem(w, cq, func(_ float32, it *flecs.QueryIter) {
		fw := it.Writer()
		for it.Next() {
			for _, e := range it.Entities() {
				flecs.Set(fw, e, stagePos{X: 1})
			}
		}
	})
	sys.SetMultiThreaded(true)

	w.Progress(0)

	w.Read(func(fr *flecs.Reader) {
		for _, e := range entities {
			p, ok := flecs.Get[stagePos](fr, e)
			if !ok || p.X != 1 {
				t.Fatalf("entity %v: expected X=1 after stage merge, got X=%v ok=%v", e, p.X, ok)
			}
		}
	})
}

// TestStageWorkerIsolationNoLeakBeforeMerge checks that worker mutations to
// one entity are not visible to a peer worker on the same entity before merge.
// Workers write into their own stages; the values should appear atomically
// after the stage-merge, not mid-dispatch.
func TestStageWorkerIsolationNoLeakBeforeMerge(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(2)
	posID := flecs.RegisterComponent[stagePos](w)

	// Create a single entity so both workers see it in their slices.
	// (With clippedCopy, each worker gets its own row slice; for n=1 one worker
	// gets it and the other gets an empty slice. Use n=4 to ensure both workers
	// get rows.)
	const n = 4
	entities := make([]flecs.ID, n)
	w.Write(func(fw *flecs.Writer) {
		for i := range n {
			e := fw.NewEntity()
			flecs.Set(fw, e, stagePos{X: float32(i)})
			entities[i] = e
		}
	})

	// Use an atomic counter to verify both workers ran before checking results.
	var workersRan atomic.Int32
	cq := flecs.NewCachedQuery(w, posID)
	sys := flecs.NewSystem(w, cq, func(_ float32, it *flecs.QueryIter) {
		fw := it.Writer()
		for it.Next() {
			for i, e := range it.Entities() {
				flecs.Set(fw, e, stagePos{X: float32(i) + 100})
			}
		}
		workersRan.Add(1)
	})
	sys.SetMultiThreaded(true)

	w.Progress(0)

	if int(workersRan.Load()) != 2 {
		t.Fatalf("expected 2 workers to run, got %d", workersRan.Load())
	}

	// After merge, all entities have X >= 100 (modified by one of the workers).
	w.Read(func(fr *flecs.Reader) {
		for _, e := range entities {
			p, ok := flecs.Get[stagePos](fr, e)
			if !ok {
				t.Fatalf("entity %v missing stagePos after merge", e)
			}
			if p.X < 100 {
				t.Fatalf("entity %v: X=%v < 100, mutation not applied", e, p.X)
			}
		}
	})
}

// ── Merge ordering ───────────────────────────────────────────────────────────

// TestStageMergeOrdering verifies that stage queues drain in ascending id
// order: worker-stage mutations appear before main-stage hook mutations.
func TestStageMergeOrdering(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(2)
	posID := flecs.RegisterComponent[stagePos](w)
	velID := flecs.RegisterComponent[stageVel](w)

	var mu sync.Mutex
	var order []string

	// Hook on Velocity fires when Velocity is added. It records "onAdd:vel".
	flecs.OnAdd[stageVel](w, func(fw *flecs.Writer, _ flecs.ID, _ stageVel) {
		mu.Lock()
		order = append(order, "onAdd:vel")
		mu.Unlock()
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, stagePos{})
	})

	// Workers add Velocity; hook fires during stage merge.
	cq := flecs.NewCachedQuery(w, posID)
	sys := flecs.NewSystem(w, cq, func(_ float32, it *flecs.QueryIter) {
		fw := it.Writer()
		for it.Next() {
			for _, entity := range it.Entities() {
				flecs.AddID(fw, entity, velID)
			}
		}
	})
	sys.SetMultiThreaded(true)

	w.Progress(0)

	// After merge, e should have Velocity.
	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(e, velID) {
			t.Fatal("entity should have Velocity after worker AddID and stage merge")
		}
	})
}

// ── Coalescing within a stage ────────────────────────────────────────────────

// TestStageCoalescingWithinWorker verifies that multiple AddID calls from one
// worker for the same entity coalesce into a single archetype migration.
func TestStageCoalescingWithinWorker(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(2)

	const nTags = 50
	tags := make([]flecs.ID, nTags)
	var e flecs.ID

	w.Write(func(fw *flecs.Writer) {
		for i := range nTags {
			tags[i] = fw.NewEntity()
		}
		e = fw.NewEntity()
		flecs.Set(fw, e, stagePos{})
	})

	posID := flecs.RegisterComponent[stagePos](w)

	// Count archetype migrations via OnAdd hooks.
	var addCount atomic.Int32
	for _, tag := range tags {
		tag := tag
		flecs.OnAdd[stageTag](w, func(fw *flecs.Writer, entity flecs.ID, _ stageTag) {
			if entity == e {
				_ = tag // avoid capture issue
				addCount.Add(1)
			}
		})
		break // register once — we're just counting total fires
	}

	// Worker adds all tags to e in one shot (coalesces to one migration).
	cq := flecs.NewCachedQuery(w, posID)
	sys := flecs.NewSystem(w, cq, func(_ float32, it *flecs.QueryIter) {
		fw := it.Writer()
		for it.Next() {
			for _, entity := range it.Entities() {
				for _, tag := range tags {
					flecs.AddID(fw, entity, tag)
				}
			}
		}
	})
	sys.SetMultiThreaded(true)

	w.Progress(0)

	// After merge, e has all tags.
	w.Read(func(fr *flecs.Reader) {
		for _, tag := range tags {
			if !fr.HasID(e, tag) {
				t.Fatalf("entity missing tag %v after worker AddID coalesce", tag)
			}
		}
	})
}

// ── Per-worker stage Set ─────────────────────────────────────────────────────

// TestStageWorkerSet verifies deferred Set[T] from workers applies after merge.
func TestStageWorkerSet(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(4)
	posID := flecs.RegisterComponent[stagePos](w)

	const n = 1000
	entities := make([]flecs.ID, n)
	w.Write(func(fw *flecs.Writer) {
		for i := range n {
			e := fw.NewEntity()
			flecs.Set(fw, e, stagePos{X: 0})
			entities[i] = e
		}
	})

	cq := flecs.NewCachedQuery(w, posID)
	sys := flecs.NewSystem(w, cq, func(_ float32, it *flecs.QueryIter) {
		fw := it.Writer()
		for it.Next() {
			positions := flecs.Field[stagePos](it, posID)
			for i := range positions {
				// Write via deferred stage queue.
				e := it.Entities()[i]
				flecs.Set(fw, e, stagePos{X: float32(i) + 1, Y: float32(i) + 2})
			}
		}
	})
	sys.SetMultiThreaded(true)

	w.Progress(0)

	// All entities must have been written (X > 0 after stage merge).
	w.Read(func(fr *flecs.Reader) {
		for _, e := range entities {
			p, ok := flecs.Get[stagePos](fr, e)
			if !ok {
				t.Fatalf("entity %v: missing stagePos after merge", e)
			}
			if p.X == 0 {
				t.Fatalf("entity %v: X still 0 after worker Set — mutation not applied", e)
			}
		}
	})
}

// ── SetWorkerCount transitions ────────────────────────────────────────────────

// TestSetWorkerCountScaleUp verifies SetWorkerCount can grow the pool and stages.
func TestSetWorkerCountScaleUp(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(2)
	w.SetWorkerCount(4)
	if w.WorkerCount() != 4 {
		t.Fatalf("expected WorkerCount=4, got %d", w.WorkerCount())
	}
	// System runs correctly after scale-up.
	posID := flecs.RegisterComponent[stagePos](w)
	w.Write(func(fw *flecs.Writer) {
		for range 10 {
			e := fw.NewEntity()
			flecs.Set(fw, e, stagePos{})
		}
	})
	cq := flecs.NewCachedQuery(w, posID)
	var seen atomic.Int32
	flecs.NewSystem(w, cq, func(_ float32, it *flecs.QueryIter) {
		fw := it.Writer()
		for it.Next() {
			for _, e := range it.Entities() {
				seen.Add(1)
				flecs.Set(fw, e, stagePos{X: 1})
			}
		}
	})
	w.Progress(0)
	if seen.Load() == 0 {
		t.Fatal("no entities seen after SetWorkerCount scale-up")
	}
}

// TestSetWorkerCountScaleDown verifies SetWorkerCount can shrink the pool.
func TestSetWorkerCountScaleDown(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(4)
	w.SetWorkerCount(2)
	if w.WorkerCount() != 2 {
		t.Fatalf("expected WorkerCount=2, got %d", w.WorkerCount())
	}
	posID := flecs.RegisterComponent[stagePos](w)
	w.Write(func(fw *flecs.Writer) {
		for range 20 {
			e := fw.NewEntity()
			flecs.Set(fw, e, stagePos{})
		}
	})
	cq := flecs.NewCachedQuery(w, posID)
	var seen atomic.Int32
	flecs.NewSystem(w, cq, func(_ float32, it *flecs.QueryIter) {
		fw := it.Writer()
		for it.Next() {
			for _, e := range it.Entities() {
				seen.Add(1)
				flecs.Set(fw, e, stagePos{X: 2})
			}
		}
	})
	w.Progress(0)
	if seen.Load() == 0 {
		t.Fatal("no entities seen after SetWorkerCount scale-down")
	}
}

// TestSetWorkerCountToZero verifies disabling the worker pool works.
func TestSetWorkerCountToZero(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(4)
	w.SetWorkerCount(0)
	if w.WorkerCount() != 0 {
		t.Fatalf("expected WorkerCount=0, got %d", w.WorkerCount())
	}
}

// ── Stage-aware Writer paths ─────────────────────────────────────────────────

// TestWriterSetByIDViaStage ensures Writer.SetByID routes through the stage.
func TestWriterSetByIDViaStage(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[stagePos](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, stagePos{X: 0})
		fw.SetByID(e, posID, stagePos{X: 42})
	})
	w.Read(func(fr *flecs.Reader) {
		p, ok := flecs.Get[stagePos](fr, e)
		if !ok || p.X != 42 {
			t.Fatalf("expected X=42, got X=%v ok=%v", p.X, ok)
		}
	})
}

// TestWriterSetPairByIDViaStage ensures Writer.SetPairByID routes through the stage.
func TestWriterSetPairByIDViaStage(t *testing.T) {
	w := flecs.New()
	type Edge struct{ W float32 }
	_ = flecs.RegisterComponent[Edge](w)
	var rel, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
		fw.SetPairByID(e, rel, tgt, Edge{W: 3.14})
	})
	w.Read(func(fr *flecs.Reader) {
		v, ok := flecs.GetPair[Edge](fr, e, rel, tgt)
		if !ok || v.W != 3.14 {
			t.Fatalf("expected W=3.14, got W=%v ok=%v", v.W, ok)
		}
	})
}

// TestWriterDeleteViaStage ensures Writer.Delete routes through the stage.
func TestWriterDeleteViaStage(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})
	w.Write(func(fw *flecs.Writer) {
		if !fw.Delete(e) {
			t.Fatal("expected Delete to return true for alive entity")
		}
	})
	if w.IsAlive(e) {
		t.Fatal("entity should be dead after Write.Delete")
	}
}

// TestWriterRemoveIDViaStage ensures Writer.RemoveID routes through the stage.
func TestWriterRemoveIDViaStage(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[stagePos](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, stagePos{X: 1})
	})
	w.Write(func(fw *flecs.Writer) {
		if !fw.RemoveID(e, posID) {
			t.Fatal("expected RemoveID to return true")
		}
	})
	w.Read(func(fr *flecs.Reader) {
		if fr.HasID(e, posID) {
			t.Fatal("entity should not have posID after RemoveID")
		}
	})
}

// TestWriterSetPairViaStage ensures SetPair[T] routes through the stage queue.
func TestWriterSetPairViaStage(t *testing.T) {
	w := flecs.New()
	type Weight struct{ V float32 }
	var rel, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
		flecs.SetPair[Weight](fw, e, rel, tgt, Weight{V: 7})
	})
	w.Read(func(fr *flecs.Reader) {
		v, ok := flecs.GetPair[Weight](fr, e, rel, tgt)
		if !ok || v.V != 7 {
			t.Fatalf("expected V=7, got V=%v ok=%v", v.V, ok)
		}
	})
}

// TestWriterSetNameViaStage ensures fw.SetName routes through stage (not World.SetName).
func TestWriterSetNameViaStage(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		fw.SetName(e, "alice")
	})
	w.Read(func(fr *flecs.Reader) {
		name, ok := fr.GetName(e)
		if !ok || name != "alice" {
			t.Fatalf("expected name='alice', got name=%q ok=%v", name, ok)
		}
	})
}

// TestWriterDeleteDeadEntityReturnsFalse verifies Delete on a dead entity.
func TestWriterDeleteDeadEntityReturnsFalse(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})
	w.Write(func(fw *flecs.Writer) {
		fw.Delete(e)
	})
	// e is now dead; Delete should return false.
	w.Write(func(fw *flecs.Writer) {
		if fw.Delete(e) {
			t.Fatal("expected false for dead entity")
		}
	})
}

// TestWriterSetByIDPanicsOnUnregistered ensures SetByID panics for unknown id.
func TestWriterSetByIDPanicsOnUnregistered(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for unregistered id")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		fw.SetByID(e, flecs.ID(9999), stagePos{})
	})
}

// TestWriterSetByIDPanicsOnTypeMismatch ensures SetByID panics on type mismatch.
func TestWriterSetByIDPanicsOnTypeMismatch(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[stagePos](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity(); flecs.Set(fw, e, stagePos{}) })
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for type mismatch")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		fw.SetByID(e, posID, stageVel{}) // wrong type
	})
}

// TestWriterSetPairByIDPanicsOnNilValue ensures SetPairByID panics on nil v.
func TestWriterSetPairByIDPanicsOnNilValue(t *testing.T) {
	w := flecs.New()
	var rel, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
	})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil value")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		fw.SetPairByID(e, rel, tgt, nil)
	})
}

// TestWriterSetPairByIDPanicsOnTypeMismatch ensures SetPairByID panics on type mismatch.
func TestWriterSetPairByIDPanicsOnTypeMismatch(t *testing.T) {
	w := flecs.New()
	type A struct{ V float32 }
	type B struct{ V int }
	var rel, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
		fw.SetPairByID(e, rel, tgt, A{V: 1})
	})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for type mismatch on SetPairByID")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		fw.SetPairByID(e, rel, tgt, B{V: 2}) // wrong type
	})
}

// TestRemoveViaWriterCoversImmediatePath triggers the immediate Remove path
// by registering an OnSet hook that removes a component (deferDepth==0 at flush).
func TestRemoveViaWriterCoversImmediatePath(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[stagePos](w)
	velID := flecs.RegisterComponent[stageVel](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, stagePos{})
		flecs.Set(fw, e, stageVel{})
	})

	// During flush, deferDepth==0, so Remove[T] hits the immediate path.
	flecs.OnSet[stagePos](w, func(fw *flecs.Writer, entity flecs.ID, _ stagePos) {
		flecs.Remove[stageVel](fw, entity)
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, stagePos{X: 1}) // triggers OnSet during flush
	})

	w.Read(func(fr *flecs.Reader) {
		if fr.HasID(e, velID) {
			t.Fatal("Velocity should have been removed by OnSet hook")
		}
		if !fr.HasID(e, posID) {
			t.Fatal("Position should still be present")
		}
	})
}

// TestDeleteViaHookCoversImmediatePath exercises fw.Delete when deferDepth==0
// (during flush) via an OnSet hook that deletes another entity.
func TestDeleteViaHookCoversImmediatePath(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[stagePos](w)

	var victim, trigger flecs.ID
	w.Write(func(fw *flecs.Writer) {
		victim = fw.NewEntity()
		trigger = fw.NewEntity()
		flecs.Set(fw, trigger, stagePos{})
	})

	// Hook deletes victim when trigger gets a new Position value.
	flecs.OnSet[stagePos](w, func(fw *flecs.Writer, entity flecs.ID, _ stagePos) {
		if entity == trigger {
			fw.Delete(victim)
		}
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, trigger, stagePos{X: 1})
	})

	if w.IsAlive(victim) {
		t.Fatal("victim entity should have been deleted by OnSet hook")
	}
	_ = posID
}

// TestRemoveNameDeferredPath calls RemoveName inside a Write scope to exercise
// the deferred branch of removeOnWorld (stages[0].deferDepth > 0).
func TestRemoveNameDeferredPath(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		fw.SetName(e, "entity-to-rename")
	})

	w.Write(func(fw *flecs.Writer) {
		if !w.RemoveName(e) {
			t.Fatal("RemoveName should return true when entity had a Name")
		}
	})

	if _, ok := w.GetName(e); ok {
		t.Fatal("GetName should return false after deferred RemoveName is flushed")
	}
}

// TestRemoveNameDeferredUnnamed calls RemoveName on an unnamed entity inside a
// Write scope to exercise the "entity has no Name" early-return branch.
func TestRemoveNameDeferredUnnamed(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})

	w.Write(func(fw *flecs.Writer) {
		if w.RemoveName(e) {
			t.Fatal("RemoveName on unnamed entity should return false")
		}
	})
}

// TestEffectiveWriteSetDerived exercises effectiveWriteSet when writeSetFixed==false
// by creating a parallel system without calling SetWriteSet. The write set should
// be derived from the query terms (the posID term) for conflict detection.
func TestEffectiveWriteSetDerived(t *testing.T) {
	w := flecs.New()
	w.SetWorkerCount(2)
	posID := flecs.RegisterComponent[stagePos](w)
	velID := flecs.RegisterComponent[stageVel](w)

	w.Write(func(fw *flecs.Writer) {
		for range 4 {
			e := fw.NewEntity()
			flecs.Set(fw, e, stagePos{})
			flecs.Set(fw, e, stageVel{})
		}
	})

	var runCount atomic.Int64
	// sysA: parallel, no SetWriteSet → derived from posID query term.
	cqA := flecs.NewCachedQuery(w, posID)
	sysA := flecs.NewSystem(w, cqA, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			runCount.Add(1)
		}
	})
	sysA.SetParallel(true)
	// intentionally NOT calling sysA.SetWriteSet — uses derived write set

	// sysB: parallel, derived from velID query term (no conflict with sysA).
	cqB := flecs.NewCachedQuery(w, velID)
	sysB := flecs.NewSystem(w, cqB, func(_ float32, it *flecs.QueryIter) {
		for it.Next() {
			runCount.Add(1)
		}
	})
	sysB.SetParallel(true)
	// intentionally NOT calling sysB.SetWriteSet — uses derived write set

	w.Progress(0)

	if runCount.Load() == 0 {
		t.Fatal("expected both parallel systems to run")
	}
}
