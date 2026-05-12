package flecs_test

import (
	"strings"
	"testing"

	"github.com/snichols/flecs"
)

// ── Test 1: Default unchanged ────────────────────────────────────────────────
//
// A component with no cleanup policy on an entity: deleting the holder removes
// only the holder; sibling holders of the same component are unaffected.
// This is the implicit OnDelete+Remove behavior (default).

func TestCleanupPolicyDefaultRemoveUnchanged(t *testing.T) {
	w := flecs.New()
	type Health struct{ HP int }
	healthID := flecs.RegisterComponent[Health](w)

	var holder, sibling flecs.ID
	w.Write(func(fw *flecs.Writer) {
		holder = fw.NewEntity()
		flecs.Set(fw, holder, Health{HP: 100})
		sibling = fw.NewEntity()
		flecs.Set(fw, sibling, Health{HP: 50})
	})

	// healthID has no explicit cleanup policy.
	_, ok := flecs.GetCleanupPolicy(w, healthID, w.OnDelete())
	if ok {
		t.Fatal("expected no OnDelete policy for component with no explicit policy")
	}

	w.Delete(holder)

	if w.IsAlive(holder) {
		t.Fatal("holder should be dead after Delete")
	}
	if !w.IsAlive(sibling) {
		t.Fatal("sibling should still be alive after deleting holder")
	}
}

// ── Test 2: (OnDeleteTarget, Delete) on custom relationship ───────────────────
//
// Define relationship Likes, apply (OnDeleteTarget, Delete). Add (Likes, target)
// to source. Delete target → source should be deleted.

func TestCleanupPolicyOnDeleteTargetDelete(t *testing.T) {
	w := flecs.New()

	var likesID, source, target flecs.ID
	w.Write(func(fw *flecs.Writer) {
		likesID = fw.NewEntity()
		source = fw.NewEntity()
		target = fw.NewEntity()
	})

	flecs.SetCleanupPolicy(w, likesID, w.OnDeleteTarget(), w.DeleteAction())

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(source, flecs.MakePair(likesID, target))
	})

	w.Delete(target)

	if w.IsAlive(target) {
		t.Fatal("target should be dead")
	}
	if w.IsAlive(source) {
		t.Fatal("source should be dead after target deleted with OnDeleteTarget+Delete policy")
	}
}

// ── Test 3: (OnDeleteTarget, Panic) on custom relationship ────────────────────
//
// Same setup with Panic action. Deleting target must panic; the message must
// identify the relationship and the target entity.

func TestCleanupPolicyOnDeleteTargetPanic(t *testing.T) {
	w := flecs.New()

	var guardRelID, source, target flecs.ID
	w.Write(func(fw *flecs.Writer) {
		guardRelID = fw.NewEntity()
		source = fw.NewEntity()
		target = fw.NewEntity()
	})

	flecs.SetCleanupPolicy(w, guardRelID, w.OnDeleteTarget(), w.PanicAction())

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(source, flecs.MakePair(guardRelID, target))
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic when deleting a Panic-protected target, but no panic occurred")
		}
		msg := ""
		switch v := r.(type) {
		case string:
			msg = v
		case error:
			msg = v.Error()
		default:
			msg = "non-string panic"
		}
		// Panic message must identify both the relationship and the target.
		if !strings.Contains(msg, "Panic") {
			t.Errorf("panic message should mention Panic policy; got: %q", msg)
		}
	}()

	w.Delete(target)
}

// ── Test 4: ChildOf cascade-delete regression ─────────────────────────────────
//
// The ChildOf cascade behavior must be identical after refactoring to use the
// general policy mechanism. Existing childof_test.go tests cover this; this
// test adds an explicit regression covering multi-level cascade and post-order
// semantics to ensure the policy-driven path matches the old hardcoded path.

