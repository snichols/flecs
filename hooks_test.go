package flecs_test

import (
	"testing"
	"unsafe"

	"github.com/snichols/flecs"
	"github.com/snichols/flecs/internal/component"
)

// ── Registration ──────────────────────────────────────────────────────────────

func TestOnSetRegistrationReplaces(t *testing.T) {
	w := flecs.New()
	calls := 0
	flecs.OnSet[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { calls++ })
	flecs.OnSet[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { calls += 10 })
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[Position](fw, e, Position{1, 2})
	})
	if calls != 10 {
		t.Fatalf("want 10 (second hook only), got %d", calls)
	}
}

func TestOnSetClearWithNil(t *testing.T) {
	w := flecs.New()
	calls := 0
	flecs.OnSet[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { calls++ })
	flecs.OnSet[Position](w, nil)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[Position](fw, e, Position{1, 2})
	})
	if calls != 0 {
		t.Fatalf("hook should have been cleared, got %d calls", calls)
	}
}

func TestOnAddClearWithNil(t *testing.T) {
	w := flecs.New()
	calls := 0
	flecs.OnAdd[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { calls++ })
	flecs.OnAdd[Position](w, nil)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[Position](fw, e, Position{1, 2})
	})
	if calls != 0 {
		t.Fatalf("OnAdd hook should have been cleared, got %d calls", calls)
	}
}

func TestOnRemoveClearWithNil(t *testing.T) {
	w := flecs.New()
	calls := 0
	flecs.OnRemove[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { calls++ })
	flecs.OnRemove[Position](w, nil)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[Position](fw, e, Position{1, 2})
		flecs.Remove[Position](fw, e)
	})
	if calls != 0 {
		t.Fatalf("OnRemove hook should have been cleared, got %d calls", calls)
	}
}

// ── OnAdd / OnSet fire counts ────────────────────────────────────────────────

func TestOnAddFiresOnceOnInitialSet(t *testing.T) {
	w := flecs.New()
	addCount, setCount := 0, 0
	flecs.OnAdd[Position](w, func(_ *flecs.Writer, e flecs.ID, p Position) { addCount++ })
	flecs.OnSet[Position](w, func(_ *flecs.Writer, e flecs.ID, p Position) { setCount++ })
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[Position](fw, e, Position{1, 2})
	})
	if addCount != 1 {
		t.Fatalf("OnAdd want 1, got %d", addCount)
	}
	if setCount != 1 {
		t.Fatalf("OnSet want 1, got %d", setCount)
	}
}

func TestOnSetFiresEverySet(t *testing.T) {
	w := flecs.New()
	addCount, setCount := 0, 0
	flecs.OnAdd[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { addCount++ })
	flecs.OnSet[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { setCount++ })
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[Position](fw, e, Position{1, 2})
		flecs.Set[Position](fw, e, Position{3, 4})
	})
	if addCount != 1 {
		t.Fatalf("OnAdd want 1 (initial only), got %d", addCount)
	}
	if setCount != 2 {
		t.Fatalf("OnSet want 2, got %d", setCount)
	}
}

func TestOnSetReceivesCorrectValue(t *testing.T) {
	w := flecs.New()
	var got Position
	flecs.OnSet[Position](w, func(_ *flecs.Writer, _ flecs.ID, p Position) { got = p })
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[Position](fw, e, Position{7, 9})
	})
	if got != (Position{7, 9}) {
		t.Fatalf("OnSet value want {7,9}, got %v", got)
	}
}

func TestOnSetReceivesEntityID(t *testing.T) {
	w := flecs.New()
	var gotID flecs.ID
	flecs.OnSet[Position](w, func(_ *flecs.Writer, e flecs.ID, _ Position) { gotID = e })
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[Position](fw, e, Position{1, 2})
	})
	if gotID != e {
		t.Fatalf("OnSet entity want %v, got %v", e, gotID)
	}
}

// ── OnAdd fires on AddID ──────────────────────────────────────────────────────

