## Goal

Ship three small entity-registry gaps as one bundle in v0.56.0:

- **`Clear(w *Writer, e ID)`** — remove all components from an entity in O(table-transition), leaving the ID alive. Fires `OnRemove` per component. Mirrors C `ecs_clear` (`src/entity.c:1614`).
- **`MakeAlive(w *Writer, id ID)`** — claim a specific entity ID (e.g. for networked ID sync). Panics if the ID is in use at a different generation; silently bumps the registry's generation when the slot is free. Mirrors C `ecs_make_alive` (`src/entity.c:3111`).
- **`SetVersion(w *Writer, versionedID ID)`** — override the generation counter on an alive entity. Mirrors C `ecs_set_version` (`src/entity.c:3219`).

All three flip lines 70/90/94 in `docs/EntitiesComponents.md` (and docs/README.md gap lines for the Phase 14.1 Entities section) from "Not yet ported" to shipped, with example links pointing at the new sections.

**Why bundle**: all three touch the same entity-id machinery (entityindex dense/recycle/generation), have orthogonal user-visible behavior, and would otherwise be three near-identical refactor cycles. Precedent: Phase 15.15 bundled Relationship/Target/Trait the same way.

**Target version**: v0.56.0 (next after v0.55.0 OnReplace, just shipped at commit `ade1a18`).

## What each function does

### Clear(fw, e)
Removes every component, tag, and pair from entity `e`, leaving it alive. Equivalent to removing each ID individually but in one table-transition rather than N. The entity ends in the empty archetype.

### MakeAlive(fw, id)
Claims a specific entity ID. If the slot is free, mark it alive at the requested generation. If the slot is alive at the same generation, no-op. If the slot is alive at a different generation, panic.

### SetVersion(fw, versionedID)
Overrides the generation counter on an alive entity's slot. Use case: deserializing entity state from a snapshot, or implementing custom ID lifecycle policies.

## C research (line numbers verified)

### `ecs_clear` — `src/entity.c:1613-1644`
- Path: if `r->table->type.count > 0`, build an `ecs_table_diff_t` with `removed = table->type` and call `flecs_commit(world, entity, r, &world->store.root, &diff, 0, 0)`. This is the one-step table-transition path; OnRemove fires for every removed component as part of `flecs_commit` (same path as a single Remove).
- After commit: calls `flecs_entity_remove_non_fragmenting(world, entity, NULL)` for sparse/DontFragment/union cleanup.
- Deferred-aware: `flecs_defer_clear(stage, entity)` returns early in defer scope.
- Declaration: `include/flecs.h:3271-3274`.

### `ecs_make_alive` — `src/entity.c:3111-3166`
- Calls `ecs_get_alive(world, (uint32_t)entity)`.
- If returned `current == entity`: no-op (`return`).
- If `current != 0` and `current != entity`: `ecs_check(!current, ECS_INVALID_OPERATION, ...)` — **fails/asserts** with message "entity %u is alive with different generation". On the asserting build this is fatal; on a release build it returns error. **In Go we panic** (matches Go-flecs convention; no error-return variants elsewhere).
- If slot is unused/free: `flecs_entities_make_alive(world, entity)` overwrites the dense-slot generation, then `flecs_entities_ensure(world, entity)` allocates the record, then appends the entity to the root (empty) table.
- Storage-level `flecs_entity_index_make_alive` (`src/storage/entity_index.c:376-385`) just writes `dense[r->dense] = entity` — no validation; the validation lives in `ecs_make_alive`.
- Declaration: `include/flecs.h:3683-3685`.

### `ecs_set_version` — `src/entity.c:3219-3235`
- No direction check. Any generation value is accepted.
- Asserts on readonly / deferred mode (Go side: panic in deferred scope).
- Calls `flecs_entities_make_alive(world, entity_with_generation)` then, if alive, updates the entity's row in `r->table->data.entities[row]` to reflect the new versioned ID.
- Declaration: `include/flecs.h:3737-3739`.

## Go-side state research

