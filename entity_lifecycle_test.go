package flecs_test

import (
	"encoding/json"
	"strings"
	"testing"
	"unsafe"

	"github.com/snichols/flecs"
)

// Component types used across entity_lifecycle tests.
type ELA struct{ V int }
type ELB struct{ V int }
type ELC struct{ V int }
type ELSparse struct{ V int } // used as a Sparse-stored component
type ELTag struct{}           // zero-size tag

// ─── Clear tests ─────────────────────────────────────────────────────────────

// Test 1: Clear removes all components; entity stays alive.
func TestClearBasic(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[ELA](fw, e, ELA{1})
		flecs.Set[ELB](fw, e, ELB{2})
		flecs.Set[ELC](fw, e, ELC{3})
	})

	w.Write(func(fw *flecs.Writer) {
		if !flecs.Clear(fw, e) {
			t.Fatal("Clear returned false for alive entity")
		}
	})

	w.Read(func(r *flecs.Reader) {
		if !r.IsAlive(e) {
			t.Fatal("entity must still be alive after Clear")
		}
		if flecs.Has[ELA](r, e) || flecs.Has[ELB](r, e) || flecs.Has[ELC](r, e) {
			t.Fatal("entity must have no components after Clear")
		}
		if got := r.EntityComponents(e); len(got) != 0 {
			t.Fatalf("EntityComponents after Clear: want [], got %v", got)
		}
	})
}

// Test 2: Clear fires OnRemove for each component; OnDelete does NOT fire.
func TestClearHookFiring(t *testing.T) {
	w := flecs.New()
	removedA, removedB, removedC := 0, 0, 0

	flecs.OnRemove[ELA](w, func(_ *flecs.Writer, _ flecs.ID, _ ELA) { removedA++ })
	flecs.OnRemove[ELB](w, func(_ *flecs.Writer, _ flecs.ID, _ ELB) { removedB++ })
	flecs.OnRemove[ELC](w, func(_ *flecs.Writer, _ flecs.ID, _ ELC) { removedC++ })

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[ELA](fw, e, ELA{})
		flecs.Set[ELB](fw, e, ELB{})
		flecs.Set[ELC](fw, e, ELC{})
	})

	// Verify OnAdd does NOT fire on Clear (only OnRemove should).
	addCount := 0
	flecs.Observe[ELA](w, flecs.EventOnAdd, func(_ *flecs.Writer, _ flecs.ID, _ ELA) { addCount++ })

	w.Write(func(fw *flecs.Writer) { flecs.Clear(fw, e) })

	if removedA != 1 || removedB != 1 || removedC != 1 {
		t.Fatalf("OnRemove counts: A=%d B=%d C=%d, want 1 each", removedA, removedB, removedC)
	}
	// OnAdd must not fire; entity must still be alive (not deleted).
	if addCount != 0 {
		t.Fatalf("OnAdd fired %d times during Clear; expected 0", addCount)
	}
	w.Read(func(r *flecs.Reader) {
		if !r.IsAlive(e) {
			t.Fatal("entity must be alive after Clear (not deleted)")
		}
	})
}

// Test 3: Entity ID is unchanged after Clear.
func TestClearPreservesEntityID(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[ELA](fw, e, ELA{42})
	})
	original := e

	w.Write(func(fw *flecs.Writer) { flecs.Clear(fw, e) })

	if e != original {
		t.Fatalf("entity ID changed: was %v, now %v", original, e)
	}
	w.Read(func(r *flecs.Reader) {
		if !r.IsAlive(e) {
			t.Fatal("entity not alive after Clear")
		}
	})
}

// Test 4: Clear on an entity that already has no components is a no-op.
func TestClearEmptyEntity(t *testing.T) {
	w := flecs.New()
	removals := 0
	flecs.OnRemove[ELA](w, func(_ *flecs.Writer, _ flecs.ID, _ ELA) { removals++ })

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() }) // in empty table

	w.Write(func(fw *flecs.Writer) {
		if !flecs.Clear(fw, e) {
			t.Fatal("Clear returned false for alive empty entity")
		}
	})

	if removals != 0 {
		t.Fatalf("OnRemove fired %d times on empty entity; expected 0", removals)
	}
	w.Read(func(r *flecs.Reader) {
		if !r.IsAlive(e) {
			t.Fatal("entity not alive after clearing empty entity")
		}
	})
}

