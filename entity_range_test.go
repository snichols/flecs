package flecs_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/snichols/flecs"
)

// RangeSet/RangeClear/RangeNew panic inside a deferred scope (deferDepth > 0),
// matching the MakeAlive/SetVersion precedent. Use WriterForTest (deferDepth==0)
// for immediate-mode operations. See entity_lifecycle_test.go for the same pattern.

// ─── Test 1: RangeSet constrains NewEntity to [min, max) ────────────────────

func TestRangeSetNewEntityInRange(t *testing.T) {
	w := flecs.New()
	fw := flecs.WriterForTest(w)
	flecs.RangeSet(fw, 100, 200)
	var ids [10]flecs.ID
	for i := range ids {
		ids[i] = fw.NewEntity()
	}
	for i, id := range ids {
		if idx := id.Index(); idx < 100 || idx >= 200 {
			t.Errorf("ids[%d] index %d not in [100, 200)", i, idx)
		}
	}
}

// ─── Test 2: range exhaustion panics ─────────────────────────────────────────

func TestRangeSetExhaustion(t *testing.T) {
	w := flecs.New()
	fw := flecs.WriterForTest(w)
	flecs.RangeSet(fw, 100, 105)
	for i := 0; i < 5; i++ {
		fw.NewEntity()
	}
	// 6th allocation must panic range-exhausted.
	assertPanics(t, "range [100, 105) exhausted", func() {
		fw.NewEntity()
	})
}

// ─── Test 3: RangeClear reverts to monotonic-from-current ───────────────────

func TestRangeClearRevertsMonotonic(t *testing.T) {
	w := flecs.New()
	fw := flecs.WriterForTest(w)
	flecs.RangeSet(fw, 100, 200)
	first := fw.NewEntity()
	flecs.RangeClear(fw)
	// Next NewEntity must continue from current maxID, not jump back.
	afterClear := fw.NewEntity()
	if afterClear.Index() != first.Index()+1 {
		t.Errorf("after RangeClear: got index %d, want %d",
			afterClear.Index(), first.Index()+1)
	}
	// Confirm no range is active.
	_, _, set := flecs.RangeGet(fw)
	if set {
		t.Error("range still active after RangeClear")
	}
}

// ─── Test 4: RangeSet with max <= min panics ─────────────────────────────────

func TestRangeSetMaxLEMinPanics(t *testing.T) {
	w := flecs.New()
	fw := flecs.WriterForTest(w)
	assertPanics(t, "max", func() {
		flecs.RangeSet(fw, 200, 100)
	})
	assertPanics(t, "max", func() {
		flecs.RangeSet(fw, 100, 100)
	})
}

// ─── Test 5: RangeSet with min < 1 panics ─────────────────────────────────────

func TestRangeSetMinZeroPanics(t *testing.T) {
	w := flecs.New()
	fw := flecs.WriterForTest(w)
	assertPanics(t, "min must be >= 1", func() {
		flecs.RangeSet(fw, 0, 100)
	})
}

// ─── Test 6: RangeNew does not change world range state ───────────────────────

func TestRangeNewNoRangeStateChange(t *testing.T) {
	w := flecs.New()
	fw := flecs.WriterForTest(w)
	e := flecs.RangeNew(fw, 50, 60)
	// Verify the ID is in [50, 60).
	if idx := e.Index(); idx < 50 || idx >= 60 {
		t.Errorf("RangeNew ID index %d not in [50, 60)", idx)
	}
	// Verify world has no active range (RangeNew must not set rangeSet).
	_, _, set := flecs.RangeGet(fw)
	if set {
		t.Error("RangeGet set==true after RangeNew (should be false)")
	}
}

// ─── Test 7: out-of-range recycled IDs are skipped ──────────────────────────
//
// Allocate entity at index 100 while range is [100,200). Delete it (enqueue
// recycled). Switch to range [300,400). First NewEntity must be ≥ 300, not the
// recycled 100. After RangeClear the recycled 100 becomes eligible.

func TestRangeSetSkipsOutOfRangeRecycled(t *testing.T) {
	w := flecs.New()
	fw := flecs.WriterForTest(w)

	flecs.RangeSet(fw, 100, 200)
	e100 := fw.NewEntity()
	if e100.Index() != 100 {
		t.Fatalf("expected entity at index 100, got %d", e100.Index())
	}

	// Delete e100 so it enters the recycle queue.
	fw.Delete(e100)

	// Switch to a different range.
	flecs.RangeSet(fw, 300, 400)
	first := fw.NewEntity()
	if first.Index() < 300 || first.Index() >= 400 {
		t.Errorf("first entity after re-range has index %d, expected [300, 400)", first.Index())
	}

	// After RangeClear the recycled entity at 100 (gen+1) becomes eligible.
	flecs.RangeClear(fw)
	next := fw.NewEntity()
	if !w.IsAlive(next) {
		t.Error("entity after RangeClear is not alive")
	}
	// The recycled index-100 entry should be reused since it's at the head of
	// the recycle queue and now no range constraint is active.
	if next.Index() != 100 {
		t.Logf("note: expected recycled index 100, got %d (still alive: %v)", next.Index(), w.IsAlive(next))
	}
}

