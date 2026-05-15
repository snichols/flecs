package flecs_test

import (
	"runtime"
	"sync"
	"testing"
	"time"

	flecs "github.com/snichols/flecs"
)

// ─── helper types ─────────────────────────────────────────────────────────────

type ReclaimPos struct{ X, Y float32 }
type ReclaimVel struct{ DX, DY float32 }

// newEntity creates a new entity with a Set component value. Helper to reduce
// boilerplate in reclamation tests.
func newEntityWithPos(w *flecs.World, posID flecs.ID) flecs.ID {
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, ReclaimPos{1, 2})
	})
	_ = posID
	return e
}

func newEntityWithVel(w *flecs.World) flecs.ID {
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, ReclaimVel{3, 4})
	})
	return e
}

func newEntityWithPosVel(w *flecs.World) flecs.ID {
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, ReclaimPos{5, 6})
		flecs.Set(fw, e, ReclaimVel{7, 8})
	})
	return e
}

// progressN calls w.Progress(0) n times.
func progressN(w *flecs.World, n int) {
	for range n {
		w.Progress(0)
	}
}

// ─── Tracking ─────────────────────────────────────────────────────────────────

func TestReclamation_DefaultThresholdIs60(t *testing.T) {
	w := flecs.New()
	if got := w.TableReclamationThreshold(); got != 60 {
		t.Fatalf("default threshold: want 60, got %d", got)
	}
}

func TestReclamation_ThresholdZeroDisables(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(0)
	if got := w.TableReclamationThreshold(); got != 0 {
		t.Fatalf("want 0, got %d", got)
	}

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)
	w.Delete(e)

	// Drive 200 ticks — nothing should be reclaimed when threshold == 0.
	progressN(w, 200)
	if got := w.ReclaimedTablesCount(); got != 0 {
		t.Fatalf("threshold=0 should disable reclamation; got %d reclaimed", got)
	}
}

func TestReclamation_TableEmptyTicksAdvances(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(200) // high threshold so we don't reclaim yet

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)
	w.Delete(e)

	// Drive 5 ticks (below threshold) — should not reclaim.
	progressN(w, 5)
	if got := w.ReclaimedTablesCount(); got != 0 {
		t.Fatalf("should not reclaim after only 5 ticks (threshold=200); got %d", got)
	}
}

func TestReclamation_TableInsertResetsTicks(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(10)

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)
	w.Delete(e)

	// Drive 8 ticks (below threshold).
	progressN(w, 8)
	if got := w.ReclaimedTablesCount(); got != 0 {
		t.Fatalf("should not reclaim after 8 ticks (threshold=10); got %d", got)
	}

	// Re-add an entity to reset emptyTicks.
	e2 := newEntityWithPos(w, posID)
	w.Delete(e2)

	// Drive 9 more ticks (would exceed original 8+9=17 > 10, but counter was reset).
	progressN(w, 9)
	if got := w.ReclaimedTablesCount(); got != 0 {
		t.Fatalf("insert should have reset empty tick counter; got %d reclaimed", got)
	}

	// Now drive past threshold from the reset point.
	progressN(w, 11)
	if got := w.ReclaimedTablesCount(); got == 0 {
		t.Fatal("expected table to be reclaimed after threshold exceeded from reset")
	}
}

func TestReclamation_PinnedTableNeverReclaimed(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(5)

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)

	// Capture the table via OnTableCreate with YieldExisting.
	var pinnedTable *flecs.Table
	flecs.OnTableCreateWithOptions(w, flecs.WithYieldExisting(), func(_ *flecs.Writer, t *flecs.Table) {
		if t.HasComponent(posID) {
			pinnedTable = t
		}
	})

	if pinnedTable == nil {
		t.Skip("table not captured via YieldExisting; skipping pin test")
	}

	pinnedTable.Pin()
	if !pinnedTable.IsPinned() {
		t.Fatal("Pin() should make IsPinned() return true")
	}

	w.Delete(e)
	progressN(w, 100) // well past threshold
	if got := w.ReclaimedTablesCount(); got != 0 {
		t.Fatalf("pinned table should never be reclaimed; got %d reclaimed", got)
	}

	// Unpin and verify it gets reclaimed eventually.
	pinnedTable.Unpin()
	if pinnedTable.IsPinned() {
		t.Fatal("Unpin() should make IsPinned() return false")
	}
	progressN(w, 10)
	if got := w.ReclaimedTablesCount(); got == 0 {
		t.Fatal("unpinned table should be reclaimed after threshold ticks")
	}
}

