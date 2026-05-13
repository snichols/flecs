package flecs_test

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/snichols/flecs"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func withMustPanic(t *testing.T, fn func()) string {
	t.Helper()
	var msg string
	func() {
		defer func() {
			if r := recover(); r != nil {
				msg, _ = r.(string)
			}
		}()
		fn()
	}()
	if msg == "" {
		t.Fatal("expected panic but did not panic")
	}
	return msg
}

// ── 1. Basic bare add ────────────────────────────────────────────────────────

func TestWith_BareAdd(t *testing.T) {
	w := flecs.New()
	var power, responsibility, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		power = fw.NewEntity()
		responsibility = fw.NewEntity()
	})
	flecs.SetWith(w, power, responsibility)

	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })
	w.Write(func(fw *flecs.Writer) { fw.AddID(e, power) })

	w.Read(func(fr *flecs.Reader) {
		if !flecs.HasID(fr, e, power) {
			t.Error("e should have Power")
		}
		if !flecs.HasID(fr, e, responsibility) {
			t.Error("e should have Responsibility (auto-added via With)")
		}
	})
}

// ── 2. Chained: A→B→C ────────────────────────────────────────────────────────

func TestWith_Chained(t *testing.T) {
	w := flecs.New()
	var a, b, c, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a = fw.NewEntity()
		b = fw.NewEntity()
		c = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetWith(w, a, b)
	flecs.SetWith(w, b, c)

	w.Write(func(fw *flecs.Writer) { fw.AddID(e, a) })

	w.Read(func(fr *flecs.Reader) {
		if !flecs.HasID(fr, e, a) {
			t.Error("e should have A")
		}
		if !flecs.HasID(fr, e, b) {
			t.Error("e should have B (chained A→B)")
		}
		if !flecs.HasID(fr, e, c) {
			t.Error("e should have C (chained A→B→C)")
		}
	})
}

// ── 3. Multiple co-adds on one source ────────────────────────────────────────

func TestWith_MultipleCoAdds(t *testing.T) {
	w := flecs.New()
	var a, b, c, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a = fw.NewEntity()
		b = fw.NewEntity()
		c = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetWith(w, a, b)
	flecs.SetWith(w, a, c)

	w.Write(func(fw *flecs.Writer) { fw.AddID(e, a) })

	w.Read(func(fr *flecs.Reader) {
		if !flecs.HasID(fr, e, a) {
			t.Error("e should have A")
		}
		if !flecs.HasID(fr, e, b) {
			t.Error("e should have B")
		}
		if !flecs.HasID(fr, e, c) {
			t.Error("e should have C")
		}
	})
}

// ── 4. Pair form: SetWith(R, S) then add (R, T) → e has (R, T) and (S, T) ──

func TestWith_PairForm(t *testing.T) {
	w := flecs.New()
	var r, s, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		s = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetWith(w, r, s)

	w.Write(func(fw *flecs.Writer) { fw.AddID(e, flecs.MakePair(r, tgt)) })

	w.Read(func(fr *flecs.Reader) {
		if !flecs.HasID(fr, e, flecs.MakePair(r, tgt)) {
			t.Error("e should have (R, T)")
		}
		if !flecs.HasID(fr, e, flecs.MakePair(s, tgt)) {
			t.Error("e should have (S, T) — pair-form With co-add inherits target")
		}
	})
}

// ── 5. Pair form chained: R1→R2→R3 ──────────────────────────────────────────

func TestWith_PairFormChained(t *testing.T) {
	w := flecs.New()
	var r1, r2, r3, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r1 = fw.NewEntity()
		r2 = fw.NewEntity()
		r3 = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetWith(w, r1, r2)
	flecs.SetWith(w, r2, r3)

	w.Write(func(fw *flecs.Writer) { fw.AddID(e, flecs.MakePair(r1, tgt)) })

	w.Read(func(fr *flecs.Reader) {
		if !flecs.HasID(fr, e, flecs.MakePair(r1, tgt)) {
			t.Error("e should have (R1, T)")
		}
		if !flecs.HasID(fr, e, flecs.MakePair(r2, tgt)) {
			t.Error("e should have (R2, T) — chained pair With")
		}
		if !flecs.HasID(fr, e, flecs.MakePair(r3, tgt)) {
			t.Error("e should have (R3, T) — chained R1→R2→R3")
		}
	})
}

// ── 6. Cycle detection: SetWith(A, B) + SetWith(B, A) panics ─────────────────

