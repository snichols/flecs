package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// Component type used in cleanup-observer tests.
type coPos struct{ X, Y float32 }

// ── OnDelete event ────────────────────────────────────────────────────────────

func TestOnDelete_FiresOnDelete(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	var fired int
	flecs.OnDelete(w, func(_ *flecs.Reader, got flecs.ID) {
		if got == e {
			fired++
		}
	})

	w.Delete(e)

	if fired != 1 {
		t.Fatalf("want 1, got %d", fired)
	}
}

func TestOnDelete_FiresOncePerEntity(t *testing.T) {
	w := flecs.New()

	var parent flecs.ID
	var children [5]flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		for i := range children {
			children[i] = fw.NewEntity()
			fw.AddID(children[i], flecs.MakePair(w.ChildOf(), parent))
		}
	})

	fired := make(map[flecs.ID]int)
	flecs.OnDelete(w, func(_ *flecs.Reader, e flecs.ID) {
		fired[e]++
	})

	w.Delete(parent)

	// parent + 5 children = 6 total
	if len(fired) != 6 {
		t.Fatalf("want 6 distinct entities, got %d", len(fired))
	}
	for e, cnt := range fired {
		if cnt != 1 {
			t.Fatalf("entity %v fired %d times, want 1", e, cnt)
		}
	}
}

func TestOnDelete_BeforeRemove(t *testing.T) {
	w := flecs.New()

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, coPos{X: 1, Y: 2})
	})

	var order []string
	flecs.OnDelete(w, func(_ *flecs.Reader, got flecs.ID) {
		if got == e {
			order = append(order, "OnDelete")
		}
	})
	_ = flecs.Observe[coPos](w, flecs.EventOnRemove, func(_ *flecs.Writer, got flecs.ID, _ coPos) {
		if got == e {
			order = append(order, "OnRemove")
		}
	})

	w.Delete(e)

	if len(order) != 2 {
		t.Fatalf("want 2 events, got %v", order)
	}
	if order[0] != "OnDelete" {
		t.Fatalf("want OnDelete first, got %v", order)
	}
	if order[1] != "OnRemove" {
		t.Fatalf("want OnRemove second, got %v", order)
	}
}

func TestOnDelete_HandlerReadsState(t *testing.T) {
	w := flecs.New()

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, coPos{X: 3, Y: 7})
	})

	var readX float32
	flecs.OnDelete(w, func(fr *flecs.Reader, got flecs.ID) {
		if got != e {
			return
		}
		v, ok := flecs.Get[coPos](fr, got)
		if !ok {
			return
		}
		readX = v.X
	})

	w.Delete(e)

	if readX != 3 {
		t.Fatalf("want X=3, got %v", readX)
	}
}

func TestOnDelete_MultiTermFilter(t *testing.T) {
	w := flecs.New()

	var eWithPos, eNoPos flecs.ID
	w.Write(func(fw *flecs.Writer) {
		eWithPos = fw.NewEntity()
		flecs.Set(fw, eWithPos, coPos{X: 1})
		eNoPos = fw.NewEntity() // no coPos
	})

	posID := flecs.RegisterComponent[coPos](w)

	var fired []flecs.ID
	flecs.OnDeleteWithOptions(w, flecs.WithQuery(flecs.With(posID)), func(_ *flecs.Reader, e flecs.ID) {
		fired = append(fired, e)
	})

	w.Delete(eWithPos)
	w.Delete(eNoPos)

	if len(fired) != 1 || fired[0] != eWithPos {
		t.Fatalf("want only eWithPos; got %v", fired)
	}
}

func TestOnDelete_NoFireOnAlive(t *testing.T) {
	w := flecs.New()

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	var fired int
	flecs.OnDelete(w, func(_ *flecs.Reader, _ flecs.ID) { fired++ })

	// Don't delete e — observer must NOT fire.
	if fired != 0 {
		t.Fatalf("observer fired for alive entity: got %d", fired)
	}
	if !w.IsAlive(e) {
		t.Fatal("entity should still be alive")
	}
}

func TestOnDelete_ObserverDisabled(t *testing.T) {
	w := flecs.New()

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	var fired int
	obs := flecs.OnDelete(w, func(_ *flecs.Reader, _ flecs.ID) { fired++ })
	obs.SetEnabled(false)

	w.Delete(e)

	if fired != 0 {
		t.Fatalf("disabled observer must not fire; got %d", fired)
	}
}

