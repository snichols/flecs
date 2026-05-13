package flecs_test

import (
	"strings"
	"testing"

	"github.com/snichols/flecs"
)

// mustPanicWith calls fn and asserts it panics with a message containing want.
func mustPanicWith(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic containing %q, got none", want)
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T: %v", r, r)
		}
		if !strings.Contains(msg, want) {
			t.Errorf("panic message %q does not contain %q", msg, want)
		}
	}()
	fn()
}

// mustNotPanic calls fn and asserts it does not panic.
func mustNotPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	fn()
}

// ── Test 1: Relationship bare-tag add panics ─────────────────────────────────

func TestUsageConstraint_Relationship_BareTagPanics(t *testing.T) {
	w := flecs.New()
	var R, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		R = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetRelationship(w, R)

	mustPanicWith(t, "Relationship", func() {
		w.Write(func(fw *flecs.Writer) {
			flecs.AddID(fw, e, R)
		})
	})
}

// ── Test 2: Relationship in relationship slot succeeds ───────────────────────

func TestUsageConstraint_Relationship_PairRelSlotSucceeds(t *testing.T) {
	w := flecs.New()
	var R, T, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		R = fw.NewEntity()
		T = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetRelationship(w, R)

	mustNotPanic(t, func() {
		w.Write(func(fw *flecs.Writer) {
			flecs.AddID(fw, e, flecs.MakePair(R, T))
		})
	})
}

// ── Test 3: Relationship in target slot panics ───────────────────────────────

func TestUsageConstraint_Relationship_PairTargetSlotPanics(t *testing.T) {
	w := flecs.New()
	var R, T, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		R = fw.NewEntity()
		T = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetRelationship(w, R)

	mustPanicWith(t, "Relationship", func() {
		w.Write(func(fw *flecs.Writer) {
			flecs.AddID(fw, e, flecs.MakePair(T, R))
		})
	})
}

// ── Test 4: Target bare-tag add panics ──────────────────────────────────────

func TestUsageConstraint_Target_BareTagPanics(t *testing.T) {
	w := flecs.New()
	var T, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		T = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetTarget(w, T)

	mustPanicWith(t, "Target", func() {
		w.Write(func(fw *flecs.Writer) {
			flecs.AddID(fw, e, T)
		})
	})
}

// ── Test 5: Target in target slot succeeds ───────────────────────────────────

func TestUsageConstraint_Target_PairTargetSlotSucceeds(t *testing.T) {
	w := flecs.New()
	var R, T, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		R = fw.NewEntity()
		T = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetTarget(w, T)

	mustNotPanic(t, func() {
		w.Write(func(fw *flecs.Writer) {
			flecs.AddID(fw, e, flecs.MakePair(R, T))
		})
	})
}

// ── Test 6: Target in relationship slot panics ───────────────────────────────

func TestUsageConstraint_Target_PairRelSlotPanics(t *testing.T) {
	w := flecs.New()
	var T, X, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		T = fw.NewEntity()
		X = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetTarget(w, T)

	mustPanicWith(t, "Target", func() {
		w.Write(func(fw *flecs.Writer) {
			flecs.AddID(fw, e, flecs.MakePair(T, X))
		})
	})
}

// ── Test 7: Trait exemption allows Relationship in target slot ───────────────

func TestUsageConstraint_TraitExemption_Succeeds(t *testing.T) {
	w := flecs.New()
	var R, M, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		R = fw.NewEntity()
		M = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetRelationship(w, R)
	flecs.SetTrait(w, M)

	// Without Trait, (R, M) would panic because M has Relationship. With Trait it succeeds.
	mustNotPanic(t, func() {
		w.Write(func(fw *flecs.Writer) {
			flecs.AddID(fw, e, flecs.MakePair(R, M))
		})
	})
}

// ── Test 8: Built-in Relationship bootstrap ──────────────────────────────────

func TestUsageConstraint_BuiltinRelationshipBootstrap(t *testing.T) {
	w := flecs.New()
	w.Read(func(r *flecs.Reader) {
		for _, id := range []flecs.ID{w.IsA(), w.ChildOf(), w.OnDelete(), w.OnDeleteTarget(), w.OnInstantiate()} {
			if !flecs.IsRelationship(r, id) {
				t.Errorf("expected IsRelationship(s, %v) == true at world init", id)
			}
		}
	})
}

// ── Test 9: Built-in Target bootstrap ───────────────────────────────────────

