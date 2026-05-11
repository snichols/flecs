// Package entityindex implements the entity ID allocator and per-entity record
// map for the flecs Go port. It hands out entity IDs, tracks aliveness, recycles
// freed IDs with bumped generations, and stores per-entity location records.
//
// This is an algorithmic port of the C entity_index in
// flecs/src/storage/entity_index.{h,c}. The Go port uses 32-bit generations
// (vs. 16-bit in the C upstream), replaces the C allocator with Go's GC, and
// uses a separate FIFO recycle queue (vs. the C "dense-tail" approach) to
// guarantee first-freed-first-reused ordering.
package entityindex

import (
	"github.com/snichols/flecs/internal/ids"
	"github.com/snichols/flecs/internal/storage/table"
)

// entityPageSize is the number of records per page, mirroring
// FLECS_ENTITY_PAGE_SIZE (1 << FLECS_ENTITY_PAGE_BITS = 1 << 6 = 64).
const entityPageSize = 64

// entityPageMask indexes a slot within a page.
const entityPageMask = entityPageSize - 1

// Record is the per-entity location record stored in the entity index.
type Record struct {
	// Row is the row index within the entity's table component array.
	Row uint32
	// Dense is the index of this entity's slot in the Index dense vector while
	// the entity is alive. Zero means the entity is not currently alive (never
	// allocated, or has been freed). The generation of the alive entity is
	// encoded in dense[Dense] itself, not in this struct.
	Dense uint32
	// Table is the archetype table this entity currently lives in.
	// Nil until the entity is placed into a table (Phase 1.5+).
	Table *table.Table
}

// page is a fixed-size block of entity records, allocated lazily.
type page [entityPageSize]Record

// Index is the entity index. It owns the dense alive-entity vector, a FIFO
// recycle queue, and a paged sparse record array.
//
// Dense vector layout:
//
//	index 0          : reserved sentinel (always ID(0)); never referenced
//	[1, aliveCount)  : alive entity IDs in their current generations
//
// Index 0 is permanently reserved as the null/invalid entity ID and is never
// returned by Alloc. Alive entities start at index 1.
//
// The recycle queue (separate from dense) stores freed IDs in FIFO order with
// their generations already bumped by 1. Alloc drains the queue before
// allocating a fresh index.
type Index struct {
	dense      []ids.ID // dense[0] = sentinel; [1:aliveCount] = alive entities
	recycle    []ids.ID // FIFO queue of recycled IDs (gen already bumped)
	pages      []*page  // sparse: pages[id>>6] is *[64]Record
	aliveCount int      // length of alive section including sentinel at [0]
	maxID      uint32   // highest raw entity index ever issued
}

// New returns an initialized, empty entity index.
func New() *Index {
	return &Index{
		dense:      make([]ids.ID, 1), // dense[0] = sentinel ID(0)
		aliveCount: 1,
	}
}

// ensurePage returns the page covering raw entity index id, allocating it
// lazily if needed. Pages are never freed or moved; pointers into them remain
// stable for the lifetime of the Index.
func (idx *Index) ensurePage(id uint32) *page {
	pageIdx := int(id >> 6)
	for len(idx.pages) <= pageIdx {
		idx.pages = append(idx.pages, nil)
	}
	if idx.pages[pageIdx] == nil {
		idx.pages[pageIdx] = new(page)
	}
	return idx.pages[pageIdx]
}

// tryGetRecord returns the Record for the given raw entity index, or nil if
// the page has not been allocated or the record's Dense field is zero (entity
// was never allocated or has been freed).
func (idx *Index) tryGetRecord(rawIdx uint32) *Record {
	pageIdx := int(rawIdx >> 6)
	if pageIdx >= len(idx.pages) {
		return nil
	}
	r := &idx.pages[pageIdx][rawIdx&entityPageMask]
	if r.Dense == 0 {
		return nil
	}
	return r
}

// Alloc issues a fresh entity ID.
//
// Alloc first drains the FIFO recycle queue; only when the queue is empty does
// it grow the dense vector with a new raw entity index (starting from 1; index 0
// is permanently reserved as the null/invalid entity sentinel, matching ECS
// convention). The returned ID encodes the raw index in the lower 32 bits and
// the generation counter in the upper 32 bits.
func (idx *Index) Alloc() ids.ID {
	if len(idx.recycle) > 0 {
		// Recycle: pop oldest freed ID (FIFO).
		rID := idx.recycle[0]
		idx.recycle = idx.recycle[1:]

		rawIdx := rID.Index()
		p := idx.ensurePage(rawIdx)
		rec := &p[rawIdx&entityPageMask]
		rec.Dense = uint32(idx.aliveCount)
		// Freeing an entity leaves its slot in dense as stale data without
		// shrinking the slice, so there is always a reusable slot here.
		idx.dense[idx.aliveCount] = rID
		idx.aliveCount++
		return rID
	}

	// Fresh allocation.
	idx.maxID++
	id := ids.MakeEntity(idx.maxID, 0)
	idx.dense = append(idx.dense, id)

	p := idx.ensurePage(idx.maxID)
	rec := &p[idx.maxID&entityPageMask]
	rec.Dense = uint32(idx.aliveCount)
	idx.aliveCount++

	return id
}

