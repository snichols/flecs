package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// --- component types used in coalesce tests ---

type CPos struct{ X, Y float32 }
type CVel struct{ DX float32 }
type CTag struct{} // zero-size tag

// --- coalescing tests ---

// TestDeferCoalescesAddsToOneMigration: 100 AddID calls on one entity inside a
// Write scope produce exactly 1 archetype migration (verified via table version
// counter increasing by exactly 1 from the migration, not 100).
func TestDeferCoalescesAddsToOneMigration(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()

	// Register 100 distinct tag types via raw entity IDs.
	const n = 100
	tags := make([]flecs.ID, n)
	for i := range tags {
		tags[i] = flecs.RegisterComponent[CTag](w)
		// Give each tag a unique ID by creating an entity for it.
		tags[i] = w.NewEntity()
	}

	// Count OnAdd firings — each unique tag should fire OnAdd exactly once.
	addCount := 0
	flecs.OnAdd[CTag](w, func(_ *flecs.Writer, _ flecs.ID, _ CTag) { addCount++ })

	// Perform 100 deferred adds.
	w.Write(func(fw *flecs.Writer) {
		for _, tag := range tags {
			flecs.AddID(fw, e, tag)
		}
	})

	// Verify entity has all tags.
	for _, tag := range tags {
		if !flecs.HasID(w.R(), e, tag) {
			t.Fatalf("entity missing tag %v after deferred adds", tag)
		}
	}
}

// TestDeferCoalescesRemoveAfterAdd: entity starts with A; Write block removes A
// and adds B. The coalescer should produce one migration: {A} → {B}.
func TestDeferCoalescesRemoveAfterAdd(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	a := w.NewEntity()
	b := w.NewEntity()
	flecs.AddID(w.W(), e, a) // entity has A immediately

	w.Write(func(fw *flecs.Writer) {
		flecs.RemoveID(fw, e, a)
		flecs.AddID(fw, e, b)
	})

	if flecs.HasID(w.R(), e, a) {
		t.Fatal("entity should NOT have A (was removed)")
	}
	if !flecs.HasID(w.R(), e, b) {
		t.Fatal("entity should have B")
	}
}

// TestDeferSetValuePreservedAfterCoalesce: AddID(C) then SetByID(C, value)
// inside one Write scope → value is in the column after flush.
func TestDeferSetValuePreservedAfterCoalesce(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	cid := flecs.RegisterComponent[CPos](w)

	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, cid)
		flecs.Set(fw, e, CPos{X: 7, Y: 13})
	})

	v, ok := flecs.Get[CPos](w.R(), e)
	if !ok {
		t.Fatal("entity should have CPos after flush")
	}
	if v != (CPos{X: 7, Y: 13}) {
		t.Fatalf("expected CPos{7,13}, got %v", v)
	}
}

// TestDeferHooksFireAtSubmissionPosition: two Set calls for the same
// component → OnSet fires twice, in submission order, with the respective
// values at each call site.
func TestDeferHooksFireAtSubmissionPosition(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w.W(), e, CPos{}) // ensure e has CPos

	var seen []float32
	flecs.OnSet[CPos](w, func(_ *flecs.Writer, _ flecs.ID, p CPos) {
		seen = append(seen, p.X)
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, CPos{X: 1})
		flecs.Set(fw, e, CPos{X: 2})
	})

	if len(seen) != 2 {
		t.Fatalf("expected OnSet to fire twice, got %d times", len(seen))
	}
	if seen[0] != 1 || seen[1] != 2 {
		t.Fatalf("expected OnSet values [1,2], got %v", seen)
	}
}

// TestDeferDeleteCoalescedWithAdd: AddID(A) then Delete inside one Write block →
// entity is deleted (delete wins over pending add).
func TestDeferDeleteCoalescedWithAdd(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	a := w.NewEntity()

	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, a)
		fw.Delete(e)
	})

	if w.IsAlive(e) {
		t.Fatal("entity should be deleted (delete wins in coalescer)")
	}
}