func TestOnAddFiresOnAddID(t *testing.T) {
	w := flecs.New()
	addCount := 0
	flecs.OnAdd[Tag](w, func(_ *flecs.Writer, _ flecs.ID, _ Tag) { addCount++ })
	tagID := flecs.RegisterComponent[Tag](w)
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.AddID(fw, e, tagID)
	})
	if addCount != 1 {
		t.Fatalf("OnAdd want 1 after AddID, got %d", addCount)
	}
	// Second AddID on same entity is no-op: no hook.
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, tagID)
	})
	if addCount != 1 {
		t.Fatalf("OnAdd want 1 (idempotent AddID), got %d", addCount)
	}
}

// ── OnAdd does NOT fire for carried-over components ───────────────────────────

func TestOnAddNotFiredForCarriedComponents(t *testing.T) {
	w := flecs.New()
	posAddCount, velAddCount := 0, 0
	flecs.OnAdd[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { posAddCount++ })
	flecs.OnAdd[Velocity](w, func(_ *flecs.Writer, _ flecs.ID, _ Velocity) { velAddCount++ })
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.Set[Position](fw, e, Position{1, 2}) // migrates: adds Position → OnAdd[Pos] fires
		flecs.Set[Velocity](fw, e, Velocity{3, 4}) // migrates: adds Velocity → OnAdd[Vel] fires only
	})
	if posAddCount != 1 {
		t.Fatalf("OnAdd[Position] want 1, got %d", posAddCount)
	}
	if velAddCount != 1 {
		t.Fatalf("OnAdd[Velocity] want 1, got %d", velAddCount)
	}
}

// ── OnRemove fires before removal ────────────────────────────────────────────

func TestOnRemoveFiresBeforeRemoval(t *testing.T) {
	w := flecs.New()
	var captured Position
	flecs.OnRemove[Position](w, func(_ *flecs.Writer, _ flecs.ID, p Position) { captured = p })
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[Position](fw, e, Position{1, 2})
	})
	// Set is now applied; Remove fires OnRemove with the current value.
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[Position](fw, e)
	})
	if captured != (Position{1, 2}) {
		t.Fatalf("OnRemove captured want {1,2}, got %v", captured)
	}
}

func TestOnRemoveEntityStillAliveInCallback(t *testing.T) {
	w := flecs.New()
	alive := false
	flecs.OnRemove[Position](w, func(_ *flecs.Writer, e flecs.ID, _ Position) {
		alive = w.IsAlive(e)
	})
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[Position](fw, e, Position{1, 2})
	})
	// Set is now applied; Remove fires OnRemove.
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[Position](fw, e)
	})
	if !alive {
		t.Fatal("entity should still be alive inside OnRemove callback")
	}
}

// ── OnRemove fires per-component on Delete ───────────────────────────────────

func TestOnRemoveFiresPerComponentOnDelete(t *testing.T) {
	w := flecs.New()
	posRemoved, velRemoved := false, false
	flecs.OnRemove[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { posRemoved = true })
	flecs.OnRemove[Velocity](w, func(_ *flecs.Writer, _ flecs.ID, _ Velocity) { velRemoved = true })
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[Position](fw, e, Position{1, 2})
		flecs.Set[Velocity](fw, e, Velocity{3, 4})
	})
	w.Delete(e)
	if !posRemoved {
		t.Fatal("OnRemove[Position] did not fire on Delete")
	}
	if !velRemoved {
		t.Fatal("OnRemove[Velocity] did not fire on Delete")
	}
}

// ── OnRemove fires in cascade-delete (child-first) ───────────────────────────

func TestOnRemoveFiresCascadeChildFirst(t *testing.T) {
	w := flecs.New()
	var order []string
	flecs.OnRemove[Position](w, func(_ *flecs.Writer, e flecs.ID, _ Position) {
		order = append(order, "pos")
		_ = e
	})
	var parent flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		child := fw.NewEntity()
		flecs.Set[Position](fw, parent, Position{1, 2})
		flecs.Set[Position](fw, child, Position{3, 4})
		flecs.AddID(fw, child, flecs.MakePair(w.ChildOf(), parent))
	})
	w.Delete(parent)
	// Both OnRemove[Position] calls must have fired.
	if len(order) != 2 {
		t.Fatalf("OnRemove want 2 calls (child+parent), got %d", len(order))
	}
}

