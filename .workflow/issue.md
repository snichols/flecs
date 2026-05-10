## Goal

Wire up the dormant `Hooks` machinery that has existed since Phase 1.5 so that user code can react to component add/set/remove storage events. After this phase, users can register typed hooks via a generic API:

```go
flecs.OnSet[Position](w, func(e flecs.ID, p *Position) {
    log.Printf("entity %d position is now %v", e.Index(), *p)
})

flecs.OnAdd[Position](w, func(e flecs.ID, p *Position) {
    // p is the zero-value at this point (just added, not yet set)
})

flecs.OnRemove[Position](w, func(e flecs.ID, p *Position) {
    // p still points to the old value being removed
})

flecs.Set[Position](w, e, Position{X: 1, Y: 2})
// → OnAdd fires (component newly added)
// → OnSet fires (value assigned)

flecs.Set[Position](w, e, Position{X: 3, Y: 4})
// → OnSet fires (no OnAdd — already present)

flecs.Remove[Position](w, e)
// → OnRemove fires (before column removed)

w.Delete(e)
// → OnRemove fires for each of e's components
```

The work has two halves: (a) a small user-facing registration API in a new `hooks.go` that converts typed `func(e ID, v *T)` into the existing `component.EntityCallback` signature, and (b) wiring fire-sites at the existing storage chokepoints — primarily `migrate()` in `world.go`, plus the direct-write path in `Set[T]`, plus per-component invocations in `deleteOne()` / cascade-delete.

### Deliverables

1. **`hooks.go` (new file) — user-facing registration API in the root `flecs` package:**
   - `func OnAdd[T any](w *World, fn func(e ID, v *T))`
   - `func OnSet[T any](w *World, fn func(e ID, v *T))`
   - `func OnRemove[T any](w *World, fn func(e ID, v *T))`
   - Each auto-registers `T` first (idempotent), then mutates the TypeInfo's `Hooks` field to install a wrapper `EntityCallback`. The wrapper does the `*T` conversion from `unsafe.Pointer` and discards the `world any` argument.
   - `fn == nil` clears the hook of that kind for `T`. Document.
   - **Hook replacement semantics:** calling `OnSet[T]` twice replaces the OnSet hook (single hook per kind per type). Phase 5.2 will introduce a multi-observer system; this is the lightweight per-type hook.
   - **Zero-size T (tag):** OnAdd/OnRemove get `v *T` pointing to a zero-byte view. Document that dereferencing a size-0 pointer is technically valid in Go but the value is meaningless. OnSet on a tag has no value to set — document; allow registration without panic.
   - Internal wrapper shape:
     ```go
     info.Hooks.OnSet = func(world any, e ID, ptr unsafe.Pointer) {
         fn(e, (*T)(ptr))
     }
     ```

2. **Wire invocation in `world.go` (and `id_ops.go` as needed):**
   - **OnAdd fires AFTER a component is newly added.** Trigger sites:
     - `Set[T]` when the component is NOT yet present (auto-add path).
     - `AddID(w, e, id)` when the id is newly being added (tag, pair, or regular component).
     - `SetPair[T]` when the pair-id is newly being added.
   - **OnSet fires AFTER a value is assigned.** Trigger sites:
     - `Set[T]` — every call.
     - `SetPair[T]` — every call.
     - NOT on `AddID` (no value).
   - **OnRemove fires BEFORE removal.** Trigger sites:
     - `Remove[T]` / `RemoveID` — before the migration that removes the column.
     - `Delete(e)` — for each component on `e`, before the entity is freed. For cascade, OnRemove fires per-component per-entity in post-order (children's components before parents', matching Phase 4.2 cascade semantics).
   - **Source of `ptr`:**
     - OnAdd: pointer to the newly-allocated column slot in the destination table (post-migration). For zero-size T, can be nil or a zero-byte pointer; implementer's call, document.
     - OnSet: pointer to the column slot AFTER the value was assigned.
     - OnRemove: pointer to the column slot in the SOURCE table BEFORE the migration (still intact).
   - **Source of `world` in EntityCallback:** the World passes itself (`w` as `any`); the typed wrapper discards it.
   - **Order across multiple components in one migration:** if a migration causes multiple component changes (Phase 5.1 only sees this in `Delete` cascade), fire OnRemove for each removed component before doing the actual storage migration. Iteration order is the entity's current signature order (sorted by ID). Document.

