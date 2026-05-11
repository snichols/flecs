## Goal

Ship traversal as **explicit helper functions** that cover ~90% of the use case at ~10% of the complexity of query-term traversal modifiers. Three free functions — `GetUp[T]`, `HasUp`, `TargetUp` — walk a relationship up from an entity and return the first match. Users compose them inside `Each<N>` callbacks for common patterns (e.g., "for each entity with LocalToWorld, look up Position from a parent via ChildOf").

### Context — what's on master

Master HEAD `94bb324`. Phase 3.3 just landed: structured Term API with `With`/`Without`/`Maybe` and `NewQueryFromTerms`. v0.1.0 is tagged at `7fc1d6c`.

`IsA` inheritance (Phase 4.3) already provides transitive component lookup: `Get[T]` walks `(IsA, prefab)` chains to find inherited components. `ChildOf` (Phase 4.2) is a relationship with cascade-delete but does NOT provide inheritance.

The originally-planned Phase 6.2 was "Up/Down/Cascade query traversal modifiers" (flecs's `up(ChildOf)` syntax). Implementing it as query terms would require:
- Widening `Term` with `Source` and `Traversal` fields
- Per-entity matching (vs per-table)
- Pair-edge cache invalidation when a parent changes
- A new field accessor signature for "value comes from an ancestor"

That's a major architectural undertaking for v0. **Instead, this issue ships traversal as explicit helper functions.**

### After this lands

```go
type LocalToWorld struct { Matrix [16]float32 }
type WorldPosition struct { X, Y, Z float32 }

ltwID := flecs.RegisterComponent[LocalToWorld](w)
posID := flecs.RegisterComponent[WorldPosition](w)

// Child entity inherits WorldPosition from a parent via ChildOf.
flecs.Each1[LocalToWorld](w, func(e flecs.ID, ltw *LocalToWorld) {
    // Look up WorldPosition on self first, then walk ChildOf parents.
    if pos, ok := flecs.GetUp[WorldPosition](w, e, w.ChildOf()); ok {
        // Apply pos to ltw.Matrix translation
    }
})

// Find which ancestor owns the WorldPosition (e.g., for debugging).
if owner, ok := flecs.TargetUp(w, e, posID, w.ChildOf()); ok {
    name, _ := w.GetName(owner)
    log.Printf("WorldPosition inherited from %s", name)
}

// Has-style check (cheaper than Get when you don't need the value).
if flecs.HasUp(w, e, deadID, w.ChildOf()) {
    // Some ancestor is dead — child should also be ignored.
}
```

### Deliverables

1. **New file `traversal.go`** in root `flecs` package.

2. **Three generic free functions:**
   ```go
   // GetUp walks the relationship up from e, returning the first T found on
   // e or any ancestor reachable via rel. Returns (zero, false) if no entity
   // in the chain has T, if e is not alive, or if T is not registered.
   //
   // The chain is traversed via pairs (rel, target) in each entity's
   // signature. If multiple (rel, *) pairs exist on an entity, the first
   // one in signature order is followed.
   //
   // Cycle detection terminates the walk if any entity is visited twice.
   // Depth is limited to 64 levels; deeper chains return (zero, false).
   func GetUp[T any](w *World, e ID, rel ID) (T, bool)

   // HasUp reports whether e or any ancestor reachable via rel has the
   // component identified by id. Cheaper than GetUp when the value isn't
   // needed. Returns false if e is not alive.
   func HasUp(w *World, e ID, id ID, rel ID) bool

   // TargetUp returns the ID of the first entity in the chain (e or an
   // ancestor via rel) that owns id. Returns (0, false) if no entity in the
   // chain has id, or if e is not alive.
   //
   // Useful for asking "which ancestor owns this component?" — e.g., to
   // attribute inherited data to a specific parent.
   func TargetUp(w *World, e ID, id ID, rel ID) (ID, bool)
   ```

3. **Implementation pattern:**
   - All three functions share an internal `walkUp(w, e, rel, fn func(ID) bool) (ID, bool)` helper that:
     1. Starts at `e`.
     2. Checks alive; bail if not.
     3. At each step, invoke `fn(current)` — if it returns true, terminate with the current ID.
     4. Find the first pair `(rel, target)` in the current entity's signature.
     5. If no such pair, terminate (no match).
     6. Step to `target`; check `seen` for cycle; increment depth; loop.
   - `GetUp[T]` uses `walkUp` with `fn` that checks `Owns[T](w, current)`; on match, reads the value and returns it.
   - `HasUp` uses `walkUp` with `fn` that checks `OwnsID(w, current, id)`; on match, returns true.
   - `TargetUp` uses `walkUp` with `fn` that checks `OwnsID(w, current, id)`; on match, returns the current ID.
   - The `walkUp` helper is unexported; document its contract in code comments.

4. **Semantic precision — "Owns" vs "Has":**
   - The walk should use `OwnsID` (local-only) at each step, NOT `HasID` (which walks IsA inheritance). Reason: we want to find the entity that LOCALLY owns the component, not one that inherits it. If a parent inherits Position from a prefab via IsA, that parent doesn't "own" Position — its prefab does. Walking up ChildOf should find the parent that locally has Position.
   - Document this clearly in the godoc.
   - **Subtle alternative:** if the user wants IsA-aware traversal (find ANY entity that has the component, even via inheritance), they can compose: `if pos, ok := GetUp[Position](w, e, ChildOf); !ok { /* try IsA */ }`. Don't bake this into the primary path.

5. **Self-first semantics:**
   - `GetUp[T](w, e, rel)` returns `Get[T](w, e)` if `e` itself owns T. Document.
   - `HasUp` and `TargetUp` similarly check `e` first before walking.
   - Rationale: matches flecs's `Up | Self` flag which is the most common pattern. (Pure-Up that skips self can be a future variant if needed.)

6. **Cycle detection:**
   - Internal `seen map[ID]struct{}` allocated only when entering the second step (lazy, matching the IsA-fallback pattern from Phase 8.3).
   - First step: just check `e`. No allocation.
   - Second step onward: allocate `seen` map; populate before recursing.
   - If a cycle is detected (e.g., entity that's its own ChildOf), terminate the walk cleanly with `(zero, false)`.

7. **Depth limit:**
   - Constant `const maxTraversalDepth = 64`.
   - Document why: prevents pathological infinite chains in case cycle detection has a bug or in case of malformed relationship graphs.
   - Beyond depth 64, return `(zero, false)` silently. Don't panic; this is a safety net.

8. **Dead-prefab guard:**
   - If a target entity in the chain is not alive (`w.IsAlive(target) == false`), stop walking that branch. Matches the IsA Get/Has dead-prefab semantic from Phase 4.3.

9. **Tests** in `traversal_test.go`:
   - **Single-level inheritance:** parent has Position; child via ChildOf doesn't. `GetUp[Position](w, child, w.ChildOf())` returns parent's value.
   - **Multi-level inheritance:** grandparent → parent → child via ChildOf. Only grandparent has Position. `GetUp[Position](w, child, ...)` returns grandparent's value.
   - **Self-first:** child itself has Position. `GetUp[Position](w, child, ...)` returns child's OWN value, NOT the parent's.
   - **HasUp basics:** parent has Position; HasUp on child returns true. No ChildOf at all → false.
   - **HasUp on dead entity:** returns false.
   - **TargetUp basics:** returns the ID of the entity in the chain that owns the component.
   - **TargetUp self:** if self owns it, returns self.
   - **No relationship:** entity has no ChildOf pair. GetUp returns (zero, false).
   - **Cycle: self-loop:** entity has `(ChildOf, self)`. GetUp terminates cleanly (returns false unless self has the component).
   - **Cycle: two-entity:** A has `(ChildOf, B)`, B has `(ChildOf, A)`; neither has Position. GetUp terminates cleanly.
   - **Dead parent:** parent has Position, then is deleted. Child's pair-ID `(ChildOf, dead_parent)` is still in the signature. GetUp returns (zero, false) — dead-target check kicks in.
   - **Depth limit:** create a 100-deep ChildOf chain. GetUp terminates at depth 64 and returns (zero, false) even if a deeper ancestor has the component. Document.
   - **Works for IsA too:** the relationship parameter is arbitrary; using `w.IsA()` walks prefab chains. Verify GetUp[Position] on a prefab-chained entity returns the first-owner's Position.
   - **Works for custom relationships:** user defines `flecs.NewEntity()` as a custom relationship; GetUp walks via it.
   - **Zero-allocation when component is on self:** GetUp[Position] on entity that owns Position locally — verify no `seen` map allocation (per Phase 8.3 lazy pattern). Test via a microbenchmark or by counting allocations.
   - **Existing tests stay green.**

10. **Mechanical acceptance**
    - `go test ./... -race -count=2` passes.
    - `go vet ./...` clean.
    - `golangci-lint run` clean.
    - Coverage on `flecs` ≥ 90% (no regression from 97.1%).
    - All exported symbols have godoc.
    - Add 2-3 traversal-targeted entries to `bench_test.go` (e.g., `BenchmarkGetUp_Depth1`, `BenchmarkGetUp_Depth5`, `BenchmarkGetUp_SelfHit`) and append baseline numbers to BENCH.md under "Phase 6.2 baseline."

### Non-goals

- NO query-term traversal modifiers (`up(rel)` in NewQueryFromTerms).
- NO Down/Cascade walks.
- NO traversal result caching.
- NO multi-rel composition (e.g., walk ChildOf, then IsA, then ChildOf).
- NO `Pure-Up` variant that skips self. Only Self+Up is shipped.
- NO change to `Get[T]`/`Has[T]`/`Owns[T]` semantics — these stay unchanged.
- NO change to Each helpers / queries / observers / hooks.

### Constraints / pointers for the implementer

- Reuse `pair_internal.go`'s `firstPairTarget` (already extracted in Phase 4.3) to find the next-step target.
- Use `OwnsID` for local-component checks; do NOT use `HasID` (which walks IsA).
- Implement the lazy `seen` pattern from `isa.go` (Phase 8.3 — `seen` is allocated only after the first step).
- The depth limit is a defense-in-depth measure on top of cycle detection. Both must be present.
- DO NOT modify the IsA fallback in `Get[T]`/`Has[T]`. Traversal helpers are orthogonal.
- DO NOT add a new built-in entity for traversal. The relationship is passed by the caller.
- DO NOT import third-party deps.
- The dead-target guard is critical for correctness when a parent has been deleted but the child's pair-ID lingers. Mirror the IsA dead-prefab guard.

### C reference (cite paths — read but do not paraphrase)

- `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` — search "ecs_get_target" for the per-entity relationship lookup contract.
- `/work/agents/claude/projects/SanderMertens/flecs/src/search.c` — `flecs_search_relation`. The C version of "walk relationship looking for component."
- `/work/agents/claude/projects/SanderMertens/flecs/src/query/engine/eval_up.c` — the C version of query-time Up traversal (informational; we're NOT porting this).

## Constraints

- @world.go — World API surface; new helpers take `*World` as first arg and live in root package alongside it.
- @id_ops.go — `OwnsID` (local-only component check) is the per-step predicate; do NOT use `HasID` which walks IsA.
- @isa.go — Lazy `seen`-map pattern (allocate only on second step) and dead-prefab guard semantics; mirror both in `walkUp`.
- @childof.go — `w.ChildOf()` is the canonical relationship arg for the motivating use case; traversal helpers must work over it without special-casing.
- @pair_internal.go — Reuse `firstPairTarget` to find next-step target from an entity's signature.
- @id.go — `ID` type and pair construction; no changes here, but used by callers.
- @bench_test.go — Extend with `BenchmarkGetUp_Depth1`, `BenchmarkGetUp_Depth5`, `BenchmarkGetUp_SelfHit`; append baseline to BENCH.md under "Phase 6.2 baseline."

Not a bug — feature work. No `--label bug`. Apply `snichols/queued`.