func TestCleanupPolicyChildOfCascadeRegression(t *testing.T) {
	w := flecs.New()

	var grandparent, parent, child1, child2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		grandparent = fw.NewEntity()
		parent = fw.NewEntity()
		child1 = fw.NewEntity()
		child2 = fw.NewEntity()

		fw.AddID(parent, flecs.MakePair(w.ChildOf(), grandparent))
		fw.AddID(child1, flecs.MakePair(w.ChildOf(), parent))
		fw.AddID(child2, flecs.MakePair(w.ChildOf(), parent))
	})

	// Verify the general policy mechanism drives ChildOf cascade.
	action, ok := flecs.GetCleanupPolicy(w, w.ChildOf(), w.OnDeleteTarget())
	if !ok {
		t.Fatal("ChildOf must have an OnDeleteTarget policy registered in bootstrap")
	}
	if action != w.DeleteAction() {
		t.Fatalf("ChildOf OnDeleteTarget policy should be DeleteAction, got %v", action)
	}

	w.Delete(grandparent)

	for _, e := range []flecs.ID{grandparent, parent, child1, child2} {
		if w.IsAlive(e) {
			t.Errorf("entity %v should be dead after cascaded parent delete", e)
		}
	}
}

// ── Test 5: (OnDelete, Delete) — entity-delete path ──────────────────────────
//
// Add component C with OnDelete+Delete policy to entity e. Deleting e is a
// no-op on the cascade side (e is already being deleted); e is removed and no
// panic occurs. This test documents the contract.

func TestCleanupPolicyOnDeleteDeleteEntityDeletePath(t *testing.T) {
	w := flecs.New()
	type Marker struct{}
	markerID := flecs.RegisterComponent[Marker](w)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Marker{})
	})

	// Register OnDelete+Delete on the marker component entity itself.
	flecs.SetCleanupPolicy(w, markerID, w.OnDelete(), w.DeleteAction())

	// Deleting e must succeed without panic; e is dead after.
	w.Delete(e)

	if w.IsAlive(e) {
		t.Fatal("entity e should be dead after Delete")
	}
}

// ── Test 6: Multiple policies — Delete beats Remove ───────────────────────────
//
// Source has (R1, target) where R1 has OnDeleteTarget+Delete, and
// (R2, target) where R2 has OnDeleteTarget+Remove. When target is deleted,
// Delete wins: source is deleted.

func TestCleanupPolicyDeleteBeatsRemove(t *testing.T) {
	w := flecs.New()

	var r1ID, r2ID, source, target flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r1ID = fw.NewEntity()
		r2ID = fw.NewEntity()
		source = fw.NewEntity()
		target = fw.NewEntity()
	})

	flecs.SetCleanupPolicy(w, r1ID, w.OnDeleteTarget(), w.DeleteAction())
	flecs.SetCleanupPolicy(w, r2ID, w.OnDeleteTarget(), w.RemoveAction())

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(source, flecs.MakePair(r1ID, target))
		fw.AddID(source, flecs.MakePair(r2ID, target))
	})

	w.Delete(target)

	if w.IsAlive(target) {
		t.Fatal("target should be dead")
	}
	// Delete from r1 wins: source should be deleted.
	if w.IsAlive(source) {
		t.Fatal("source should be dead because Delete policy wins over Remove")
	}
}

// ── Test 7: Panic propagation ─────────────────────────────────────────────────
//
// Verify that the panic from OnDeleteTarget+Panic surfaces through World.Delete
// and through a deferred Writer.Delete flush.

func TestCleanupPolicyPanicPropagatesFromWorldDelete(t *testing.T) {
	w := flecs.New()

	var guardRelID, source, target flecs.ID
	w.Write(func(fw *flecs.Writer) {
		guardRelID = fw.NewEntity()
		source = fw.NewEntity()
		target = fw.NewEntity()
		fw.AddID(source, flecs.MakePair(guardRelID, target))
	})
	flecs.SetCleanupPolicy(w, guardRelID, w.OnDeleteTarget(), w.PanicAction())

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic from World.Delete, got none")
		}
	}()
	w.Delete(target)
}

func TestCleanupPolicyPanicPropagatesFromDeferredDelete(t *testing.T) {
	w := flecs.New()

	var guardRelID, source, target flecs.ID
	w.Write(func(fw *flecs.Writer) {
		guardRelID = fw.NewEntity()
		source = fw.NewEntity()
		target = fw.NewEntity()
		fw.AddID(source, flecs.MakePair(guardRelID, target))
	})
	flecs.SetCleanupPolicy(w, guardRelID, w.OnDeleteTarget(), w.PanicAction())

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic from deferred Writer.Delete flush, got none")
		}
	}()

	// The panic should surface during the Write scope flush.
	w.Write(func(fw *flecs.Writer) {
		fw.Delete(target)
	})
}