func TestWith_CycleDetection(t *testing.T) {
	w := flecs.New()
	var a, b, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a = fw.NewEntity()
		b = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetWith(w, a, b)
	flecs.SetWith(w, b, a)

	msg := withMustPanic(t, func() {
		w.Write(func(fw *flecs.Writer) { fw.AddID(e, a) })
	})
	if !strings.Contains(msg, "cycle") && !strings.Contains(msg, "With") {
		t.Errorf("panic message should mention cycle or With, got: %q", msg)
	}
}

// ── 7. Idempotent SetWith ─────────────────────────────────────────────────────

func TestWith_Idempotent(t *testing.T) {
	w := flecs.New()
	var a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a = fw.NewEntity()
		b = fw.NewEntity()
	})
	flecs.SetWith(w, a, b)
	flecs.SetWith(w, a, b) // second call — idempotent

	var coAdds []flecs.ID
	w.Read(func(fr *flecs.Reader) {
		coAdds = flecs.HasWith(fr, a)
	})
	count := 0
	for _, id := range coAdds {
		if id == b {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected B to appear exactly once in HasWith result, got %d", count)
	}
}

// ── 8. Deferred path bare add ─────────────────────────────────────────────────

func TestWith_DeferredBareAdd(t *testing.T) {
	w := flecs.New()
	var power, responsibility, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		power = fw.NewEntity()
		responsibility = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetWith(w, power, responsibility)

	// Deferred add: both Power and Responsibility land after Write closes.
	w.Write(func(fw *flecs.Writer) { fw.AddID(e, power) })

	w.Read(func(fr *flecs.Reader) {
		if !flecs.HasID(fr, e, power) {
			t.Error("e should have Power after deferred add")
		}
		if !flecs.HasID(fr, e, responsibility) {
			t.Error("e should have Responsibility after deferred coalesce")
		}
	})
}

// ── 9. HasWith round-trip ────────────────────────────────────────────────────

func TestWith_HasWith(t *testing.T) {
	w := flecs.New()
	var a, b, c flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a = fw.NewEntity()
		b = fw.NewEntity()
		c = fw.NewEntity()
	})
	flecs.SetWith(w, a, b)
	flecs.SetWith(w, a, c)

	var coAdds []flecs.ID
	w.Read(func(fr *flecs.Reader) {
		coAdds = flecs.HasWith(fr, a)
	})
	if len(coAdds) != 2 {
		t.Fatalf("HasWith: expected 2 co-adds, got %d: %v", len(coAdds), coAdds)
	}
	found := map[flecs.ID]bool{b: false, c: false}
	for _, id := range coAdds {
		if _, ok := found[id]; ok {
			found[id] = true
		}
	}
	for id, seen := range found {
		if !seen {
			t.Errorf("HasWith: expected co-add %v not found", id)
		}
	}
}

// ── 10. IsA interaction: With does NOT fire on IsA walk ───────────────────────

// With fires on direct add only. e inheriting Power via IsA does not re-trigger
// With for the inheritor — WithDirect fires only inside flecs_find_table_with
// (direct id-add path), not on IsA chain walks. This matches C semantics.
func TestWith_IsANoRetrigger(t *testing.T) {
	w := flecs.New()
	var power, responsibility, template, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		power = fw.NewEntity()
		responsibility = fw.NewEntity()
		template = fw.NewEntity()
	})
	flecs.SetWith(w, power, responsibility)

	// template gets Power directly — triggers With, so template owns both.
	w.Write(func(fw *flecs.Writer) { fw.AddID(template, power) })

	// e inherits from template via IsA. No direct AddID(power) happens on e.
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		fw.AddID(e, flecs.MakePair(w.IsA(), template))
	})

	w.Read(func(fr *flecs.Reader) {
		// e inherits Power via IsA chain lookup.
		if !flecs.HasID(fr, e, power) {
			t.Error("e should inherit Power via IsA")
		}
		// e should NOT locally own Responsibility — With does not fire on IsA walk.
		if flecs.OwnsID(fr, e, responsibility) {
			t.Error("e should not locally own Responsibility — With does not fire on IsA walk")
		}
	})
}

// ── 11. Exclusive + With interaction ────────────────────────────────────────