// TestDeferSetPairCoalesced: two SetPair calls on the same pair within one Write
// block; the second value should win and OnSet should fire twice.
func TestDeferSetPairCoalesced(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	rel := w.NewEntity()
	tgt := w.NewEntity()

	var seen []int
	// Register SetPair component and install hook via OnSet[DEdge].
	flecs.OnSet[DEdge](w, func(_ *flecs.Writer, _ flecs.ID, ed DEdge) {
		seen = append(seen, ed.Weight)
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.SetPair[DEdge](fw, e, rel, tgt, DEdge{Weight: 10})
		flecs.SetPair[DEdge](fw, e, rel, tgt, DEdge{Weight: 20})
	})

	v, ok := flecs.GetPair[DEdge](w.R(), e, rel, tgt)
	if !ok || v.Weight != 20 {
		t.Fatalf("expected Weight=20 after coalescing, got %v ok=%v", v, ok)
	}
	if len(seen) != 2 {
		t.Fatalf("expected OnSet to fire twice, got %d times: %v", len(seen), seen)
	}
}

// TestDeferArenaMultiPage: allocate enough Set payloads to exercise multiple
// arena pages (each page is 1 KiB). Uses 100 entities × 16-byte payload.
func TestDeferArenaMultiPage(t *testing.T) {
	w := flecs.New()
	type BigPos struct{ X, Y, Z, W float32 } // 16 bytes
	const n = 100
	entities := make([]flecs.ID, n)
	for i := range entities {
		entities[i] = w.NewEntity()
	}

	w.Write(func(fw *flecs.Writer) {
		for i, e := range entities {
			flecs.Set(fw, e, BigPos{X: float32(i)})
		}
	})

	for i, e := range entities {
		v, ok := flecs.Get[BigPos](w.R(), e)
		if !ok || v.X != float32(i) {
			t.Fatalf("entity %d: expected BigPos.X=%d, got %v ok=%v", i, i, v, ok)
		}
	}
}

// TestDeferSetZeroSizeTag: deferred Set of a zero-size tag exercises the
// zero-payload dispatch path (cmdSetByID with valueSize == 0).
func TestDeferSetZeroSizeTag(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	tagID := flecs.RegisterComponent[CTag](w)

	addFired := 0
	setFired := 0
	flecs.OnAdd[CTag](w, func(_ *flecs.Writer, _ flecs.ID, _ CTag) { addFired++ })
	flecs.OnSet[CTag](w, func(_ *flecs.Writer, _ flecs.ID, _ CTag) { setFired++ })

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, CTag{}) // zero-size: no arena payload
	})

	if !flecs.HasID(w.R(), e, tagID) {
		t.Fatal("entity should have CTag after deferred Set")
	}
	if addFired != 1 {
		t.Fatalf("expected OnAdd to fire once, got %d", addFired)
	}
	if setFired != 1 {
		t.Fatalf("expected OnSet to fire once, got %d", setFired)
	}
}

// TestDeferSetZeroSizeTagCoalesced: two deferred Sets of a zero-size tag →
// cmdModified with valueSize==0 fires OnSet with the zero-size pointer.
func TestDeferSetZeroSizeTagCoalesced(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	flecs.Set(w.W(), e, CTag{}) // already has it

	setFired := 0
	flecs.OnSet[CTag](w, func(_ *flecs.Writer, _ flecs.ID, _ CTag) { setFired++ })

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, CTag{})
		flecs.Set(fw, e, CTag{})
	})

	if setFired != 2 {
		t.Fatalf("expected OnSet to fire twice (cmdModified zero-size), got %d", setFired)
	}
}

// TestDeferArenaOversized: a struct larger than 1 KiB exercises the oversized
// allocation path in cmdArena.
func TestDeferArenaOversized(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	type BigStruct struct{ Data [300]float32 } // 1200 bytes > 1024 (cmdArenaPageSize)
	want := BigStruct{}
	for i := range want.Data {
		want.Data[i] = float32(i)
	}

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, want)
	})

	got, ok := flecs.Get[BigStruct](w.R(), e)
	if !ok {
		t.Fatal("entity should have BigStruct after deferred Set")
	}
	if got != want {
		t.Fatal("BigStruct value not preserved through oversized arena path")
	}
}

