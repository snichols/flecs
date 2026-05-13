package flecs_test

import (
	"strings"
	"testing"

	"github.com/snichols/flecs"
)

// Test 1: Tag-form add is unaffected after SetPairIsTag.
func TestPairIsTag_TagFormAddUnaffected(t *testing.T) {
	w := flecs.New()
	var rel, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetPairIsTag(w, rel)

	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, flecs.MakePair(rel, tgt))
	})

	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(e, flecs.MakePair(rel, tgt)) {
			t.Error("entity should have tag-form pair after SetPairIsTag")
		}
	})
	// No per-pair TypeInfo with non-zero size should exist.
	if info, ok := w.ComponentInfo(flecs.MakePair(rel, tgt)); ok && info.Size > 0 {
		t.Error("pair should not have non-zero-size TypeInfo after tag-only add")
	}
}

// Test 2: Value-bearing SetPair panics after SetPairIsTag.
func TestPairIsTag_SetPairPanics(t *testing.T) {
	w := flecs.New()
	var rel, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetPairIsTag(w, rel)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on SetPair after SetPairIsTag")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "PairIsTag") {
			t.Errorf("panic message should mention PairIsTag, got: %v", r)
		}
	}()

	w.Write(func(fw *flecs.Writer) {
		flecs.SetPair[Position](fw, e, rel, tgt, Position{X: 1})
	})
}

// Test 3: Value-bearing SetPairByID panics after SetPairIsTag.
func TestPairIsTag_SetPairByIDPanics(t *testing.T) {
	w := flecs.New()
	var rel, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetPairIsTag(w, rel)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on SetPairByID after SetPairIsTag")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "PairIsTag") {
			t.Errorf("panic message should mention PairIsTag, got: %v", r)
		}
	}()

	w.Write(func(fw *flecs.Writer) {
		fw.SetPairByID(e, rel, tgt, Position{X: 1})
	})
}

// Test 4: Pre-existing pair data blocks SetPairIsTag.
func TestPairIsTag_SetAfterDataPanics(t *testing.T) {
	w := flecs.New()
	var rel, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
	})

	// Establish data pair first.
	w.Write(func(fw *flecs.Writer) {
		flecs.SetPair[Position](fw, e, rel, tgt, Position{X: 42})
	})

	// Verify original value is still readable.
	w.Read(func(fr *flecs.Reader) {
		v, ok := flecs.GetPair[Position](fr, e, rel, tgt)
		if !ok || v.X != 42 {
			t.Errorf("expected pair value X=42, got ok=%v v=%+v", ok, v)
		}
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on SetPairIsTag after data pair registered")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "already has data registered") {
			t.Errorf("panic message should say 'already has data registered', got: %v", r)
		}
	}()

	flecs.SetPairIsTag(w, rel)
}

// Test 5: Bare-tag dispatch via AddID sets PairIsTag.
func TestPairIsTag_BareTagDispatch(t *testing.T) {
	w := flecs.New()
	var rel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, rel, w.PairIsTag())
	})

	w.Read(func(fr *flecs.Reader) {
		if !flecs.IsPairIsTag(fr, rel) {
			t.Error("expected IsPairIsTag=true after bare AddID")
		}
	})
}

// Test 6: SetPairIsTag is idempotent; IsPairIsTag round-trips true.
func TestPairIsTag_Idempotent(t *testing.T) {
	w := flecs.New()
	var rel flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
	})

	// Two consecutive calls must not panic.
	flecs.SetPairIsTag(w, rel)
	flecs.SetPairIsTag(w, rel)

	w.Read(func(fr *flecs.Reader) {
		if !flecs.IsPairIsTag(fr, rel) {
			t.Error("expected IsPairIsTag=true after double SetPairIsTag")
		}
	})
}

// Test 7: Bootstrap — IsA and ChildOf are PairIsTag from world creation.
func TestPairIsTag_BootstrappedBuiltins(t *testing.T) {
	w := flecs.New()
	w.Read(func(fr *flecs.Reader) {
		if !flecs.IsPairIsTag(fr, w.IsA()) {
			t.Error("expected IsA to be bootstrapped PairIsTag")
		}
		if !flecs.IsPairIsTag(fr, w.ChildOf()) {
			t.Error("expected ChildOf to be bootstrapped PairIsTag")
		}
	})
}

