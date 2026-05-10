## Goal

Add a built-in `IsA` relationship with prefab-style component inheritance for reads, plus a new local-only `Owns` API to distinguish local vs inherited components. Master HEAD `bf5f944` (post-Phase 4.2, which added the built-in `ChildOf` and cascade delete on `(ChildOf, e)`).

`IsA` is a pair-relationship: `(IsA, prefab)` on an entity means "this entity inherits prefab's components." Reads (`Get`/`Has`) consult the IsA chain transitively when the local lookup misses; writes (`Set`/`Add`) always land locally on the child (copy-on-write override). `Owns` is the new local-only check that gives users a way to ask "is this component literally in this entity's table?"

### Shape after this phase lands

```go
prefab := w.NewEntity()
flecs.Set[Position](w, prefab, Position{1, 2})

child := w.NewEntity()
flecs.AddID(w, child, flecs.MakePair(w.IsA(), prefab))

// Inheritance:
p, ok := flecs.Get[Position](w, child)  // (Position{1, 2}, true) — from prefab
flecs.Has[Position](w, child)           // true — visible via IsA
flecs.Owns[Position](w, child)          // false — not local
flecs.Set[Position](w, child, Position{99, 99})  // local override
flecs.Get[Position](w, child)           // (Position{99, 99}, true) — local
flecs.Owns[Position](w, child)          // true — now local
```

### Semantic distinction

- `Has[T]` / `HasID` — **inheritance-aware**. True if reachable from the entity, locally or via any IsA prefab (transitively).
- `Owns[T]` / `OwnsID` — **NEW**, local-only. True iff the component is in the entity's own table's signature.
- `Get[T]` — reads from own table first, falls back to IsA chain on miss.
- `Set[T]` — unchanged signature. Always writes locally. Uses `Owns` semantics internally to decide whether migration is needed (NOT `Has`). If a prefab has `Position` and you `Set[Position](child, ...)`, the child's table grows a `Position` column (copy-on-write override).
- `Remove[T]` / `RemoveID` — unchanged. Removes the local component only. After remove, `Has` may still return true (inherited); `Owns` returns false.

## Deliverables

1. **Allocate `IsA` built-in entity at `World.New()`.** In `world.go` `New()`, after allocating ChildOf, allocate `isAID` via the same pattern (`w.index.Alloc()`, seat in empty table). Order: empty table → ChildOf (index 1) → IsA (index 2). Document: first user entity is now index 3. Add unexported field `isAID ID` on `*World`.

2. **`func (w *World) IsA() ID`** — accessor for the built-in IsA entity.

3. **`func PrefabOf(w *World, e ID) (ID, bool)`** — free function (matches `ParentOf` shape but as a function so it composes with the generic helpers). Returns the first IsA target on `e`. Returns `(0, false)` if `e` is not alive or has no IsA relationship.

4. **`func (w *World) EachPrefab(e ID, fn func(prefab ID) bool)`** — iterate direct IsA targets on `e`. Returns early if `fn` returns false. **Direct only — does NOT transitively walk multi-level prefab chains.** Document.

5. **Modify `Get[T]` in `world.go`:**
   - First: check `Owns[T]` on `e`. If owned, read locally as before.
   - On miss: walk the IsA chain. For each pair `(IsA, prefab_i)` in `e`'s signature (in signature order), recursively call `Get[T]` on `prefab_i`. Return the first hit.
   - Cycle detection: maintain a `seen map[ID]struct{}` allocated lazily at the recursion entry point. If a prefab is already in `seen`, skip. Add `e` to `seen` BEFORE recursing.
   - Optional depth limit at 32+ with a clear panic message — implementer's call; document if added.
   - Returns `(zero, false)` if entity is not alive OR no IsA path yields the component.

6. **Modify `Has[T]` and `HasID` to be inheritance-aware.** Same walk as Get's, presence only. Returns false if entity is not alive. Cycle detection identical.

7. **Add `Owns[T any](w *World, e ID) bool`** — local-only check on the entity's table. Equivalent to Phase 1.5 `Has[T]` semantics. Auto-registers `T` (matches existing `Has[T]` policy).

8. **Add `OwnsID(w *World, e ID, id ID) bool`** — local-only raw-ID check. No auto-registration.

9. **`Set[T]` and `AddID` behavior verification.** Both continue to use `Owns` semantics internally (NOT inheritance-aware presence). **Critical**: if a child has `(IsA, prefab)` and prefab has `Position`, calling `Set[Position](child, ...)` must add the `Position` column to the child's table. Test this; document as copy-on-write override. Do NOT modify signatures.

10. **Auto-register policy for raw-ID variants:**
    - `HasID` — does NOT auto-register (matches existing behavior).
    - `OwnsID` — does NOT auto-register.
    - `AddID` / `RemoveID` — unchanged (Phase 4.1 semantics preserved).

