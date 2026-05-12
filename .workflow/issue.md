## Goal

Port C flecs's **configurable cleanup policies** to the Go port: the `OnDelete` and `OnDeleteTarget` trait relationships together with the action set `Remove` / `Delete` / `Panic`. This replaces the hardcoded `ChildOf` cascade-delete (Phase 4.2) with a general per-component / per-relationship policy mechanism, of which `(ChildOf, OnDeleteTarget, Delete)` becomes one concrete bootstrap-registered application.

This is the first feature phase after the 13-phase docs port (Phase 14.0–14.12, v0.19.0–v0.31.0), which surfaced ~80 feature gaps in `docs/README.md`. Configurable cleanup is the most cross-referenced of those gaps — cited in `docs/EntitiesComponents.md` (cleanup cascade), `docs/Relationships.md` (configurable cleanup for relationship targets), `docs/HierarchiesManual.md` (configurable hierarchy cleanup), `docs/PrefabsManual.md` (configurable IsA cleanup), `docs/ComponentTraits.md` (trait roadmap), and `docs/README.md` (gap list lines 71-72, 103, 123, 152). It is load-bearing for any non-trivial entity lifecycle and builds directly on existing ChildOf cascade-delete code, making it the natural opener for the post-docs feature work.

**Target version:** v0.32.0.

### What C does (grounded in source)

**Declarations** (`include/flecs.h:1950–1967`): `EcsOnDelete`, `EcsOnDeleteTarget`, `EcsRemove`, `EcsDelete`, `EcsPanic` are public entity IDs. `OnDelete` and `OnDeleteTarget` are relationships; `Remove`/`Delete`/`Panic` are tags used only as targets in pairs.

**Flag bits** (`include/flecs/private/api_flags.h:52–63`): each cleanup action is a single bit on the component record's `flags` field:
- `EcsIdOnDeleteRemove  (1u << 0)`
- `EcsIdOnDeleteDelete  (1u << 1)`
- `EcsIdOnDeletePanic   (1u << 2)`
- `EcsIdOnDeleteTargetRemove (1u << 3)`
- `EcsIdOnDeleteTargetDelete (1u << 4)`
- `EcsIdOnDeleteTargetPanic  (1u << 5)`

The trait-as-pair representation: adding `(OnDelete, Delete)` to a component entity sets the `EcsIdOnDeleteDelete` flag on the corresponding component record via the `flecs_register_on_delete` observer (`src/bootstrap.c:294–309`). Users never poke flags directly — they add pairs and the bootstrapped observers translate.

**Bootstrap policies actually installed** (`src/bootstrap.c`):
- Line 705: `cr_childof_wildcard->flags |= EcsIdOnDeleteTargetDelete` — `(ChildOf, *)` is flagged to cascade-delete sources when targets die. Also done lazily per-target in `src/storage/component_index.c:589`.
- Line 981: `ecs_add_pair(world, EcsFlecs, EcsOnDelete, EcsPanic)` — the flecs module entity itself is panic-protected, which transitively protects builtins via `(ChildOf, EcsFlecs)`.

Note: **IsA does NOT get `(OnDeleteTarget, Panic)` by default in C.** The originating goal description suggested this, but `grep -n "EcsIsA" src/bootstrap.c` shows IsA only gets `EcsTrait`, `EcsTraversable`, `EcsTransitive`, `EcsReflexive`, `EcsPairIsTag`, `EcsRelationship`, and `(OnInstantiate, Inherit)` — no `(OnDeleteTarget, Panic)`. Prefab safety in C comes from the prefab being a child of `EcsFlecs` or from users opting in explicitly. **This Go port should match C: do NOT auto-install `(OnDeleteTarget, Panic)` on IsA.** We document the recipe so users can opt in.

**Dispatch** (`src/on_delete.c`):
- `flecs_on_delete_mark` (line 408–439): when an id is being deleted, if no explicit action was supplied, derive it from `ECS_ID_ON_DELETE(cr->flags)`. If the resulting action is `Panic`, throw immediately. Otherwise call `flecs_component_mark_for_delete`.
- `flecs_component_mark_for_delete` (line 312–406): for each table holding the id, consult `flecs_get_delete_action` which inspects `ECS_ID_ON_DELETE_TARGET(crr->flags)` on the target's component record. `Delete` recurses (marks the table for delete, then recurses into its targets); `Panic` throws via `flecs_throw_invalid_delete`; `Remove` (the default) just removes the component/pair without touching the source entity.
- Precedence rule (line 118–120 in `flecs_get_delete_action`): when a table has multiple matching pairs with different policies, **`Delete` beats `Remove`**. Panic short-circuits before precedence comes into play.