// Free releases an entity ID. It returns true if the ID was alive (and is now
// freed), false if the ID was already dead, stale (correct index but old
// generation), or unknown.
//
// The freed entity's record Dense field is zeroed. Its generation-bumped ID is
// appended to the FIFO recycle queue so the next Alloc reuses it with gen+1.
//
// If the entity's generation is already math.MaxUint32, Free panics. Wrapping
// silently would produce duplicate live IDs after 2^32 frees of the same slot;
// panicking is safer for this practically-impossible case.
func (idx *Index) Free(id ids.ID) bool {
	rawIdx := id.Index()
	r := idx.tryGetRecord(rawIdx)
	if r == nil {
		return false
	}
	densePos := int(r.Dense)
	if idx.dense[densePos] != id {
		// Stale handle: slot has been recycled to a newer generation.
		return false
	}

	gen := id.Generation()
	if gen == ^uint32(0) {
		panic("entityindex: generation overflow: entity index freed 2^32 times")
	}

	// Swap with the last alive entity to keep the alive section contiguous.
	lastAlivePos := idx.aliveCount - 1
	if densePos != lastAlivePos {
		swapID := idx.dense[lastAlivePos]
		swapRec := idx.tryGetRecord(swapID.Index())
		swapRec.Dense = uint32(densePos)
		idx.dense[densePos] = swapID
	}

	idx.aliveCount--
	r.Dense = 0 // mark as dead; tryGetRecord returns nil from now on
	r.Table = nil

	// Enqueue the gen-bumped ID for FIFO recycling.
	idx.recycle = append(idx.recycle, ids.MakeEntity(rawIdx, gen+1))
	return true
}

// IsAlive reports whether id is currently alive.
//
// An ID is alive if and only if its raw index has an alive record AND the ID
// stored in dense at that record's Dense position matches id (same generation).
// Index 0 and any never-allocated or freed ID return false.
func (idx *Index) IsAlive(id ids.ID) bool {
	rawIdx := id.Index()
	if rawIdx == 0 {
		return false
	}
	r := idx.tryGetRecord(rawIdx)
	if r == nil {
		return false
	}
	return idx.dense[r.Dense] == id
}

// Get returns a pointer to the Record for an alive id, or nil if id is not
// alive. The returned pointer is read/write; callers may modify Row directly.
//
// Pointer stability: the returned pointer remains valid as long as the page
// that contains the entity is not removed. Pages are fixed-size heap arrays
// ([64]Record) that are never moved or freed; growing the pages slice only
// updates the slice header, leaving existing *page values unchanged. A pointer
// obtained via Get is therefore safe to hold across any number of Alloc/Free
// calls that do not affect that specific page.
func (idx *Index) Get(id ids.ID) *Record {
	rawIdx := id.Index()
	if rawIdx == 0 {
		return nil
	}
	r := idx.tryGetRecord(rawIdx)
	if r == nil {
		return nil
	}
	if idx.dense[r.Dense] != id {
		return nil
	}
	return r
}

// Count returns the number of currently alive entities.
func (idx *Index) Count() int {
	return idx.aliveCount - 1 // subtract the sentinel at dense[0]
}

// Each calls fn for every alive entity in dense order (dense[1:aliveCount]).
//
// Callbacks must not call Alloc or Free during iteration. Doing so modifies the
// dense slice in ways that may skip or double-visit entries. If early exit is
// needed, use a separate boolean flag inside fn.
func (idx *Index) Each(fn func(id ids.ID, rec *Record)) {
	for i := 1; i < idx.aliveCount; i++ {
		id := idx.dense[i]
		rawIdx := id.Index()
		p := idx.pages[rawIdx>>6]
		rec := &p[rawIdx&entityPageMask]
		fn(id, rec)
	}
}

// EachID calls fn for every alive entity in dense order (dense[1:aliveCount]).
// fn returns false to stop iteration early. Behavior is undefined if fn calls
// Alloc or Free during iteration.
func (idx *Index) EachID(fn func(id ids.ID) bool) {
	for i := 1; i < idx.aliveCount; i++ {
		if !fn(idx.dense[i]) {
			return
		}
	}
}