// SetExclusive(R) + SetWith(R, S): adding (R, T2) after (R, T1) replaces the
// relationship but co-adds (S, T2). The previously co-added (S, T1) is NOT
// removed — With is one-way add-only. The user must clean up (S, T1) manually.
func TestWith_ExclusiveInteraction(t *testing.T) {
	w := flecs.New()
	var r, s, t1, t2, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		s = fw.NewEntity()
		t1 = fw.NewEntity()
		t2 = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetExclusive(w, r)
	flecs.SetWith(w, r, s)

	w.Write(func(fw *flecs.Writer) { fw.AddID(e, flecs.MakePair(r, t1)) })
	w.Read(func(fr *flecs.Reader) {
		if !flecs.HasID(fr, e, flecs.MakePair(r, t1)) {
			t.Error("e should have (R, T1)")
		}
		if !flecs.HasID(fr, e, flecs.MakePair(s, t1)) {
			t.Error("e should have (S, T1) from first With co-add")
		}
	})

	// Exclusive replacement: (R, T1) → (R, T2).
	w.Write(func(fw *flecs.Writer) { fw.AddID(e, flecs.MakePair(r, t2)) })
	w.Read(func(fr *flecs.Reader) {
		if flecs.HasID(fr, e, flecs.MakePair(r, t1)) {
			t.Error("(R, T1) should have been replaced by Exclusive")
		}
		if !flecs.HasID(fr, e, flecs.MakePair(r, t2)) {
			t.Error("e should have (R, T2) after replacement")
		}
		if !flecs.HasID(fr, e, flecs.MakePair(s, t2)) {
			t.Error("e should have (S, T2) co-added by With after Exclusive replacement")
		}
		// With is one-way: (S, T1) is NOT auto-removed.
		if !flecs.HasID(fr, e, flecs.MakePair(s, t1)) {
			t.Error("(S, T1) should remain — With is one-way; removing (R, T1) does not remove (S, T1)")
		}
	})
}

// ── 12. One-way remove: With does not auto-remove on Remove ──────────────────

func TestWith_OneWayRemove(t *testing.T) {
	w := flecs.New()
	var power, responsibility, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		power = fw.NewEntity()
		responsibility = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetWith(w, power, responsibility)

	w.Write(func(fw *flecs.Writer) { fw.AddID(e, power) })
	w.Read(func(fr *flecs.Reader) {
		if !flecs.HasID(fr, e, power) || !flecs.HasID(fr, e, responsibility) {
			t.Fatal("setup: e should have Power and Responsibility")
		}
	})

	w.Write(func(fw *flecs.Writer) { fw.RemoveID(e, power) })
	w.Read(func(fr *flecs.Reader) {
		if flecs.HasID(fr, e, power) {
			t.Error("Power should have been removed")
		}
		// Responsibility remains — With is one-way add-only.
		if !flecs.HasID(fr, e, responsibility) {
			t.Error("Responsibility should NOT be auto-removed (With is one-way)")
		}
	})
}

// ── Bonus A. Deferred path with pair form ────────────────────────────────────

func TestWith_DeferredPairForm(t *testing.T) {
	w := flecs.New()
	var r, s, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		s = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetWith(w, r, s)

	w.Write(func(fw *flecs.Writer) { fw.AddID(e, flecs.MakePair(r, tgt)) })

	w.Read(func(fr *flecs.Reader) {
		if !flecs.HasID(fr, e, flecs.MakePair(r, tgt)) {
			t.Error("e should have (R, T) after deferred pair add")
		}
		if !flecs.HasID(fr, e, flecs.MakePair(s, tgt)) {
			t.Error("e should have (S, T) after deferred pair With co-add")
		}
	})
}

// Bonus B. Hook ordering: Power's OnAdd fires before Responsibility's OnAdd.
func TestWith_HookOrdering(t *testing.T) {
	w := flecs.New()
	var power, responsibility, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		power = fw.NewEntity()
		responsibility = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetWith(w, power, responsibility)

	var order []string
	flecs.ObserveID(w, power, flecs.EventOnAdd, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
		order = append(order, "power")
	})
	flecs.ObserveID(w, responsibility, flecs.EventOnAdd, func(_ *flecs.Writer, _ flecs.ID, _ unsafe.Pointer) {
		order = append(order, "responsibility")
	})

	w.Write(func(fw *flecs.Writer) { fw.AddID(e, power) })

	if len(order) < 2 {
		t.Fatalf("expected 2 OnAdd hook fires, got %d: %v", len(order), order)
	}
	if order[0] != "power" {
		t.Errorf("expected Power's OnAdd first, got %v", order)
	}
	if order[1] != "responsibility" {
		t.Errorf("expected Responsibility's OnAdd second, got %v", order)
	}
}

// ── Batched deferred path (2+ cmds per entity → batchForEntity → expandWithIntoScratch) ──