3. **Refactor: extract small `nil`-safe fire helpers on `*World`:**
   - `(w *World) fireOnAdd(info *component.TypeInfo, e ID, ptr unsafe.Pointer)`
   - `(w *World) fireOnSet(info *component.TypeInfo, e ID, ptr unsafe.Pointer)`
   - `(w *World) fireOnRemove(info *component.TypeInfo, e ID, ptr unsafe.Pointer)`
   - Each: if `info == nil || info.Hooks.OnX == nil`, no-op. Components without hooks pay zero cost.
   - DO NOT invent a deferred event queue. Fire synchronously.

4. **Panic handling:** if a hook panics, propagate. Do NOT recover. The operation that triggered the hook will fail; the world state may be in a transitional state. Document as a known limitation. Phase 5.3's deferred commands will offer a safer pattern.

5. **Re-entrancy:** a hook may call other World operations (Set/Get/Remove/Delete). For Phase 5.1, document that behavior is **undefined** if a hook calls Set/Remove/Delete on the entity it's currently observing (or any entity in a current migration). For READ operations (Get/Has/Owns/IsAlive/Count/etc.) re-entrancy is safe. Phase 5.3's deferred commands fix this.

6. **Tests in `hooks_test.go`:**
   - **Registration:** `OnSet[Position](w, fn)` returns nothing, idempotently replaces existing hook.
   - **Clearing:** `OnSet[Position](w, nil)` removes the OnSet hook.
   - **OnAdd fires once on initial Set:** Set[Position] on a fresh entity — OnAdd called once with the new value pointer; OnSet called once.
   - **OnSet fires every Set:** Set then Set again on same entity — OnAdd called once total, OnSet called twice.
   - **OnAdd fires on AddID:** AddID a tag entity — OnAdd fires for that ID.
   - **OnAdd does NOT fire on archetype migration without adding the component:** Set[Position] then Set[Velocity] (which migrates but Position is already there) — OnAdd[Position] is NOT fired during the second migration. Only OnAdd[Velocity].
   - **OnRemove fires before removal:** Set Position{1,2}, register OnRemove that copies `*p` to a captured variable; Remove[Position]; assert captured == Position{1,2}.
   - **OnRemove fires per-component on Delete:** entity with Position + Velocity; OnRemove fires for both before Delete completes.
   - **OnRemove fires in cascade-delete:** parent + child (via ChildOf), both with Position. Delete(parent) fires OnRemove[Position] for both, child-first (post-order).
   - **No hook fires for inherited components:** entity with `(IsA, prefab)` and prefab has Position — child's `Get[Position]` works via inheritance, but no OnAdd / OnSet was ever called on the child for Position.
   - **Override fires OnAdd:** child with `(IsA, prefab)` where prefab has Position; `Set[Position](w, child, ...)` (copy-on-write override) — OnAdd[Position] fires for the child because Position is now newly local.
   - **Hook handles nil:** no hook registered → no callback fires; existing tests stay green (no regression).
   - **Hook panic propagates:** OnSet[Position] that panics; call Set[Position]; verify the panic propagates through Set.
   - **Re-entrancy: read-only is safe:** OnSet[Position] callback calls Get[Velocity] (different component on same entity) — works.
   - **Tag (Size==0):** OnAdd[Marker] (Marker = struct{}); AddID-equivalent; OnAdd fires.
   - **SetPair[T] fires OnAdd and OnSet on the pair's TypeInfo:** hard for users to trigger via the typed API (which is keyed by Go type), so the test mutates the pair-TypeInfo's Hooks field directly and verifies the world's wiring at the call site still invokes them.
   - Existing tests stay green.