func TestUsageConstraint_BuiltinTargetBootstrap(t *testing.T) {
	w := flecs.New()
	w.Read(func(r *flecs.Reader) {
		for _, id := range []flecs.ID{w.Override(), w.Inherit(), w.DontInherit()} {
			if !flecs.IsTarget(r, id) {
				t.Errorf("expected IsTarget(s, %v) == true at world init", id)
			}
		}
		// Upstream does NOT mark Remove/Delete/Panic as Target.
		for _, id := range []flecs.ID{w.RemoveAction(), w.DeleteAction(), w.PanicAction()} {
			if flecs.IsTarget(r, id) {
				t.Errorf("expected IsTarget(s, %v) == false (not bootstrapped Target in upstream)", id)
			}
		}
	})
}

// ── Test 10: Built-in Trait bootstrap ───────────────────────────────────────

func TestUsageConstraint_BuiltinTraitBootstrap(t *testing.T) {
	w := flecs.New()
	w.Read(func(r *flecs.Reader) {
		for _, id := range []flecs.ID{w.IsA(), w.ChildOf()} {
			if !flecs.IsTrait(r, id) {
				t.Errorf("expected IsTrait(s, %v) == true at world init", id)
			}
		}
	})
}

// ── Test 11: Deferred path panics at coalesce time ───────────────────────────
//
// Inside w.Write(), all mutations are queued (deferred). The constraint check
// fires at flush time (when Write returns), not at submission time inside fn.

func TestUsageConstraint_DeferredPath_BarePanicsAtCoalesce(t *testing.T) {
	w := flecs.New()
	var R, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		R = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetRelationship(w, R)

	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				if msg, ok := r.(string); ok && strings.Contains(msg, "Relationship") {
					panicked = true
				}
			}
		}()
		// Inside Write, the Add is queued without panic (submission time is safe).
		// The panic fires during queue flush when Write returns (coalesce time).
		w.Write(func(fw *flecs.Writer) {
			flecs.AddID(fw, e, R) // queued, no panic here
		}) // flush fires → panic propagates out of Write
	}()
	if !panicked {
		t.Fatal("expected deferred-path panic containing \"Relationship\", got none")
	}
}

func TestUsageConstraint_DeferredPath_PairPanicsAtCoalesce(t *testing.T) {
	w := flecs.New()
	var R, T, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		R = fw.NewEntity()
		T = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetRelationship(w, R)

	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				if msg, ok := r.(string); ok && strings.Contains(msg, "Relationship") {
					panicked = true
				}
			}
		}()
		w.Write(func(fw *flecs.Writer) {
			flecs.AddID(fw, e, flecs.MakePair(T, R)) // queued, R in target slot
		}) // flush → panic
	}()
	if !panicked {
		t.Fatal("expected deferred-path pair panic containing \"Relationship\", got none")
	}
}

// ── Test 12: Idempotence — set twice → still true ────────────────────────────

func TestUsageConstraint_Idempotence(t *testing.T) {
	w := flecs.New()
	var R, T, M flecs.ID
	w.Write(func(fw *flecs.Writer) {
		R = fw.NewEntity()
		T = fw.NewEntity()
		M = fw.NewEntity()
	})
	flecs.SetRelationship(w, R)
	flecs.SetRelationship(w, R) // second call: no-op
	flecs.SetTarget(w, T)
	flecs.SetTarget(w, T)
	flecs.SetTrait(w, M)
	flecs.SetTrait(w, M)

	w.Read(func(r *flecs.Reader) {
		if !flecs.IsRelationship(r, R) {
			t.Error("IsRelationship false after double set")
		}
		if !flecs.IsTarget(r, T) {
			t.Error("IsTarget false after double set")
		}
		if !flecs.IsTrait(r, M) {
			t.Error("IsTrait false after double set")
		}
	})

	// Setting on already-bootstrapped built-in entities is also a no-op.
	flecs.SetRelationship(w, w.IsA())
	w.Read(func(r *flecs.Reader) {
		if !flecs.IsRelationship(r, w.IsA()) {
			t.Error("IsRelationship(IsA) false after redundant set")
		}
	})
}

// ── Test 13: Composition with other traits ───────────────────────────────────

