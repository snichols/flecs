package flecs_test

import (
	"testing"

	"github.com/snichols/flecs"
)

// Test 1: Without SetOneOf, any (R, x) add succeeds regardless of x's parentage.
func TestOneOf_Default_NoConstraint(t *testing.T) {
	w := flecs.New()
	var r, parent, child, unrelated flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		parent = fw.NewEntity()
		child = fw.NewEntity()
		unrelated = fw.NewEntity()
		fw.AddID(child, flecs.MakePair(w.ChildOf(), parent))
	})
	// No OneOf constraint on r — both child-of-parent and unrelated targets must succeed.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(fw.NewEntity(), flecs.MakePair(r, child))
		fw.AddID(fw.NewEntity(), flecs.MakePair(r, unrelated))
	})
	w.Read(func(fr *flecs.Reader) {
		_, ok := flecs.IsOneOf(fr, r)
		if ok {
			t.Error("expected IsOneOf(r) = false before SetOneOf")
		}
	})
}

// Test 2: SetOneOf(w, R, Colors); AddID(e, MakePair(R, red)) where red is ChildOf Colors succeeds.
func TestOneOf_PairForm_ValidTargetSucceeds(t *testing.T) {
	w := flecs.New()
	var r, colors, red, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		colors = fw.NewEntity()
		red = fw.NewEntity()
		e = fw.NewEntity()
		fw.AddID(red, flecs.MakePair(w.ChildOf(), colors))
	})
	flecs.SetOneOf(w, r, colors)
	// Must not panic: red is a direct child of colors.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, flecs.MakePair(r, red))
	})
	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(e, flecs.MakePair(r, red)) {
			t.Error("expected (R, red) on e — red is a valid child of Colors")
		}
	})
}

// Test 3: SetOneOf(w, R, Colors); AddID(e, MakePair(R, stranger)) where stranger is not
// a child of Colors must panic with a constraint-violated message.
func TestOneOf_PairForm_InvalidTargetPanics(t *testing.T) {
	w := flecs.New()
	var r, colors, stranger, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		colors = fw.NewEntity()
		stranger = fw.NewEntity() // not parented to colors
		e = fw.NewEntity()
	})
	flecs.SetOneOf(w, r, colors)

	defer func() {
		if recover() == nil {
			t.Error("expected panic: stranger is not a child of colors (OneOf violated)")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, flecs.MakePair(r, stranger))
	})
}

// Test 4: SetOneOf(w, R, R) (self-tag form); AddID(e, MakePair(R, child)) where child
// is ChildOf R succeeds.
func TestOneOf_SelfTagForm_ValidTargetSucceeds(t *testing.T) {
	w := flecs.New()
	var r, child, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		child = fw.NewEntity()
		e = fw.NewEntity()
		fw.AddID(child, flecs.MakePair(w.ChildOf(), r))
	})
	flecs.SetOneOf(w, r, r) // self-tag: target must be a child of R itself
	// Must not panic: child is a direct child of r.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, flecs.MakePair(r, child))
	})
	w.Read(func(fr *flecs.Reader) {
		if !fr.HasID(e, flecs.MakePair(r, child)) {
			t.Error("expected (R, child) on e — child is a valid child of R (self-tag form)")
		}
	})
}

// Test 5: SetOneOf(w, R, P) then IsOneOf(w, R) returns (P, true);
// an unconfigured relation returns (0, false).
func TestOneOf_GetOneOf_RoundTrip(t *testing.T) {
	w := flecs.New()
	var r, p, unconfigured flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		p = fw.NewEntity()
		unconfigured = fw.NewEntity()
	})
	flecs.SetOneOf(w, r, p)

	w.Read(func(fr *flecs.Reader) {
		parent, ok := flecs.IsOneOf(fr, r)
		if !ok {
			t.Error("expected IsOneOf(r) = true after SetOneOf(r, p)")
		}
		if parent.Index() != p.Index() {
			t.Errorf("expected parent.Index() = %d, got %d", p.Index(), parent.Index())
		}
		_, ok2 := flecs.IsOneOf(fr, unconfigured)
		if ok2 {
			t.Error("expected IsOneOf(unconfigured) = false")
		}
	})

	// Also works from a *Writer.
	w.Write(func(fw *flecs.Writer) {
		parent, ok := flecs.IsOneOf(fw, r)
		if !ok || parent.Index() != p.Index() {
			t.Errorf("expected IsOneOf via *Writer to return (p, true), got (%v, %v)", parent, ok)
		}
	})
}

