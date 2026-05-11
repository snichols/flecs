package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// --- helper types used only in defer tests ---

type DPos struct{ X, Y float32 }
type DVel struct{ DX, DY float32 }
type DEdge struct{ Weight int }
type DTag struct{} // zero-size tag

// --- tests ---

// TestDeferBasicQueueAndFlush verifies that Set[T] inside a deferred block
// is not visible until the block exits.
func TestDeferBasicQueueAndFlush(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()

	flecs.DeferBeginForTest(w)
	flecs.Set[DPos](w.W(), e, DPos{1, 2})
	if flecs.Has[DPos](w.R(), e) {
		t.Fatal("expected DPos not yet applied during defer")
	}
	flecs.DeferEndForTest(w)

	if !flecs.Has[DPos](w.R(), e) {
		t.Fatal("expected DPos applied after DeferEnd")
	}
	v, ok := flecs.Get[DPos](w.R(), e)
	if !ok || v != (DPos{1, 2}) {
		t.Fatalf("expected DPos{1,2}, got %v ok=%v", v, ok)
	}
}

// TestDeferGetSeesOldState verifies that Get inside a deferred block returns
// the pre-defer value, not the queued value.
func TestDeferGetSeesOldState(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set[DPos](w.W(), e, DPos{1, 2})

	flecs.DeferBeginForTest(w)
	flecs.Set[DPos](w.W(), e, DPos{99, 99})
	v, ok := flecs.Get[DPos](w.R(), e)
	if !ok || v != (DPos{1, 2}) {
		t.Fatalf("during defer: expected DPos{1,2}, got %v ok=%v", v, ok)
	}
	flecs.DeferEndForTest(w)

	v, ok = flecs.Get[DPos](w.R(), e)
	if !ok || v != (DPos{99, 99}) {
		t.Fatalf("after DeferEnd: expected DPos{99,99}, got %v ok=%v", v, ok)
	}
}

// TestDeferOrderPreserved queues three Sets with different values for the same
// entity; after flush, Get returns the LAST value.
func TestDeferOrderPreserved(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()

	flecs.DeferBeginForTest(w)
	flecs.Set[DPos](w.W(), e, DPos{1, 0})
	flecs.Set[DPos](w.W(), e, DPos{2, 0})
	flecs.Set[DPos](w.W(), e, DPos{3, 0})
	flecs.DeferEndForTest(w)

	v, ok := flecs.Get[DPos](w.R(), e)
	if !ok || v.X != 3 {
		t.Fatalf("expected X==3, got %v ok=%v", v, ok)
	}
}

// TestDeferMultiOperation queues Set + Delete on different entities; both
// apply on flush.
func TestDeferMultiOperation(t *testing.T) {
	w := flecs.New()
	e1 := w.NewEntity()
	e2 := w.NewEntity()

	flecs.DeferBeginForTest(w)
	flecs.Set[DPos](w.W(), e1, DPos{5, 6})
	w.Delete(e2)
	flecs.DeferEndForTest(w)

	if v, ok := flecs.Get[DPos](w.R(), e1); !ok || v != (DPos{5, 6}) {
		t.Fatalf("expected DPos{5,6} on e1, got %v ok=%v", v, ok)
	}
	if w.IsAlive(e2) {
		t.Fatal("expected e2 deleted after DeferEnd")
	}
}

// TestDeferNested verifies that nested deferred blocks do not flush until the
// outermost block exits.
func TestDeferNested(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()

	flecs.DeferBeginForTest(w) // depth 1
	flecs.DeferBeginForTest(w) // depth 2
	flecs.Set[DPos](w.W(), e, DPos{7, 8})
	flecs.DeferEndForTest(w) // depth 1 — no flush yet
	if flecs.Has[DPos](w.R(), e) {
		t.Fatal("expected DPos still deferred after inner DeferEnd")
	}
	flecs.DeferEndForTest(w) // depth 0 — flush now

	if !flecs.Has[DPos](w.R(), e) {
		t.Fatal("expected DPos applied after outer DeferEnd")
	}
}