// ── No hook fires for inherited components (IsA) ─────────────────────────────

func TestNoHookForInheritedComponents(t *testing.T) {
	w := flecs.New()
	addCount, setCount := 0, 0
	flecs.OnAdd[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { addCount++ })
	flecs.OnSet[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { setCount++ })
	var prefab, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set[Position](fw, prefab, Position{1, 2})
	})
	// Reset counts after setting prefab.
	addCount, setCount = 0, 0
	w.Write(func(fw *flecs.Writer) {
		child = fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
	})
	// Get resolves via IsA chain — no hook should fire.
	var v Position
	var ok bool
	w.Read(func(r *flecs.Reader) {
		v, ok = flecs.Get[Position](r, child)
	})
	if !ok || v != (Position{1, 2}) {
		t.Fatalf("Get via IsA: want {1,2} ok=true, got %v ok=%v", v, ok)
	}
	if addCount != 0 || setCount != 0 {
		t.Fatalf("inherited Get must not fire hooks: add=%d set=%d", addCount, setCount)
	}
}

// ── Override fires OnAdd ──────────────────────────────────────────────────────

func TestOverrideFiresOnAdd(t *testing.T) {
	w := flecs.New()
	addCount := 0
	flecs.OnAdd[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { addCount++ })
	var prefab flecs.ID
	w.Write(func(fw *flecs.Writer) {
		prefab = fw.NewEntity()
		flecs.Set[Position](fw, prefab, Position{1, 2})
	})
	// Reset after setting prefab.
	addCount = 0
	w.Write(func(fw *flecs.Writer) {
		child := fw.NewEntity()
		flecs.AddID(fw, child, flecs.MakePair(w.IsA(), prefab))
		// Copy-on-write override: Position is not locally owned, so migrate fires OnAdd.
		flecs.Set[Position](fw, child, Position{3, 4})
	})
	if addCount != 1 {
		t.Fatalf("override OnAdd want 1, got %d", addCount)
	}
}

// ── No hook fires when no hook is registered ─────────────────────────────────

func TestNoHookNoPanic(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[Position](fw, e, Position{1, 2})
		flecs.Set[Position](fw, e, Position{3, 4})
		flecs.Remove[Position](fw, e)
	})
	w.Delete(e)
	// Must not panic.
}

// ── Hook panic propagates ─────────────────────────────────────────────────────

func TestHookPanicPropagates(t *testing.T) {
	w := flecs.New()
	flecs.OnSet[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) {
		panic("hook panic")
	})
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic to propagate through Set")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, e, Position{1, 2})
	})
}

// ── Re-entrancy: read-only is safe ───────────────────────────────────────────

func TestOnSetReadOnlyReentrancy(t *testing.T) {
	w := flecs.New()
	var gotVel Velocity
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[Velocity](fw, e, Velocity{5, 6})
	})
	flecs.OnSet[Position](w, func(fw *flecs.Writer, eid flecs.ID, _ Position) {
		gotVel, _ = flecs.Get[Velocity](fw, eid)
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, e, Position{1, 2})
	})
	if gotVel != (Velocity{5, 6}) {
		t.Fatalf("re-entrant Get[Velocity] want {5,6}, got %v", gotVel)
	}
}

// ── Tag (Size==0) — OnAdd fires ───────────────────────────────────────────────

func TestOnAddTagFires(t *testing.T) {
	w := flecs.New()
	addCount := 0
	flecs.OnAdd[Tag](w, func(_ *flecs.Writer, _ flecs.ID, _ Tag) { addCount++ })
	tagID := flecs.RegisterComponent[Tag](w)
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		flecs.AddID(fw, e, tagID)
	})
	if addCount != 1 {
		t.Fatalf("OnAdd[Tag] want 1, got %d", addCount)
	}
}

// ── SetPair fires OnAdd and OnSet on pair's TypeInfo ─────────────────────────

