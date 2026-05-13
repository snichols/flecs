package flecs_test

import (
	"strings"
	"testing"

	"github.com/snichols/flecs"
)

// Types used across singleton tests.
type TimeOfDay struct{ Hour float32 }
type GameSettings struct{ Volume int }

// Test 1: Without SetSingleton, a component may be added to multiple entities.
func TestSingleton_DefaultNoConstraint(t *testing.T) {
	w := flecs.New()
	var a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a = fw.NewEntity()
		b = fw.NewEntity()
		flecs.Set(fw, a, TimeOfDay{Hour: 6})
		flecs.Set(fw, b, TimeOfDay{Hour: 12})
	})
	w.Read(func(fr *flecs.Reader) {
		v1, ok1 := flecs.Get[TimeOfDay](fr, a)
		v2, ok2 := flecs.Get[TimeOfDay](fr, b)
		if !ok1 || !ok2 {
			t.Fatal("expected both entities to hold TimeOfDay")
		}
		if v1.Hour != 6 || v2.Hour != 12 {
			t.Errorf("unexpected values: %v %v", v1.Hour, v2.Hour)
		}
	})
}

// Test 2: SetSingleton then Set on A succeeds; Set on B panics naming both entities and the component.
func TestSingleton_SecondHolderPanics(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	flecs.SetSingleton(w, posID)

	var a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a = fw.NewEntity()
		b = fw.NewEntity()
	})
	// First Set succeeds.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, a, Position{X: 1, Y: 2})
	})
	// Second Set on different entity must panic.
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic when adding singleton to second entity")
		}
		msg := ""
		if s, ok := r.(string); ok {
			msg = s
		}
		if !strings.Contains(msg, "singleton") {
			t.Errorf("panic message should mention singleton, got: %q", msg)
		}
		_ = a
		_ = b
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, b, Position{X: 3, Y: 4})
	})
}

// Test 3: SetSingleton + Set on A, Remove[T] from A, then Set on B succeeds.
func TestSingleton_RemoveReleasesSlot(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	flecs.SetSingleton(w, posID)

	var a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a = fw.NewEntity()
		b = fw.NewEntity()
		flecs.Set(fw, a, Position{X: 10})
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[Position](fw, a)
	})
	// Slot released — Set on B must succeed.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, b, Position{X: 20})
	})
	w.Read(func(fr *flecs.Reader) {
		v, ok := flecs.Get[Position](fr, b)
		if !ok {
			t.Fatal("expected b to hold Position after slot release")
		}
		if v.X != 20 {
			t.Errorf("expected X=20, got %v", v.X)
		}
	})
}

// Test 4: IsSingleton round-trip.
func TestSingleton_IsSingleton_RoundTrip(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)

	w.Read(func(fr *flecs.Reader) {
		if flecs.IsSingleton(fr, posID) {
			t.Error("expected IsSingleton(pos) = false before marking")
		}
	})

	flecs.SetSingleton(w, posID)

	w.Read(func(fr *flecs.Reader) {
		if !flecs.IsSingleton(fr, posID) {
			t.Error("expected IsSingleton(pos) = true after SetSingleton")
		}
		if flecs.IsSingleton(fr, velID) {
			t.Error("expected IsSingleton(vel) = false — vel not marked")
		}
		if flecs.IsSingleton(fr, w.ChildOf()) {
			t.Error("expected IsSingleton(ChildOf) = false — built-in entity, not a data component")
		}
	})

	// Also accessible from *Writer.
	w.Write(func(fw *flecs.Writer) {
		if !flecs.IsSingleton(fw, posID) {
			t.Error("expected IsSingleton(pos) = true from *Writer")
		}
	})
}

// Test 5: SingletonEntity returns (holder, true) after Set, (_, false) before or after Remove.
func TestSingleton_SingletonEntity(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	flecs.SetSingleton(w, posID)

	w.Read(func(fr *flecs.Reader) {
		_, ok := flecs.SingletonEntity(fr, posID)
		if ok {
			t.Error("expected SingletonEntity = (_, false) before any Set")
		}
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, Position{X: 5})
	})

	w.Read(func(fr *flecs.Reader) {
		holder, ok := flecs.SingletonEntity(fr, posID)
		if !ok {
			t.Fatal("expected SingletonEntity to return (e, true) after Set")
		}
		if holder.Index() != e.Index() {
			t.Errorf("expected holder index %d, got %d", e.Index(), holder.Index())
		}
	})

	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[Position](fw, e)
	})

	w.Read(func(fr *flecs.Reader) {
		_, ok := flecs.SingletonEntity(fr, posID)
		if ok {
			t.Error("expected SingletonEntity = (_, false) after Remove")
		}
	})
}