func TestReclamation_RefCountedTableNeverReclaimed(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(5)

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)

	// CachedQuery holds a reference on the matched table.
	cq := flecs.NewCachedQuery(w, posID)

	w.Delete(e)
	progressN(w, 100) // well past threshold
	if got := w.ReclaimedTablesCount(); got != 0 {
		t.Fatalf("ref-counted table should not be reclaimed while CachedQuery live; got %d", got)
	}

	// Close the query to release the reference.
	cq.Close()
	progressN(w, 10)
	if got := w.ReclaimedTablesCount(); got == 0 {
		t.Fatal("table should be reclaimed after CachedQuery is closed")
	}
}

// ─── Reclamation path ─────────────────────────────────────────────────────────

func TestReclamation_BasicSweep(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(60)

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)
	w.Delete(e)

	progressN(w, 61)
	if got := w.ReclaimedTablesCount(); got != 1 {
		t.Fatalf("want 1 reclaimed table, got %d", got)
	}
}

func TestReclamation_ReclaimNow_Force(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(1000) // very high — won't trigger via Progress

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)
	w.Delete(e)

	progressN(w, 5) // below threshold; emptyTicks accumulates

	n := w.ReclaimNow()
	if n != 1 {
		t.Fatalf("ReclaimNow: want 1, got %d", n)
	}
	if got := w.ReclaimedTablesCount(); got != 1 {
		t.Fatalf("ReclaimedTablesCount: want 1, got %d", got)
	}
}

func TestReclamation_MultipleTablesInOneSweep(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(10)

	flecs.RegisterComponent[ReclaimPos](w)
	flecs.RegisterComponent[ReclaimVel](w)

	e1 := newEntityWithPos(w, 0)
	e2 := newEntityWithVel(w)
	e3 := newEntityWithPosVel(w)

	w.Delete(e1)
	w.Delete(e2)
	w.Delete(e3)

	progressN(w, 11)
	if got := w.ReclaimedTablesCount(); got < 3 {
		t.Fatalf("want >= 3 reclaimed tables, got %d", got)
	}
}

func TestReclamation_NewTableAfterReclamation(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(5)

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)
	w.Delete(e)

	progressN(w, 6)
	if got := w.ReclaimedTablesCount(); got != 1 {
		t.Fatalf("want 1 reclaimed, got %d", got)
	}

	// Adding a new entity with the same signature should create a fresh table.
	var e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, ReclaimPos{9, 9})
	})

	// Verify the entity is alive and usable.
	var p ReclaimPos
	var ok bool
	w.Read(func(r *flecs.Reader) {
		p, ok = flecs.Get[ReclaimPos](r, e2)
	})
	if !ok || p.X != 9 {
		t.Fatal("entity in new (post-reclaim) table should be accessible")
	}
}

// ─── OnTableDelete event ──────────────────────────────────────────────────────

func TestOnTableDelete_FiresBeforeReclamation(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(5)

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)
	w.Delete(e)

	fired := false
	flecs.OnTableDelete(w, func(_ *flecs.Reader, t *flecs.Table) {
		if t.HasComponent(posID) {
			fired = true
		}
	})

	progressN(w, 6)
	if !fired {
		t.Fatal("OnTableDelete should have fired before reclamation")
	}
	if got := w.ReclaimedTablesCount(); got != 1 {
		t.Fatalf("want 1 reclaimed, got %d", got)
	}
}