func TestOnDelete_YieldExisting_IsNoOp(t *testing.T) {
	w := flecs.New()

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// Register with WithYieldExisting — must not fire at registration time.
	var fired int
	flecs.OnDeleteWithOptions(w, flecs.WithYieldExisting(), func(_ *flecs.Reader, _ flecs.ID) {
		fired++
	})

	if fired != 0 {
		t.Fatalf("WithYieldExisting must be no-op for OnDelete; got %d", fired)
	}
	_ = e
}

// ── OnDeleteTarget event ──────────────────────────────────────────────────────

func TestOnDeleteTarget_FiresPerDependent(t *testing.T) {
	w := flecs.New()

	var parent flecs.ID
	var children [3]flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		for i := range children {
			children[i] = fw.NewEntity()
			fw.AddID(children[i], flecs.MakePair(w.ChildOf(), parent))
		}
	})

	var fired int
	flecs.OnDeleteTarget(w, func(_ *flecs.Reader, _ flecs.ID, _ flecs.ID, _ flecs.ID) {
		fired++
	})

	w.Delete(parent)

	if fired != 3 {
		t.Fatalf("want 3 (one per child); got %d", fired)
	}
}

func TestOnDeleteTarget_PassesPairRel(t *testing.T) {
	w := flecs.New()

	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		child = fw.NewEntity()
		fw.AddID(child, flecs.MakePair(w.ChildOf(), parent))
	})

	var seenTarget, seenDependent, seenPairRel flecs.ID
	flecs.OnDeleteTarget(w, func(_ *flecs.Reader, tgt flecs.ID, dep flecs.ID, rel flecs.ID) {
		seenTarget = tgt
		seenDependent = dep
		seenPairRel = rel
	})

	w.Delete(parent)

	if seenTarget != parent {
		t.Fatalf("target: want %v, got %v", parent, seenTarget)
	}
	if seenDependent != child {
		t.Fatalf("dependent: want %v, got %v", child, seenDependent)
	}
	if seenPairRel.Index() != w.ChildOf().Index() {
		t.Fatalf("pairRelID: want ChildOf (%v), got %v", w.ChildOf(), seenPairRel)
	}
}

func TestOnDeleteTarget_FilterByRelationship(t *testing.T) {
	w := flecs.New()

	// relA: custom relationship with OnDeleteTarget+Delete.
	var relA, target, depA, depChild flecs.ID
	w.Write(func(fw *flecs.Writer) {
		relA = fw.NewEntity()
		target = fw.NewEntity()
		depA = fw.NewEntity()
		depChild = fw.NewEntity()
	})
	flecs.SetCleanupPolicy(w, relA, w.OnDeleteTarget(), w.DeleteAction())

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(depA, flecs.MakePair(relA, target))
		fw.AddID(depChild, flecs.MakePair(w.ChildOf(), target))
	})

	// Observer filtered to relA only.
	var fired []flecs.ID
	flecs.OnDeleteTargetWithOptions(w, flecs.WithRelationship(relA), func(_ *flecs.Reader, _ flecs.ID, dep flecs.ID, _ flecs.ID) {
		fired = append(fired, dep)
	})

	w.Delete(target)

	if len(fired) != 1 || fired[0] != depA {
		t.Fatalf("want only depA; got %v", fired)
	}
}

func TestOnDeleteTarget_PolicyInteraction_Remove(t *testing.T) {
	// Remove policy with parent-storage: fires, then dependent's pair is removed.
	w := flecs.New()

	var rel, target, dep flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		target = fw.NewEntity()
		dep = fw.NewEntity()
	})
	// Set up parent storage + RemoveAction policy.
	flecs.SetRelationship(w, rel)
	flecs.SetExclusive(w, rel)
	flecs.SetParentStorage(w, rel)
	flecs.SetCleanupPolicy(w, rel, w.OnDeleteTarget(), w.RemoveAction())

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(dep, flecs.MakePair(rel, target))
	})

	var fired int
	flecs.OnDeleteTarget(w, func(_ *flecs.Reader, tgt flecs.ID, d flecs.ID, pairRel flecs.ID) {
		if tgt == target && d == dep && pairRel.Index() == rel.Index() {
			fired++
		}
	})

	w.Delete(target)

	if fired != 1 {
		t.Fatalf("want 1 fire; got %d", fired)
	}
	if !w.IsAlive(dep) {
		t.Fatal("dep should still be alive after Remove policy")
	}
	// dep should no longer have (rel, target) pair.
	var hasPair bool
	w.Read(func(fr *flecs.Reader) {
		hasPair = flecs.HasID(fr, dep, flecs.MakePair(rel, target))
	})
	if hasPair {
		t.Fatal("dep should no longer have (rel, target) pair after Remove policy")
	}
}