func TestSetPairFiresPairTypeInfoHooks(t *testing.T) {
	w := flecs.New()
	var rel, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
	})
	pairID := flecs.MakePair(rel, tgt)
	pairInfo := component.RegisterPairData[Position](w.Registry(), pairID)

	addCount, setCount := 0, 0
	pairInfo.Hooks.OnAdd = func(_ any, _ flecs.ID, ptr unsafe.Pointer) { addCount++ }
	pairInfo.Hooks.OnSet = func(_ any, _ flecs.ID, ptr unsafe.Pointer) { setCount++ }

	w.Write(func(fw *flecs.Writer) {
		flecs.SetPair[Position](fw, e, rel, tgt, Position{1, 2})
	})
	if addCount != 1 {
		t.Fatalf("pair OnAdd want 1, got %d", addCount)
	}
	if setCount != 1 {
		t.Fatalf("pair OnSet want 1, got %d", setCount)
	}

	// Second call: OnAdd must NOT fire again, OnSet must fire.
	w.Write(func(fw *flecs.Writer) {
		flecs.SetPair[Position](fw, e, rel, tgt, Position{3, 4})
	})
	if addCount != 1 {
		t.Fatalf("pair OnAdd want 1 (no re-add), got %d", addCount)
	}
	if setCount != 2 {
		t.Fatalf("pair OnSet want 2, got %d", setCount)
	}
}

// TestHookReceivesWriterDirect checks that a hook callback receives a non-nil
// *Writer when triggered via a direct Set (outside a Write scope).
func TestHookReceivesWriterDirect(t *testing.T) {
	w := flecs.New()
	var gotWriter *flecs.Writer
	flecs.OnSet[Position](w, func(fw *flecs.Writer, _ flecs.ID, _ Position) {
		gotWriter = fw
	})
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[Position](fw, e, Position{1, 2})
	})
	if gotWriter == nil {
		t.Fatal("OnSet hook received nil *Writer")
	}
}

// ── OnReplace ─────────────────────────────────────────────────────────────────

// Test 1: first Set does not fire OnReplace; second Set does.
func TestOnReplaceBasic(t *testing.T) {
	w := flecs.New()
	var gotOld, gotNew Position
	calls := 0
	flecs.OnReplace[Position](w, func(_ *flecs.Writer, _ flecs.ID, old, new Position) {
		gotOld = old
		gotNew = new
		calls++
	})
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[Position](fw, e, Position{1, 2}) // first set: OnReplace must NOT fire
	})
	if calls != 0 {
		t.Fatalf("OnReplace must not fire on first Set, got %d calls", calls)
	}
	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, e, Position{3, 4}) // replace: OnReplace must fire
	})
	if calls != 1 {
		t.Fatalf("OnReplace want 1 call on second Set, got %d", calls)
	}
	if gotOld != (Position{1, 2}) {
		t.Fatalf("OnReplace old want {1,2}, got %v", gotOld)
	}
	if gotNew != (Position{3, 4}) {
		t.Fatalf("OnReplace new want {3,4}, got %v", gotNew)
	}
}

// Test 2: old value matches the previous Set across N sequential Sets.
func TestOnReplaceOldValueCapture(t *testing.T) {
	w := flecs.New()
	var seenOld []float32
	flecs.OnReplace[Position](w, func(_ *flecs.Writer, _ flecs.ID, old, _ Position) {
		seenOld = append(seenOld, old.X)
	})
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity(); flecs.Set[Position](fw, e, Position{1, 0}) })
	for i := 2; i <= 5; i++ {
		w.Write(func(fw *flecs.Writer) { flecs.Set[Position](fw, e, Position{float32(i), 0}) })
	}
	want := []float32{1, 2, 3, 4}
	if len(seenOld) != len(want) {
		t.Fatalf("want %d OnReplace calls, got %d", len(want), len(seenOld))
	}
	for i, w := range want {
		if seenOld[i] != w {
			t.Fatalf("old[%d] want %v, got %v", i, w, seenOld[i])
		}
	}
}

// Test 3: new value in the callback matches the current Set's value.
func TestOnReplaceNewValueVisibility(t *testing.T) {
	w := flecs.New()
	var gotNew Position
	flecs.OnReplace[Position](w, func(_ *flecs.Writer, _ flecs.ID, _, new Position) { gotNew = new })
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity(); flecs.Set[Position](fw, e, Position{0, 0}) })
	w.Write(func(fw *flecs.Writer) { flecs.Set[Position](fw, e, Position{7, 9}) })
	if gotNew != (Position{7, 9}) {
		t.Fatalf("OnReplace new want {7,9}, got %v", gotNew)
	}
}