// Test 6: IsOneOf(w, w.IsA()) and IsOneOf(w, w.ChildOf()) return (0, false) —
// no built-in relationship ships OneOf by default, matching C bootstrap.
func TestOneOf_BootstrappedRelationships_NoConstraint(t *testing.T) {
	w := flecs.New()
	w.Read(func(fr *flecs.Reader) {
		_, ok := flecs.IsOneOf(fr, w.IsA())
		if ok {
			t.Error("expected IsOneOf(IsA) = false — IsA ships no OneOf constraint")
		}
		_, ok = flecs.IsOneOf(fr, w.ChildOf())
		if ok {
			t.Error("expected IsOneOf(ChildOf) = false — ChildOf ships no OneOf constraint")
		}
	})
}

// Test 7: R is both OneOf(Colors) and Exclusive; replacing (R, red) with (R, blue)
// (both valid children of Colors) migrates atomically without re-violating the constraint.
func TestOneOf_WithExclusive_AtomicReplacement(t *testing.T) {
	w := flecs.New()
	var r, colors, red, blue, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		colors = fw.NewEntity()
		red = fw.NewEntity()
		blue = fw.NewEntity()
		e = fw.NewEntity()
		fw.AddID(red, flecs.MakePair(w.ChildOf(), colors))
		fw.AddID(blue, flecs.MakePair(w.ChildOf(), colors))
	})
	flecs.SetOneOf(w, r, colors)
	flecs.SetExclusive(w, r)

	// Add initial valid pair (R, red).
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, flecs.MakePair(r, red))
	})
	// Replace with (R, blue) — also a valid child of Colors; must not panic.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, flecs.MakePair(r, blue))
	})
	w.Read(func(fr *flecs.Reader) {
		if fr.HasID(e, flecs.MakePair(r, red)) {
			t.Error("expected (R, red) to be gone after atomic replacement")
		}
		if !fr.HasID(e, flecs.MakePair(r, blue)) {
			t.Error("expected (R, blue) on e after atomic replacement")
		}
	})
}

// Test 8: fw.AddID(R, w.OneOf()) (bare tag) is equivalent to SetOneOf(w, R, R);
// subsequent fw.AddID(R, MakePair(w.OneOf(), parent)) updates to the pair form.
func TestOneOf_BareTagAddID(t *testing.T) {
	w := flecs.New()
	var r, child, parent, childOfParent flecs.ID
	w.Write(func(fw *flecs.Writer) {
		r = fw.NewEntity()
		child = fw.NewEntity()
		parent = fw.NewEntity()
		childOfParent = fw.NewEntity()
		// child is a child of r (for the self-tag form).
		fw.AddID(child, flecs.MakePair(w.ChildOf(), r))
		// childOfParent is a child of parent (for the pair form).
		fw.AddID(childOfParent, flecs.MakePair(w.ChildOf(), parent))
		// Bare-tag form: fw.AddID(R, w.OneOf()) sets self-tag (target must be child of R).
		fw.AddID(r, w.OneOf())
	})

	// After bare-tag: self-tag form is active — child (of r) must be valid.
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		fw.AddID(e, flecs.MakePair(r, child)) // must not panic
	})
	w.Read(func(fr *flecs.Reader) {
		p, ok := flecs.IsOneOf(fr, r)
		if !ok {
			t.Error("expected IsOneOf(r) = true after bare-tag AddID")
		}
		if p.Index() != r.Index() {
			t.Errorf("expected self-tag: parent.Index() == r.Index(), got %d vs %d", p.Index(), r.Index())
		}
	})

	// Update to pair form: fw.AddID(R, MakePair(w.OneOf(), parent)).
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(r, flecs.MakePair(w.OneOf(), parent))
	})
	w.Read(func(fr *flecs.Reader) {
		p, ok := flecs.IsOneOf(fr, r)
		if !ok {
			t.Error("expected IsOneOf(r) = true after pair-form AddID update")
		}
		if p.Index() != parent.Index() {
			t.Errorf("expected pair-form: parent.Index() == %d, got %d", parent.Index(), p.Index())
		}
	})

	// Now childOfParent (child of parent) must be a valid target.
	w.Write(func(fw *flecs.Writer) {
		e := fw.NewEntity()
		fw.AddID(e, flecs.MakePair(r, childOfParent)) // must not panic
	})
}