// TestDeferConveniencePanic verifies that even when fn panics, DeferEnd runs
// and the queue is flushed (applying ops queued before the panic).
func TestDeferConveniencePanic(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()

	func() {
		defer func() { _ = recover() }()
		flecs.DeferForTest(w, func() {
			flecs.Set[DPos](w.W(), e, DPos{42, 0})
			panic("test panic")
		})
	}()

	// DeferEnd ran via defer keyword; the queued Set should have applied.
	if v, ok := flecs.Get[DPos](w.R(), e); !ok || v.X != 42 {
		t.Fatalf("expected Set applied despite panic, got %v ok=%v", v, ok)
	}
}

// TestDeferMismatchedPanics verifies that DeferEndForTest without a matching
// DeferBeginForTest panics.
func TestDeferMismatchedPanics(t *testing.T) {
	w := flecs.New()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic from unmatched DeferEnd")
		}
	}()
	flecs.DeferEndForTest(w)
}

// TestDeferIsDeferred verifies that IsDeferred returns false before, true
// inside, and false after a deferred block.
func TestDeferIsDeferred(t *testing.T) {
	w := flecs.New()
	if w.IsDeferred() {
		t.Fatal("expected not deferred initially")
	}
	flecs.DeferBeginForTest(w)
	if !w.IsDeferred() {
		t.Fatal("expected deferred inside DeferBegin/DeferEnd")
	}
	flecs.DeferEndForTest(w)
	if w.IsDeferred() {
		t.Fatal("expected not deferred after DeferEnd")
	}
}

// TestDeferObserverFiresDuringFlush registers an observer, defers a Set, then
// verifies the observer fires exactly once after flush.
func TestDeferObserverFiresDuringFlush(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	fired := 0
	flecs.Observe[DPos](w, flecs.EventOnSet, func(_ *flecs.Writer, ev flecs.ID, _ DPos) { fired++ })

	flecs.DeferBeginForTest(w)
	flecs.Set[DPos](w.W(), e, DPos{1, 2})
	if fired != 0 {
		t.Fatalf("observer should not fire before DeferEnd, fired=%d", fired)
	}
	flecs.DeferEndForTest(w)

	if fired != 1 {
		t.Fatalf("observer should fire exactly once after DeferEnd, fired=%d", fired)
	}
}

// TestDeferObserverQueueingInObserver verifies that a mutation issued from
// within an observer during flush (deferDepth==0) applies immediately.
func TestDeferObserverQueueingInObserver(t *testing.T) {
	w := flecs.New()
	e1 := w.NewEntity()
	e2 := w.NewEntity()

	// When DPos is set on e1, the observer immediately sets DVel on e2.
	// At flush time deferDepth==0, so the nested Set applies immediately.
	flecs.Observe[DPos](w, flecs.EventOnSet, func(_ *flecs.Writer, _ flecs.ID, _ DPos) {
		flecs.Set[DVel](w.W(), e2, DVel{10, 20})
	})

	flecs.DeferBeginForTest(w)
	flecs.Set[DPos](w.W(), e1, DPos{1, 0})
	flecs.DeferEndForTest(w)

	if v, ok := flecs.Get[DVel](w.R(), e2); !ok || v != (DVel{10, 20}) {
		t.Fatalf("expected DVel applied by observer during flush, got %v ok=%v", v, ok)
	}
}

// TestDeferWrappedIteration verifies that deletes queued inside a
// Write-wrapped Each1 apply after iteration without corrupting it.
func TestDeferWrappedIteration(t *testing.T) {
	w := flecs.New()
	var entities []flecs.ID
	for i := 0; i < 5; i++ {
		e := w.NewEntity()
		flecs.Set[DPos](w.W(), e, DPos{float32(i - 2), 0}) // some negative X
		entities = append(entities, e)
	}

	var deleted []flecs.ID
	w.Write(func(fw *flecs.Writer) {
		flecs.Each1[DPos](w.R(), func(e flecs.ID, p *DPos) {
			if p.X < 0 {
				fw.Delete(e)
				deleted = append(deleted, e)
			}
		})
	})

	for _, e := range deleted {
		if w.IsAlive(e) {
			t.Fatalf("entity %v should be deleted after Write scope", e)
		}
	}
	// Non-deleted entities (X >= 0) should still be alive.
	for _, e := range entities {
		pos, ok := flecs.Get[DPos](w.R(), e)
		if !ok {
			continue // already deleted — OK
		}
		if pos.X < 0 {
			t.Fatalf("entity with negative X should have been deleted")
		}
	}
}