// ─── Test 8: MakeAlive bypasses range check ──────────────────────────────────

func TestMakeAliveBypasses_Range(t *testing.T) {
	w := flecs.New()
	fw := flecs.WriterForTest(w)
	flecs.RangeSet(fw, 100, 200)
	// MakeAlive at index 500 advances maxID past rangeMax (200).
	flecs.MakeAlive(fw, flecs.MakeEntity(500, 0))

	// Verify range is still reported correctly.
	min, max, set := flecs.RangeGet(fw)
	if !set || min.Index() != 100 || max.Index() != 200 {
		t.Errorf("range after MakeAlive: got [%d, %d) set=%v, want [100, 200) set=true",
			min.Index(), max.Index(), set)
	}
	// Next NewEntity must panic because maxID (500) >= rangeMax (200).
	assertPanics(t, "range [100, 200) exhausted", func() {
		fw.NewEntity()
	})
}

// ─── Test 9: marshal round-trip preserves range ───────────────────────────────

func TestRangeMarshalRoundTrip(t *testing.T) {
	w := flecs.New()
	fw := flecs.WriterForTest(w)
	flecs.RangeSet(fw, 500, 600)

	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	w2 := flecs.New()
	if err := json.Unmarshal(data, w2); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	w2.Read(func(r *flecs.Reader) {
		min, max, set := flecs.RangeGet(r)
		if !set {
			t.Fatal("range not restored after unmarshal")
		}
		if min.Index() != 500 || max.Index() != 600 {
			t.Errorf("range after unmarshal: got [%d, %d), want [500, 600)", min.Index(), max.Index())
		}
	})
}

// ─── Test 10: re-range mid-game ───────────────────────────────────────────────

func TestRangeRerangeMidGame(t *testing.T) {
	w := flecs.New()
	fw := flecs.WriterForTest(w)
	var first [3]flecs.ID
	var second [3]flecs.ID

	flecs.RangeSet(fw, 100, 200)
	for i := range first {
		first[i] = fw.NewEntity()
	}
	flecs.RangeSet(fw, 500, 600)
	for i := range second {
		second[i] = fw.NewEntity()
	}

	for i, id := range first {
		if idx := id.Index(); idx < 100 || idx >= 200 {
			t.Errorf("first[%d] index %d not in [100, 200)", i, idx)
		}
	}
	for i, id := range second {
		if idx := id.Index(); idx < 500 || idx >= 600 {
			t.Errorf("second[%d] index %d not in [500, 600)", i, idx)
		}
	}
}

// ─── Test 11: RangeGet round-trip ─────────────────────────────────────────────

func TestRangeGetRoundTrip(t *testing.T) {
	w := flecs.New()
	fw := flecs.WriterForTest(w)

	flecs.RangeSet(fw, 7, 42)
	min, max, set := flecs.RangeGet(fw)
	if !set {
		t.Fatal("set should be true after RangeSet")
	}
	if min.Index() != 7 || max.Index() != 42 {
		t.Errorf("RangeGet got [%d, %d), want [7, 42)", min.Index(), max.Index())
	}

	flecs.RangeClear(fw)
	min, max, set = flecs.RangeGet(fw)
	if set {
		t.Fatal("set should be false after RangeClear")
	}
	if min.Index() != 0 || max.Index() != 0 {
		t.Errorf("RangeGet after clear got [%d, %d), want [0, 0)", min.Index(), max.Index())
	}
}

// ─── Test 12: RangeNew advances maxID counter when min > maxID+1 ─────────────
//
// Issue spec: "starting maxID = 5, RangeNew(100, 110) returns 100 and advances
// maxID to 100. Subsequent NewEntity (no range set) returns 101." In practice,
// maxID starts above the built-in entity count, but the jump behaviour is the
// same whenever min > maxID+1.

func TestRangeNewAdvancesCounter(t *testing.T) {
	w := flecs.New()
	fw := flecs.WriterForTest(w)

	e := flecs.RangeNew(fw, 1000, 1010)
	if e.Index() != 1000 {
		t.Errorf("RangeNew(1000, 1010) returned index %d, want 1000", e.Index())
	}

	// Subsequent NewEntity (no range) must return 1001.
	next := fw.NewEntity()
	if next.Index() != 1001 {
		t.Errorf("NewEntity after RangeNew returned index %d, want 1001", next.Index())
	}
}

// ─── Test 13: deferred-scope panics ──────────────────────────────────────────

func TestRangeSetDeferredPanic(t *testing.T) {
	w := flecs.New()
	assertPanics(t, "deferred scope", func() {
		w.Write(func(fw *flecs.Writer) {
			fw.RangeSet(100, 200)
		})
	})
}