// Test 6: Entity deletion clears the singleton slot.
func TestSingleton_EntityDeleteClearsSlot(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	flecs.SetSingleton(w, posID)

	var a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a = fw.NewEntity()
		b = fw.NewEntity()
		flecs.Set(fw, a, Position{X: 7})
	})

	// Delete a — slot must be cleared.
	w.Delete(a)

	// Set on b must now succeed.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, b, Position{X: 99})
	})

	w.Read(func(fr *flecs.Reader) {
		holder, ok := flecs.SingletonEntity(fr, posID)
		if !ok {
			t.Fatal("expected holder after b.Set")
		}
		if holder.Index() != b.Index() {
			t.Errorf("expected holder = b, got index %d", holder.Index())
		}
	})
}

// Test 7: Singleton + Exclusive composition — singleton is component-level,
// exclusive is pair-level; no cross-interference.
func TestSingleton_WithExclusive_NoInterference(t *testing.T) {
	w := flecs.New()

	// Set up: Likes is an Exclusive relationship; Position is a Singleton component.
	var likes flecs.ID
	posID := flecs.RegisterComponent[Position](w)
	flecs.SetSingleton(w, posID)

	w.Write(func(fw *flecs.Writer) {
		likes = fw.NewEntity()
	})
	flecs.SetExclusive(w, likes)

	var holder, target1, target2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		holder = fw.NewEntity()
		target1 = fw.NewEntity()
		target2 = fw.NewEntity()

		// Add singleton Position to holder.
		flecs.Set(fw, holder, Position{X: 1})
		// Add (Likes, target1) — exclusive; holder gets a single target.
		fw.AddID(holder, flecs.MakePair(likes, target1))
	})

	// Replace (Likes, target1) with (Likes, target2) via Exclusive replacement.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(holder, flecs.MakePair(likes, target2))
	})

	w.Read(func(fr *flecs.Reader) {
		// Exclusive replacement succeeded.
		if fr.HasID(holder, flecs.MakePair(likes, target1)) {
			t.Error("expected (Likes, target1) replaced by exclusive swap")
		}
		if !fr.HasID(holder, flecs.MakePair(likes, target2)) {
			t.Error("expected (Likes, target2) after exclusive swap")
		}
		// Singleton slot still recorded correctly.
		h, ok := flecs.SingletonEntity(fr, posID)
		if !ok || h.Index() != holder.Index() {
			t.Error("expected singleton slot = holder after exclusive swap")
		}
	})
}

// Test 8: Singleton[T] and WriteSingleton[T] typed accessors.
func TestSingleton_TypedAccessors(t *testing.T) {
	w := flecs.New()

	w.Read(func(fr *flecs.Reader) {
		ptr, ok := flecs.Singleton[TimeOfDay](fr)
		if ok || ptr != nil {
			t.Error("expected Singleton[TimeOfDay] = (nil, false) before any setup")
		}
	})

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})

	// WriteSingleton registers T as singleton and sets it on e.
	w.Write(func(fw *flecs.Writer) {
		flecs.WriteSingleton(fw, e, TimeOfDay{Hour: 18})
	})

	w.Read(func(fr *flecs.Reader) {
		ptr, ok := flecs.Singleton[TimeOfDay](fr)
		if !ok || ptr == nil {
			t.Fatal("expected Singleton[TimeOfDay] = (&v, true) after WriteSingleton")
		}
		if ptr.Hour != 18 {
			t.Errorf("expected Hour=18, got %v", ptr.Hour)
		}
	})

	// WriteSingleton is idempotent — same entity, updated value.
	w.Write(func(fw *flecs.Writer) {
		flecs.WriteSingleton(fw, e, TimeOfDay{Hour: 22})
	})
	w.Read(func(fr *flecs.Reader) {
		ptr, ok := flecs.Singleton[TimeOfDay](fr)
		if !ok || ptr == nil || ptr.Hour != 22 {
			t.Errorf("expected Hour=22 after idempotent update, got ok=%v ptr=%v", ok, ptr)
		}
	})

	// Deferred smoke test: enforcement fires via cmd replay inside a Write scope.
	var other flecs.ID
	w.Write(func(fw *flecs.Writer) {
		other = fw.NewEntity()
	})
	defer func() {
		if recover() == nil {
			t.Error("expected panic from deferred WriteSingleton on second entity")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.WriteSingleton(fw, other, TimeOfDay{Hour: 0})
	})
}

