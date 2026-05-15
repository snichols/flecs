package flecs

import (
	"context"
	"iter"
)

// Pair holds pointers to two component values yielded by [All2].
// The pointers are valid only within the body of the yield call; any mutation
// that triggers a table migration (Add/Remove/Set on a new component) may
// invalidate them. Dereference and stack-copy the values before mutating the
// world.
type Pair[A, B any] struct {
	A *A
	B *B
}

// Triple holds pointers to three component values yielded by [All3].
// The same pointer-stability caveat as [Pair] applies.
type Triple[A, B, C any] struct {
	A *A
	B *B
	C *C
}

// Quad holds pointers to four component values yielded by [All4].
// The same pointer-stability caveat as [Pair] applies.
type Quad[A, B, C, D any] struct {
	A *A
	B *B
	C *C
	D *D
}

// QueryAll returns an [iter.Seq][ID] that yields each entity ID matched by q.
// Break is fully supported: breaking from the range loop cleanly terminates
// iteration without visiting further tables or entities.
//
//	for e := range flecs.QueryAll(q, r) {
//	    // use e
//	}
func QueryAll(q *Query, _ scope) iter.Seq[ID] {
	return func(yield func(ID) bool) {
		it := q.Iter()
		for it.Next() {
			for _, e := range it.Entities() {
				if !yield(e) {
					return
				}
			}
		}
	}
}

// CachedQueryAll returns an [iter.Seq][ID] that yields each entity ID matched
// by the cached query cq. Break is fully supported.
//
//	for e := range flecs.CachedQueryAll(cq, r) {
//	    // use e
//	}
func CachedQueryAll(cq *CachedQuery, _ scope) iter.Seq[ID] {
	return func(yield func(ID) bool) {
		it := cq.Iter()
		for it.Next() {
			for _, e := range it.Entities() {
				if !yield(e) {
					return
				}
			}
		}
	}
}

