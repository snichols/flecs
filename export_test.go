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

// DeferBeginForTest increments stages[0].deferDepth. For defer-behavior tests only.
func DeferBeginForTest(w *World) {
	w.stages[0].deferDepth++
}

// DeferEndForTest decrements stages[0].deferDepth and flushes if depth reaches zero.
// Panics if called without a matching DeferBeginForTest.
func DeferEndForTest(w *World) {
	s0 := w.stages[0]
	if s0.deferDepth <= 0 {
		panic("flecs: DeferEndForTest called without matching DeferBeginForTest")
	}
	s0.deferDepth--
	if s0.deferDepth > 0 {
		return
	}
	q := s0.queue
	s0.queue = acquireCmdQueue()
	q.flush(w)
	releaseCmdQueue(q)
}

// DeferForTest wraps fn in DeferBeginForTest/DeferEndForTest.
func DeferForTest(w *World, fn func()) {
	DeferBeginForTest(w)
	defer DeferEndForTest(w)
	fn()
}

// WriterForTest returns the world's cached *Writer for use inside manually
// opened deferred scopes (DeferBeginForTest). For tests only.
func WriterForTest(w *World) *Writer {
	return &w.writeCapability
}

// BuiltinPhaseEntityIDs returns the entity IDs of the four built-in pipeline
// phases (PreUpdate, OnUpdate, PostUpdate, OnFixedUpdate) allocated in New().
// These entities exist at indices 4-7 in the world's entity index but are no
// longer exposed via the public World.PreUpdate() etc. accessors (which now
// return *Phase). For tests only: used to build accurate built-in skip sets.
func BuiltinPhaseEntityIDs(w *World) []ID {
	return []ID{w.preUpdateID, w.onUpdateID, w.postUpdateID, w.onFixedUpdateID}
}

// SnapshotBlob returns s's internal payload blob. For snapshot tests only.
func SnapshotBlob(s *Snapshot) []byte { return s.blob }

// SnapshotWorldID returns s's world-identity token. For snapshot tests only.
func SnapshotWorldID(s *Snapshot) uint64 { return s.worldID }

// NewSnapshotRaw creates a Snapshot from raw worldID and blob. For snapshot
// truncation/corruption tests only.
func NewSnapshotRaw(worldID uint64, blob []byte) *Snapshot {
	return &Snapshot{blob: blob, worldID: worldID}
}

// InjectUnitCycle injects a cycle into the world's unit registry for testing
// the cycle-detection path in rootFactor. For units tests only.
// After calling this, Convert between the two unit IDs will return ok=false.
func InjectUnitCycle(w *World, a, b ID) {
	uA := w.unitDefs[a]
	uB := w.unitDefs[b]
	uA.Base = b
	uB.Base = a
	uA.Factor = 1
	uB.Factor = 1
	w.unitDefs[a] = uA
	w.unitDefs[b] = uB
}

// InjectCompoundCycle injects a cycle into the compound unit registry so that
// a's compound def now includes b as a factor. For compound cycle tests only.
func InjectCompoundCycle(w *World, b, a ID) {
	if w.compoundDefs == nil {
		w.compoundDefs = make(map[ID]*compoundDef)
	}
	// Set a's compound def to reference b, creating a↔b cycle.
	w.compoundDefs[a] = &compoundDef{numerators: []ID{b}}
}

// ZeroUnitSymbolForTest clears the Symbol field of unitID's unit def.
// For testing UnitSymbol's autoSymbol fallback path (line 136) only.
func ZeroUnitSymbolForTest(w *World, id ID) {
	if u, ok := w.unitDefs[id]; ok {
		u.Symbol = ""
		w.unitDefs[id] = u
	}
}

// InjectCompoundDefFull replaces id's compound def with the given num/denom lists.
// For targeted siCanonical and validateCompound denom-cycle tests only.
func InjectCompoundDefFull(w *World, id ID, num, denom []ID) {
	if w.compoundDefs == nil {
		w.compoundDefs = make(map[ID]*compoundDef)
	}
	w.compoundDefs[id] = &compoundDef{numerators: num, denominators: denom}
}