// Test 8a: Bare-tag AddID form — fw.AddID(posID, w.Singleton()) marks posID as singleton.
func TestSingleton_BareTagAddID(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)

	// Mark via bare-tag AddID (non-deferred path).
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(posID, w.Singleton())
	})

	w.Read(func(fr *flecs.Reader) {
		if !flecs.IsSingleton(fr, posID) {
			t.Error("expected IsSingleton(pos) = true after bare-tag AddID")
		}
	})

	// Enforcement should now be active.
	var a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a = fw.NewEntity()
		b = fw.NewEntity()
		flecs.Set(fw, a, Position{X: 1})
	})
	defer func() {
		if recover() == nil {
			t.Error("expected panic from second entity after bare-tag Singleton")
		}
		_ = b
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, b, Position{X: 2})
	})
	_ = a
}

// Test 8b_coalesced: Deferred (coalesced) removal of singleton component releases slot.
// Uses a Write block that adds some other component AND removes the singleton in one flush.
func TestSingleton_CoalescedDeferredRemoveReleasesSlot(t *testing.T) {
	w := flecs.New()
	posID := flecs.RegisterComponent[Position](w)
	velID := flecs.RegisterComponent[Velocity](w)
	flecs.SetSingleton(w, posID)

	var a, b flecs.ID
	w.Write(func(fw *flecs.Writer) {
		a = fw.NewEntity()
		b = fw.NewEntity()
		flecs.Set(fw, a, Position{X: 5})
	})

	// Coalesced batch: add Velocity AND remove Position in same Write block.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, a, Velocity{DX: 1})
		flecs.Remove[Position](fw, a)
	})
	_ = velID

	// Slot released — b can now hold Position.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, b, Position{X: 99})
	})
	w.Read(func(fr *flecs.Reader) {
		holder, ok := flecs.SingletonEntity(fr, posID)
		if !ok || holder.Index() != b.Index() {
			t.Errorf("expected holder = b after coalesced removal, ok=%v", ok)
		}
	})
}

// Test 8c_deferred_pair: Deferred coalesced add of a singleton-first pair enforces constraint.
func TestSingleton_CoalescedDeferredPairAdd_EnforcesConstraint(t *testing.T) {
	w := flecs.New()
	var rel, tgt, e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
	})
	flecs.SetSingleton(w, rel)

	// Deferred (coalesced): add Velocity AND (rel, tgt) pair in the same Write block.
	velID := flecs.RegisterComponent[Velocity](w)
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e1, Velocity{DX: 1})
		fw.AddID(e1, flecs.MakePair(rel, tgt))
	})
	_ = velID

	w.Read(func(fr *flecs.Reader) {
		holder, ok := flecs.SingletonEntity(fr, rel)
		if !ok || holder.Index() != e1.Index() {
			t.Errorf("expected e1 as singleton holder, ok=%v", ok)
		}
	})

	// e2 trying to get the same pair must panic.
	defer func() {
		if recover() == nil {
			t.Error("expected panic from coalesced deferred pair add on second entity")
		}
		_ = e2
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e2, Velocity{DX: 2})
		fw.AddID(e2, flecs.MakePair(rel, tgt))
	})
}

// Test 8d: Same entity adds two pairs sharing a singleton First — second add
// hits the same-holder no-op path in checkSingleton (pair-form branch).
func TestSingleton_PairFormSameHolder_NoOp(t *testing.T) {
	w := flecs.New()
	var rel, t1, t2, e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		t1 = fw.NewEntity()
		t2 = fw.NewEntity()
		e = fw.NewEntity()
	})
	// Mark the relationship entity itself as a singleton component.
	flecs.SetSingleton(w, rel)
	// First pair add: records e as holder for rel.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, flecs.MakePair(rel, t1))
	})
	// Second pair add with different target but same First on same entity:
	// checkSingleton fires and finds existing == e no-op (must not panic).
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e, flecs.MakePair(rel, t2))
	})
	w.Read(func(fr *flecs.Reader) {
		holder, ok := flecs.SingletonEntity(fr, rel)
		if !ok || holder.Index() != e.Index() {
			t.Errorf("expected singleton holder = e, got ok=%v", ok)
		}
	})
}