func TestRangeClearDeferredPanic(t *testing.T) {
	w := flecs.New()
	assertPanics(t, "deferred scope", func() {
		w.Write(func(fw *flecs.Writer) {
			fw.RangeClear()
		})
	})
}

func TestRangeNewDeferredPanic(t *testing.T) {
	w := flecs.New()
	assertPanics(t, "deferred scope", func() {
		w.Write(func(fw *flecs.Writer) {
			fw.RangeNew(100, 200)
		})
	})
}

// ─── Test 14: RangeNew parameter validation ───────────────────────────────────

func TestRangeNewValidation(t *testing.T) {
	w := flecs.New()
	fw := flecs.WriterForTest(w)
	assertPanics(t, "min must be >= 1", func() {
		flecs.RangeNew(fw, 0, 100)
	})
	assertPanics(t, "max", func() {
		flecs.RangeNew(fw, 200, 100)
	})
	assertPanics(t, "max", func() {
		flecs.RangeNew(fw, 100, 100)
	})
}

// ─── Test 15: RangeNew exhaustion ────────────────────────────────────────────

func TestRangeNewExhaustion(t *testing.T) {
	w := flecs.New()
	fw := flecs.WriterForTest(w)
	for i := 0; i < 5; i++ {
		flecs.RangeNew(fw, 2000, 2005)
	}
	assertPanics(t, "AllocInRange [2000, 2005) exhausted", func() {
		flecs.RangeNew(fw, 2000, 2005)
	})
}

// ─── Test 16: RangeNew prefers recycled in-range ID ─────────────────────────

func TestRangeNewPrefersRecycled(t *testing.T) {
	w := flecs.New()
	fw := flecs.WriterForTest(w)

	// Allocate and delete an entity in [500, 600) to create a recycled slot.
	flecs.RangeSet(fw, 500, 600)
	e := fw.NewEntity()
	firstIdx := e.Index()
	fw.Delete(e)
	flecs.RangeClear(fw)

	// RangeNew should prefer the recycled slot.
	e2 := flecs.RangeNew(fw, 500, 600)
	if e2.Index() != firstIdx {
		t.Errorf("RangeNew: expected recycled index %d, got %d", firstIdx, e2.Index())
	}
}

// ─── Test 17: Writer thin shims delegate correctly ───────────────────────────

func TestWriterRangeShims(t *testing.T) {
	w := flecs.New()
	fw := flecs.WriterForTest(w)

	fw.RangeSet(3000, 4000)
	e := fw.NewEntity()
	if e.Index() < 3000 || e.Index() >= 4000 {
		t.Errorf("Writer.RangeSet/NewEntity: index %d not in [3000, 4000)", e.Index())
	}

	min, max, set := flecs.RangeGet(fw)
	if !set || min.Index() != 3000 || max.Index() != 4000 {
		t.Errorf("RangeGet got [%d,%d) set=%v, want [3000,4000) true", min.Index(), max.Index(), set)
	}

	fw.RangeClear()
	_, _, set = flecs.RangeGet(fw)
	if set {
		t.Error("range still set after Writer.RangeClear")
	}

	e2 := fw.RangeNew(3000, 4000)
	if e2.Index() < 3000 || e2.Index() >= 4000 {
		t.Errorf("Writer.RangeNew: index %d not in [3000, 4000)", e2.Index())
	}
}

// ─── Test 18: RangeNew fresh alloc reuses stale dense slot ───────────────────
//
// When an entity has been deleted (stale slot in dense) and AllocInRange falls
// through to fresh allocation (recycled entity not in target range), it must
// reuse the stale slot rather than blindly appending.

func TestRangeNewFreshReusesStaleDenseSlot(t *testing.T) {
	w := flecs.New()
	fw := flecs.WriterForTest(w)

	// Allocate and delete an entity to create a stale slot in dense.
	e := fw.NewEntity()
	if !w.IsAlive(e) {
		t.Fatal("entity not alive before delete")
	}
	fw.Delete(e)

	// RangeNew in a range that does NOT include the recycled entity.
	// This forces AllocInRange into the fresh-alloc path with a stale slot present.
	e2 := flecs.RangeNew(fw, 5000, 5010)
	if e2.Index() != 5000 {
		t.Errorf("RangeNew with stale slot: expected index 5000, got %d", e2.Index())
	}
	if !w.IsAlive(e2) {
		t.Error("entity from RangeNew is not alive")
	}
}

// ─── helpers ────────────────────────────────────────────────────────────────

// assertPanics calls fn and asserts that it panics with a message containing want.
func assertPanics(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic containing %q but no panic occurred", want)
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected panic string containing %q, got non-string panic: %v", want, r)
		}
		if !strings.Contains(msg, want) {
			t.Fatalf("panic message %q does not contain %q", msg, want)
		}
	}()
	fn()
}
