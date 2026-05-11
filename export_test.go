package flecs

import (
	"github.com/snichols/flecs/internal/component"
	"github.com/snichols/flecs/internal/storage/table"
)

// Registry returns the world's component registry. For tests only.
func (w *World) Registry() *component.Registry { return w.registry }

// QueryIterCandidateCount returns the number of seed-table candidates inside
// it. For tests only: used to verify that Iter() seeds from the smallest set.
func QueryIterCandidateCount(it *QueryIter) int {
	return len(it.candidates)
}

// TableOf returns the archetype table pointer for entity e. For tests only.
func TableOf(w *World, e ID) *table.Table {
	rec := w.index.Get(e)
	if rec == nil {
		return nil
	}
	return rec.Table
}

// EachTableForCount calls EachTableFor for componentID and returns the number
// of tables visited. If stopAfter > 0, iteration stops after that many visits.
// For tests only.
func EachTableForCount(w *World, componentID ID, stopAfter int) int {
	count := 0
	w.EachTableFor(componentID, func(_ *table.Table) bool {
		count++
		if stopAfter > 0 && count >= stopAfter {
			return false
		}
		return true
	})
	return count
}

// CachedQuerySliceLen returns the length of the world's internal cachedQueries
// slice, including entries that have been marked removed but not yet compacted.
// For tests only: used to verify amortized compaction behaviour.
func CachedQuerySliceLen(w *World) int {
	return len(w.cachedQueries)
}

// SystemSliceLen returns the length of the world's internal systems slice,
// including entries that have been marked removed but not yet compacted.
// For tests only: used to verify amortized compaction behaviour.
func SystemSliceLen(w *World) int {
	return len(w.systems)
}

// ReaderEachTableForCount calls EachTableFor on a Reader for componentID and
// returns the number of tables visited. For tests only.
func ReaderEachTableForCount(w *World, componentID ID, stopAfter int) int {
	r := &Reader{world: w}
	count := 0
	r.EachTableFor(componentID, func(_ *table.Table) bool {
		count++
		if stopAfter > 0 && count >= stopAfter {
			return false
		}
		return true
	})
	return count
}

// DeferBeginForTest increments deferDepth. For defer-behavior tests only.
func DeferBeginForTest(w *World) {
	w.deferMu.Lock()
	w.deferDepth++
	w.deferMu.Unlock()
}

// DeferEndForTest decrements deferDepth and flushes if depth reaches zero.
// Panics if called without a matching DeferBeginForTest.
func DeferEndForTest(w *World) {
	w.deferMu.Lock()
	if w.deferDepth <= 0 {
		w.deferMu.Unlock()
		panic("flecs: DeferEndForTest called without matching DeferBeginForTest")
	}
	w.deferDepth--
	if w.deferDepth > 0 {
		w.deferMu.Unlock()
		return
	}
	q := w.deferred
	w.deferred = acquireCmdQueue()
	w.deferMu.Unlock()
	q.flush(w)
	releaseCmdQueue(q)
}

// DeferForTest wraps fn in DeferBeginForTest/DeferEndForTest.
func DeferForTest(w *World, fn func()) {
	DeferBeginForTest(w)
	defer DeferEndForTest(w)
	fn()
}
