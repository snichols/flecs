package flecs

import "fmt"

// RangeSet constrains the Writer's allocator to issue entity IDs in [min, max).
// Subsequent NewEntity calls return IDs within that range. RangeClear reverses
// the constraint. Re-calling RangeSet replaces the active range immediately.
//
// Panics if called in a deferred scope, if min < 1, if max <= min, or if min
// or max exceed the uint32 entity-index width (2^32 − 1).
//
// MakeAlive bypasses the range check (matching upstream). If MakeAlive advances
// maxID past rangeMax, the next NewEntity call will panic range-exhausted.
//
// Mirrors upstream ecs_entity_range_set (src/world.c:1490-1497) with the Go
// simplification that ranges are transient Writer parameters rather than
// persistent heap-allocated objects (no multi-range registry, no per-range
// recycle pools).
func RangeSet(fw *Writer, min, max ID) {
	if fw.stage.deferDepth > 0 {
		panic("flecs: RangeSet: cannot be called in a deferred scope")
	}
	if min.Index() < 1 {
		panic(fmt.Sprintf("flecs: RangeSet: min must be >= 1, got %d", min.Index()))
	}
	if max.Index() <= min.Index() {
		panic(fmt.Sprintf("flecs: RangeSet: max (%d) must be > min (%d)", max.Index(), min.Index()))
	}
	fw.world.index.SetRange(min.Index(), max.Index())
}

// RangeClear removes any active range constraint. NewEntity reverts to
// monotonic-from-current: the world's maxID is preserved and the next
// NewEntity issues maxID+1, which may be inside the previous range.
// Recycled IDs that were skipped due to the now-cleared range become eligible
// again on subsequent NewEntity calls.
//
// Panics if called in a deferred scope.
//
// Mirrors upstream ecs_entity_range_set(world, NULL) (src/world.c:1490-1497).
func RangeClear(fw *Writer) {
	if fw.stage.deferDepth > 0 {
		panic("flecs: RangeClear: cannot be called in a deferred scope")
	}
	fw.world.index.ClearRange()
}

// RangeGet inspects the current range constraint. set is false when no range
// is active, in which case min and max are both 0.
//
// Mirrors upstream ecs_entity_range_get (src/world.c:1499-1506).
func RangeGet(s scope) (min, max ID, set bool) {
	rMin, rMax, rSet := s.scopeWorld().index.GetRange()
	return ID(rMin), ID(rMax), rSet
}

// RangeNew issues a single entity ID within [min, max) without modifying the
// world's active range constraint. Useful for one-shot allocation at a specific
// ID without committing to a persistent range.
//
// The recycle queue is checked first; if no in-range recycled ID exists, a
// fresh ID is allocated. If min > current maxID+1, maxID is advanced to
// min−1 and min is issued (the gap IDs are permanently skipped — this is
// intentional; RangeNew is a hammer). If the range is exhausted at call time,
// RangeNew panics.
//
// Panics if called in a deferred scope, if min < 1, if max <= min, or if min
// or max exceed the uint32 entity-index width.
//
// Mirrors upstream ecs_entity_range_new (src/world.c:1462-1489) reduced to a
// single one-shot allocation.
func RangeNew(fw *Writer, min, max ID) ID {
	if fw.stage.deferDepth > 0 {
		panic("flecs: RangeNew: cannot be called in a deferred scope")
	}
	if min.Index() < 1 {
		panic(fmt.Sprintf("flecs: RangeNew: min must be >= 1, got %d", min.Index()))
	}
	if max.Index() <= min.Index() {
		panic(fmt.Sprintf("flecs: RangeNew: max (%d) must be > min (%d)", max.Index(), min.Index()))
	}
	e := fw.world.index.AllocInRange(min.Index(), max.Index())
	rec := fw.world.index.Get(e)
	rec.Table = fw.world.empty
	rec.Row = uint32(fw.world.empty.Append(e))
	if len(fw.scopeStack) > 0 {
		AddID(fw, e, MakePair(fw.world.childOfID, fw.scopeStack[len(fw.scopeStack)-1]))
	}
	return e
}