// QueryAllContext returns an [iter.Seq2][ID, error] that yields (id, nil) for
// each entity matched by q. If ctx is cancelled, it yields (0, ctx.Err()) once
// and stops. Checks ctx every [ctxCheckInterval] tables, matching the cadence
// of [(*Query).EachContext].
//
//	for id, err := range flecs.QueryAllContext(ctx, q, r) {
//	    if err != nil { return err }
//	    // use id
//	}
func QueryAllContext(ctx context.Context, q *Query, _ scope) iter.Seq2[ID, error] {
	return func(yield func(ID, error) bool) {
		select {
		case <-ctx.Done():
			yield(0, ctx.Err())
			return
		default:
		}
		it := q.Iter()
		n := 0
		for it.Next() {
			for _, e := range it.Entities() {
				if !yield(e, nil) {
					return
				}
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
	}
}

// CachedQueryAllContext returns an [iter.Seq2][ID, error] that yields (id, nil)
// for each entity matched by cq. If ctx is cancelled, it yields (0, ctx.Err())
// once and stops. Checks ctx every [ctxCheckInterval] tables.
func CachedQueryAllContext(ctx context.Context, cq *CachedQuery, _ scope) iter.Seq2[ID, error] {
	return func(yield func(ID, error) bool) {
		select {
		case <-ctx.Done():
			yield(0, ctx.Err())
			return
		default:
		}
		it := cq.Iter()
		n := 0
		for it.Next() {
			for _, e := range it.Entities() {
				if !yield(e, nil) {
					return
				}
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
	}
}

// All1 returns an [iter.Seq2][ID, *A] that yields each entity and a pointer
// to its A component. The pointer is valid only within the body of the yield
// call; any mutation that triggers a table migration (Add/Remove/Set on a new
// component) may invalidate it. Dereference and stack-copy the value before
// mutating the world.
//
// If A is marked inheritable via [SetInheritable], the query automatically
// matches entities that inherit A from a prefab via IsA. In that case the same
// prefab pointer is yielded for every entity in the matched table.
//
// Break is fully supported: breaking from the range loop cleanly terminates
// iteration without visiting further tables or entities.
//
//	w.Read(func(r *flecs.Reader) {
//	    for e, pos := range flecs.All1[Position](r) {
//	        fmt.Println(e, *pos)
//	    }
//	})
func All1[A any](s scope) iter.Seq2[ID, *A] {
	return func(yield func(ID, *A) bool) {
		w := s.scopeWorld()
		var ids [1]ID
		ids[0] = RegisterComponent[A](w)
		toggleA := w.canTogglePolicies[ID(ids[0].Index())]
		q := NewQuery(w, ids[:]...)
		it := q.Iter()
		for it.Next() {
			if aShared := upPtr[A](w, it, ids[0]); aShared != nil {
				for _, e := range it.Entities() {
					if !yield(e, aShared) {
						return
					}
				}
				continue
			}
			colA := Field[A](it, ids[0])
			for i, e := range it.Entities() {
				if toggleA && !it.current.IsRowEnabled(ids[0], i) {
					continue
				}
				if !yield(e, &colA[i]) {
					return
				}
			}
		}
	}
}

// All2 returns an [iter.Seq2][ID, [Pair][A, B]] that yields each entity and
// pointers to its A and B components. The pointers are valid only within the
// yield call body; see [All1] for the pointer-stability caveat.
//
//	w.Read(func(r *flecs.Reader) {
//	    for e, p := range flecs.All2[Position, Velocity](r) {
//	        pos, vel := p.A, p.B
//	        _ = pos.X + vel.DX
//	    }
//	})
func All2[A, B any](s scope) iter.Seq2[ID, Pair[A, B]] {
	return func(yield func(ID, Pair[A, B]) bool) {
		w := s.scopeWorld()
		var ids [2]ID
		ids[0] = RegisterComponent[A](w)
		ids[1] = RegisterComponent[B](w)
		toggleA := w.canTogglePolicies[ID(ids[0].Index())]
		toggleB := w.canTogglePolicies[ID(ids[1].Index())]
		q := NewQuery(w, ids[:]...)
		it := q.Iter()
		for it.Next() {
			aShared := upPtr[A](w, it, ids[0])
			bShared := upPtr[B](w, it, ids[1])
			if aShared == nil && bShared == nil {
				colA := Field[A](it, ids[0])
				colB := Field[B](it, ids[1])
				for i, e := range it.Entities() {
					if (toggleA && !it.current.IsRowEnabled(ids[0], i)) ||
						(toggleB && !it.current.IsRowEnabled(ids[1], i)) {
						continue
					}
					if !yield(e, Pair[A, B]{A: &colA[i], B: &colB[i]}) {
						return
					}
				}
				continue
			}
			var colA []A
			if aShared == nil {
				colA = Field[A](it, ids[0])
			}
			var colB []B
			if bShared == nil {
				colB = Field[B](it, ids[1])
			}
			for i, e := range it.Entities() {
				if (toggleA && aShared == nil && !it.current.IsRowEnabled(ids[0], i)) ||
					(toggleB && bShared == nil && !it.current.IsRowEnabled(ids[1], i)) {
					continue
				}
				a := aShared
				if a == nil {
					a = &colA[i]
				}
				b := bShared
				if b == nil {
					b = &colB[i]
				}
				if !yield(e, Pair[A, B]{A: a, B: b}) {
					return
				}
			}
		}
	}
}

// All3 returns an [iter.Seq2][ID, [Triple][A, B, C]] that yields each entity
// and pointers to its A, B, and C components. The pointers are valid only
// within the yield call body; see [All1] for the pointer-stability caveat.
func All3[A, B, C any](s scope) iter.Seq2[ID, Triple[A, B, C]] {
	return func(yield func(ID, Triple[A, B, C]) bool) {
		w := s.scopeWorld()
		var ids [3]ID
		ids[0] = RegisterComponent[A](w)
		ids[1] = RegisterComponent[B](w)
		ids[2] = RegisterComponent[C](w)
		toggleA := w.canTogglePolicies[ID(ids[0].Index())]
		toggleB := w.canTogglePolicies[ID(ids[1].Index())]
		toggleC := w.canTogglePolicies[ID(ids[2].Index())]
		q := NewQuery(w, ids[:]...)
		it := q.Iter()
		for it.Next() {
			aShared := upPtr[A](w, it, ids[0])
			bShared := upPtr[B](w, it, ids[1])
			cShared := upPtr[C](w, it, ids[2])
			if aShared == nil && bShared == nil && cShared == nil {
				colA := Field[A](it, ids[0])
				colB := Field[B](it, ids[1])
				colC := Field[C](it, ids[2])
				for i, e := range it.Entities() {
					if (toggleA && !it.current.IsRowEnabled(ids[0], i)) ||
						(toggleB && !it.current.IsRowEnabled(ids[1], i)) ||
						(toggleC && !it.current.IsRowEnabled(ids[2], i)) {
						continue
					}
					if !yield(e, Triple[A, B, C]{A: &colA[i], B: &colB[i], C: &colC[i]}) {
						return
					}
				}
				continue
			}
			var colA []A
			if aShared == nil {
				colA = Field[A](it, ids[0])
			}
			var colB []B
			if bShared == nil {
				colB = Field[B](it, ids[1])
			}
			var colC []C
			if cShared == nil {
				colC = Field[C](it, ids[2])
			}
			for i, e := range it.Entities() {
				if (toggleA && aShared == nil && !it.current.IsRowEnabled(ids[0], i)) ||
					(toggleB && bShared == nil && !it.current.IsRowEnabled(ids[1], i)) ||
					(toggleC && cShared == nil && !it.current.IsRowEnabled(ids[2], i)) {
					continue
				}
				a := aShared
				if a == nil {
					a = &colA[i]
				}
				b := bShared
				if b == nil {
					b = &colB[i]
				}
				c := cShared
				if c == nil {
					c = &colC[i]
				}
				if !yield(e, Triple[A, B, C]{A: a, B: b, C: c}) {
					return
				}
			}
		}
	}
}

// All4 returns an [iter.Seq2][ID, [Quad][A, B, C, D]] that yields each entity
// and pointers to its A, B, C, and D components. The pointers are valid only
// within the yield call body; see [All1] for the pointer-stability caveat.
func All4[A, B, C, D any](s scope) iter.Seq2[ID, Quad[A, B, C, D]] {
	return func(yield func(ID, Quad[A, B, C, D]) bool) {
		w := s.scopeWorld()
		var ids [4]ID
		ids[0] = RegisterComponent[A](w)
		ids[1] = RegisterComponent[B](w)
		ids[2] = RegisterComponent[C](w)
		ids[3] = RegisterComponent[D](w)
		toggleA := w.canTogglePolicies[ID(ids[0].Index())]
		toggleB := w.canTogglePolicies[ID(ids[1].Index())]
		toggleC := w.canTogglePolicies[ID(ids[2].Index())]
		toggleD := w.canTogglePolicies[ID(ids[3].Index())]
		q := NewQuery(w, ids[:]...)
		it := q.Iter()
		for it.Next() {
			aShared := upPtr[A](w, it, ids[0])
			bShared := upPtr[B](w, it, ids[1])
			cShared := upPtr[C](w, it, ids[2])
			dShared := upPtr[D](w, it, ids[3])
			if aShared == nil && bShared == nil && cShared == nil && dShared == nil {
				colA := Field[A](it, ids[0])
				colB := Field[B](it, ids[1])
				colC := Field[C](it, ids[2])
				colD := Field[D](it, ids[3])
				for i, e := range it.Entities() {
					if (toggleA && !it.current.IsRowEnabled(ids[0], i)) ||
						(toggleB && !it.current.IsRowEnabled(ids[1], i)) ||
						(toggleC && !it.current.IsRowEnabled(ids[2], i)) ||
						(toggleD && !it.current.IsRowEnabled(ids[3], i)) {
						continue
					}
					if !yield(e, Quad[A, B, C, D]{A: &colA[i], B: &colB[i], C: &colC[i], D: &colD[i]}) {
						return
					}
				}
				continue
			}
			var colA []A
			if aShared == nil {
				colA = Field[A](it, ids[0])
			}
			var colB []B
			if bShared == nil {
				colB = Field[B](it, ids[1])
			}
			var colC []C
			if cShared == nil {
				colC = Field[C](it, ids[2])
			}
			var colD []D
			if dShared == nil {
				colD = Field[D](it, ids[3])
			}
			for i, e := range it.Entities() {
				if (toggleA && aShared == nil && !it.current.IsRowEnabled(ids[0], i)) ||
					(toggleB && bShared == nil && !it.current.IsRowEnabled(ids[1], i)) ||
					(toggleC && cShared == nil && !it.current.IsRowEnabled(ids[2], i)) ||
					(toggleD && dShared == nil && !it.current.IsRowEnabled(ids[3], i)) {
					continue
				}
				a := aShared
				if a == nil {
					a = &colA[i]
				}
				b := bShared
				if b == nil {
					b = &colB[i]
				}
				c := cShared
				if c == nil {
					c = &colC[i]
				}
				d := dShared
				if d == nil {
					d = &colD[i]
				}
				if !yield(e, Quad[A, B, C, D]{A: a, B: b, C: c, D: d}) {
					return
				}
			}
		}
	}
}