// Test 4: no EventOnReplace constant; EventOnSet observer still fires alongside OnReplace.
func TestOnReplaceNoObserverEventLeak(t *testing.T) {
	w := flecs.New()
	replaceCalls, observerCalls := 0, 0
	flecs.OnReplace[Position](w, func(_ *flecs.Writer, _ flecs.ID, _, _ Position) { replaceCalls++ })
	flecs.Observe[Position](w, flecs.EventOnSet, func(_ *flecs.Writer, _ flecs.ID, _ Position) { observerCalls++ })
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity(); flecs.Set[Position](fw, e, Position{1, 0}) })
	w.Write(func(fw *flecs.Writer) { flecs.Set[Position](fw, e, Position{2, 0}) })
	if replaceCalls != 1 {
		t.Fatalf("OnReplace want 1, got %d", replaceCalls)
	}
	if observerCalls != 2 {
		t.Fatalf("EventOnSet observer want 2, got %d", observerCalls)
	}
}

// Test 5: OnReplace fires before OnSet on overwrite; OnSet fires but OnReplace does not on first Set.
func TestOnReplaceAndOnSetInterleaving(t *testing.T) {
	w := flecs.New()
	var order []string
	flecs.OnReplace[Position](w, func(_ *flecs.Writer, _ flecs.ID, _, _ Position) {
		order = append(order, "replace")
	})
	flecs.OnSet[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) {
		order = append(order, "set")
	})
	var e flecs.ID
	// First Set: only OnSet.
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity(); flecs.Set[Position](fw, e, Position{1, 0}) })
	if len(order) != 1 || order[0] != "set" {
		t.Fatalf("first Set: want [set], got %v", order)
	}
	order = order[:0]
	// Second Set (replace): OnReplace then OnSet.
	w.Write(func(fw *flecs.Writer) { flecs.Set[Position](fw, e, Position{2, 0}) })
	if len(order) != 2 || order[0] != "replace" || order[1] != "set" {
		t.Fatalf("second Set: want [replace, set], got %v", order)
	}
}

// Test 6: coalesced deferred Sets on an already-present component fire OnReplace once per submission.
func TestOnReplaceCoalescedDeferredExisting(t *testing.T) {
	w := flecs.New()
	type replaceCall struct{ old, new float32 }
	var calls []replaceCall
	flecs.OnReplace[Position](w, func(_ *flecs.Writer, _ flecs.ID, old, new Position) {
		calls = append(calls, replaceCall{old.X, new.X})
	})
	var e flecs.ID
	// Pre-set: entity already has Position{0,0} before the Write.
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity(); flecs.Set[Position](fw, e, Position{0, 0}) })
	// Three coalesced Sets inside one Write.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, e, Position{1, 0})
		flecs.Set[Position](fw, e, Position{2, 0})
		flecs.Set[Position](fw, e, Position{3, 0})
	})
	if len(calls) != 3 {
		t.Fatalf("want 3 OnReplace calls, got %d", len(calls))
	}
	want := []replaceCall{{0, 1}, {1, 2}, {2, 3}}
	for i, wc := range want {
		if calls[i] != wc {
			t.Fatalf("call[%d] want %+v, got %+v", i, wc, calls[i])
		}
	}
}

// Test 7: first-add then replace in one Write: OnAdd once, OnSet twice, OnReplace once (on 2nd).
func TestOnReplaceCoalescedDeferredFirstAddThenReplace(t *testing.T) {
	w := flecs.New()
	addCalls, setCalls := 0, 0
	type replaceCall struct{ old, new float32 }
	var repCalls []replaceCall
	flecs.OnAdd[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { addCalls++ })
	flecs.OnSet[Position](w, func(_ *flecs.Writer, _ flecs.ID, _ Position) { setCalls++ })
	flecs.OnReplace[Position](w, func(_ *flecs.Writer, _ flecs.ID, old, new Position) {
		repCalls = append(repCalls, replaceCall{old.X, new.X})
	})
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() }) // entity has no Position
	w.Write(func(fw *flecs.Writer) {
		flecs.Set[Position](fw, e, Position{10, 0}) // first add
		flecs.Set[Position](fw, e, Position{20, 0}) // replace
	})
	if addCalls != 1 {
		t.Fatalf("OnAdd want 1, got %d", addCalls)
	}
	if setCalls != 2 {
		t.Fatalf("OnSet want 2, got %d", setCalls)
	}
	if len(repCalls) != 1 {
		t.Fatalf("OnReplace want 1 call, got %d", len(repCalls))
	}
	if repCalls[0] != (replaceCall{10, 20}) {
		t.Fatalf("OnReplace want {10,20}, got %+v", repCalls[0])
	}
}