### Likely shape (refine during implementation)

1. **Five built-in entity IDs** on `*World`, allocated alongside `childOfID`:
   - `World.OnDelete() ID`, `World.OnDeleteTarget() ID` — trait relationships.
   - `World.RemoveAction() ID`, `World.DeleteAction() ID`, `World.PanicAction() ID` — action tags. The `Action` suffix avoids the collision with the existing `func (w *World) Delete(e ID) bool` in `world.go:342`. Document the naming choice in godoc.

2. **Trait-as-pair representation.** Public surface is pair-based, matching C: `w.Write(func(fw *flecs.Writer) { fw.AddID(myRel, flecs.MakePair(w.OnDeleteTarget(), w.DeleteAction())) })`. An ergonomic helper `flecs.SetCleanupPolicy(w, componentOrRelID, trait, action)` wraps the pair-add for readability; the underlying pair-add stays first-class. Both should compose with the existing `Writer.AddID`.

3. **Policy storage.** Two options, decide during implementation:
   - **(a)** Extend `internal/component.TypeInfo` with `OnDeleteAction ids.ID` and `OnDeleteTargetAction ids.ID` fields, zero = `Remove` (current implicit default).
   - **(b)** Mirror C: store action bits on the component-record-equivalent (currently `internal/component/registry.go` and the per-id metadata the world tracks). C uses bit flags on the component record, not on `TypeInfo`, because the flags must apply to pair-component records too (e.g. `(ChildOf, *)` is not a TypeInfo).
   - Prefer **(b)**: pair component records need policy storage, and `TypeInfo` is keyed by `reflect.Type` which doesn't represent pair-with-target. Investigate `internal/component/registry.go` and decide.