func TestOnDeleteTarget_PolicyInteraction_Delete(t *testing.T) {
	// Delete policy: fires, then dependent is deleted (which itself triggers OnDelete).
	w := flecs.New()

	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		child = fw.NewEntity()
		fw.AddID(child, flecs.MakePair(w.ChildOf(), parent))
	})

	var targetFired, deleteFired int
	flecs.OnDeleteTarget(w, func(_ *flecs.Reader, tgt flecs.ID, dep flecs.ID, _ flecs.ID) {
		if tgt == parent && dep == child {
			targetFired++
		}
	})
	flecs.OnDelete(w, func(_ *flecs.Reader, e flecs.ID) {
		if e == child {
			deleteFired++
		}
	})

	w.Delete(parent)

	if targetFired != 1 {
		t.Fatalf("OnDeleteTarget: want 1, got %d", targetFired)
	}
	if deleteFired != 1 {
		t.Fatalf("OnDelete for child: want 1, got %d", deleteFired)
	}
	if w.IsAlive(child) {
		t.Fatal("child should be dead after Delete policy cascade")
	}
}

func TestOnDeleteTarget_PolicyInteraction_Panic(t *testing.T) {
	w := flecs.New()

	var rel, target, dep flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		target = fw.NewEntity()
		dep = fw.NewEntity()
	})
	flecs.SetCleanupPolicy(w, rel, w.OnDeleteTarget(), w.PanicAction())
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(dep, flecs.MakePair(rel, target))
	})

	var firedBeforePanic int
	flecs.OnDeleteTarget(w, func(_ *flecs.Reader, tgt flecs.ID, d flecs.ID, _ flecs.ID) {
		if tgt == target && d == dep {
			firedBeforePanic++
		}
	})

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic from PanicAction policy")
		}
		if firedBeforePanic != 1 {
			t.Fatalf("want observer to fire before panic; got %d", firedBeforePanic)
		}
	}()
	w.Delete(target)
}

// ── Component-remove cascade ──────────────────────────────────────────────────

func TestComponentRemove_ActivelyRemoved(t *testing.T) {
	w := flecs.New()

	var e flecs.ID
	cID := flecs.RegisterComponent[coPos](w)
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, coPos{X: 1})
	})

	w.Delete(cID)

	var hasComp bool
	w.Read(func(fr *flecs.Reader) {
		hasComp = flecs.HasID(fr, e, cID)
	})
	if hasComp {
		t.Fatal("entity should no longer have component after component entity is deleted")
	}
	if !w.IsAlive(e) {
		t.Fatal("holder entity should still be alive")
	}
}

func TestComponentRemove_OnRemoveFires(t *testing.T) {
	w := flecs.New()

	var e flecs.ID
	cID := flecs.RegisterComponent[coPos](w)
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, coPos{X: 5})
	})

	var removeFired int
	flecs.OnRemove[coPos](w, func(_ *flecs.Writer, got flecs.ID, _ coPos) {
		if got == e {
			removeFired++
		}
	})

	w.Delete(cID)

	if removeFired != 1 {
		t.Fatalf("want OnRemove hook to fire once; got %d", removeFired)
	}
}

func TestComponentRemove_OnRemoveObserverFires(t *testing.T) {
	w := flecs.New()

	var e flecs.ID
	cID := flecs.RegisterComponent[coPos](w)
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, coPos{X: 5})
	})

	var removeFired int
	_ = flecs.Observe[coPos](w, flecs.EventOnRemove, func(_ *flecs.Writer, got flecs.ID, _ coPos) {
		if got == e {
			removeFired++
		}
	})

	w.Delete(cID)

	if removeFired != 1 {
		t.Fatalf("want EventOnRemove observer to fire once; got %d", removeFired)
	}
}

func TestComponentRemove_OnDeleteFires(t *testing.T) {
	w := flecs.New()

	cID := flecs.RegisterComponent[coPos](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, coPos{})
	})

	var onDeleteFired bool
	flecs.OnDelete(w, func(_ *flecs.Reader, got flecs.ID) {
		if got == cID {
			onDeleteFired = true
		}
	})

	w.Delete(cID)

	if !onDeleteFired {
		t.Fatal("OnDelete should fire for the component entity itself")
	}
	// The holder should still be alive but without the component.
	if !w.IsAlive(e) {
		t.Fatal("holder entity should still be alive")
	}
}