func TestUsageConstraint_CompositionWithOtherTraits(t *testing.T) {
	// SetRelationship + SetExclusive
	t.Run("RelationshipPlusExclusive", func(t *testing.T) {
		w := flecs.New()
		var R flecs.ID
		w.Write(func(fw *flecs.Writer) { R = fw.NewEntity() })
		mustNotPanic(t, func() {
			flecs.SetRelationship(w, R)
			flecs.SetExclusive(w, R)
		})
		w.Read(func(r *flecs.Reader) {
			if !flecs.IsRelationship(r, R) {
				t.Error("IsRelationship should be true")
			}
		})
		if !flecs.IsExclusive(w, R) {
			t.Error("IsExclusive should be true")
		}
	})

	// SetRelationship + SetTraversable
	t.Run("RelationshipPlusTraversable", func(t *testing.T) {
		w := flecs.New()
		var R flecs.ID
		w.Write(func(fw *flecs.Writer) { R = fw.NewEntity() })
		mustNotPanic(t, func() {
			flecs.SetRelationship(w, R)
			flecs.SetTraversable(w, R)
		})
		w.Read(func(r *flecs.Reader) {
			if !flecs.IsRelationship(r, R) {
				t.Error("IsRelationship should be true")
			}
			if !flecs.IsTraversable(r, R) {
				t.Error("IsTraversable should be true")
			}
		})
	})

	// SetRelationship + SetTransitive
	t.Run("RelationshipPlusTransitive", func(t *testing.T) {
		w := flecs.New()
		var R flecs.ID
		w.Write(func(fw *flecs.Writer) { R = fw.NewEntity() })
		mustNotPanic(t, func() {
			flecs.SetRelationship(w, R)
			flecs.SetTransitive(w, R)
		})
		w.Read(func(r *flecs.Reader) {
			if !flecs.IsRelationship(r, R) {
				t.Error("IsRelationship should be true")
			}
		})
		if !flecs.IsTransitive(w, R) {
			t.Error("IsTransitive should be true")
		}
	})

	// IsA is already bootstrapped with both Relationship and Traversable — verify no conflict.
	t.Run("IsABuiltinComposition", func(t *testing.T) {
		w := flecs.New()
		w.Read(func(r *flecs.Reader) {
			if !flecs.IsRelationship(r, w.IsA()) {
				t.Error("IsA should be Relationship")
			}
			if !flecs.IsTraversable(r, w.IsA()) {
				t.Error("IsA should be Traversable")
			}
			if !flecs.IsTrait(r, w.IsA()) {
				t.Error("IsA should be Trait")
			}
		})
	})
}

// ── Test 14: Component-on-Target panics ──────────────────────────────────────

type ConstraintDataComp struct{ Val int }

func TestUsageConstraint_ComponentOnTargetPanics(t *testing.T) {
	w := flecs.New()
	compID := flecs.RegisterComponent[ConstraintDataComp](w)
	flecs.SetTarget(w, compID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) { e = fw.NewEntity() })

	mustPanicWith(t, "Target", func() {
		w.Write(func(fw *flecs.Writer) {
			flecs.AddID(fw, e, compID)
		})
	})
}

// ── Test 16: Multi-cmd deferred batch exercises batchForEntity policy branches ─
//
// Adding two tags in the same Write block for the same entity creates a
// multi-cmd chain, triggering batchForEntity. Verifies that the bare-tag
// policy-apply branches in batchForEntity fire correctly.

func TestUsageConstraint_DeferredBatch_PolicyApplied(t *testing.T) {
	w := flecs.New()
	var R, T, M, anotherTag, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		R = fw.NewEntity()
		T = fw.NewEntity()
		M = fw.NewEntity()
		anotherTag = fw.NewEntity()
		e = fw.NewEntity()
	})

	// Apply Relationship, Target, Trait via the multi-cmd deferred path.
	// Add anotherTag first, then the trait tag — this forces a multi-cmd chain
	// for the same entity (R, T, or M), triggering batchForEntity.
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, R, anotherTag)
		flecs.AddID(fw, R, w.Relationship())
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, T, anotherTag)
		flecs.AddID(fw, T, w.Target())
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, M, anotherTag)
		flecs.AddID(fw, M, w.Trait())
	})

	w.Read(func(r *flecs.Reader) {
		if !flecs.IsRelationship(r, R) {
			t.Error("R should be Relationship after deferred-batch apply")
		}
		if !flecs.IsTarget(r, T) {
			t.Error("T should be Target after deferred-batch apply")
		}
		if !flecs.IsTrait(r, M) {
			t.Error("M should be Trait after deferred-batch apply")
		}
	})

	// Verify that a bare-tag add to an entity constrained via deferred-batch panics.
	mustPanicWith(t, "Relationship", func() {
		w.Write(func(fw *flecs.Writer) {
			flecs.AddID(fw, e, anotherTag)
			flecs.AddID(fw, e, R) // R now has Relationship constraint
		})
	})
}

// ── Test 15 (open decision 2): Self-pair (R,R) when R is Relationship-only panics ─

func TestUsageConstraint_SelfPairRelationshipOnlyPanics(t *testing.T) {
	w := flecs.New()
	var R, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		R = fw.NewEntity()
		e = fw.NewEntity()
	})
	flecs.SetRelationship(w, R)
	// R is not Trait, so (R, R) has Relationship in the target slot → panic.
	mustPanicWith(t, "Relationship", func() {
		w.Write(func(fw *flecs.Writer) {
			flecs.AddID(fw, e, flecs.MakePair(R, R))
		})
	})
}