4. **Hook the policy into `World.Delete(e)` / `Writer.Delete(e)` cascade** (`world.go:342–389`, `deleteImmediate` and `deleteOne`):
   - Generalize the `pairID := MakePair(w.childOfID, node)` cascade in `deleteImmediate` (line 372) to iterate **every** relationship `R` for which `(R, node)` exists and which has `OnDeleteTargetDelete` set. For relationships flagged `OnDeleteTargetPanic`, throw a Go panic with a message identifying the offending pair and entity (matching C's `flecs_throw_invalid_delete`).
   - For each component `c` on `e` flagged `OnDeleteDelete`, the C semantics are "when the component is removed, also delete the entity." In the entity-delete path this is effectively a no-op (the entity is already being deleted), but in the component-remove path (`Writer.RemoveID`) it must cascade. Phase 15.0 should cover the entity-delete side; verify the component-remove side either lands here or is explicitly punted to a follow-up.
   - **Migrate `ChildOf` cascade to the general mechanism.** Write the regression test FIRST, then refactor. After the refactor, `deleteImmediate`'s ChildOf-specific branch should be gone and replaced with a generic "for each relationship with `OnDeleteTargetDelete` flag on `(R, e)`" loop.

5. **Built-in policy in bootstrap.** In `world.go` near line 107–112 (where `childOfID` is allocated and registered), allocate the five new entities and install `(ChildOf, OnDeleteTarget, DeleteAction)` so existing ChildOf cascade-delete behavior is preserved bit-for-bit. **Do NOT install `(IsA, OnDeleteTarget, Panic)`** — C doesn't, and copying that would be a deviation. Document the IsA recipe in `docs/PrefabsManual.md` for users who want it.

6. **Public API surface:**
   - `World.OnDelete() ID`, `World.OnDeleteTarget() ID`, `World.RemoveAction() ID`, `World.DeleteAction() ID`, `World.PanicAction() ID`.
   - `flecs.SetCleanupPolicy(w *World, target ID, trait ID, action ID)` helper.
   - Read accessor `flecs.GetCleanupPolicy(w *World, target ID, trait ID) (action ID, ok bool)`.
   - The pair-add path (`Writer.AddID(rel, MakePair(w.OnDeleteTarget(), w.DeleteAction()))`) must also work; the helper is sugar, not the only entry point.

7. **Tests** in new `cleanup_policies_test.go`:
   1. **Default unchanged.** A component with no cleanup policy and an entity holding it: `World.Delete(holder)` removes the entity; sibling holders of the same component are unaffected (default `Remove`).
   2. **`(OnDeleteTarget, Delete)` on custom relationship.** Define rel `Likes`, add `(Likes, target)` to source. Delete `target`. Source is deleted.
   3. **`(OnDeleteTarget, Panic)` on custom relationship.** Same setup with Panic action. Deleting target panics; panic message identifies the relationship and the target entity.
   4. **ChildOf cascade-delete regression.** Existing `childof_test.go` behavior — parent delete cascades to all transitive children, post-order — passes without modification. Add a dedicated regression test that exercises the cascade pre-refactor and confirm it still passes post-refactor.
   5. **`(OnDelete, Delete)` on a component entity, entity-delete path.** Add component `C` with policy to entity `e`, delete `e`. Behavior is currently a no-op (e is deleted regardless); test documents the contract.
   6. **Multiple policies, precedence.** Entity holding two pairs where one relationship says `Delete` and the other says `Remove` for the same target. `Delete` wins (matches C `src/on_delete.c:118–120`).
   7. **Panic propagation.** Verify the panic surfaces through `World.Delete` and through a deferred `Writer.Delete` flush. Panic message format is stable enough to assert on (component or relationship name + entity ID).
   8. **Wildcard target delete.** Deleting an entity used as the target of a `(R, *)` pair where `R` has `(OnDeleteTarget, Delete)` cascades to all sources, matching C's `flecs_id_is_delete_target` path.

8. **Docs updates (per CONTRIBUTING.md \"Documentation\" policy — same PR):**
   - Remove \"not yet ported in Go flecs\" callouts and replace with \"Shipped in v0.32.0\" content with runnable code samples in: `docs/EntitiesComponents.md`, `docs/Relationships.md`, `docs/HierarchiesManual.md`, `docs/PrefabsManual.md`, `docs/ComponentTraits.md`.
   - In `docs/ComponentTraits.md`, mark `OnDelete` / `OnDeleteTarget` as shipped in the trait roadmap table.
   - In `docs/README.md`, remove or update the gap entries on lines 71-72, 103, 123, 152. Where appropriate, point readers to the new docs sections.
   - `docs/PrefabsManual.md`: document the `(IsA, OnDeleteTarget, Panic)` recipe explicitly — explain that the Go port does NOT auto-install this (matching C) and show users how to opt in.
   - `doc.go` if the headline example changes; `README.md` likewise.
   - `CHANGELOG.md`: new \"v0.32.0 — Phase 15.0\" entry under unreleased with Migration Guide note (existing ChildOf users see no change; new policy API is additive).
   - `ROADMAP.md`: move cleanup-policy line from Future Work to Shipped.

### Non-goals

- **No change to ChildOf cascade-delete observable semantics.** The behavior must be bit-for-bit identical, just driven by the general mechanism. Existing tests in `childof_test.go` pass unmodified.
- **No observer-driven cleanup.** C has both observer-driven and policy-driven cleanup; we port only the policy path.
- **No partial-cleanup recovery.** If `Panic` fires mid-cascade, the world is in a halted state. We do not try to roll back. Document this in godoc on the panic-action accessor and in `docs/EntitiesComponents.md`.
- **No `OnDelete` component-remove cascade in scope of Phase 15.0** unless it falls out trivially. The interesting customer-facing axis is `OnDeleteTarget`. If the component-remove side requires significant new dispatch, punt to a Phase 15.1 follow-up and note it in ROADMAP.
- **No auto-`(IsA, OnDeleteTarget, Panic)` bootstrap** — match C, document the opt-in recipe.

### Mechanical acceptance

- `go vet ./...` clean.
- `golangci-lint run ./...` clean.
- `go test ./... -race -count=3` clean.
- Coverage on main package ≥ 95% (current baseline at v0.31.0).
- All existing `childof_test.go` tests pass without modification.
- New `cleanup_policies_test.go` covers the 8 test cases listed above.
- Docs updates land in the same PR.

### Implementation style notes

- The trait-as-pair representation is conceptually subtle. Godoc on `World.OnDelete` and `World.OnDeleteTarget` should explain: \"This is a trait relationship. To apply a cleanup policy, add a pair `(OnDelete, action)` or `(OnDeleteTarget, action)` to the component or relationship entity. The action is one of `RemoveAction`, `DeleteAction`, `PanicAction`.\" Show a 3-line code sample inline.
- The `Writer.AddID` path must work for users who want explicit control without going through `SetCleanupPolicy`.
- **Write the ChildOf regression test BEFORE the refactor.** The migration from hardcoded `pairID := MakePair(w.childOfID, node)` to a general policy-driven loop is the highest-risk change in this phase. The regression test is the safety net.

## Constraints

- @world.go — built-in entity registration (lines 107–112 allocate `childOfID`; same pattern for new five entities). The `deleteImmediate` cascade at lines 355–389 is the code being generalized; the ChildOf-specific `pairID := MakePair(w.childOfID, node)` at line 372 becomes generic. `deleteOne` at lines 305–329 fires `OnRemove` per component — preserve this ordering.
- @scope.go — `Writer.Delete` at line 246 is the deferred path; cascade runs during flush. New policy dispatch must work both in immediate and deferred modes.
- @childof.go — existing hardcoded cascade lives only in `world.go`; `childof.go` is just the public accessor. Confirm there's no policy logic to migrate here, only behavior to preserve.
- @isa.go — existing IsA semantics. Do NOT add `(OnDeleteTarget, Panic)` to IsA by default (matches C); document the opt-in recipe in `docs/PrefabsManual.md`.
- @internal/component/typeinfo.go — candidate for new `OnDeleteAction` / `OnDeleteTargetAction` fields, but prefer storing on a component-record-equivalent (pair component records need policy storage; `TypeInfo` is keyed by `reflect.Type` and doesn't represent pair-with-target). Investigate `internal/component/registry.go` and decide during implementation.
- @cleanup_policies_test.go — NEW file. 8 test cases enumerated above.
- @docs/EntitiesComponents.md — replace cleanup-policy callout with shipped content + example.
- @docs/Relationships.md — replace configurable-cleanup-for-targets callout with shipped content + example.
- @docs/HierarchiesManual.md — replace hardcoded-ChildOf-only callout; document that ChildOf is now driven by the general mechanism.
- @docs/PrefabsManual.md — replace configurable-IsA callout; document `(IsA, OnDeleteTarget, Panic)` opt-in recipe.
- @docs/ComponentTraits.md — mark `OnDelete` / `OnDeleteTarget` as shipped in the roadmap table; add code example.
- @docs/README.md — update gap list lines 71-72, 103, 123, 152.
- @doc.go — update package overview if the headline example changes.
- @README.md — update if the public API surface affects the headline example.
- @CHANGELOG.md — new v0.32.0 entry with Migration Guide note.
- @ROADMAP.md — move cleanup-policy line from Future Work to Shipped.
- @CONTRIBUTING.md — \"Documentation\" section mandates docs land in the same PR as code.

### C reference (cite, don't paraphrase)

- `/work/agents/claude/projects/SanderMertens/flecs/include/flecs.h:1950–1967` — entity ID declarations.
- `/work/agents/claude/projects/SanderMertens/flecs/include/flecs/private/api_flags.h:52–63` — flag bit definitions and masks.
- `/work/agents/claude/projects/SanderMertens/flecs/src/bootstrap.c:294–309` — `flecs_register_on_delete` / `flecs_register_on_delete_object` observers that translate pair-add into flag bits.
- `/work/agents/claude/projects/SanderMertens/flecs/src/bootstrap.c:705` — bootstrap installation of `EcsIdOnDeleteTargetDelete` on `cr_childof_wildcard`.
- `/work/agents/claude/projects/SanderMertens/flecs/src/bootstrap.c:981` — `(EcsFlecs, OnDelete, Panic)` for module-entity protection.
- `/work/agents/claude/projects/SanderMertens/flecs/src/on_delete.c:85–96` — `flecs_id_is_delete_target` (wildcard-first-element detection).
- `/work/agents/claude/projects/SanderMertens/flecs/src/on_delete.c:98–140` — `flecs_get_delete_action` with the Delete-beats-Remove precedence.
- `/work/agents/claude/projects/SanderMertens/flecs/src/on_delete.c:312–406` — `flecs_component_mark_for_delete`, the per-table cleanup dispatch.
- `/work/agents/claude/projects/SanderMertens/flecs/src/on_delete.c:408–439` — `flecs_on_delete_mark`, the top-level entry point.
- `/work/agents/claude/projects/SanderMertens/flecs/src/storage/component_index.c:589` — lazy per-target flag application for `(ChildOf, tgt)` pair records.
