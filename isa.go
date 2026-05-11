package flecs

// IsA returns the built-in IsA relationship entity.
//
// Use MakePair(w.IsA(), prefab) to express that an entity inherits components
// from prefab. Reads (Get/Has) consult the IsA chain transitively on a local
// miss; writes (Set/AddID) always land locally on the child (copy-on-write
// override). The IsA entity is always alive for the lifetime of the World.
func (w *World) IsA() ID { return w.isAID }

// PrefabOf returns the first IsA prefab of entity e — the target of the first
// (IsA, *) pair found in e's archetype signature.
//
// Returns (0, false) if e is not alive or has no IsA relationship.
func PrefabOf(w *World, e ID) (ID, bool) {
	w.checkExclusiveAccessRead()
	rec := w.index.Get(e)
	if rec == nil || rec.Table == nil {
		return 0, false
	}
	return firstPairTarget(rec.Table.Type(), w.isAID.Index())
}

// EachPrefab calls fn for every direct IsA prefab of entity e — the target of
// each (IsA, prefab) pair in e's archetype signature. fn returns false to stop
// early.
//
// EachPrefab is DIRECT only: it iterates the immediate (IsA, *) pairs on e and
// does not walk multi-level chains. To traverse the full chain, call EachPrefab
// recursively on each yielded prefab.
func (w *World) EachPrefab(e ID, fn func(prefab ID) bool) {
	w.checkExclusiveAccessRead()
	rec := w.index.Get(e)
	if rec == nil || rec.Table == nil {
		return
	}
	eachPairTarget(rec.Table.Type(), w.isAID.Index(), fn)
}

// getViaIsA walks the IsA chain of e looking for component T with the given
// pre-looked-up component ID cid.
//
// seen may be nil on entry; it is allocated lazily the first time an IsA pair
// is encountered, with e pre-inserted to prevent cycles. Each prefab is added
// before recursion. Dead prefabs are skipped.
func getViaIsA[T any](w *World, e ID, cid ID, seen map[ID]struct{}) (T, bool) {
	var zero T
	rec := w.index.Get(e)
	if rec == nil || rec.Table == nil {
		return zero, false
	}
	isAIdx := w.isAID.Index()
	for _, id := range rec.Table.Type() {
		if !id.IsPair() || uint32(id.First()) != isAIdx {
			continue
		}
		prefab := id.Second()
		if !w.index.IsAlive(prefab) {
			continue
		}
		// Allocate seen lazily — only entities with at least one IsA pair reach here.
		if seen == nil {
			seen = map[ID]struct{}{e: {}}
		}
		if _, visited := seen[prefab]; visited {
			continue
		}
		seen[prefab] = struct{}{}
		prefabRec := w.index.Get(prefab)
		if prefabRec != nil && prefabRec.Table != nil && prefabRec.Table.HasComponent(cid) {
			ptr := prefabRec.Table.Get(int(prefabRec.Row), cid)
			if ptr == nil {
				return zero, true
			}
			return *(*T)(ptr), true
		}
		if v, ok := getViaIsA[T](w, prefab, cid, seen); ok {
			return v, true
		}
	}
	return zero, false
}

// hasViaIsA walks the IsA chain of e checking for the presence of component cid.
//
// seen may be nil on entry; it is allocated lazily the first time an IsA pair
// is encountered, with e pre-inserted. Dead prefabs are skipped.
func hasViaIsA(w *World, e ID, cid ID, seen map[ID]struct{}) bool {
	rec := w.index.Get(e)
	if rec == nil || rec.Table == nil {
		return false
	}
	isAIdx := w.isAID.Index()
	for _, id := range rec.Table.Type() {
		if !id.IsPair() || uint32(id.First()) != isAIdx {
			continue
		}
		prefab := id.Second()
		if !w.index.IsAlive(prefab) {
			continue
		}
		// Allocate seen lazily — only entities with at least one IsA pair reach here.
		if seen == nil {
			seen = map[ID]struct{}{e: {}}
		}
		if _, visited := seen[prefab]; visited {
			continue
		}
		seen[prefab] = struct{}{}
		prefabRec := w.index.Get(prefab)
		if prefabRec != nil && prefabRec.Table != nil && prefabRec.Table.HasComponent(cid) {
			return true
		}
		if hasViaIsA(w, prefab, cid, seen) {
			return true
		}
	}
	return false
}
