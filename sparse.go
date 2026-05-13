package flecs

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/snichols/flecs/internal/component"
	"github.com/snichols/flecs/internal/storage/table"
)

// sparseEntry is one slot in the dense vector of a sparseSet.
// data is heap-allocated for pointer stability (see sparseSet doc).
type sparseEntry struct {
	entity ID
	data   unsafe.Pointer
}

// sparseSet holds all instances of one Sparse component across all entities.
//
// Layout: dense is the authoritative list in insertion order; index maps each
// entity's raw index to its position in dense. The swap-with-last removal
// strategy keeps dense contiguous for EachSparse iteration.
//
// Pointer stability: each entry's data field points to a heap-allocated copy of
// the component value allocated via reflect.New. The pointer is stable across
// dense slice growth because we never move the allocation — only the sparseEntry
// struct (which contains the pointer) moves. The pointer returned by sparseSetGet
// and EachSparse is therefore valid indefinitely within the world's lifetime,
// independent of archetype migrations on other components.
//
// Modest GC cost is acceptable for the v1 storage path; the allocation is
// amortized over the component's lifetime on the entity.
type sparseSet struct {
	dense    []sparseEntry
	index    map[uint32]int // entity raw-index → dense slot
	typeInfo *component.TypeInfo
	// version is bumped on each structural change (insert of new entry or removal).
	// CachedQuery.Changed() consults this to detect sparse-set mutations without
	// needing to iterate the dense slice.
	version uint64
}

// Sparse returns the ID of the built-in Sparse trait entity (index 34).
//
// Marking a component with Sparse causes its data to be stored in a per-component
// sparse-set keyed by entity ID rather than in the archetype column table. The
// consequences are:
//
//   - Adding/removing a Sparse component does NOT cause an archetype transition
//     for the owning entity — the entity stays in its current archetype table.
//   - The Sparse component's address is pointer-stable: it does not move when
//     other components on the same entity change (archetype migrations do not
//     affect the sparse-set allocation).
//   - HasID and OwnsID consult the sparse-set, not the entity's archetype type.
//   - Query terms naming a Sparse component iterate the sparse-set natively
//     (v0.52.0). Pure-sparse, mixed archetype+sparse, Not, and Optional on sparse
//     terms are all supported. See docs/Queries.md for worked examples.
//
// Note: Go-flecs Sparse consolidates the upstream C EcsSparse + EcsIdDontFragment
// behaviors into a single trait. When DontFragment lands in a later phase, the
// split can be revisited. This consolidation is documented in CHANGELOG.md v0.51.0.
//
// Usage:
//
//	posID := flecs.RegisterComponent[Position](w)
//	flecs.SetSparse(w, posID)
//	// or bare-tag form:
//	fw.AddID(posID, w.Sparse())
func (w *World) Sparse() ID { return w.sparseID }

// SetSparse marks componentID as Sparse-stored. Idempotent.
//
// Panics if:
//   - componentID is not a registered data-bearing component
//   - The component has already been added to any entity via archetype storage —
//     marking an in-use component as Sparse would leave existing data in an
//     inconsistent state. Mark the component Sparse before using it.
func SetSparse(w *World, componentID ID) {
	info, ok := w.registry.LookupByID(componentID)
	if !ok || info.Size == 0 {
		panic(fmt.Sprintf("flecs: SetSparse: %v is not a registered data-bearing component", componentID))
	}
	// After-use trap: panic if the component is already held by any entity via archetype.
	var inUse int
	w.compIndex.Each(componentID, func(t *table.Table) bool {
		inUse += t.Count()
		return true
	})
	if inUse > 0 {
		panic(fmt.Sprintf(
			"flecs: cannot mark %v as Sparse: component is already in use on %d entities via archetype storage",
			componentID, inUse,
		))
	}
	applySparsePolicy(w, componentID)
}

// IsSparse reports whether componentID has the Sparse trait.
// Accepts scope so it can be called inside both Read and Write blocks.
func IsSparse(s scope, componentID ID) bool {
	w := s.scopeWorld()
	return w.sparsePolicies[ID(componentID.Index())]
}

// applySparsePolicy writes the Sparse flag for componentID.
// Called by SetSparse and by addIDImmediate when the bare Sparse tag is added
// to a component entity (fw.AddID(posID, w.Sparse())).
// Idempotent: a second call on an already-sparse component is a no-op.
func applySparsePolicy(w *World, componentID ID) {
	if w.sparsePolicies == nil {
		w.sparsePolicies = make(map[ID]bool)
	}
	key := ID(componentID.Index())
	if w.sparsePolicies[key] {
		return
	}
	w.sparsePolicies[key] = true
	if w.sparseStorage == nil {
		w.sparseStorage = make(map[ID]*sparseSet)
	}
	info, _ := w.registry.LookupByID(componentID)
	w.sparseStorage[key] = &sparseSet{
		index:    make(map[uint32]int),
		typeInfo: info,
	}
}