11. **Tests** in `isa_test.go` (or extend an existing file):
    - `World.IsA()` returns consistent ID; entity is alive.
    - Per-world IsA: distinct world instances each have an alive IsA.
    - **Inheritance basics:** prefab with Position; child with `(IsA, prefab)`; `Get` returns prefab's value; `Has` true; `Owns` false.
    - **Override (copy-on-write):** `Set[Position](w, child, ...)` after inheritance — child now owns Position locally; `Get` returns local; `Owns` true.
    - **Remove restores inheritance:** after Set then Remove, `Has` true again (via prefab), `Owns` false, `Get` returns prefab's value.
    - **Multi-level chain:** A `(IsA, B)`, B `(IsA, C)`; C has Position. `Get[Position](w, A)` returns C's. `Has` true.
    - **Multiple direct prefabs:** entity has `(IsA, p1)` AND `(IsA, p2)`; p1 has `Position{X:1}`, p2 has `Position{X:2}`. `Get` returns p1's (first-in-signature-order). Document.
    - **Cycle on Get:** `(IsA, self)` doesn't infinite-loop; returns `(zero, false)` cleanly.
    - **Cycle on Has:** same; returns false.
    - **Cycle between two entities:** A `(IsA, B)` and B `(IsA, A)`; neither has Position; `Get`/`Has` terminate cleanly.
    - **PrefabOf basics:** first IsA target; `(0, false)` for entity with no IsA.
    - **EachPrefab basics:** entity with two IsA pairs; fn called twice. Early-exit respected. Empty: fn never called.
    - **EachPrefab is DIRECT only:** A `(IsA, B)`, B `(IsA, C)`; `EachPrefab(A)` yields only B, not C.
    - **Delete prefab:** if you delete a prefab, IsA pairs hanging off children become dangling — flecs C has this footgun too. The pair-id is still in the child's signature but `IsAlive(prefab) == false`. The Get/Has walk MUST check `IsAlive(prefab)` before recursing; test that child's `Get[Position]` returns `(zero, false)` after prefab delete.
    - **Existing tests stay green:** all Phase 1-4.2 tests must pass. Phase 4.2 shifted Count baseline by +1 (ChildOf); this phase shifts by +1 more (IsA) — update existing tests accordingly.

12. **Mechanical acceptance**
    - `go test ./... -race -count=2` passes.
    - `go vet ./...` clean.
    - `golangci-lint run` clean.
    - Coverage on `flecs` >= 90% (no regression from 97.9%).
    - All exported symbols have godoc.

## Non-goals

- NO `Instantiate(prefab) ID` helper. Users compose: `e := w.NewEntity(); AddID(w, e, MakePair(w.IsA(), prefab))`.
- NO `Override` flag bits / auto-override semantics. Copy-on-write via Set is the only override mechanism.
- NO ChildOf-of-prefabs (prefab hierarchies via ChildOf). Defer.
- NO query-time IsA matching (queries matching components inherited via IsA — Phase 6+).
- NO observer firing.
- NO transitive `PrefabOf` / `Ancestors` helpers.
- NO automatic removal of stale IsA pairs when prefab is deleted. (Phase 4.2's cascade is ChildOf-only.)
- NO concurrent access.

## Implementer pointers

- Internal recursion shape: `getViaIsA[T any](w, e, seen) (T, bool)`. Caller (`Get[T]`) does the local check first, then calls `getViaIsA` with a fresh `seen` map (or nil — lazy allocation OK).
- Cycle detection: add `e` to `seen` BEFORE recursing into any prefab. Recursive call adds `prefab` before its own recursion.
- The `IsAlive(prefab)` check is critical — without it, dead prefab IDs cause stale-table reads or panics.
- Walking the entity's pairs: iterate `record.Table.Type()` and filter via `id.IsPair() && id.First() == w.isAID-as-28-bit-index`. Same idiom as `ParentOf` in `childof.go`.
- **Recommended minor refactor (Phase 4.2 reviewer flagged this):** extract a shared `firstPairTarget(rec, rel)` and/or `eachPairTarget(rec, rel, fn)` helper into a new `pair_internal.go` and refactor `ParentOf` + `EachChild` to use it. `EachChild` and `EachPrefab` are structurally identical — verify and dedupe where appropriate.
- DO NOT modify `Set[T]` / `AddID` / `Remove[T]` / `RemoveID` signatures or behavior — they already work correctly with IsA via the local-only path.
- DO NOT modify storage layers. IsA is just another pair ID.
- DO NOT auto-cascade-delete IsA descendants.
- DO NOT import any third-party deps.

## C reference (read, do not paraphrase)

- `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` — search `EcsIsA` for built-in declaration.
- `/work/agents/claude/projects/SanderMertens/flecs/src/bootstrap.c` — built-in entity allocation.
- `/work/agents/claude/projects/SanderMertens/flecs/src/instantiate.c` — prefab instantiation.
- `/work/agents/claude/projects/SanderMertens/flecs/src/entity.c` — Get/Has paths (the simplest mirror for us is "walk pairs starting with IsA in the entity's signature; for each prefab, retry the Get/Has").

## Constraints

- @world.go — World struct, `New()`/`NewEntity()`/`Delete()`/`Get`/`Has`/`Set`/`Remove`; this is where `isAID`, `IsA()`, modified `Get[T]`/`Has[T]`, and new `Owns[T]` live.
- @id_ops.go — `AddID`/`RemoveID`/`HasID`/`MakePair`/pair helpers; `HasID` becomes inheritance-aware; `OwnsID` is added here.
- @childof.go — parallel built-in pattern; `ChildOf()`/`ParentOf`/`EachChild` shapes that `IsA()`/`PrefabOf`/`EachPrefab` mirror. Candidate for shared `firstPairTarget`/`eachPairTarget` helper extraction.
- @id.go — ID layout, pair encoding (`IsPair`/`First`/`Second`); the IsA pair-walk uses `id.First() == w.isAID-as-28-bit-index`.
- @internal/component/registry.go — component registration; relevant for the `Owns[T]` auto-register policy (matches `Has[T]`).
- @internal/storage/table/table.go — table signatures; the local-only `Owns` check inspects `record.Table.Type()`. Storage layer must NOT be modified.
- Not a bug — feature work. No `--label bug`. Apply `snichols/queued` only.