func TestComponentRemove_ManyEntities_AllRemoved(t *testing.T) {
	w := flecs.New()

	const N = 1000
	cID := flecs.RegisterComponent[coPos](w)
	entities := make([]flecs.ID, N)
	w.Write(func(fw *flecs.Writer) {
		for i := range entities {
			entities[i] = fw.NewEntity()
			flecs.Set(fw, entities[i], coPos{X: float32(i)})
		}
	})

	w.Delete(cID)

	w.Read(func(fr *flecs.Reader) {
		for i, e := range entities {
			if !w.IsAlive(e) {
				t.Fatalf("entity[%d] should still be alive", i)
			}
			if flecs.HasID(fr, e, cID) {
				t.Fatalf("entity[%d] should not have component after component entity deleted", i)
			}
		}
	})
}

func TestComponentRemove_NoOrphanedSignatures(t *testing.T) {
	// After deleting a component entity, no alive entity should still carry it.
	// Empty tables may transiently retain the type in their signature until table
	// reclamation runs; "no orphaned signatures" means no entity is stuck in a
	// table that still lists the dead component.
	w := flecs.New()

	cID := flecs.RegisterComponent[coPos](w)
	entities := make([]flecs.ID, 5)
	w.Write(func(fw *flecs.Writer) {
		for i := range entities {
			entities[i] = fw.NewEntity()
			flecs.Set(fw, entities[i], coPos{X: float32(i)})
		}
	})

	w.Delete(cID)

	w.Read(func(fr *flecs.Reader) {
		for i, e := range entities {
			if flecs.HasID(fr, e, cID) {
				t.Errorf("entity[%d] (%v) still carries deleted component %v", i, e, cID)
			}
		}
	})
}

// ── Deferred scope integration ────────────────────────────────────────────────

func TestOnDelete_FiresInDeferredScope(t *testing.T) {
	w := flecs.New()

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	var fired int
	flecs.OnDelete(w, func(_ *flecs.Reader, got flecs.ID) {
		if got == e {
			fired++
		}
	})

	// Delete inside a Write scope — the delete is queued and flushed on return.
	w.Write(func(_ *flecs.Writer) {
		w.Delete(e)
	})

	if fired != 1 {
		t.Fatalf("want 1; got %d", fired)
	}
}

func TestOnDeleteTarget_OrderedWithOnDelete(t *testing.T) {
	w := flecs.New()

	var parent, child1, child2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		child1 = fw.NewEntity()
		child2 = fw.NewEntity()
		fw.AddID(child1, flecs.MakePair(w.ChildOf(), parent))
		fw.AddID(child2, flecs.MakePair(w.ChildOf(), parent))
	})

	type event struct {
		kind string
		e    flecs.ID
	}
	var events []event

	flecs.OnDeleteTarget(w, func(_ *flecs.Reader, _ flecs.ID, dep flecs.ID, _ flecs.ID) {
		events = append(events, event{"OnDeleteTarget", dep})
	})
	flecs.OnDelete(w, func(_ *flecs.Reader, e flecs.ID) {
		events = append(events, event{"OnDelete", e})
	})

	w.Write(func(_ *flecs.Writer) {
		w.Delete(parent)
	})

	// For each child, OnDeleteTarget must appear before OnDelete.
	for _, child := range []flecs.ID{child1, child2} {
		var targetIdx, deleteIdx int = -1, -1
		for i, ev := range events {
			if ev.e == child {
				if ev.kind == "OnDeleteTarget" {
					targetIdx = i
				} else if ev.kind == "OnDelete" {
					deleteIdx = i
				}
			}
		}
		if targetIdx < 0 || deleteIdx < 0 {
			t.Fatalf("child %v: missing OnDeleteTarget or OnDelete event", child)
		}
		if targetIdx > deleteIdx {
			t.Fatalf("child %v: OnDeleteTarget must fire before OnDelete (indices %d vs %d)", child, targetIdx, deleteIdx)
		}
	}
}

// ── Multi-event observers ─────────────────────────────────────────────────────