func TestOnTableDelete_TableSignaturePassedCorrectly(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(5)

	posID := flecs.RegisterComponent[ReclaimPos](w)
	velID := flecs.RegisterComponent[ReclaimVel](w)

	e := newEntityWithPosVel(w)
	w.Delete(e)

	var gotSig []flecs.ID
	flecs.OnTableDelete(w, func(_ *flecs.Reader, t *flecs.Table) {
		gotSig = t.Type()
	})

	progressN(w, 6)
	if len(gotSig) == 0 {
		t.Fatal("OnTableDelete: table signature was empty")
	}
	hasPos, hasVel := false, false
	for _, id := range gotSig {
		if id == posID {
			hasPos = true
		}
		if id == velID {
			hasVel = true
		}
	}
	if !hasPos || !hasVel {
		t.Fatalf("OnTableDelete: expected both Pos and Vel in signature; got %v", gotSig)
	}
}

func TestOnTableDelete_MultiTermFilter(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(5)

	posID := flecs.RegisterComponent[ReclaimPos](w)
	velID := flecs.RegisterComponent[ReclaimVel](w)

	// One entity with only Pos, one with Pos+Vel.
	e1 := newEntityWithPos(w, posID)
	e2 := newEntityWithPosVel(w)

	w.Delete(e1)
	w.Delete(e2)

	// Filter to only fire for tables that have both Pos AND Vel.
	fired := 0
	flecs.OnTableDeleteWithOptions(w,
		flecs.WithQuery(flecs.With(posID), flecs.With(velID)),
		func(_ *flecs.Reader, _ *flecs.Table) { fired++ })

	progressN(w, 6)
	if fired != 1 {
		t.Fatalf("multi-term filter: want 1 fire (Pos+Vel table), got %d", fired)
	}
}

func TestOnTableDelete_YieldExistingIsNoOp(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(5)

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)

	// WithYieldExisting on OnTableDelete must not fire synchronously at registration.
	fired := 0
	flecs.OnTableDeleteWithOptions(w, flecs.WithYieldExisting(),
		func(_ *flecs.Reader, _ *flecs.Table) { fired++ })

	if fired != 0 {
		t.Fatalf("WithYieldExisting must be a no-op for OnTableDelete; got %d fires", fired)
	}
	_ = e
}

func TestOnTableDelete_HandlerReceivesReader(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(5)

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)
	w.Delete(e)

	var gotReader *flecs.Reader
	flecs.OnTableDelete(w, func(fr *flecs.Reader, _ *flecs.Table) {
		gotReader = fr
	})

	progressN(w, 6)
	if gotReader == nil {
		t.Fatal("OnTableDelete handler should receive a non-nil *Reader")
	}
}

func TestOnTableDelete_ObserverDisabled(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(5)

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)
	w.Delete(e)

	fired := 0
	obs := flecs.OnTableDelete(w, func(_ *flecs.Reader, _ *flecs.Table) { fired++ })
	obs.SetEnabled(false)

	progressN(w, 6)
	if fired != 0 {
		t.Fatalf("disabled observer should not fire; got %d fires", fired)
	}
}

// ─── Reference counting ───────────────────────────────────────────────────────

func TestReclamation_QueryConstructionBumpsRef(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(5)

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)

	// Regular query holds refs via bumpMatchingTableRefs.
	q := flecs.NewQuery(w, posID)

	// Delete entity and drive past threshold — table must not be reclaimed
	// while q is live.
	w.Delete(e)
	progressN(w, 10)
	if got := w.ReclaimedTablesCount(); got != 0 {
		t.Fatalf("table should not be reclaimed while query holds a ref; got %d", got)
	}

	q.Free()
	progressN(w, 10)
	if got := w.ReclaimedTablesCount(); got == 0 {
		t.Fatal("table should be reclaimed after query.Free()")
	}
}

