package flecs

// Disabled returns the ID of the built-in Disabled tag entity (index 36).
//
// An entity that carries the Disabled tag is excluded from ordinary queries by
// default. Queries opt in to seeing disabled entities by mentioning Disabled in
// any term kind (With, Without, Maybe, Or).
//
// This is a per-archetype table flag: when any entity in a table acquires the
// Disabled tag, all entities in that table are excluded from ordinary queries.
// The exclusion is O(1) per table (a single HasComponent test) — no per-entity
// cost. Mirrors C EcsDisabled / EcsTableIsDisabled.
//
// Usage:
//
//	flecs.DisableEntity(fw, entityID)   // add Disabled tag
//	flecs.EnableEntity(fw, entityID)    // remove Disabled tag
//	flecs.IsDisabled(r, entityID)       // predicate
//
//	// Opt-in query:
//	q := flecs.NewQueryFromTerms(w, flecs.With(posID), flecs.With(w.Disabled()))
func (w *World) Disabled() ID { return w.disabledID }

// Prefab returns the ID of the built-in Prefab tag entity (index 37).
//
// An entity that carries the Prefab tag is excluded from ordinary queries by
// default, matching the same opt-in semantics as Disabled. Prefabs are
// typically used as templates for IsA-based inheritance: an entity with
// (IsA, prefabEntity) inherits components from the prefab while the prefab
// itself stays invisible to ordinary iteration.
//
// The Prefab tag carries DontInherit semantics (bootstrapped at world
// construction), so entities inheriting from a prefab via IsA do NOT
// automatically acquire the Prefab tag. Mirrors C EcsPrefab /
// EcsTableIsPrefab and bootstrap.c:1308.
//
// Usage:
//
//	flecs.MarkPrefab(fw, entityID)      // add Prefab tag (idempotent)
//	flecs.IsPrefab(r, entityID)         // predicate
func (w *World) Prefab() ID { return w.prefabID }

// DisableEntity adds the Disabled tag to entity e, excluding it from ordinary
// queries. Idempotent: calling DisableEntity on an already-disabled entity is
// a no-op. Deferred when called inside a Write scope.
//
// The Disabled tag resides in the entity's archetype: adding it causes an
// archetype migration (same as any tag). The resulting table is then
// excluded from query evaluation at the per-table level — O(1), no
// per-entity cost.
//
// Note: the name DisableEntity distinguishes this from the CanToggle generic
// Disable[T](fw, e) which operates on per-component toggle bits.
func DisableEntity(fw *Writer, e ID) {
	AddID(fw, e, fw.world.disabledID)
}

// EnableEntity removes the Disabled tag from entity e, re-including it in
// ordinary queries. No-op if e does not carry Disabled. Deferred when called
// inside a Write scope.
//
// Note: the name EnableEntity distinguishes this from the CanToggle generic
// Enable[T](fw, e) which operates on per-component toggle bits.
func EnableEntity(fw *Writer, e ID) {
	RemoveID(fw, e, fw.world.disabledID)
}

// IsDisabled reports whether e currently carries the Disabled tag.
// Accepts scope so it can be called inside both Read and Write blocks.
func IsDisabled(s scope, e ID) bool {
	w := s.scopeWorld()
	rec := w.index.Get(e)
	if rec == nil || rec.Table == nil {
		return false
	}
	return rec.Table.HasComponent(w.disabledID)
}

// MarkPrefab adds the Prefab tag to entity e, excluding it from ordinary
// queries. Idempotent: calling MarkPrefab on an already-marked entity is a
// no-op. Deferred when called inside a Write scope.
//
// To remove the Prefab tag: flecs.RemoveID(fw, e, w.Prefab()).
func MarkPrefab(fw *Writer, e ID) {
	AddID(fw, e, fw.world.prefabID)
}

// IsPrefab reports whether e currently carries the Prefab tag.
// Note: entities that inherit from a prefab via IsA do NOT acquire the Prefab
// tag (it is bootstrapped with DontInherit), so IsPrefab(e) is false for
// instances even when their base is a prefab.
// Accepts scope so it can be called inside both Read and Write blocks.
func IsPrefab(s scope, e ID) bool {
	w := s.scopeWorld()
	rec := w.index.Get(e)
	if rec == nil || rec.Table == nil {
		return false
	}
	return rec.Table.HasComponent(w.prefabID)
}