// Test 5: Clear cleans up both archetype and Sparse components.
func TestClearSparseAndArchetypeMix(t *testing.T) {
	w := flecs.New()

	// Register ELSparse as a Sparse component.
	cid := flecs.RegisterComponent[ELSparse](w)
	flecs.SetSparse(w, cid)

	removedRegular, removedSparse := 0, 0
	flecs.OnRemove[ELA](w, func(_ *flecs.Writer, _ flecs.ID, _ ELA) { removedRegular++ })
	flecs.OnRemove[ELSparse](w, func(_ *flecs.Writer, _ flecs.ID, _ ELSparse) { removedSparse++ })

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[ELA](fw, e, ELA{1})
		flecs.Set[ELSparse](fw, e, ELSparse{99})
	})

	w.Write(func(fw *flecs.Writer) { flecs.Clear(fw, e) })

	if removedRegular != 1 {
		t.Fatalf("OnRemove[ELA] count: want 1, got %d", removedRegular)
	}
	if removedSparse != 1 {
		t.Fatalf("OnRemove[ELSparse] count: want 1, got %d", removedSparse)
	}

	// Sparse-set must have no entry for e.
	w.Read(func(r *flecs.Reader) {
		if _, ok := flecs.Get[ELSparse](r, e); ok {
			t.Fatal("sparse component still present after Clear")
		}
		if !r.IsAlive(e) {
			t.Fatal("entity not alive after Clear")
		}
	})
}

// Test 6: Clear removes a union pair from the union store.
func TestClearUnionPair(t *testing.T) {
	w := flecs.New()

	var rel, tgt flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		flecs.SetUnion(w, rel)
	})

	// Observe the full pair ID — unionStoreRemoveEntity dispatches on MakePair(rel,tgt).
	removals := 0
	flecs.ObserveID(w, flecs.MakePair(rel, tgt), flecs.EventOnRemove, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) { removals++ })

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		fw.AddID(e, flecs.MakePair(rel, tgt))
	})

	w.Read(func(r *flecs.Reader) {
		if !r.HasID(e, flecs.MakePair(rel, tgt)) {
			t.Fatal("union pair not present before Clear")
		}
	})

	w.Write(func(fw *flecs.Writer) { flecs.Clear(fw, e) })

	w.Read(func(r *flecs.Reader) {
		if r.HasID(e, flecs.MakePair(rel, tgt)) {
			t.Fatal("union pair still present after Clear")
		}
		if !r.IsAlive(e) {
			t.Fatal("entity not alive after Clear")
		}
	})
	if removals != 1 {
		t.Fatalf("union removal observer fired %d times; expected 1", removals)
	}
}

// Test 7: Clear removes entity from an ordered parent's child list.
func TestClearOrderedChildren(t *testing.T) {
	w := flecs.New()

	var parent, child flecs.ID
	w.Write(func(fw *flecs.Writer) {
		parent = fw.NewEntity()
		flecs.SetOrderedChildren(w, parent)
		child = fw.NewEntity()
		// Make child a child of parent via ChildOf pair.
		fw.AddID(child, flecs.MakePair(w.ChildOf(), parent))
	})

	// Verify child is in parent's ordered list.
	children := elChildren(w, parent)
	if len(children) != 1 || children[0] != child {
		t.Fatalf("before Clear: expected child in parent's list, got %v", children)
	}

	// Clear the child — its ChildOf pair is removed.
	w.Write(func(fw *flecs.Writer) { flecs.Clear(fw, child) })

	// After Clear, child must not appear in parent's ordered list.
	children = collectChildren(w, parent)
	for _, c := range children {
		if c == child {
			t.Fatal("child still in parent's ordered list after Clear")
		}
	}
	// child is still alive.
	w.Read(func(r *flecs.Reader) {
		if !r.IsAlive(child) {
			t.Fatal("child not alive after Clear")
		}
	})
}

// Test 8: Deferred Clear supersedes prior AddID; subsequent AddID survives.
func TestClearDeferredCoalescer(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	cid1 := flecs.RegisterComponent[ELA](w)
	cid2 := flecs.RegisterComponent[ELB](w)

	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// Deferred: AddID(c1), Clear, AddID(c2) → only c2 must survive.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, cid1)
		flecs.Clear(fw, e)
		fw.AddID(e, cid2)
	})

	w.Read(func(r *flecs.Reader) {
		if flecs.OwnsID(r, e, cid1) {
			t.Fatal("c1 must be gone after coalesced Clear")
		}
		if !flecs.OwnsID(r, e, cid2) {
			t.Fatal("c2 must survive post-Clear AddID")
		}
	})
}