// TestDeferCascade verifies that a deferred Delete triggers cascade deletion of
// children when the flush runs.
func TestDeferCascade(t *testing.T) {
	w := flecs.New()
	parent := w.NewEntity()
	child1 := w.NewEntity()
	child2 := w.NewEntity()
	flecs.AddID(w.W(), child1, flecs.MakePair(w.ChildOf(), parent))
	flecs.AddID(w.W(), child2, flecs.MakePair(w.ChildOf(), parent))

	flecs.DeferBeginForTest(w)
	w.Delete(parent)
	// All three still alive during defer.
	if !w.IsAlive(parent) || !w.IsAlive(child1) || !w.IsAlive(child2) {
		t.Fatal("expected all alive during defer")
	}
	flecs.DeferEndForTest(w)

	if w.IsAlive(parent) || w.IsAlive(child1) || w.IsAlive(child2) {
		t.Fatal("expected parent and children deleted after DeferEnd")
	}
}

// TestDeferAddID verifies that AddID is properly queued and applied on flush.
func TestDeferAddID(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	tagID := flecs.RegisterComponent[DTag](w)

	flecs.DeferBeginForTest(w)
	flecs.AddID(w.W(), e, tagID)
	if flecs.HasID(w.R(), e, tagID) {
		t.Fatal("expected tag not yet added during defer")
	}
	flecs.DeferEndForTest(w)

	if !flecs.HasID(w.R(), e, tagID) {
		t.Fatal("expected tag added after DeferEnd")
	}
}

// TestDeferRemoveID verifies that RemoveID is properly queued and applied on flush.
func TestDeferRemoveID(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	tagID := flecs.RegisterComponent[DTag](w)
	flecs.AddID(w.W(), e, tagID)

	flecs.DeferBeginForTest(w)
	flecs.RemoveID(w.W(), e, tagID)
	if !flecs.HasID(w.R(), e, tagID) {
		t.Fatal("expected tag still present during defer")
	}
	flecs.DeferEndForTest(w)

	if flecs.HasID(w.R(), e, tagID) {
		t.Fatal("expected tag removed after DeferEnd")
	}
}

// TestDeferSetPair verifies that SetPair is properly queued and applied on flush.
func TestDeferSetPair(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	rel := w.NewEntity()
	tgt := w.NewEntity()

	flecs.DeferBeginForTest(w)
	flecs.SetPair[DEdge](w.W(), e, rel, tgt, DEdge{Weight: 99})
	if _, ok := flecs.GetPair[DEdge](w.R(), e, rel, tgt); ok {
		t.Fatal("expected pair not yet set during defer")
	}
	flecs.DeferEndForTest(w)

	v, ok := flecs.GetPair[DEdge](w.R(), e, rel, tgt)
	if !ok || v.Weight != 99 {
		t.Fatalf("expected DEdge{99} after DeferEnd, got %v ok=%v", v, ok)
	}
}

// TestDeferSetName verifies that SetName is deferred and that Lookup works after flush.
func TestDeferSetName(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()

	flecs.DeferBeginForTest(w)
	w.SetName(e, "alice")
	if _, ok := w.Lookup("alice"); ok {
		t.Fatal("expected Lookup to fail during defer")
	}
	flecs.DeferEndForTest(w)

	found, ok := w.Lookup("alice")
	if !ok || found != e {
		t.Fatalf("expected Lookup('alice') == e after DeferEnd, ok=%v", ok)
	}
}

// TestDeferRegisterComponentNotDeferred verifies that RegisterComponent always
// applies immediately regardless of defer depth.
func TestDeferRegisterComponentNotDeferred(t *testing.T) {
	w := flecs.New()
	flecs.DeferBeginForTest(w)
	type ImmediateType struct{ V int }
	id := flecs.RegisterComponent[ImmediateType](w)
	if id == 0 {
		t.Fatal("expected valid component ID immediately inside DeferBegin")
	}
	flecs.DeferEndForTest(w)
}

// TestDeferNewEntityNotDeferred verifies that NewEntity is always synchronous.
func TestDeferNewEntityNotDeferred(t *testing.T) {
	w := flecs.New()
	flecs.DeferBeginForTest(w)
	e := w.NewEntity()
	if e == 0 {
		t.Fatal("expected non-zero entity ID")
	}
	if !w.IsAlive(e) {
		t.Fatal("expected entity alive immediately inside DeferBegin")
	}
	flecs.DeferEndForTest(w)
}