// TestWith_BatchedDeferred exercises expandWithIntoScratch via batchForEntity:
// two AddID calls for the same entity in one Write block form a multi-cmd chain,
// causing the two-pass coalescer to run instead of dispatch→addIDImmediate.
func TestWith_BatchedDeferred(t *testing.T) {
	w := flecs.New()
	var marker, a, b, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		marker = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetWith(w, a, b)

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, marker) // first cmd for e
		fw.AddID(e, a)      // second cmd → nextForEntity<0 → batchForEntity → expandWithIntoScratch
	})

	w.Read(func(fr *flecs.Reader) {
		if !flecs.HasID(fr, e, marker) {
			t.Error("e should have marker")
		}
		if !flecs.HasID(fr, e, a) {
			t.Error("e should have a")
		}
		if !flecs.HasID(fr, e, b) {
			t.Error("e should have b via With co-add (batched deferred)")
		}
	})
}

// TestWith_BatchedDeferredPairForm covers the id.IsPair() branch in
// expandWithIntoScratchHelper: adding a pair (R, T) where R has (With, S)
// should co-add (S, T) preserving the target.
func TestWith_BatchedDeferredPairForm(t *testing.T) {
	w := flecs.New()
	var marker, r, s, tgt, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		marker = fw.NewEntity()
		r = fw.NewEntity()
		s = fw.NewEntity()
		tgt = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetWith(w, r, s)

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, marker)
		fw.AddID(e, flecs.MakePair(r, tgt)) // pair: srcID=r, coAdd=(s,tgt)
	})

	w.Read(func(fr *flecs.Reader) {
		if !flecs.HasID(fr, e, flecs.MakePair(r, tgt)) {
			t.Error("e should have (R, T)")
		}
		if !flecs.HasID(fr, e, flecs.MakePair(s, tgt)) {
			t.Error("e should have (S, T) via pair-form With co-add (batched deferred)")
		}
	})
}

// TestWith_BatchedDeferredDiamond covers the alreadyPresent=true branch in
// expandWithIntoScratchHelper: A→C and B→C; adding A and B in one Write block
// expands C from A first, then when expanding B's co-adds, C is already in dst.
func TestWith_BatchedDeferredDiamond(t *testing.T) {
	w := flecs.New()
	var marker, a, b, c, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		marker = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		c = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetWith(w, a, c)
	flecs.SetWith(w, b, c)

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, marker)
		fw.AddID(e, a)
		fw.AddID(e, b)
	})

	w.Read(func(fr *flecs.Reader) {
		if !flecs.HasID(fr, e, a) {
			t.Error("e should have A")
		}
		if !flecs.HasID(fr, e, b) {
			t.Error("e should have B")
		}
		if !flecs.HasID(fr, e, c) {
			t.Error("e should have C (diamond dedup)")
		}
	})
}

// TestWith_BatchedDeferredCycle covers cycle detection in expandWithIntoScratchHelper:
// A→B→A with 2+ cmds on same entity forces batchForEntity path.
func TestWith_BatchedDeferredCycle(t *testing.T) {
	w := flecs.New()
	var marker, a, b, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		marker = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetWith(w, a, b)
	flecs.SetWith(w, b, a)

	msg := withMustPanic(t, func() {
		w.Write(func(fw *flecs.Writer) {
			fw.AddID(e, marker) // need 2+ cmds for batchForEntity
			fw.AddID(e, a)
		})
	})
	if !strings.Contains(msg, "cycle") && !strings.Contains(msg, "With") {
		t.Errorf("expected cycle panic, got: %q", msg)
	}
}

// ── Targeted coverage tests ───────────────────────────────────────────────────

// TestWith_SourceWithExtraTag covers the `continue` branch in applyWithCoAdds:
// the source entity has a non-With tag in its type (scanned before the With pair),
// causing the guard condition to be true → continue.
func TestWith_SourceWithExtraTag(t *testing.T) {
	w := flecs.New()
	var extraTag, a, b, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		extraTag = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetWith(w, a, b)
	// Add extraTag to a so a's type = [extraTag, (With, b)].
	// The extraTag entry triggers the `continue` in applyWithCoAdds' scanning loop.
	w.Write(func(fw *flecs.Writer) { fw.AddID(a, extraTag) })

	w.Write(func(fw *flecs.Writer) { fw.AddID(e, a) })

	w.Read(func(fr *flecs.Reader) {
		if !flecs.HasID(fr, e, a) {
			t.Error("e should have a")
		}
		if !flecs.HasID(fr, e, b) {
			t.Error("e should have b via With co-add")
		}
	})
}