// ─── MakeAlive tests ──────────────────────────────────────────────────────────

// Test 9: MakeAlive on a never-allocated raw index at generation 0.
// MakeAlive panics in a deferred scope, so we call it via WriterForTest with
// deferDepth == 0 (outside any w.Write call).
func TestMakeAliveUnusedAtGen0(t *testing.T) {
	w := flecs.New()
	target := flecs.MakeEntity(999, 0)

	fw := flecs.WriterForTest(w)
	got := flecs.MakeAlive(fw, target)

	if got != target {
		t.Fatalf("MakeAlive returned %v; want %v", got, target)
	}
	w.Read(func(r *flecs.Reader) {
		if !r.IsAlive(got) {
			t.Fatal("entity not alive after MakeAlive")
		}
	})
}

// Test 10: MakeAlive on a free slot at non-zero generation bumps the registry gen.
func TestMakeAliveUnusedAtNonZeroGen(t *testing.T) {
	w := flecs.New()
	target := flecs.MakeEntity(998, 5)

	fw := flecs.WriterForTest(w)
	got := flecs.MakeAlive(fw, target)

	if got.Index() != 998 {
		t.Fatalf("raw index: want 998, got %d", got.Index())
	}
	if got.Generation() != 5 {
		t.Fatalf("generation: want 5, got %d", got.Generation())
	}
	w.Read(func(r *flecs.Reader) {
		if !r.IsAlive(got) {
			t.Fatal("entity not alive after MakeAlive with gen=5")
		}
	})
}

// Test 11: MakeAlive on an alive entity at the same generation is a no-op.
func TestMakeAliveAlreadyAlive(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	fw := flecs.WriterForTest(w)
	got := flecs.MakeAlive(fw, e)

	if got != e {
		t.Fatalf("MakeAlive changed ID: was %v, got %v", e, got)
	}
	w.Read(func(r *flecs.Reader) {
		if !r.IsAlive(e) {
			t.Fatal("entity not alive after no-op MakeAlive")
		}
	})
}

// Test 12: MakeAlive on a live entity at a different generation panics.
func TestMakeAliveConflictPanic(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	wrongID := flecs.MakeEntity(e.Index(), e.Generation()+1)
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on generation conflict, got none")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value type %T, want string", r)
		}
		if !strings.Contains(msg, "generation") {
			t.Fatalf("panic message %q lacks 'generation'", msg)
		}
	}()
	fw := flecs.WriterForTest(w)
	flecs.MakeAlive(fw, wrongID)
}

// Test 13: MakeAlive removes the raw index from the recycle queue.
func TestMakeAliveRemovesFromRecycleQueue(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	// Allocate then delete so the raw index enters the recycle queue.
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	rawIdx := e.Index()
	w.Delete(e)

	// Claim the freed slot at a higher generation.
	claimed := flecs.MakeEntity(rawIdx, 7)
	fw := flecs.WriterForTest(w)
	flecs.MakeAlive(fw, claimed)

	// NewEntity must NOT re-issue the claimed slot.
	var next flecs.ID
	w.Write(func(fw2 *flecs.Writer) { next = fw2.NewEntity() })
	if next.Index() == rawIdx {
		t.Fatalf("NewEntity reissued the MakeAlive-claimed index %d", rawIdx)
	}
}

// Test 14: MakeAlive panics when called inside a deferred scope.
func TestMakeAliveDeferredPanic(t *testing.T) {
	w := flecs.New()
	target := flecs.MakeEntity(500, 0)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic in deferred scope, got none")
		}
	}()

	flecs.DeferBeginForTest(w)
	defer flecs.DeferEndForTest(w)
	fw := flecs.WriterForTest(w)
	flecs.MakeAlive(fw, target)
}

// ─── SetVersion tests ─────────────────────────────────────────────────────────