// TestDeferReadAPIsNotDeferred verifies that Get/Has/HasID/IsAlive all see
// current state inside a defer block.
func TestDeferReadAPIsNotDeferred(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set[DPos](w.W(), e, DPos{3, 4})

	flecs.DeferBeginForTest(w)

	// Get sees current state.
	v, ok := flecs.Get[DPos](w.R(), e)
	if !ok || v != (DPos{3, 4}) {
		t.Fatalf("Get: expected DPos{3,4} inside defer, got %v ok=%v", v, ok)
	}

	// Has sees current state.
	if !flecs.Has[DPos](w.R(), e) {
		t.Fatal("Has: expected true inside defer")
	}

	// IsAlive sees current state.
	if !w.IsAlive(e) {
		t.Fatal("IsAlive: expected true inside defer")
	}

	// Queue a Set so we can verify Has still sees old state.
	flecs.Set[DVel](w.W(), e, DVel{1, 2})
	if flecs.Has[DVel](w.R(), e) {
		t.Fatal("Has: DVel should not be visible before DeferEnd")
	}

	flecs.DeferEndForTest(w)

	if !flecs.Has[DVel](w.R(), e) {
		t.Fatal("Has: DVel should be visible after DeferEnd")
	}
}

// TestDeferConvenienceNoPanic verifies the normal (no-panic) deferred path.
func TestDeferConvenienceNoPanic(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()

	w.Write(func(fw *flecs.Writer) {
		flecs.Set[DPos](fw, e, DPos{11, 22})
	})

	if v, ok := flecs.Get[DPos](w.R(), e); !ok || v != (DPos{11, 22}) {
		t.Fatalf("expected DPos{11,22} after Write, got %v ok=%v", v, ok)
	}
}

// TestDeferRemoveT verifies that Remove[T] is properly queued and applied on flush.
func TestDeferRemoveT(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set[DPos](w.W(), e, DPos{1, 2})

	flecs.DeferBeginForTest(w)
	ok := flecs.Remove[DPos](w.W(), e)
	if !ok {
		t.Fatal("Remove[DPos] should return true when T is present at queue time")
	}
	if !flecs.Has[DPos](w.R(), e) {
		t.Fatal("DPos should still be present during defer (not yet removed)")
	}
	flecs.DeferEndForTest(w)

	if flecs.Has[DPos](w.R(), e) {
		t.Fatal("expected DPos removed after DeferEnd")
	}
}

// TestDeferRemoveTNotPresent verifies that Remove[T] returns false when T is
// not on the entity at queue time.
func TestDeferRemoveTNotPresent(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()

	flecs.DeferBeginForTest(w)
	ok := flecs.Remove[DPos](w.W(), e)
	if ok {
		t.Fatal("Remove[DPos] should return false when T is not present at queue time")
	}
	flecs.DeferEndForTest(w)
}

// TestDeferDeleteDeadEntity verifies that Delete returns false inside a deferred
// block when the entity is already dead.
func TestDeferDeleteDeadEntity(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	w.Delete(e)

	flecs.DeferBeginForTest(w)
	ok := w.Delete(e) // entity is dead; should return false without queuing
	if ok {
		t.Fatal("expected Delete of dead entity to return false inside defer")
	}
	flecs.DeferEndForTest(w)
}

// TestDeferAddIDDeadEntity verifies that AddID panics inside a deferred block
// when the entity is not alive.
func TestDeferAddIDDeadEntity(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	tagID := flecs.RegisterComponent[DTag](w)
	w.Delete(e)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic from AddID on dead entity inside defer")
		}
	}()
	flecs.DeferBeginForTest(w)
	flecs.AddID(w.W(), e, tagID) // should panic: entity is dead
}

// TestDeferRemoveIDNotPresent verifies that RemoveID returns false when the id
// is not on the entity at queue time.
func TestDeferRemoveIDNotPresent(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	tagID := flecs.RegisterComponent[DTag](w)

	flecs.DeferBeginForTest(w)
	ok := flecs.RemoveID(w.W(), e, tagID)
	if ok {
		t.Fatal("expected RemoveID to return false when id not present")
	}
	flecs.DeferEndForTest(w)
}
