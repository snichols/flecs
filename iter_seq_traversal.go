package flecs

import (
	"context"
	"iter"
	"unsafe"

	"github.com/snichols/flecs/internal/component"
	"github.com/snichols/flecs/internal/storage/table"
)

// Entities returns an [iter.Seq][ID] that yields each alive entity in dense
// allocation order. Break is fully supported.
//
// Mutation during iteration has undefined behavior — the same contract as
// [(*Reader).EachEntity]. For a snapshot-safe enumeration use
// [(*Reader).AliveEntities].
//
//	w.Read(func(r *flecs.Reader) {
//	    for e := range r.Entities() {
//	        fmt.Println(e)
//	    }
//	})
func (r *Reader) Entities() iter.Seq[ID] {
	return func(yield func(ID) bool) {
		r.EachEntity(func(e ID) bool {
			return yield(e)
		})
	}
}

// EntitiesContext returns an [iter.Seq2][ID, error] that yields (id, nil) for
// each alive entity. If ctx is cancelled, it yields (0, ctx.Err()) once and
// stops. Checks ctx every [ctxCheckInterval] entities, matching the cadence of
// [(*Query).EachContext].
//
//	w.Read(func(r *flecs.Reader) {
//	    for id, err := range r.EntitiesContext(ctx) {
//	        if err != nil { return err }
//	        // use id
//	    }
//	})
func (r *Reader) EntitiesContext(ctx context.Context) iter.Seq2[ID, error] {
	return func(yield func(ID, error) bool) {
		select {
		case <-ctx.Done():
			yield(0, ctx.Err())
			return
		default:
		}
		n := 0
		r.EachEntity(func(e ID) bool {
			if !yield(e, nil) {
				return false
			}
			n++
			if n >= ctxCheckInterval {
				n = 0
				select {
				case <-ctx.Done():
					yield(0, ctx.Err())
					return false
				default:
				}
			}
			return true
		})
	}
}

// Children returns an [iter.Seq][ID] that yields each direct child of parent.
// If parent has the [OrderedChildren] trait, children are yielded in insertion
// order; otherwise archetype-derived order, consistent with
// [(*Reader).EachChild]. Break is fully supported.
//
// ctx variant omitted — child population is bounded by a single parent;
// cancellation latency is negligible.
//
//	w.Read(func(r *flecs.Reader) {
//	    for child := range r.Children(parent) {
//	        fmt.Println(child)
//	    }
//	})
func (r *Reader) Children(parent ID) iter.Seq[ID] {
	return func(yield func(ID) bool) {
		r.EachChild(parent, func(id ID) bool {
			return yield(id)
		})
	}
}

// Prefabs returns an [iter.Seq][ID] that yields each direct IsA prefab of
// entity e. Only direct prefabs are visited (not the transitive IsA chain),
// consistent with [(*Reader).EachPrefab]. Break is fully supported.
//
// ctx variant omitted — IsA targets per entity are tiny.
//
//	w.Read(func(r *flecs.Reader) {
//	    for prefab := range r.Prefabs(e) {
//	        fmt.Println(prefab)
//	    }
//	})
func (r *Reader) Prefabs(e ID) iter.Seq[ID] {
	return func(yield func(ID) bool) {
		r.EachPrefab(e, func(prefab ID) bool {
			return yield(prefab)
		})
	}
}

// Systems returns an [iter.Seq][*System] that yields each registered
// non-closed system in phase, in topological (DependsOn) order with
// registration order as the tiebreaker. Disabled systems are included.
// Break is fully supported. Panics if phase is nil.
//
// ctx variant omitted — system count per phase is bounded and small.
//
//	w.Read(func(r *flecs.Reader) {
//	    for sys := range r.Systems(w.OnUpdate()) {
//	        fmt.Println(sys.Name())
//	    }
//	})
func (r *Reader) Systems(phase *Phase) iter.Seq[*System] {
	return func(yield func(*System) bool) {
		r.EachSystem(phase, func(s *System) bool {
			return yield(s)
		})
	}
}

// Union returns an [iter.Seq2][ID, ID] that yields each (entity, target) pair
// active for relID in insertion order, consistent with [EachUnion].
// Break is fully supported: yield returning false stops iteration immediately
// without visiting further entries.
//
// ctx variant omitted — union membership per relationship is bounded.
//
//	w.Read(func(r *flecs.Reader) {
//	    for e, target := range flecs.Union(r, movementRel) {
//	        fmt.Println(e, target)
//	    }
//	})
func Union(s scope, relID ID) iter.Seq2[ID, ID] {
	return func(yield func(ID, ID) bool) {
		w := s.scopeWorld()
		store, ok := w.unionStore[ID(relID.Index())]
		if !ok {
			return
		}
		snap := make([]unionEntry, len(store.dense))
		copy(snap, store.dense)
		for _, entry := range snap {
			if !yield(entry.entity, entry.target) {
				return
			}
		}
	}
}

