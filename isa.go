package flecs

// IsA returns the built-in IsA relationship entity.
//
// Use MakePair(w.IsA(), prefab) to express that an entity inherits components
// from prefab. Reads (Get/Has) consult the IsA chain transitively on a local
// miss; writes (Set/AddID) always land locally on the child (copy-on-write
// override). The IsA entity is always alive for the lifetime of the World.
//
// IsA is intentionally NOT exclusive: an entity may have multiple prefab bases
// (multiple (IsA, *) pairs), enabling mixin-style composition. This matches C
// flecs — only ChildOf, OnDelete, OnDeleteTarget, and OnInstantiate are
// bootstrapped exclusive.
func (w *World) IsA() ID { return w.isAID }

// OnInstantiate returns the built-in OnInstantiate relationship entity.
// Mirror of C's EcsOnInstantiate. Used with Inherit/Override/DontInherit as
// targets to form the (OnInstantiate, Inherit) pair. In the Go port the
// preferred API is SetInheritable[T] / World.SetInheritable(cid), which sets a
// bool on the component's TypeInfo directly; the pair form is exposed for API
// symmetry and potential future use.
func (w *World) OnInstantiate() ID { return w.onInstantiateID }

// Inherit returns the built-in Inherit trait entity.
// The pair (OnInstantiate, Inherit) on a component entity is the C-flecs way to
// mark it as inheritable. In the Go port, use SetInheritable[T] instead.
func (w *World) Inherit() ID { return w.inheritID }

// Override returns the built-in Override trait entity. Exposed for API symmetry
// with C flecs; the Go port's Set already performs copy-on-write override
// semantics (Phase 4.3) and does not consult this entity.
func (w *World) Override() ID { return w.overrideID }

// DontInherit returns the built-in DontInherit trait entity. Exposed for API
// symmetry; has no runtime effect in the Go port because non-inheritable is the
// default — components opt IN via SetInheritable rather than opting out.
func (w *World) DontInherit() ID { return w.dontInheritID }

// prefabOfInternal returns the first IsA prefab of entity e.
// Internal helper; no exclusive-access check.
func prefabOfInternal(w *World, e ID) (ID, bool) {
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

// getViaIsAPooled is the pool-acquiring entry point for getViaIsA.
// Call this instead of getViaIsA(w, e, cid, nil) at external call sites.
func getViaIsAPooled[T any](w *World, e ID, cid ID) (T, bool) {
	seen := idSeenPool.Get().(map[ID]struct{})
	seen[e] = struct{}{}
	v, ok := getViaIsA[T](w, e, cid, seen)
	clear(seen)
	idSeenPool.Put(seen)
	return v, ok
}

// getViaIsA walks the IsA chain of e looking for component T with the given
// pre-looked-up component ID cid.
//
// seen is provided by the caller (non-nil when called from getViaIsAPooled or
// recursively). Recursive calls pass the same seen map to detect cycles.
// Dead prefabs are skipped.
func getViaIsA[T any](w *World, e ID, cid ID, seen map[ID]struct{}) (T, bool) {
	var zero T
	// DontInherit takes precedence over Inheritable: do not walk the IsA chain.
	if w.instantiatePolicies[cid]&policyOnInstantiateDontInherit != 0 {
		return zero, false
	}
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

// hasViaIsAPooled is the pool-acquiring entry point for hasViaIsA.
// Call this instead of hasViaIsA(w, e, cid, nil) at external call sites.
func hasViaIsAPooled(w *World, e ID, cid ID) bool {
	seen := idSeenPool.Get().(map[ID]struct{})
	seen[e] = struct{}{}
	result := hasViaIsA(w, e, cid, seen)
	clear(seen)
	idSeenPool.Put(seen)
	return result
}

// hasViaIsA walks the IsA chain of e checking for the presence of component cid.
//
// seen is provided by the caller (non-nil when called from hasViaIsAPooled or
// recursively). Recursive calls pass the same seen map to detect cycles.
// Dead prefabs are skipped.
func hasViaIsA(w *World, e ID, cid ID, seen map[ID]struct{}) bool {
	// DontInherit takes precedence over Inheritable: do not walk the IsA chain.
	if w.instantiatePolicies[cid]&policyOnInstantiateDontInherit != 0 {
		return false
	}
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