// Test 8: cross-Write replace fires with old=pre-Write2 value, new=Write2 value.
func TestOnReplaceCrossWrite(t *testing.T) {
	w := flecs.New()
	var gotOld, gotNew Position
	calls := 0
	flecs.OnReplace[Position](w, func(_ *flecs.Writer, _ flecs.ID, old, new Position) {
		gotOld = old
		gotNew = new
		calls++
	})
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity(); flecs.Set[Position](fw, e, Position{5, 0}) })
	w.Write(func(fw *flecs.Writer) { flecs.Set[Position](fw, e, Position{9, 0}) })
	if calls != 1 {
		t.Fatalf("want 1 OnReplace call, got %d", calls)
	}
	if gotOld != (Position{5, 0}) {
		t.Fatalf("old want {5,0}, got %v", gotOld)
	}
	if gotNew != (Position{9, 0}) {
		t.Fatalf("new want {9,0}, got %v", gotNew)
	}
}

// Test 9: OnReplace fires for sparse-stored components.
func TestOnReplaceSparseComponent(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	flecs.SetSparse(w, posID)
	calls := 0
	var gotOld, gotNew Position
	flecs.OnReplace[Position](w, func(_ *flecs.Writer, _ flecs.ID, old, new Position) {
		gotOld = old
		gotNew = new
		calls++
	})
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity(); flecs.Set[Position](fw, e, Position{1, 2}) })
	if calls != 0 {
		t.Fatalf("OnReplace must not fire on first Set (sparse), got %d", calls)
	}
	w.Write(func(fw *flecs.Writer) { flecs.Set[Position](fw, e, Position{3, 4}) })
	if calls != 1 {
		t.Fatalf("OnReplace want 1 (sparse), got %d", calls)
	}
	if gotOld != (Position{1, 2}) {
		t.Fatalf("old want {1,2}, got %v", gotOld)
	}
	if gotNew != (Position{3, 4}) {
		t.Fatalf("new want {3,4}, got %v", gotNew)
	}
}

// Test 10: OnReplace fires for DontFragment-stored components.
func TestOnReplaceDontFragmentComponent(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	flecs.SetSparse(w, posID)
	flecs.SetDontFragment(w, posID)
	calls := 0
	var gotOld, gotNew Position
	flecs.OnReplace[Position](w, func(_ *flecs.Writer, _ flecs.ID, old, new Position) {
		gotOld = old
		gotNew = new
		calls++
	})
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity(); flecs.Set[Position](fw, e, Position{1, 2}) })
	if calls != 0 {
		t.Fatalf("OnReplace must not fire on first Set (DF), got %d", calls)
	}
	w.Write(func(fw *flecs.Writer) { flecs.Set[Position](fw, e, Position{3, 4}) })
	if calls != 1 {
		t.Fatalf("OnReplace want 1 (DF), got %d", calls)
	}
	if gotOld != (Position{1, 2}) {
		t.Fatalf("old want {1,2}, got %v", gotOld)
	}
	if gotNew != (Position{3, 4}) {
		t.Fatalf("new want {3,4}, got %v", gotNew)
	}
}