// Test 8: Composition with Exclusive — T1 replaced by T2, no data column.
func TestPairIsTag_ComposesWithExclusive(t *testing.T) {
	w := flecs.New()
	var rel, t1, t2, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		t1 = fw.NewEntity()
		t2 = fw.NewEntity()
		e = fw.NewEntity()
	})

	flecs.SetExclusive(w, rel)
	flecs.SetPairIsTag(w, rel)

	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, flecs.MakePair(rel, t1))
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, flecs.MakePair(rel, t2))
	})

	w.Read(func(fr *flecs.Reader) {
		if fr.HasID(e, flecs.MakePair(rel, t1)) {
			t.Error("Exclusive: (rel,t1) should be replaced by (rel,t2)")
		}
		if !fr.HasID(e, flecs.MakePair(rel, t2)) {
			t.Error("Exclusive: (rel,t2) should be present after replacement")
		}
	})

	// No data column allocated.
	if info, ok := w.ComponentInfo(flecs.MakePair(rel, t2)); ok && info.Size > 0 {
		t.Error("pair should not have non-zero-size TypeInfo with PairIsTag")
	}
}

// Test 9: Deferred SetPair panics at enqueue time (inside the Write block).
func TestPairIsTag_DeferredSetPairPanicsAtEnqueue(t *testing.T) {
	w := flecs.New()
	var rel, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetPairIsTag(w, rel)

	panicCaught := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				msg, _ := r.(string)
				if strings.Contains(msg, "PairIsTag") {
					panicCaught = true
				}
			}
		}()
		w.Write(func(fw *flecs.Writer) {
			// deferDepth > 0 here — checkPairIsTag fires at enqueue.
			flecs.SetPair[Position](fw, e, rel, tgt, Position{X: 1})
		})
	}()

	if !panicCaught {
		t.Fatal("expected panic with PairIsTag message on deferred SetPair enqueue")
	}
}

// Test 10: RemoveID still works after SetPairIsTag.
func TestPairIsTag_RemoveWorks(t *testing.T) {
	w := flecs.New()
	var rel, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetPairIsTag(w, rel)

	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, flecs.MakePair(rel, tgt))
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.RemoveID(fw, e, flecs.MakePair(rel, tgt))
	})

	w.Read(func(fr *flecs.Reader) {
		if fr.HasID(e, flecs.MakePair(rel, tgt)) {
			t.Error("pair should be removed after RemoveID")
		}
	})
}

// Test 11a: SetPairIsTag skips pairs from unrelated relationships (coverage helper).
// Ensures the "skip different relationship" branch in SetPairIsTag is exercised.
func TestPairIsTag_SkipsUnrelatedPairs(t *testing.T) {
	w := flecs.New()
	var rel1, rel2, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel1 = fw.NewEntity()
		rel2 = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
	})

	// Register a data pair for rel2.
	w.Write(func(fw *flecs.Writer) {
		flecs.SetPair[Position](fw, e, rel2, tgt, Position{X: 7})
	})

	// SetPairIsTag for rel1 must not panic even though rel2 has data pairs.
	flecs.SetPairIsTag(w, rel1)

	w.Read(func(fr *flecs.Reader) {
		if !flecs.IsPairIsTag(fr, rel1) {
			t.Error("expected rel1 to be PairIsTag")
		}
		if flecs.IsPairIsTag(fr, rel2) {
			t.Error("rel2 should not be PairIsTag")
		}
	})
}

// Test 11: IsPairIsTag accepts a pair ID and answers based on the relationship side.
func TestPairIsTag_AcceptsPairID(t *testing.T) {
	w := flecs.New()
	var rel, tgt flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
	})
	flecs.SetPairIsTag(w, rel)

	w.Read(func(fr *flecs.Reader) {
		pairID := flecs.MakePair(rel, tgt)
		if !flecs.IsPairIsTag(fr, pairID) {
			t.Error("IsPairIsTag(MakePair(R,T)) should return true when R is PairIsTag")
		}
		if flecs.IsPairIsTag(fr, pairID) != flecs.IsPairIsTag(fr, rel) {
			t.Error("IsPairIsTag(MakePair(R,T)) should equal IsPairIsTag(R)")
		}
	})
}