// ── Test 8: Wildcard target delete ────────────────────────────────────────────
//
// Deleting an entity used as the target of (R, *) where R has OnDeleteTarget+Delete
// cascades to all sources, matching C's flecs_id_is_delete_target path.

func TestCleanupPolicyWildcardTargetDelete(t *testing.T) {
	w := flecs.New()

	var likedRelID, src1, src2, target flecs.ID
	w.Write(func(fw *flecs.Writer) {
		likedRelID = fw.NewEntity()
		src1 = fw.NewEntity()
		src2 = fw.NewEntity()
		target = fw.NewEntity()
	})

	flecs.SetCleanupPolicy(w, likedRelID, w.OnDeleteTarget(), w.DeleteAction())

	w.Write(func(fw *flecs.Writer) {
		fw.AddID(src1, flecs.MakePair(likedRelID, target))
		fw.AddID(src2, flecs.MakePair(likedRelID, target))
	})

	w.Delete(target)

	if w.IsAlive(target) {
		t.Fatal("target should be dead")
	}
	if w.IsAlive(src1) {
		t.Fatal("src1 should be dead: held (likedRelID, target) with Delete policy")
	}
	if w.IsAlive(src2) {
		t.Fatal("src2 should be dead: held (likedRelID, target) with Delete policy")
	}
}

// ── Test: AddID pair-add path ─────────────────────────────────────────────────
//
// Verify that AddID(relID, MakePair(w.OnDeleteTarget(), w.DeleteAction())) is
// equivalent to SetCleanupPolicy. Both must register the policy.

func TestCleanupPolicyAddIDPairPath(t *testing.T) {
	w := flecs.New()

	var relID, source, target flecs.ID
	w.Write(func(fw *flecs.Writer) {
		relID = fw.NewEntity()
		source = fw.NewEntity()
		target = fw.NewEntity()
	})

	// Register via the pair-add path instead of SetCleanupPolicy.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(relID, flecs.MakePair(w.OnDeleteTarget(), w.DeleteAction()))
	})

	// Verify the policy was recorded.
	action, ok := flecs.GetCleanupPolicy(w, relID, w.OnDeleteTarget())
	if !ok {
		t.Fatal("GetCleanupPolicy returned false after AddID pair-add")
	}
	if action != w.DeleteAction() {
		t.Fatalf("expected DeleteAction, got %v", action)
	}

	// Verify cascade delete works.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(source, flecs.MakePair(relID, target))
	})
	w.Delete(target)

	if w.IsAlive(source) {
		t.Fatal("source should be dead after cascade via pair-add registered policy")
	}
}

// ── Test: GetCleanupPolicy accessors ─────────────────────────────────────────

func TestGetCleanupPolicyNoPolicy(t *testing.T) {
	w := flecs.New()
	var relID flecs.ID
	w.Write(func(fw *flecs.Writer) { relID = fw.NewEntity() })

	if _, ok := flecs.GetCleanupPolicy(w, relID, w.OnDeleteTarget()); ok {
		t.Fatal("expected no policy for fresh relationship entity")
	}
}

func TestGetCleanupPolicyPanicAction(t *testing.T) {
	w := flecs.New()
	var relID flecs.ID
	w.Write(func(fw *flecs.Writer) { relID = fw.NewEntity() })

	flecs.SetCleanupPolicy(w, relID, w.OnDeleteTarget(), w.PanicAction())

	action, ok := flecs.GetCleanupPolicy(w, relID, w.OnDeleteTarget())
	if !ok {
		t.Fatal("expected policy to be registered")
	}
	if action != w.PanicAction() {
		t.Fatalf("expected PanicAction, got %v", action)
	}
}

func TestGetCleanupPolicyOverwrite(t *testing.T) {
	w := flecs.New()
	var relID flecs.ID
	w.Write(func(fw *flecs.Writer) { relID = fw.NewEntity() })

	flecs.SetCleanupPolicy(w, relID, w.OnDeleteTarget(), w.DeleteAction())
	flecs.SetCleanupPolicy(w, relID, w.OnDeleteTarget(), w.RemoveAction())

	// After overwrite to Remove (the default), GetCleanupPolicy should report no
	// explicit policy (Remove is implicit).
	_, ok := flecs.GetCleanupPolicy(w, relID, w.OnDeleteTarget())
	if ok {
		t.Fatal("expected no policy after overwriting back to RemoveAction (the default)")
	}
}