- `@world.go` — `World.index *entityindex.Index` (field at line 44). `index.Alloc()` / `index.Free(id)` / `index.IsAlive(id)` / `index.Get(id) *Record` / `index.Each` / `index.Count`. `newEntityInternal` at line 625; `deleteImmediate` at line 744; `deleteOne` (the table-removal + sparse/union cleanup primitive used by both Delete and cascade) at line 641.
- `@internal/storage/entityindex/entityindex.go` — the registry. Dense vector + FIFO recycle queue; 32-bit generations. **Currently exposes only `Alloc`/`Free`/`IsAlive`/`Get`/`Count`/`Each`/`EachID` — no `MakeAlive`/`SetVersion` primitive yet**. This phase adds them.
- `@id.go` — 64-bit ID: low 32 bits = entity index, high 32 bits = generation. `MakeEntity(index, generation uint32) ID`, `id.Index()`, `id.Generation()`.
- `@id_ops.go` — `addIDImmediate` (line 57), `removeIDImmediate` (line 366) for component manipulation. Phase 16.1 does NOT call these per-component; instead it computes the empty-archetype move analogous to C `flecs_commit`.
- `@scope.go` — `Writer` (line 31); `fw.NewEntity` (line 303), `fw.Delete` (line 318), `fw.AddID`/`fw.RemoveID` (lines 308/313). New methods `fw.Clear`, `fw.MakeAlive`, `fw.SetVersion` slot here.
- `@cmd.go` / `@cmd_queue.go` — deferred command queue. `cmdKind` (cmd.go:5-15) currently has `cmdSkip / cmdAddID / cmdRemoveID / cmdSetByID / cmdDelete / cmdSetPair / cmdModified`. Phase 16.1 adds `cmdClear` (new kind). Coalescer pass-1 (cmd_queue.go:111) must learn that `cmdClear` resets the running net-archetype state to empty; the post-clear `AddID`s remain effective.
- `@hooks.go` — `fireOnRemove` (line 174); already called from `deleteOne` for every component in the table type. Clear should fire identically.
- `@world.go:650-711` — the per-component OnRemove + sparseHeld + unionStoreRemoveEntity + orderedChildren cleanup block. **Clear reuses this exact pattern** (without calling `w.index.Free(e)` at the end).
- `@marshal.go` — entity serialization uses serial numbers, NOT raw entity index or generation. Therefore custom-versioned entities do NOT round-trip through `MarshalJSON`/`UnmarshalJSON` in their original versions. This is a deliberate non-goal: the marshal format is logical-shape only.

## Deliverables

### 1. `entity_lifecycle.go` (new file)