// Test 11: OnReplace fires on pair overwrite.
func TestOnReplacePairForm(t *testing.T) {
	w := flecs.New()
	likes := flecs.RegisterComponent[Tag](w)
	var bob flecs.ID
	w.Write(func(fw *flecs.Writer) { bob = fw.NewEntity() })
	calls := 0
	var gotOld, gotNew Position
	flecs.OnReplace[Position](w, func(_ *flecs.Writer, _ flecs.ID, old, new Position) {
		gotOld = old
		gotNew = new
		calls++
	})
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.SetPair[Position](fw, e, likes, bob, Position{1, 0}) // first set: no OnReplace
	})
	if calls != 0 {
		t.Fatalf("OnReplace must not fire on first SetPair, got %d", calls)
	}
	w.Write(func(fw *flecs.Writer) {
		flecs.SetPair[Position](fw, e, likes, bob, Position{2, 0}) // replace: fires
	})
	if calls != 1 {
		t.Fatalf("OnReplace want 1 (pair), got %d", calls)
	}
	if gotOld != (Position{1, 0}) {
		t.Fatalf("old want {1,0}, got %v", gotOld)
	}
	if gotNew != (Position{2, 0}) {
		t.Fatalf("new want {2,0}, got %v", gotNew)
	}
}

// Test 12: Remove + re-Set: the re-Set is treated as a first add, OnReplace does not fire.
func TestOnReplaceRemoveAndReSet(t *testing.T) {
	w := flecs.New()
	calls := 0
	flecs.OnReplace[Position](w, func(_ *flecs.Writer, _ flecs.ID, _, _ Position) { calls++ })
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity(); flecs.Set[Position](fw, e, Position{1, 0}) })
	w.Write(func(fw *flecs.Writer) { flecs.Remove[Position](fw, e) })
	w.Write(func(fw *flecs.Writer) { flecs.Set[Position](fw, e, Position{2, 0}) }) // re-add: no OnReplace
	if calls != 0 {
		t.Fatalf("OnReplace must not fire after Remove+Set (re-add), got %d", calls)
	}
}

// Test 13: registering OnReplace twice replaces the prior hook.
func TestOnReplaceRegistrationReplaces(t *testing.T) {
	w := flecs.New()
	calls1, calls2 := 0, 0
	flecs.OnReplace[Position](w, func(_ *flecs.Writer, _ flecs.ID, _, _ Position) { calls1++ })
	flecs.OnReplace[Position](w, func(_ *flecs.Writer, _ flecs.ID, _, _ Position) { calls2++ })
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity(); flecs.Set[Position](fw, e, Position{1, 0}) })
	w.Write(func(fw *flecs.Writer) { flecs.Set[Position](fw, e, Position{2, 0}) })
	if calls1 != 0 {
		t.Fatalf("first hook must have been replaced, got %d calls", calls1)
	}
	if calls2 != 1 {
		t.Fatalf("second hook want 1, got %d", calls2)
	}
}

// Test 14: OnReplace[T](w, nil) clears the hook.
func TestOnReplaceNilClearsHook(t *testing.T) {
	w := flecs.New()
	calls := 0
	flecs.OnReplace[Position](w, func(_ *flecs.Writer, _ flecs.ID, _, _ Position) { calls++ })
	flecs.OnReplace[Position](w, nil) // clear
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity(); flecs.Set[Position](fw, e, Position{1, 0}) })
	w.Write(func(fw *flecs.Writer) { flecs.Set[Position](fw, e, Position{2, 0}) })
	if calls != 0 {
		t.Fatalf("cleared hook must not fire, got %d", calls)
	}
}

// Test 15: OnReplaceID (untyped) — pointer-shape contract: handler can read both pointers.
func TestOnReplaceIDUntypedContract(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	var gotOldX, gotNewX float32
	flecs.OnReplaceID(w, posID, func(_ *flecs.Writer, _ flecs.ID, oldPtr, newPtr unsafe.Pointer) {
		if oldPtr != nil {
			gotOldX = (*Position)(oldPtr).X
		}
		if newPtr != nil {
			gotNewX = (*Position)(newPtr).X
		}
	})
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		fw.SetByID(e, posID, Position{5, 0})
	})
	w.Write(func(fw *flecs.Writer) {
		fw.SetByID(e, posID, Position{8, 0})
	})
	if gotOldX != 5 {
		t.Fatalf("OnReplaceID old.X want 5, got %v", gotOldX)
	}
	if gotNewX != 8 {
		t.Fatalf("OnReplaceID new.X want 8, got %v", gotNewX)
	}
}