// Test 15: SetVersion to a higher generation invalidates old ID, validates new one.
// SetVersion panics in a deferred scope, so we call it via WriterForTest with
// deferDepth == 0 (outside any w.Write call).
func TestSetVersionHigher(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	oldID := e
	newID := flecs.MakeEntity(e.Index(), e.Generation()+10)

	fw := flecs.WriterForTest(w)
	flecs.SetVersion(fw, newID)

	w.Read(func(r *flecs.Reader) {
		if r.IsAlive(oldID) {
			t.Fatal("old ID must not be alive after SetVersion")
		}
		if !r.IsAlive(newID) {
			t.Fatal("new versioned ID must be alive after SetVersion")
		}
	})
}

// Test 16: SetVersion to a lower generation panics.
func TestSetVersionLowerPanic(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// First raise the generation, then try to lower it.
	higherID := flecs.MakeEntity(e.Index(), e.Generation()+5)
	fw := flecs.WriterForTest(w)
	flecs.SetVersion(fw, higherID)

	lowerID := flecs.MakeEntity(e.Index(), 0)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on lower generation, got none")
		}
	}()
	flecs.SetVersion(fw, lowerID)
}

// Test 17: SetVersion on a dead entity panics.
func TestSetVersionDeadEntityPanic(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	w.Delete(e)

	// e is now dead; SetVersion should panic.
	newID := flecs.MakeEntity(e.Index(), e.Generation()+1)
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for dead entity, got none")
		}
	}()
	fw := flecs.WriterForTest(w)
	flecs.SetVersion(fw, newID)
}

// Test 18: SetVersion panics inside a deferred scope.
func TestSetVersionDeferredPanic(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	newID := flecs.MakeEntity(e.Index(), e.Generation()+1)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic in deferred scope, got none")
		}
	}()

	flecs.DeferBeginForTest(w)
	defer flecs.DeferEndForTest(w)
	fw := flecs.WriterForTest(w)
	flecs.SetVersion(fw, newID)
}

// Test 19: Marshal round-trip does NOT preserve custom generation.
// Versions are not preserved; the marshal format uses serial numbers, not raw
// indices or generations. This test asserts the deliberate non-goal to prevent
// future regressions where someone tries to "fix" it without understanding the
// implication.
func TestSetVersionMarshalRoundTrip(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[ELA](fw, e, ELA{77})
	})
	originalGen := e.Generation()

	// Bump generation.
	newID := flecs.MakeEntity(e.Index(), originalGen+99)
	fw := flecs.WriterForTest(w)
	flecs.SetVersion(fw, newID)
	e = newID // update our handle

	// Marshal and unmarshal into a fresh world.
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	w2 := flecs.New()
	flecs.RegisterComponent[ELA](w2)
	if err := json.Unmarshal(data, w2); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}

	// The restored world should have the entity with ELA=77 but at generation 0
	// (the first entity gets generation 0 on restoration).
	found := false
	w2.Read(func(r *flecs.Reader) {
		r.EachEntity(func(id flecs.ID) bool {
			if v, ok := flecs.Get[ELA](r, id); ok && v.V == 77 {
				found = true
				// Generation is NOT preserved; it is reset to 0 in the restored world.
				if id.Generation() == originalGen+99 {
					t.Errorf("generation was unexpectedly preserved after marshal round-trip")
				}
			}
			return true
		})
	})
	if !found {
		t.Fatal("entity with ELA{77} not found after marshal round-trip")
	}
}

// ─── coverage gap fillers ─────────────────────────────────────────────────────

// TestClearImmediate covers the deferDepth==0 path in Clear (WriterForTest
// with no DeferBeginForTest means deferDepth is 0 → clearImmediate is called
// directly through Clear, not queued).
func TestClearImmediate(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[ELA](fw, e, ELA{5})
	})

	fw := flecs.WriterForTest(w)
	if !flecs.Clear(fw, e) {
		t.Fatal("Clear returned false for alive entity (immediate path)")
	}

	w.Read(func(r *flecs.Reader) {
		if !r.IsAlive(e) {
			t.Fatal("entity not alive after immediate Clear")
		}
		if flecs.Has[ELA](r, e) {
			t.Fatal("ELA still present after immediate Clear")
		}
	})
}

// TestClearDeferredDeadEntity covers the !IsAlive early return in the deferred
// Clear path (deferDepth > 0 but entity is already dead).
func TestClearDeferredDeadEntity(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	w.Delete(e) // e is now dead

	w.Write(func(fw *flecs.Writer) {
		if flecs.Clear(fw, e) {
			t.Fatal("Clear must return false for dead entity in deferred scope")
		}
	})
}