// Sparse returns an [iter.Seq2][ID, *T] that yields each (entity, *T) pair in
// dense (insertion) order, consistent with [EachSparse]. The pointer is the
// stable boxed pointer to the entity's component data, valid for the world's
// lifetime. Break is fully supported.
//
// ctx variant omitted — wrap with a break guard if cancellation is needed.
//
//	w.Read(func(r *flecs.Reader) {
//	    for e, pos := range flecs.Sparse[Position](r) {
//	        fmt.Println(e, pos.X, pos.Y)
//	    }
//	})
func Sparse[T any](s scope) iter.Seq2[ID, *T] {
	return func(yield func(ID, *T) bool) {
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
		snap := make([]sparseEntry, len(ss.dense))
		copy(snap, ss.dense)
		for _, entry := range snap {
			if !yield(entry.entity, (*T)(entry.data)) {
				return
			}
		}
	}
}

// ByID returns an [iter.Seq2][ID, unsafe.Pointer] that yields each
// (entity, ptr) pair for all entities holding componentID, consistent with
// [EachByID]. Handles both the sparse/DontFragment dense-store path and the
// archetype-table path, filtering dead entities exactly as [EachByID] does.
//
// The pointer is valid only for the duration of the yield call.
// For tag components (size == 0) the pointer is nil. Break is fully supported.
//
// For context-aware iteration use [ByIDContext].
//
//	w.Read(func(r *flecs.Reader) {
//	    for e, ptr := range flecs.ByID(r, posID) {
//	        p := (*Position)(ptr)
//	        fmt.Println(e, p.X, p.Y)
//	    }
//	})
func ByID(s scope, componentID ID) iter.Seq2[ID, unsafe.Pointer] {
	return func(yield func(ID, unsafe.Pointer) bool) {
		w := s.scopeWorld()
		w.checkExclusiveAccessRead()
		_, ok := w.registry.LookupByID(componentID)
		if !ok {
			return
		}
		iIdx := ID(componentID.Index())
		if !componentID.IsPair() {
			if w.sparsePolicies[iIdx] || w.dontFragmentPolicies[iIdx] {
				ss, ssOK := w.sparseStorage[iIdx]
				if !ssOK {
					return
				}
				for _, entry := range ss.dense {
					if !yield(entry.entity, entry.data) {
						return
					}
				}
				return
			}
		}
		w.compIndex.Each(componentID, func(t *table.Table) bool {
			entities := t.Entities()
			for i, eid := range entities {
				if w.index.IsAlive(eid) {
					if !yield(eid, t.Get(i, componentID)) {
						return false
					}
				}
			}
			return true
		})
	}
}

// ByIDContext returns an [iter.Seq2][ID, error] that yields (id, nil) for each
// entity holding componentID. If ctx is cancelled, it yields (0, ctx.Err())
// once and stops. Checks ctx every [ctxCheckInterval] entities, matching the
// cadence of [(*Query).EachContext].
//
// The payload pointer is collapsed — this variant yields entity IDs only,
// consistent with [QueryAllContext]. Use [ByID] for pointer access.
//
//	w.Read(func(r *flecs.Reader) {
//	    for id, err := range flecs.ByIDContext(ctx, r, posID) {
//	        if err != nil { return err }
//	        // use id
//	    }
//	})
func ByIDContext(ctx context.Context, s scope, componentID ID) iter.Seq2[ID, error] {
	return func(yield func(ID, error) bool) {
		select {
		case <-ctx.Done():
			yield(0, ctx.Err())
			return
		default:
		}
		w := s.scopeWorld()
		w.checkExclusiveAccessRead()
		_, ok := w.registry.LookupByID(componentID)
		if !ok {
			return
		}
		iIdx := ID(componentID.Index())
		if !componentID.IsPair() {
			if w.sparsePolicies[iIdx] || w.dontFragmentPolicies[iIdx] {
				ss, ssOK := w.sparseStorage[iIdx]
				if !ssOK {
					return
				}
				n := 0
				for _, entry := range ss.dense {
					if !yield(entry.entity, nil) {
						return
					}
					n++
					if n >= ctxCheckInterval {
						n = 0
						select {
						case <-ctx.Done():
							yield(0, ctx.Err())
							return
						default:
						}
					}
				}
				return
			}
		}
		n := 0
		w.compIndex.Each(componentID, func(t *table.Table) bool {
			for _, eid := range t.Entities() {
				if w.index.IsAlive(eid) {
					if !yield(eid, nil) {
						return false
					}
				}
			}
			n++
			if n >= ctxCheckInterval {
				n = 0
				select {
				case <-ctx.Done():
					yield(0, ctx.Err())
					return false
				default:
				}
			}
			return true
		})
	}
}