7. **Mechanical acceptance**
   - `go test ./... -race -count=2` passes.
   - `go vet ./...` clean.
   - `golangci-lint run` clean.
   - Coverage on `flecs` ≥ 90% (no regression from 97.4%).
   - All exported symbols have godoc.

### Implementation pointers

- Read `src/component_actions.c` for the C analog (`flecs_run_component_actions`). Match the algorithm shape: fire-after-add, fire-after-set, fire-before-remove.
- The trickiest part is `migrate()`. Lifecycle: source-has-X-but-dest-doesn't → fire OnRemove[X] BEFORE the swap. Dest-has-Y-but-source-didn't → fire OnAdd[Y] AFTER the append+copy. Components in BOTH (carried over): no hook.
- Pointer validity:
  - OnRemove ptr must point into the SOURCE table (still intact pre-migration). After migration the source row is swap-removed and the pointer is invalid.
  - OnAdd ptr must point into the DESTINATION table (post-migration, post-copy, post-set if a value was provided).
  - OnSet ptr is the destination column slot after the value was written.
- Fire helpers are `nil`-safe so components without hooks pay zero cost.
- DO NOT change `EntityCallback` signature — Phase 1.5 chose `world any` to avoid the import cycle; that's still correct.
- DO NOT import any third-party deps. DO NOT add a new package. Everything lives in `world.go`, the new `hooks.go`, and possibly minor adjustments to `id_ops.go` for the AddID/RemoveID/SetPair paths.

### Non-goals

- NO observer entities (Phase 5.2).
- NO deferred commands / batched events (Phase 5.3).
- NO multi-subscriber per (type, event). One hook replaces the prior.
- NO hook priority levels.
- NO pair-id-keyed user-facing hook API. Pair hooks are deferred. Internally the wiring fires pair-TypeInfo hooks if any are set via direct TypeInfo mutation (covered by one test).
- NO query-time / iterator-time hook firing. Hooks are tied to add/set/remove storage events only.
- NO panic recovery.
- NO async / goroutine-safe hook invocation.

## Constraints

- @world.go — single chokepoint via `migrate()` and `deleteOne()`; fire-sites must be wired here for add/remove/migration paths.
- @id_ops.go — `AddID` / `RemoveID` / `SetPair` paths need OnAdd/OnRemove/OnSet invocation around their migration calls.
- @childof.go — cascade-delete post-order semantics (Phase 4.2); OnRemove must fire child-first per the same traversal.
- @isa.go — inheritance semantics: no hook fires when `Get[T]` resolves via IsA; OnAdd fires when a child overrides an inherited component via `Set[T]`.
- @pair_internal.go — `SetPair` is a fire-site for OnAdd (when pair-id is new) and OnSet (every call); pair-TypeInfo hooks are invoked internally even though the user-facing API doesn't expose pair-keyed registration.
- @id.go — `ID` is the public handle passed to user hook callbacks as `func(e ID, v *T)`.
- @internal/component/typeinfo.go — `Hooks` struct (`Move`, `OnAdd`, `OnSet`, `OnRemove`) is the storage site for hook callbacks; mutated by the user-facing registration API.
- @internal/component/registry.go — `EntityCallback` signature (`func(world any, entity ID, ptr unsafe.Pointer)`) is fixed; the `any` is to avoid import cycles. Wrappers do `*T` conversion.
- @internal/storage/table/table.go — column slot pointers come from here; OnRemove ptr is into the source table pre-migration, OnAdd ptr is into the destination table post-migration.
- C reference (read but do not paraphrase): `/work/agents/claude/projects/SanderMertens/flecs/src/component_actions.c` (`flecs_run_component_actions`) and `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h` (search "type_hooks" / "ecs_iter_action_t").