// TestClearSwapFixup covers the movedRec.Row update path in clearImmediate
// when a second entity is swapped into the cleared entity's row.
func TestClearSwapFixup(t *testing.T) {
	w := flecs.New()
	var e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
		flecs.Set[ELA](fw, e1, ELA{10})
		flecs.Set[ELA](fw, e2, ELA{20})
	})

	// Clearing e1 swap-removes it; e2 fills row 0. This covers the movedRec path.
	w.Write(func(fw *flecs.Writer) { flecs.Clear(fw, e1) })

	w.Read(func(r *flecs.Reader) {
		if !r.IsAlive(e1) || !r.IsAlive(e2) {
			t.Fatal("both entities must survive")
		}
		if flecs.Has[ELA](r, e1) {
			t.Fatal("e1 still has ELA after Clear")
		}
		v, ok := flecs.Get[ELA](r, e2)
		if !ok || v.V != 20 {
			t.Fatalf("e2 ELA wrong after swap: %v %v", v, ok)
		}
	})
}

// TestClearImmediateDeadEntity covers the rec==nil path in clearImmediate:
// calling Clear in immediate mode (deferDepth==0) on a dead entity returns false.
func TestClearImmediateDeadEntity(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	w.Delete(e) // e is now dead

	fw := flecs.WriterForTest(w)
	if flecs.Clear(fw, e) {
		t.Fatal("Clear must return false for dead entity in immediate mode")
	}
}

// TestClearSingletonHolder covers the singletonInstances cleanup path in
// clearImmediate. If entity e holds a singleton component, clearing e must
// remove the singleton instance record.
func TestClearSingletonHolder(t *testing.T) {
	w := flecs.New()
	cid := flecs.RegisterComponent[ELA](w)
	flecs.SetSingleton(w, cid)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[ELA](fw, e, ELA{7})
	})

	w.Write(func(fw *flecs.Writer) { flecs.Clear(fw, e) })

	// After Clear, no singleton holder should be recorded.
	w.Read(func(r *flecs.Reader) {
		if _, ok := flecs.SingletonEntity(r, cid); ok {
			t.Fatal("singleton instance record not cleaned up after Clear")
		}
		if !r.IsAlive(e) {
			t.Fatal("entity not alive after Clear")
		}
	})
}

// TestClearWriteOnceHolder covers the writeOnceHasBeenSet cleanup path in
// clearImmediate. Clearing an entity that holds a WriteOnce component removes
// its write-once tracking slot, allowing a future Set on the same entity.
func TestClearWriteOnceHolder(t *testing.T) {
	w := flecs.New()
	cid := flecs.RegisterComponent[ELB](w)
	flecs.SetWriteOnce(w, cid)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set[ELB](fw, e, ELB{3})
	})

	w.Write(func(fw *flecs.Writer) { flecs.Clear(fw, e) })

	// After Clear the writeOnce tracking must be gone: re-setting on e must succeed.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set[ELB](fw, e, ELB{9}) // would panic if tracking slot not cleared
	})

	w.Read(func(r *flecs.Reader) {
		v, ok := flecs.Get[ELB](r, e)
		if !ok || v.V != 9 {
			t.Fatalf("ELB after re-set: want {9}, got %v ok=%v", v, ok)
		}
	})
}

// TestSetVersionNoOp covers the newGen == currentGen early return in SetVersion.
func TestSetVersionNoOp(t *testing.T) {
	w := flecs.New()
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	// SetVersion with the same generation should be a silent no-op.
	sameID := flecs.MakeEntity(e.Index(), e.Generation())
	fw := flecs.WriterForTest(w)
	flecs.SetVersion(fw, sameID) // must not panic

	w.Read(func(r *flecs.Reader) {
		if !r.IsAlive(e) {
			t.Fatal("entity not alive after no-op SetVersion")
		}
	})
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func elChildren(w *flecs.World, parent flecs.ID) []flecs.ID {
	var result []flecs.ID
	w.Read(func(r *flecs.Reader) {
		r.EachChild(parent, func(child flecs.ID) bool {
			result = append(result, child)
			return true
		})
	})
	return result
}