// TestWith_BatchedDeferredSourceWithExtraTag covers the `continue` branch in
// expandWithIntoScratchHelper via the batched deferred path.
func TestWith_BatchedDeferredSourceWithExtraTag(t *testing.T) {
	w := flecs.New()
	var marker, extraTag, a, b, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		marker = fw.NewEntity()
		extraTag = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetWith(w, a, b)
	w.Write(func(fw *flecs.Writer) { fw.AddID(a, extraTag) })

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, marker) // first cmd
		fw.AddID(e, a)      // second cmd → batchForEntity → expandWithIntoScratchHelper
	})

	w.Read(func(fr *flecs.Reader) {
		if !flecs.HasID(fr, e, a) {
			t.Error("e should have a")
		}
		if !flecs.HasID(fr, e, b) {
			t.Error("e should have b via With co-add (batched)")
		}
	})
}

// TestWith_HasWithNullEntity covers the rec==nil branch in HasWith by passing
// the null entity (index 0), which always returns nil from the entity index.
func TestWith_HasWithNullEntity(t *testing.T) {
	w := flecs.New()
	var result []flecs.ID
	w.Read(func(fr *flecs.Reader) {
		result = flecs.HasWith(fr, flecs.ID(0)) // null sentinel → rec == nil
	})
	if result != nil {
		t.Errorf("HasWith(null-entity): expected nil, got %v", result)
	}
}

// TestWith_DeadSourceImmediate covers srcRec==nil in applyWithCoAdds: adding a
// deleted entity as a bare tag to another entity means the source record is gone,
// so no co-add fires.
func TestWith_DeadSourceImmediate(t *testing.T) {
	w := flecs.New()
	var a, b, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a = fw.NewEntity()
		b = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetWith(w, a, b)
	deadA := a
	w.Write(func(fw *flecs.Writer) { fw.Delete(a) })

	// Add deleted entity as bare tag; applyWithCoAdds: srcRec==nil → return early.
	w.Write(func(fw *flecs.Writer) { fw.AddID(e, deadA) })

	w.Read(func(fr *flecs.Reader) {
		if flecs.HasID(fr, e, b) {
			t.Error("b must not be co-added when source entity is dead")
		}
	})
}

// TestWith_DeadSourceBatched covers srcRec==nil in expandWithIntoScratchHelper
// via the batched deferred path.
func TestWith_DeadSourceBatched(t *testing.T) {
	w := flecs.New()
	var marker, a, b, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		marker = fw.NewEntity()
		a = fw.NewEntity()
		b = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetWith(w, a, b)
	deadA := a
	w.Write(func(fw *flecs.Writer) { fw.Delete(a) })

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, marker) // first cmd
		fw.AddID(e, deadA)  // second cmd → batchForEntity → expandWithIntoScratchHelper srcRec==nil
	})

	w.Read(func(fr *flecs.Reader) {
		if !flecs.HasID(fr, e, marker) {
			t.Error("e should have marker")
		}
		if flecs.HasID(fr, e, b) {
			t.Error("b must not be co-added when source entity is dead (batched)")
		}
	})
}

// ── Accessor and index tests ──────────────────────────────────────────────────

func TestWith_HasWithEmpty(t *testing.T) {
	w := flecs.New()
	var a flecs.ID
	w.Write(func(fw *flecs.Writer) { a = fw.NewEntity() })
	var result []flecs.ID
	w.Read(func(fr *flecs.Reader) { result = flecs.HasWith(fr, a) })
	if result != nil {
		t.Errorf("HasWith on entity with no registrations: expected nil, got %v", result)
	}
}

func TestWith_Accessor(t *testing.T) {
	w := flecs.New()
	id := w.With()
	if id == 0 {
		t.Fatal("w.With() returned zero ID")
	}
	if id.Index() != 32 {
		t.Errorf("w.With() index: want 32, got %d", id.Index())
	}
}

func TestWith_BuiltinIndexShift(t *testing.T) {
	w := flecs.New()
	if w.OrderedChildren().Index() != 33 {
		t.Errorf("OrderedChildren index: want 33, got %d", w.OrderedChildren().Index())
	}
	if w.Wildcard().Index() != 34 {
		t.Errorf("Wildcard index: want 34, got %d", w.Wildcard().Index())
	}
	if w.Any().Index() != 35 {
		t.Errorf("Any index: want 35, got %d", w.Any().Index())
	}
}