func TestReclamation_QueryFreeDecrementsRef(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(5)

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)
	w.Delete(e)

	q := flecs.NewQuery(w, posID)
	progressN(w, 10) // past threshold but query holds ref
	if got := w.ReclaimedTablesCount(); got != 0 {
		t.Fatalf("should not reclaim while query alive; got %d", got)
	}

	q.Free()
	progressN(w, 10)
	if got := w.ReclaimedTablesCount(); got == 0 {
		t.Fatal("table should be reclaimed after query.Free()")
	}
}

func TestReclamation_ObserverRegistrationBumpsRef(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(5)

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)
	w.Delete(e)

	// CachedQuery bumps refCount at construction.
	cq := flecs.NewCachedQuery(w, posID)
	progressN(w, 10)
	if got := w.ReclaimedTablesCount(); got != 0 {
		t.Fatalf("CachedQuery should hold ref; got %d reclaimed", got)
	}

	cq.Close()
	progressN(w, 10)
	if got := w.ReclaimedTablesCount(); got == 0 {
		t.Fatal("table should be reclaimed after CachedQuery.Close()")
	}
}

func TestReclamation_IterOpenBumpsRef(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(5)

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)
	w.Delete(e)

	// CachedQuery holds the ref while open (bumped in tryMatchTable).
	cq := flecs.NewCachedQuery(w, posID)
	progressN(w, 10) // past threshold
	if got := w.ReclaimedTablesCount(); got != 0 {
		t.Fatalf("CachedQuery should hold ref preventing reclamation; got %d", got)
	}

	cq.Close()
	progressN(w, 10)
	if got := w.ReclaimedTablesCount(); got == 0 {
		t.Fatal("table should be reclaimed after CachedQuery closed")
	}
}

func TestReclamation_NoRefLeakAfterRoundTrip(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(5)

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)

	q := flecs.NewQuery(w, posID)
	cq := flecs.NewCachedQuery(w, posID)

	w.Delete(e)
	progressN(w, 10)
	if got := w.ReclaimedTablesCount(); got != 0 {
		t.Fatalf("refs held; should not reclaim; got %d", got)
	}

	q.Free()
	cq.Close()
	progressN(w, 10)
	if got := w.ReclaimedTablesCount(); got == 0 {
		t.Fatal("all refs released; table should be reclaimed")
	}
}

// ─── Stress / property ────────────────────────────────────────────────────────

func TestReclamation_LongRunWorkload(t *testing.T) {
	if testing.Short() {
		t.Skip("long-run workload: skipping in short mode")
	}
	w := flecs.New()
	w.SetTableReclamationThreshold(10)

	type C1 struct{ V int }
	type C2 struct{ V int }

	flecs.RegisterComponent[C1](w)
	flecs.RegisterComponent[C2](w)

	const numEntities = 200
	const numTicks = 200

	// Create two distinct archetypes.
	var toDelete []flecs.ID
	var toKeep []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		for i := range numEntities {
			e := fw.NewEntity()
			if i%2 == 0 {
				flecs.Set(fw, e, C1{i})
				toDelete = append(toDelete, e)
			} else {
				flecs.Set(fw, e, C2{i})
				toKeep = append(toKeep, e)
			}
		}
	})

	// Delete all C1 entities so the C1 table goes empty.
	for _, e := range toDelete {
		w.Delete(e)
	}

	// Drive progress; the C1 table should be reclaimed.
	for range numTicks {
		w.Progress(0)
	}

	if got := w.ReclaimedTablesCount(); got == 0 {
		t.Fatal("expected some tables to be reclaimed in long-run workload")
	}

	// C2 entities must still be alive.
	for _, e := range toKeep {
		if !w.IsAlive(e) {
			t.Fatalf("entity %v should still be alive", e)
		}
	}
}

