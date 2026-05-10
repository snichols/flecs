package flecs

// Each1 calls fn once for every entity that has component A.
//
// Auto-registration: if A has not been registered with w, it is registered
// before the query runs (matching the Set/Has convention). This diverges from
// Get, which does NOT auto-register.
//
// Pointer lifetime: the pointers passed to fn point into the live column slot
// for the current entity. They are valid only for the duration of that fn call;
// do not retain them across calls.
//
// Concurrent modification: calling Set, Remove, or Delete on w from inside fn
// produces undefined behaviour.
//
// Iteration order: dense within each archetype table; across tables, undefined.
func Each1[A any](w *World, fn func(e ID, a *A)) {
	var ids [1]ID
	ids[0] = RegisterComponent[A](w)
	q := NewQuery(w, ids[:]...)
	it := q.Iter()
	for it.Next() {
		colA := Field[A](it, ids[0])
		for i, e := range it.Entities() {
			fn(e, &colA[i])
		}
	}
}

// Each2 calls fn once for every entity that has all of components A and B.
//
// Auto-registration: unregistered types are registered before the query runs.
// See Each1 for full semantics on pointer lifetime and concurrent modification.
func Each2[A, B any](w *World, fn func(e ID, a *A, b *B)) {
	var ids [2]ID
	ids[0] = RegisterComponent[A](w)
	ids[1] = RegisterComponent[B](w)
	q := NewQuery(w, ids[:]...)
	it := q.Iter()
	for it.Next() {
		colA := Field[A](it, ids[0])
		colB := Field[B](it, ids[1])
		for i, e := range it.Entities() {
			fn(e, &colA[i], &colB[i])
		}
	}
}

// Each3 calls fn once for every entity that has all of components A, B, and C.
//
// Auto-registration: unregistered types are registered before the query runs.
// See Each1 for full semantics on pointer lifetime and concurrent modification.
func Each3[A, B, C any](w *World, fn func(e ID, a *A, b *B, c *C)) {
	var ids [3]ID
	ids[0] = RegisterComponent[A](w)
	ids[1] = RegisterComponent[B](w)
	ids[2] = RegisterComponent[C](w)
	q := NewQuery(w, ids[:]...)
	it := q.Iter()
	for it.Next() {
		colA := Field[A](it, ids[0])
		colB := Field[B](it, ids[1])
		colC := Field[C](it, ids[2])
		for i, e := range it.Entities() {
			fn(e, &colA[i], &colB[i], &colC[i])
		}
	}
}

// Each4 calls fn once for every entity that has all of components A, B, C, and D.
//
// Auto-registration: unregistered types are registered before the query runs.
// See Each1 for full semantics on pointer lifetime and concurrent modification.
//
// Users needing more than four components should fall back to NewQuery / Iter / Field.
func Each4[A, B, C, D any](w *World, fn func(e ID, a *A, b *B, c *C, d *D)) {
	var ids [4]ID
	ids[0] = RegisterComponent[A](w)
	ids[1] = RegisterComponent[B](w)
	ids[2] = RegisterComponent[C](w)
	ids[3] = RegisterComponent[D](w)
	q := NewQuery(w, ids[:]...)
	it := q.Iter()
	for it.Next() {
		colA := Field[A](it, ids[0])
		colB := Field[B](it, ids[1])
		colC := Field[C](it, ids[2])
		colD := Field[D](it, ids[3])
		for i, e := range it.Entities() {
			fn(e, &colA[i], &colB[i], &colC[i], &colD[i])
		}
	}
}