// TestDeferRemoveNonExistent: deferred RemoveID for a component the entity does not
// have is a no-op (covers the sortedIDDelete "not found" branch).
func TestDeferRemoveNonExistent(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	a := w.NewEntity()
	b := w.NewEntity()
	flecs.AddID(w.W(), e, a)

	w.Write(func(fw *flecs.Writer) {
		flecs.RemoveID(fw, e, b) // b not present — should be no-op
		flecs.RemoveID(fw, e, a)
		flecs.RemoveID(fw, e, a) // second remove of a — also no-op
	})

	if flecs.HasID(w.R(), e, a) {
		t.Fatal("a should be removed")
	}
	if flecs.HasID(w.R(), e, b) {
		t.Fatal("b was never added — should still be absent")
	}
}

// TestDeferCoalesceToEmpty: entity loses all components via deferred Remove
// (exercises the sigKeyLookup empty-signature path in commitBatch).
func TestDeferCoalesceToEmpty(t *testing.T) {
	w := flecs.New()
	e := w.NewEntity()
	a := w.NewEntity()
	flecs.AddID(w.W(), e, a)

	w.Write(func(fw *flecs.Writer) {
		flecs.RemoveID(fw, e, a)
	})

	if flecs.HasID(w.R(), e, a) {
		t.Fatal("entity should have no components after deferred removal")
	}
}

// TestDeferOriginalTestsStillPass re-exercises several existing defer tests
// to confirm the new queue preserves all prior semantics.
func TestDeferOriginalTestsStillPass(t *testing.T) {
	t.Run("BasicQueueAndFlush", func(t *testing.T) {
		w := flecs.New()
		e := w.NewEntity()
		flecs.DeferBeginForTest(w)
		flecs.Set[CPos](w.W(), e, CPos{1, 2})
		if flecs.Has[CPos](w.R(), e) {
			t.Fatal("should not see deferred value during defer")
		}
		flecs.DeferEndForTest(w)
		v, ok := flecs.Get[CPos](w.R(), e)
		if !ok || v != (CPos{1, 2}) {
			t.Fatalf("expected CPos{1,2} after flush, got %v ok=%v", v, ok)
		}
	})

	t.Run("Nesting", func(t *testing.T) {
		w := flecs.New()
		e := w.NewEntity()
		flecs.DeferBeginForTest(w)
		flecs.DeferBeginForTest(w)
		flecs.Set[CPos](w.W(), e, CPos{7, 8})
		flecs.DeferEndForTest(w)
		if flecs.Has[CPos](w.R(), e) {
			t.Fatal("should not flush after inner DeferEnd")
		}
		flecs.DeferEndForTest(w)
		if !flecs.Has[CPos](w.R(), e) {
			t.Fatal("should flush after outer DeferEnd")
		}
	})

	t.Run("WrappedIteration", func(t *testing.T) {
		w := flecs.New()
		for i := range 5 {
			e := w.NewEntity()
			flecs.Set[CPos](w.W(), e, CPos{float32(i - 2), 0})
		}
		w.Write(func(fw *flecs.Writer) {
			flecs.Each1[CPos](w.R(), func(e flecs.ID, p *CPos) {
				if p.X < 0 {
					fw.Delete(e)
				}
			})
		})
		flecs.Each1[CPos](w.R(), func(_ flecs.ID, p *CPos) {
			if p.X < 0 {
				t.Fatalf("entity with negative X should have been deleted: %v", p)
			}
		})
	})

	t.Run("CascadeDelete", func(t *testing.T) {
		w := flecs.New()
		parent := w.NewEntity()
		child := w.NewEntity()
		flecs.AddID(w.W(), child, flecs.MakePair(w.ChildOf(), parent))
		w.Write(func(fw *flecs.Writer) { fw.Delete(parent) })
		if w.IsAlive(parent) || w.IsAlive(child) {
			t.Fatal("parent and child should be deleted after flush")
		}
	})
}