// sparseSetInsert inserts or updates the value at srcPtr for entity e in the
// sparse-set for componentID. Caller is responsible for firing hooks.
func sparseSetInsert(w *World, e ID, componentID ID, srcPtr unsafe.Pointer) {
	key := ID(componentID.Index())
	ss := w.sparseStorage[key]
	info := ss.typeInfo
	eIdx := e.Index()

	if slot, ok := ss.index[eIdx]; ok {
		// Update existing slot in-place; pointer is stable. Version not bumped for updates.
		dst := ss.dense[slot].data
		copy(unsafe.Slice((*byte)(dst), info.Size), unsafe.Slice((*byte)(srcPtr), info.Size))
	} else {
		// New entry: allocate a heap copy for pointer stability. Bump version so
		// CachedQuery.Changed() detects the structural change.
		pv := reflect.New(info.Type)
		dst := pv.UnsafePointer()
		copy(unsafe.Slice((*byte)(dst), info.Size), unsafe.Slice((*byte)(srcPtr), info.Size))
		slot := len(ss.dense)
		ss.dense = append(ss.dense, sparseEntry{entity: e, data: dst})
		ss.index[eIdx] = slot
		ss.version++
		// Record that e now holds this component (for O(k) entity-delete cleanup).
		if w.sparseHeld == nil {
			w.sparseHeld = make(map[uint32][]ID)
		}
		w.sparseHeld[eIdx] = append(w.sparseHeld[eIdx], componentID)
	}
}

// sparseSetGet returns a pointer to the data for entity e in the set for
// componentID, or nil if e does not hold the component.
func sparseSetGet(w *World, e ID, componentID ID) unsafe.Pointer {
	key := ID(componentID.Index())
	ss, ok := w.sparseStorage[key]
	if !ok {
		return nil
	}
	slot, ok := ss.index[e.Index()]
	if !ok {
		return nil
	}
	return ss.dense[slot].data
}

// sparseSetRemove removes entity e's entry from the sparse-set for componentID.
// Uses swap-with-last for O(1) removal. Caller fires OnRemove before calling this.
func sparseSetRemove(w *World, e ID, componentID ID) {
	key := ID(componentID.Index())
	ss, ok := w.sparseStorage[key]
	if !ok {
		return
	}
	eIdx := e.Index()
	slot, ok := ss.index[eIdx]
	if !ok {
		return
	}
	last := len(ss.dense) - 1
	if slot != last {
		lastEntry := ss.dense[last]
		ss.dense[slot] = lastEntry
		ss.index[lastEntry.entity.Index()] = slot
	}
	ss.dense = ss.dense[:last]
	delete(ss.index, eIdx)
	ss.version++ // structural change; CachedQuery.Changed() detects this

	// Update sparseHeld so entity-delete cleanup is accurate.
	if held, ok := w.sparseHeld[eIdx]; ok {
		for i, cid := range held {
			if cid.Index() == componentID.Index() {
				lastIdx := len(held) - 1
				held[i] = held[lastIdx]
				w.sparseHeld[eIdx] = held[:lastIdx]
				if len(w.sparseHeld[eIdx]) == 0 {
					delete(w.sparseHeld, eIdx)
				}
				break
			}
		}
	}
}

// isSparseTermID reports whether a query term with the given id refers to a
// component stored in sparse-set storage. For non-pair IDs, this checks the
// sparse policy directly. For pair IDs, pairs are stored in archetype tables in
// v0.52.0 regardless of whether the relationship is also a sparse scalar
// component — so pair terms are never considered sparse.
func isSparseTermID(w *World, id ID) bool {
	if id.IsPair() {
		return false // pairs are archetype-stored in v0.52.0
	}
	return w.sparsePolicies[ID(id.Index())]
}

// EachSparse iterates all entities that hold component T as Sparse, calling fn
// for each in dense (insertion) order. The pointer v is the stable boxed pointer
// to the entity's component data.
//
// Iteration order is dense (insertion order in the sparse-set); this is
// deterministic and predictable, unlike archetype table order which varies with
// entity composition changes.
func EachSparse[T any](s scope, fn func(e ID, v *T)) {
	w := s.scopeWorld()
	info, ok := component.LookupByType[T](w.registry)
	if !ok || info.Component == 0 {
		return
	}
	key := ID(info.Component.Index())
	ss, ok := w.sparseStorage[key]
	if !ok {
		return
	}
	// Snapshot to allow safe mutation during callback.
	snap := make([]sparseEntry, len(ss.dense))
	copy(snap, ss.dense)
	for _, entry := range snap {
		fn(entry.entity, (*T)(entry.data))
	}
}