// Test 8c: Pair-form singleton — removing a (rel, tgt) pair releases the singleton slot.
func TestSingleton_PairFormRemoveReleasesSlot(t *testing.T) {
	w := flecs.New()
	var rel, tgt, e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
	})
	flecs.SetSingleton(w, rel)

	// Add (rel, tgt) to e1.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e1, flecs.MakePair(rel, tgt))
	})
	// Remove (rel, tgt) from e1 — releases singleton slot.
	w.Write(func(fw *flecs.Writer) {
		fw.RemoveID(e1, flecs.MakePair(rel, tgt))
	})
	// Now e2 may hold it (must not panic).
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e2, flecs.MakePair(rel, tgt))
	})
	w.Read(func(fr *flecs.Reader) {
		holder, ok := flecs.SingletonEntity(fr, rel)
		if !ok || holder.Index() != e2.Index() {
			t.Errorf("expected holder = e2 after slot re-assignment")
		}
	})
}

// Test 8e: Bare Singleton tag via coalesced deferred path (multiple cmds for same entity).
// Uses a NewEntity-created component entity (which has a table) rather than RegisterComponent.
func TestSingleton_DeferredBareTagCoalesced(t *testing.T) {
	w := flecs.New()
	// Create the "component" entity via NewEntity (has a proper table).
	var compEnt flecs.ID
	w.Write(func(fw *flecs.Writer) {
		compEnt = fw.NewEntity()
	})

	// Coalesced: two trait adds for compEnt in one Write block triggers batchForEntity.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(compEnt, w.CanToggle())
		fw.AddID(compEnt, w.Singleton())
	})
	w.Read(func(fr *flecs.Reader) {
		if !flecs.IsSingleton(fr, compEnt) {
			t.Error("expected Singleton via coalesced deferred bare-tag path")
		}
	})
}

// Test 8f: Pair-form singleton removal via coalesced deferred batch.
func TestSingleton_CoalescedPairRemoveReleasesSlot(t *testing.T) {
	w := flecs.New()
	var rel, tgt, e1, e2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		rel = fw.NewEntity()
		tgt = fw.NewEntity()
		e1 = fw.NewEntity()
		e2 = fw.NewEntity()
	})
	flecs.SetSingleton(w, rel)

	// Add (rel, tgt) pair and some other component to e1 in separate Write blocks.
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e1, flecs.MakePair(rel, tgt))
	})

	// Coalesced: remove (rel,tgt) AND add a component in same Write block.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e1, Position{X: 1})
		fw.RemoveID(e1, flecs.MakePair(rel, tgt))
	})

	// Slot released — e2 may now hold (rel, tgt).
	w.Write(func(fw *flecs.Writer) {
		fw.AddID(e2, flecs.MakePair(rel, tgt))
	})
	w.Read(func(fr *flecs.Reader) {
		holder, ok := flecs.SingletonEntity(fr, rel)
		if !ok || holder.Index() != e2.Index() {
			t.Errorf("expected e2 as holder after coalesced pair removal, ok=%v", ok)
		}
	})
}

// Test 9: Multiple singletons of different types coexist independently.
func TestSingleton_MultipleTypesCoexist(t *testing.T) {
	w := flecs.New()

	todID := flecs.RegisterComponent[TimeOfDay](w)
	gsID := flecs.RegisterComponent[GameSettings](w)
	flecs.SetSingleton(w, todID)
	flecs.SetSingleton(w, gsID)

	var tod, gs flecs.ID
	w.Write(func(fw *flecs.Writer) {
		tod = fw.NewEntity()
		gs = fw.NewEntity()
		flecs.Set(fw, tod, TimeOfDay{Hour: 8})
		// GameSettings may be on the same or different entity.
		flecs.Set(fw, gs, GameSettings{Volume: 80})
	})

	w.Read(func(fr *flecs.Reader) {
		// Each singleton tracked independently.
		todHolder, ok1 := flecs.SingletonEntity(fr, todID)
		gsHolder, ok2 := flecs.SingletonEntity(fr, gsID)
		if !ok1 || !ok2 {
			t.Fatal("expected both singletons to have holders")
		}
		if todHolder.Index() != tod.Index() {
			t.Errorf("TimeOfDay holder mismatch: expected %d, got %d", tod.Index(), todHolder.Index())
		}
		if gsHolder.Index() != gs.Index() {
			t.Errorf("GameSettings holder mismatch: expected %d, got %d", gs.Index(), gsHolder.Index())
		}
	})

	// Attempt second holder for TimeOfDay must panic.
	var extra flecs.ID
	w.Write(func(fw *flecs.Writer) {
		extra = fw.NewEntity()
	})
	defer func() {
		if recover() == nil {
			t.Error("expected panic when adding TimeOfDay to second entity")
		}
		_ = extra
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, extra, TimeOfDay{Hour: 0})
	})
}