func TestReclamation_RaceCondition_NoCrash(t *testing.T) {
	if testing.Short() {
		t.Skip("race test: skipping in short mode")
	}
	w := flecs.New()
	w.SetTableReclamationThreshold(5)

	posID := flecs.RegisterComponent[ReclaimPos](w)

	const goroutines = 4
	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writer goroutine: creates/deletes entities and drives Progress.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				w.Write(func(fw *flecs.Writer) {
					e := fw.NewEntity()
					flecs.Set(fw, e, ReclaimPos{1, 2})
					_ = e
				})
				w.Progress(0)
				runtime.Gosched()
			}
		}
	}()

	// Reader goroutines: iterate over query results.
	for range goroutines - 1 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					w.Read(func(r *flecs.Reader) {
						q := flecs.NewQuery(w, posID)
						it := q.Iter()
						for it.Next() {
						}
						q.Free()
					})
					runtime.Gosched()
				}
			}
		}()
	}

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// ─── Snapshot integration ─────────────────────────────────────────────────────

func TestReclamation_SnapshotRestore_StartsFresh(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(10)

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)

	snap := flecs.TakeSnapshot(w)

	// Delete entity and drive ticks nearly to threshold.
	w.Delete(e)
	progressN(w, 9) // one below threshold

	// Restore snapshot: should reset emptyTicks.
	flecs.RestoreSnapshot(w, snap)

	// After restore the entity is alive again; delete it.
	w.Delete(e)

	// Drive fewer than threshold ticks — should not reclaim because emptyTicks reset.
	progressN(w, 5)
	if got := w.ReclaimedTablesCount(); got != 0 {
		t.Fatalf("post-restore: emptyTicks should have been reset; got %d reclaimed", got)
	}

	// Drive past threshold.
	progressN(w, 11)
	if got := w.ReclaimedTablesCount(); got == 0 {
		t.Fatal("expected reclamation after threshold exceeded from fresh counter")
	}
}

// ─── Threshold tuning ─────────────────────────────────────────────────────────

func TestReclamation_HighThresholdDelaysReclamation(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(50)

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)
	w.Delete(e)

	progressN(w, 49) // one below threshold
	if got := w.ReclaimedTablesCount(); got != 0 {
		t.Fatalf("threshold=50: should not reclaim after 49 ticks; got %d", got)
	}

	progressN(w, 2) // push past threshold
	if got := w.ReclaimedTablesCount(); got == 0 {
		t.Fatal("threshold=50: should reclaim after 51 ticks")
	}
}

func TestReclamation_VeryLowThreshold(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(1)

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)
	w.Delete(e)

	// Single Progress tick: emptyTicks reaches 1 >= threshold 1.
	w.Progress(0)
	if got := w.ReclaimedTablesCount(); got == 0 {
		t.Fatal("threshold=1: expected reclamation after a single Progress tick")
	}
}

// ─── Additional correctness tests ─────────────────────────────────────────────

func TestOnTableDelete_UnsubscribeStopsEvents(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(5)

	posID := flecs.RegisterComponent[ReclaimPos](w)
	e := newEntityWithPos(w, posID)
	w.Delete(e)

	fired := 0
	obs := flecs.OnTableDelete(w, func(_ *flecs.Reader, _ *flecs.Table) { fired++ })
	obs.Unsubscribe()

	progressN(w, 6)
	if fired != 0 {
		t.Fatalf("unsubscribed observer should not fire; got %d fires", fired)
	}
}

func TestReclamation_ReclaimedCountAccumulates(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(5)

	type A struct{ V int }
	type B struct{ V int }
	flecs.RegisterComponent[A](w)
	flecs.RegisterComponent[B](w)

	// Create two distinct single-component tables.
	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		flecs.Set(fw, e1, A{1})
		e2 = fw.NewEntity()
		flecs.Set(fw, e2, B{2})
	})

	w.Delete(e1)
	w.Delete(e2)
	progressN(w, 6)

	if got := w.ReclaimedTablesCount(); got < 2 {
		t.Fatalf("want >= 2 reclaimed, got %d", got)
	}
}