**`Clear(fw *Writer, e ID)`** and free function plus method on `*Writer`:
- Fast path (immediate): if the entity is alive, iterate `rec.Table.Type()`, fire `OnRemove` for each non-sparse-only component (matching `deleteOne`'s split), then move the entity to `w.empty` in one step via the existing table-transition machinery. Then run the sparse / DontFragment / union / OrderedChildren cleanup block from `deleteOne` (lines 680-711) — but **do not** call `w.index.Free`. The entity remains alive.
- With auto-add: Clear does NOT trigger `With` expansion (it's removing, not adding).
- Deferred path: append `cmdClear`. The coalescer must reset the running per-entity net-archetype state to empty and treat all prior `cmdAddID`/`cmdSetByID` for the same entity as superseded (rewrite to `cmdSkip`). Subsequent `cmdAddID`/`cmdSetByID` for the same entity after the Clear continue to apply.
- Clear of empty entity (entity in `w.empty` table with no components): no-op, no hook fire, no panic. Sparse/union cleanup still runs (no-op if entry doesn't exist).

**`MakeAlive(fw *Writer, id ID) ID`**:
- **Panics** in deferred scope (matches C's defer assertion).
- If `id` matches an alive entity at the same generation: no-op, return `id`.
- If the slot is alive at a different generation: panic with message `"flecs: MakeAlive: entity index %d is alive with generation %d; requested generation %d"`.
- If the slot is free (or never allocated): bump the registry's generation to match `id.Generation()`, mark alive, attach to the empty table. Update the recycle queue / dense / record to reflect the claim.
- Return the canonical ID.
- Requires new primitive on `entityindex.Index`: `MakeAlive(id ids.ID) bool` (returns false if conflict at different generation; the panic decision lives at the flecs-package layer).

**`SetVersion(fw *Writer, versionedID ID)`**:
- **Panics** in deferred scope.
- Panics if the entity index (low 32 bits) is not alive.
- Recommendation: **require monotonic non-decrease**. Panic if the requested generation is strictly less than the current generation. Document rationale: a decrease silently invalidates outstanding handles in surprising ways; if the caller wants to reuse a slot at lower generation, they should `Delete` + `MakeAlive` explicitly. (C accepts any version, but C has different error-handling discipline.)
- Updates the dense vector's stored generation at the entity's slot. Existing handles holding the old generation now fail `IsAlive`.
- Requires new primitive: `entityindex.Index.SetVersion(rawIndex uint32, newGen uint32)`.

### 2. New primitives on `entityindex.Index`

In `@internal/storage/entityindex/entityindex.go`:
- `MakeAlive(id ids.ID) (canonical ids.ID, ok bool)` — slot-free → mark alive at requested generation, return (id, true). Slot-alive-same-gen → no-op, return (id, true). Slot-alive-different-gen → return (current, false). Removes the slot from the recycle queue if present (FIFO linear scan is acceptable for this rare op).
- `SetVersion(rawIndex uint32, newGen uint32)` — preconditions: slot is alive. Updates `dense[r.Dense]` to `MakeEntity(rawIndex, newGen)`.

Both have unit tests at the entityindex level (the test file already has `fastForwardGen` for generation manipulation — extend that pattern).

### 3. Tests in `entity_lifecycle_test.go` (≥ 12 cases)

1. **Clear basic** — entity with 3 components → Clear → 0 components, `IsAlive(id)` still true.
2. **Clear hook firing** — register `OnRemove[A]`, `OnRemove[B]`, `OnRemove[C]`; Clear; verify all three fire with the entity ID. Verify `OnDelete` does NOT fire.
3. **Clear preserves entity ID** — after Clear, `id == originalID`.
4. **Clear of empty entity** — no-op, no hook fire, no panic.
5. **Clear sparse + archetype mix** — entity has a regular component and a Sparse component; both are cleaned up; sparseHeld for the entity becomes empty.
6. **Clear with union pair** — entity has a union (R, T1); after Clear, union store has no entry for the entity.
7. **Clear with OrderedChildren** — entity is a child in an ordered parent's list; after Clear, the entity is removed from the ordered list (its own components are gone). (Verify against upstream — possibly Clear keeps ChildOf? Re-check C semantics during implementation. Mention this as an open verify point in the iterate session.)
8. **Clear in deferred path / coalescer** — `w.Write(func(fw){ AddID(fw, e, c1); Clear(fw, e); AddID(fw, e, c2) })`: only c2 remains after flush. Verify with `OwnsID`.
9. **MakeAlive on unused id at matching generation** — `MakeAlive(MakeEntity(rawIdx, 0))` on a never-allocated raw index → succeeds, entity is alive at gen 0.
10. **MakeAlive on unused id at non-zero generation** — `MakeAlive(MakeEntity(rawIdx, 5))` on free slot → succeeds, registry generation bumped to 5.
11. **MakeAlive on already-alive id at matching generation** — no-op, no panic.
12. **MakeAlive on alive id at different generation** — panic with informative message.
13. **MakeAlive then NewEntity** — claimed id removed from recycle queue; subsequent `fw.NewEntity` does not reissue it.
14. **MakeAlive in deferred scope** — panic.
15. **SetVersion to higher value** — succeeds; `IsAlive(oldID)` is false; `IsAlive(newID)` is true.
16. **SetVersion to lower value** — panic.
17. **SetVersion on dead entity** — panic.
18. **SetVersion in deferred scope** — panic.
19. **Marshal round-trip with custom-versioned entities** — document and assert: versions are NOT preserved (marshal uses serial numbers). This is a behavioral assertion, not a feature; prevents future regression where someone tries to "fix" it without thinking it through.

Coverage ≥ 95.0%.

### 4. Doc updates per `@CONTRIBUTING.md`

- `@docs/EntitiesComponents.md`:
  - Replace the "Not yet ported" stub at line 70 (Clearing) with the full Clear section: signature, behavior (one-step transition, OnRemove fires, entity stays alive), example, C-equivalent reference.
  - Replace the stub at line 90 (Manual IDs / MakeAlive): signature, behavior (claim specific ID, panic on different-generation conflict, generation bump on free slot), example with network sync rationale, C-equivalent reference.
  - Replace the stub at line 94 (Manual Versioning / SetVersion): signature, behavior (overrides generation, monotonic requirement, invalidates old handles), example, C-equivalent reference.
- `@docs/README.md` lines 96-98 — flip to shipped v0.56.0 with links to EntitiesComponents.md sections (mirror the v0.41.0/v0.46.0 "shipped in" pattern at lines 80 and 122).
- `@README.md` — feature list bump.
- `@CHANGELOG.md` — v0.56.0 entry at top, formatted like the v0.55.0 entry.
- `@ROADMAP.md` — heading "Shipped (through v0.56.0)"; add v0.56.0 row.
- `@MIGRATING.md` — no entry needed (pure additions, no breaking changes).

## Mechanical acceptance

- `go vet ./...` clean
- `golangci-lint run` clean
- `go test ./... -race -count=3` passes
- Coverage ≥ 95.0%
- No regression in existing entity-lifecycle tests (`entityindex_test.go`, `world_test.go`, `defer_test.go`, `defer_coalesce_test.go`).

## Explicit non-goals

- No entity ID ranges (docs/README.md line 99 — separate phase).
- No Enable/Disable (line 100 — separate phase).
- No on_replace hook (shipped v0.55.0).
- No dynamic component registration (line 102 — separate phase).
- No marshal-format change to preserve raw entity index / generation (deliberate; see deliverable 3 test 19).

## Open decision points

1. **Clear's hook-firing semantics**: fire `OnRemove` per id (Recommended; matches C `flecs_commit` path and user's mental model of bulk Remove). State and verify in implementation.
2. **MakeAlive generation-mismatch on free slot**: silent generation bump (Recommended; one call does the right thing; matches C). Document.
3. **SetVersion direction enforcement**: require non-decreasing generation (Recommended; deliberate divergence from C, justified by Go-flecs's stronger handle-invalidation discipline). Document the divergence in EntitiesComponents.md and the CHANGELOG entry.
4. **Clear and ChildOf**: does C's `ecs_clear` remove the ChildOf pair (it's a pair like any other)? Implementation must verify; the ordered-children test (test 7) depends on this. If C removes it, Go matches. If C preserves it, document and add a test for the preserved relationship.

## Constraints

- @CONTRIBUTING.md — Style and Documentation sections govern doc/test/code conventions
- @docs/README.md — feature-gap tracker; lines 96-98 flip in this phase
- @docs/EntitiesComponents.md — sections 68-95 (Clearing / Manual IDs / Manual Versioning) need their "Not yet ported" stubs replaced
- @world.go — `World.index` field, `deleteOne` cleanup pattern (lines 641-718) is the template for Clear
- @internal/storage/entityindex/entityindex.go — new `MakeAlive` and `SetVersion` primitives go here
- @id.go — `MakeEntity(index, generation)` is the ID constructor; bit layout is documented
- @scope.go — Writer methods `fw.Clear` / `fw.MakeAlive` / `fw.SetVersion` slot at line 318 alongside `fw.Delete`
- @cmd.go — new `cmdClear` kind needed; coalescer in @cmd_queue.go (pass-1 ~line 111) must handle Clear as net-archetype reset
- @hooks.go — `fireOnRemove` already correct; Clear reuses it
- @marshal.go — serializer uses serial numbers, not raw indices; custom versions deliberately do NOT round-trip (assert in test 19)
- /work/agents/claude/projects/SanderMertens/flecs/src/entity.c lines 1613, 3111, 3219 — C reference implementations; line numbers cited above
- /work/agents/claude/projects/SanderMertens/flecs/include/flecs.h lines 3271, 3683, 3737 — C API declarations

## Process

- Feature, not bug.
- Label: `snichols/queued`.