func TestOnDelete_AndOnRemove_SameComponent(t *testing.T) {
	w := flecs.New()

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, coPos{})
	})

	var order []string
	flecs.OnDelete(w, func(_ *flecs.Reader, got flecs.ID) {
		if got == e {
			order = append(order, "OnDelete")
		}
	})
	_ = flecs.Observe[coPos](w, flecs.EventOnRemove, func(_ *flecs.Writer, got flecs.ID, _ coPos) {
		if got == e {
			order = append(order, "OnRemove")
		}
	})

	w.Delete(e)

	if len(order) != 2 {
		t.Fatalf("want 2 events; got %v", order)
	}
	if order[0] != "OnDelete" || order[1] != "OnRemove" {
		t.Fatalf("wrong order: %v", order)
	}
}

func TestOnDelete_PanicInHandler_DoesNotCorruptState(t *testing.T) {
	w := flecs.New()

	// A survivor entity: must remain accessible after the panic.
	var survivor flecs.ID
	w.Write(func(fw *flecs.Writer) {
		survivor = fw.NewEntity()
		flecs.Set(fw, survivor, coPos{X: 42})
	})

	// A separate entity whose deletion triggers the panicking observer.
	var victim flecs.ID
	w.Write(func(fw *flecs.Writer) { victim = fw.NewEntity() })

	flecs.OnDelete(w, func(_ *flecs.Reader, e flecs.ID) {
		if e == victim {
			panic("intentional panic in OnDelete handler")
		}
	})

	func() {
		defer func() { recover() }() //nolint:errcheck
		w.Delete(victim)
	}()

	// World must still be query-able for unrelated entities.
	w.Read(func(fr *flecs.Reader) {
		v, ok := flecs.Get[coPos](fr, survivor)
		if !ok {
			t.Error("survivor lost its component after unrelated panic")
			return
		}
		if v.X != 42 {
			t.Errorf("survivor component corrupted: want X=42, got %v", v.X)
		}
	})
}

// ── Reclamation interaction ───────────────────────────────────────────────────

func TestOnDelete_BeforeTableReclamation(t *testing.T) {
	w := flecs.New()
	w.SetTableReclamationThreshold(1) // reclaim after 1 empty tick

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, coPos{X: 1})
	})

	var order []string
	flecs.OnDelete(w, func(_ *flecs.Reader, got flecs.ID) {
		if got == e {
			order = append(order, "OnDelete")
		}
	})
	flecs.OnTableDelete(w, func(_ *flecs.Reader, _ *flecs.Table) {
		order = append(order, "OnTableDelete")
	})

	// Delete e: fires OnDelete immediately; table becomes empty.
	w.Delete(e)

	// Progress one tick: sweep reclaims the now-empty table → fires OnTableDelete.
	w.Progress(0)

	if len(order) < 2 {
		t.Fatalf("want at least 2 events; got %v", order)
	}
	if order[0] != "OnDelete" {
		t.Fatalf("want OnDelete first; got %v", order)
	}
	found := false
	for _, ev := range order[1:] {
		if ev == "OnTableDelete" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("OnTableDelete never fired; events: %v", order)
	}
}

// ── WithRelationship + AndQuery coverage ─────────────────────────────────────

func TestWithRelationship_PanicOnZero(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic from WithRelationship(0)")
		}
	}()
	flecs.WithRelationship(0)
}

func TestOnDeleteTarget_AndQuery(t *testing.T) {
	// WithRelationship(rel).AndQuery(With(posID)) — combines both filters.
	// Observer should fire only for dependents that have coPos AND are under rel cascade.
	w := flecs.New()

	var rel, target, depWithPos, depNoPos flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		target = fw.NewEntity()
		depWithPos = fw.NewEntity()
		depNoPos = fw.NewEntity()
	})
	flecs.SetCleanupPolicy(w, rel, w.OnDeleteTarget(), w.DeleteAction())
	posID := flecs.RegisterComponent[coPos](w)
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(depWithPos, flecs.MakePair(rel, target))
		flecs.Set(fw, depWithPos, coPos{X: 1})
		fw.AddID(depNoPos, flecs.MakePair(rel, target))
		// depNoPos has no coPos
	})

	var fired []flecs.ID
	opts := flecs.WithRelationship(rel).AndQuery(flecs.With(posID))
	flecs.OnDeleteTargetWithOptions(w, opts, func(_ *flecs.Reader, _ flecs.ID, dep flecs.ID, _ flecs.ID) {
		fired = append(fired, dep)
	})

	w.Delete(target)

	if len(fired) != 1 || fired[0] != depWithPos {
		t.Fatalf("want only depWithPos; got %v", fired)
	}
}
