package flecs_test

import (
	"strings"
	"testing"

	"github.com/snichols/flecs"
)

// Types used across writeonce tests.
type ConfigValue struct{ N int }
type Score struct{ Points int }

// Test 1: SetWriteOnce + first Set succeeds; value is readable.
func TestWriteOnce_FirstSetSucceeds(t *testing.T) {
	w := flecs.New()
	cfgID := flecs.RegisterComponent[ConfigValue](w)
	flecs.SetWriteOnce(w, cfgID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, ConfigValue{N: 42})
	})
	w.Read(func(fr *flecs.Reader) {
		v, ok := flecs.Get[ConfigValue](fr, e)
		if !ok {
			t.Fatal("expected ConfigValue to be present")
		}
		if v.N != 42 {
			t.Errorf("expected N=42, got %d", v.N)
		}
	})
}

// Test 2: SetWriteOnce + first Set + second Set: second panics naming entity and component.
func TestWriteOnce_SecondSetPanics(t *testing.T) {
	w := flecs.New()
	cfgID := flecs.RegisterComponent[ConfigValue](w)
	flecs.SetWriteOnce(w, cfgID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, ConfigValue{N: 1})
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on second Set, got none")
		}
		msg := r.(string)
		if !strings.Contains(msg, "WriteOnce") {
			t.Errorf("panic message does not mention WriteOnce: %q", msg)
		}
	}()

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, ConfigValue{N: 2}) // should panic
	})
}

// Test 3: Add then Set then Set again — first Set succeeds, second panics.
// Confirms Add is not the trigger; hasBeenSet tracks the Set, not component presence.
func TestWriteOnce_AddThenSetThenSetPanics(t *testing.T) {
	w := flecs.New()
	cfgID := flecs.RegisterComponent[ConfigValue](w)
	flecs.SetWriteOnce(w, cfgID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.AddID(fw, e, cfgID) // Add: zero value, does NOT count as first write
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, ConfigValue{N: 10}) // first Set: OK
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on second Set, got none")
		}
	}()

	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, ConfigValue{N: 20}) // second Set: should panic
	})
}

// Test 4: Set then Remove then Add then Set succeeds. Remove clears tracking.
func TestWriteOnce_RemoveClearsTracking(t *testing.T) {
	w := flecs.New()
	cfgID := flecs.RegisterComponent[ConfigValue](w)
	flecs.SetWriteOnce(w, cfgID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, ConfigValue{N: 1})
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[ConfigValue](fw, e)
	})
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, e, cfgID)
	})
	// After Remove, Set should succeed again.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, ConfigValue{N: 99})
	})
	w.Read(func(fr *flecs.Reader) {
		v, ok := flecs.Get[ConfigValue](fr, e)
		if !ok {
			t.Fatal("expected ConfigValue present after re-set")
		}
		if v.N != 99 {
			t.Errorf("expected N=99, got %d", v.N)
		}
	})
}

// Test 5: Deferred-coalesced path — second SetID inside the same Write panics during coalesce.
func TestWriteOnce_DeferredCoalescedPanics(t *testing.T) {
	w := flecs.New()
	cfgID := flecs.RegisterComponent[ConfigValue](w)
	flecs.SetWriteOnce(w, cfgID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic during coalesce, got none")
		}
		msg := r.(string)
		if !strings.Contains(msg, "WriteOnce") {
			t.Errorf("panic message does not mention WriteOnce: %q", msg)
		}
	}()

	// Both SetByID calls for the same (e, cfgID) land in the same Write block.
	// The second one should panic during flush/coalesce.
	w.Write(func(fw *flecs.Writer) {
		fw.SetByID(e, cfgID, ConfigValue{N: 1})
		fw.SetByID(e, cfgID, ConfigValue{N: 2}) // second write, should panic on flush
	})
}

// Test 6: Pair-form — WriteOnce on relationship R applies to every (R, T) pair.
// Each (entity, (R, T)) slot is tracked independently.
func TestWriteOnce_PairForm(t *testing.T) {
	w := flecs.New()
	scoreID := flecs.RegisterComponent[Score](w)
	flecs.SetWriteOnce(w, scoreID)

	var e, t1, t2 flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		t1 = fw.NewEntity()
		t2 = fw.NewEntity()
		// First write to (e, (Score, t1)) — OK.
		flecs.SetPair[Score](fw, e, scoreID, t1, Score{Points: 100})
	})

	// Second write to (e, (Score, t1)) — should panic.
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on second pair write, got none")
		}
	}()

	w.Write(func(fw *flecs.Writer) {
		// A different target t2 is a separate slot — should succeed.
		flecs.SetPair[Score](fw, e, scoreID, t2, Score{Points: 200})
		// Same target t1 a second time — should panic.
		flecs.SetPair[Score](fw, e, scoreID, t1, Score{Points: 999})
	})
}

// Test 7: SetWriteOnce on a non-component entity panics at trait-application time;
// IsWriteOnce on a non-component entity returns false without panic.
func TestWriteOnce_NonComponentTargetPanics(t *testing.T) {
	w := flecs.New()

	var bare flecs.ID
	w.Write(func(fw *flecs.Writer) {
		bare = fw.NewEntity()
	})

	// IsWriteOnce on a non-component returns false, no panic.
	var isWO bool
	w.Read(func(fr *flecs.Reader) {
		isWO = flecs.IsWriteOnce(fr, bare)
	})
	if isWO {
		t.Error("expected IsWriteOnce to return false for non-component entity")
	}

	// SetWriteOnce on a non-component panics.
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic applying WriteOnce to non-component, got none")
		}
		msg := r.(string)
		if !strings.Contains(msg, "not a component") {
			t.Errorf("unexpected panic message: %q", msg)
		}
	}()
	flecs.SetWriteOnce(w, bare)
}

// Test 8: IsWriteOnce round-trip; idempotent SetWriteOnce (calling twice is safe).
func TestWriteOnce_IdempotentAndRoundTrip(t *testing.T) {
	w := flecs.New()
	cfgID := flecs.RegisterComponent[ConfigValue](w)

	w.Read(func(fr *flecs.Reader) {
		if flecs.IsWriteOnce(fr, cfgID) {
			t.Error("expected IsWriteOnce false before SetWriteOnce")
		}
	})

	flecs.SetWriteOnce(w, cfgID) // first call
	flecs.SetWriteOnce(w, cfgID) // idempotent second call — must not panic

	w.Read(func(fr *flecs.Reader) {
		if !flecs.IsWriteOnce(fr, cfgID) {
			t.Error("expected IsWriteOnce true after SetWriteOnce")
		}
	})
}

// Test 9: Bonus — applying WriteOnce to w.WriteOnce() itself (the marker entity)
// should panic at trait-application time since WriteOnce is not a registered component.
func TestWriteOnce_MarkerEntityIsNotAComponent(t *testing.T) {
	w := flecs.New()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic applying WriteOnce to its own marker entity")
		}
	}()
	flecs.SetWriteOnce(w, w.WriteOnce())
}

// Test 10: Remove on a WriteOnce component that was never Set (map never initialized)
// must not panic. Covers the nil-map early-return path in clearWriteOnceTracking.
func TestWriteOnce_RemoveWithoutPriorSet(t *testing.T) {
	w := flecs.New()
	cfgID := flecs.RegisterComponent[ConfigValue](w)
	flecs.SetWriteOnce(w, cfgID)

	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.AddID(fw, e, cfgID)
	})
	// Remove without ever calling Set — clearWriteOnceTracking with nil map must not panic.
	w.Write(func(fw *flecs.Writer) {
		flecs.Remove[ConfigValue](fw, e)
	})
	// After remove, a fresh Set should succeed.
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, ConfigValue{N: 7})
	})
	w.Read(func(fr *flecs.Reader) {
		v, ok := flecs.Get[ConfigValue](fr, e)
		if !ok || v.N != 7 {
			t.Errorf("expected N=7 after remove+set, got ok=%v v=%v", ok, v)
		}
	})
}

// Test 11: Bare-tag form via AddID(compID, w.WriteOnce()) is equivalent to SetWriteOnce.
func TestWriteOnce_BareTagFormEquivalent(t *testing.T) {
	w := flecs.New()
	cfgID := flecs.RegisterComponent[ConfigValue](w)

	// Apply via bare-tag form inside a deferred block.
	w.Write(func(fw *flecs.Writer) {
		flecs.AddID(fw, cfgID, w.WriteOnce())
	})

	w.Read(func(fr *flecs.Reader) {
		if !flecs.IsWriteOnce(fr, cfgID) {
			t.Error("expected IsWriteOnce true after bare-tag AddID")
		}
	})

	// Confirm enforcement still works after bare-tag application.
	var e flecs.ID
	w.Write(func(fw *flecs.Writer) {
		e = fw.NewEntity()
		flecs.Set(fw, e, ConfigValue{N: 55})
	})
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on second Set after bare-tag WriteOnce")
		}
	}()
	w.Write(func(fw *flecs.Writer) {
		flecs.Set(fw, e, ConfigValue{N: 66})
	})
}
